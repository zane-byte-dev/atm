package taskrun

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type localProcess struct{}

func (localProcess) LookPath(binary string) (string, error) {
	return exec.LookPath(binary)
}

type localLogReader struct {
	*os.File
}

func (reader localLogReader) Size() (int64, error) {
	info, err := reader.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

type localLogs struct{}

func (localLogs) Open(path string) (LogReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return localLogReader{File: file}, nil
}

func (localLogs) Append(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

type localSessionEvents struct{}

func (localSessionEvents) ReportEnded(run Run, at time.Time) error {
	sessionID := store.ResumableThreadID(run.SessionID)
	if sessionID == "" {
		return nil
	}
	return agentevent.Deliver(agentevent.Envelope{
		Version:   agentevent.Version,
		Source:    agentevent.SourceCodex,
		Event:     agentevent.KindSessionEnd,
		SessionID: sessionID,
		CWD:       run.WorkDir,
		Reason:    "task_run_finished",
		At:        at.Format(time.RFC3339),
	})
}

type localClock struct{}

func (localClock) Now() time.Time {
	return time.Now().In(config.Loc)
}

func (localClock) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
