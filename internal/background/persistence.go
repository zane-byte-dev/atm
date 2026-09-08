package background

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const controlWriteTimeout = 250 * time.Millisecond

// All snapshots of one accepted job share this fence. Only the serialized
// writer accesses it; it is not persisted or returned in the wire contract.
type jobWriteState struct {
	highest   uint64
	events    sync.Mutex
	published uint64
}

func (m *Manager) acquireAdmission(ctx context.Context) error {
	select {
	case m.admission <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) releaseAdmission() { <-m.admission }

func (m *Manager) persist(job Job) error {
	return m.persistContext(context.Background(), job)
}

// State writes have their own short deadline, independent of canceled external
// work. A failed completion is recovered by the durability worker, never by
// rerunning the external operation. No caller holds Manager.mu here.
func (m *Manager) persistContext(parent context.Context, job Job) error {
	ctx, cancel := context.WithTimeout(parent, controlWriteTimeout)
	defer cancel()
	select {
	case m.writeGate <- struct{}{}:
		defer func() { <-m.writeGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if job.writes != nil {
		if job.revision < job.writes.highest {
			return nil
		}
		// Fence even a failed terminal write: an older progress receipt must not
		// replace it while durable finalization is pending.
		job.writes.highest = job.revision
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return m.options.updateJob(ctx, m.db, job.ID, job.Status, string(data))
}

func (m *Manager) flushProgress() {
	m.mu.Lock()
	progress := m.pendingProgress
	m.pendingProgress = map[string]Job{}
	m.mu.Unlock()
	for id, job := range progress {
		_ = m.persist(job)
		m.mu.Lock()
		entry := m.active[id]
		current := entry != nil && !entry.finalizing && !entry.job.Terminal() && !entry.job.CancelRequested && entry.job.revision == job.revision
		m.mu.Unlock()
		if current {
			m.changed(job)
		}
	}
}
