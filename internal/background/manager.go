// Package background owns the bounded jobs and periodic maintenance performed
// by atm serve. It never runs arbitrary commands or starts an Agent session.
package background

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type Kind string

const (
	SessionSync         Kind = "session.sync"
	CollectionRun       Kind = "collect.run"
	CollectionReprocess Kind = "collect.reprocess"
	DayRebuild          Kind = "day.rebuild"
	QuotaRefresh        Kind = "quota.refresh"
	TodoRefine          Kind = "todo.refine"
)

type Request struct {
	Kind         Kind   `json:"kind"`
	Agent        string `json:"agent,omitempty"`
	SourceID     string `json:"source_id,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	Day          string `json:"day,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	TodoID       string `json:"todo_id,omitempty"`
	ExpectedETag string `json:"expected_etag,omitempty"`
	Hint         string `json:"hint,omitempty"`
	// DueOnly is set by the trusted scheduler. Web adapters must reject it.
	DueOnly bool `json:"due_only,omitempty"`
}

type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Job struct {
	ID              string          `json:"id"`
	Kind            Kind            `json:"kind"`
	TodoID          string          `json:"todo_id,omitempty"`
	Status          string          `json:"status"`
	Phase           string          `json:"phase"`
	CreatedAt       string          `json:"created_at"`
	StartedAt       string          `json:"started_at,omitempty"`
	FinishedAt      string          `json:"finished_at,omitempty"`
	CancelRequested bool            `json:"cancel_requested,omitempty"`
	Error           *JobError       `json:"error,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	// Collection is an ephemeral projection for the runtime notification
	// callback. It deliberately stays out of the persisted/background-job JSON:
	// collection titles belong in the notification feed, not in the generic job
	// history returned to every workspace client.
	Collection *CollectionCompletion `json:"-"`
}

func (j Job) Terminal() bool { return j.Status != "queued" && j.Status != "running" }

type Executor func(context.Context, application.Call, Request, func(string)) (any, error)
type ConfigGate func(context.Context, func(context.Context) error) error

type Options struct {
	DataDir string
	OpenDB  func() (*sql.DB, error)
	Execute Executor
	Refine  TodoRefineOptions
	// WithConfig pins mutable package configuration for the entire operation.
	WithConfig ConfigGate
	OnChange   func(Job)
	// Schedule opts into the existing automatic work. Zero value has no timers.
	Schedule     bool
	TickInterval time.Duration
	SyncInterval time.Duration
	DayInterval  time.Duration
	JobTimeout   time.Duration
	Now          func() time.Time
	// CollectionDue computes the configured source cadence under WithConfig.
	CollectionDue func(context.Context, time.Time) (bool, error)
	// updateJob and durabilityRetry are narrow test seams. Production uses the
	// store writer and a short retry cadence; neither retries job execution.
	updateJob       func(context.Context, *sql.DB, string, string, string) error
	durabilityRetry time.Duration
}

type queuedJob struct {
	job     Job
	request Request
	call    application.Call
	cancel  context.CancelFunc
	// Accounting failures never turn a completed external/model operation into
	// a retryable failed job. A durable journal is retried independently.
	usagePending bool
	usageLost    bool
}

type Manager struct {
	options                           Options
	db                                *sql.DB
	mu                                sync.Mutex
	started, closed                   bool
	ctx                               context.Context
	cancel                            context.CancelFunc
	queue                             chan *queuedJob
	active                            map[string]*queuedJob
	wg                                sync.WaitGroup
	done                              chan struct{}
	lastSync, lastDay, lastCollection time.Time
	durabilityWake                    chan struct{}
	pendingFinal                      map[string]Job
	pendingUsage                      map[string]struct{}
}

func New(options Options) (*Manager, error) {
	if options.DataDir == "" {
		options.DataDir = config.AtmDir
	}
	if options.OpenDB == nil {
		options.OpenDB = store.Open
	}
	if options.Execute == nil {
		options.Execute = DefaultExecutor(options.DataDir, options.Refine)
	}
	if options.WithConfig == nil {
		options.WithConfig = func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.TickInterval <= 0 {
		options.TickInterval = time.Minute
	}
	if options.SyncInterval <= 0 {
		options.SyncInterval = 5 * time.Minute
	}
	if options.DayInterval <= 0 {
		options.DayInterval = 7 * time.Minute
	}
	if options.JobTimeout <= 0 {
		options.JobTimeout = 10 * time.Minute
	}
	if options.CollectionDue == nil {
		options.CollectionDue = collectionDue
	}
	if options.updateJob == nil {
		options.updateJob = store.UpdateBackgroundJob
	}
	if options.durabilityRetry <= 0 {
		options.durabilityRetry = 2 * time.Second
	}
	db, err := options.OpenDB()
	if err != nil {
		return nil, err
	}
	return &Manager{
		options: options, db: db, queue: make(chan *queuedJob, 16),
		active: map[string]*queuedJob{}, done: make(chan struct{}),
		durabilityWake: make(chan struct{}, 1), pendingFinal: map[string]Job{}, pendingUsage: map[string]struct{}{},
	}, nil
}

// Start recovers receipt state before accepting work. A crashed external call
// stays interrupted until a human explicitly retries with a new key.
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		return invalid("context is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return busy("background runtime is closed")
	}
	if m.started {
		return nil
	}
	records, err := store.ListBackgroundJobs(ctx, m.db, 200, true)
	if err != nil {
		return err
	}
	for _, record := range records {
		var job Job
		if err := json.Unmarshal([]byte(record.ResultJSON), &job); err != nil {
			return err
		}
		job.Status = "interrupted"
		job.Phase = "上次运行已中断"
		job.FinishedAt = m.timestamp()
		job.Error = &JobError{Code: "interrupted", Message: "服务重启前未完成，未自动重试"}
		if err := m.persist(job); err != nil {
			return err
		}
	}
	if err := m.options.WithConfig(ctx, func(ctx context.Context) error { return RecoverUsage(ctx, m.options.DataDir) }); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	m.wg.Add(1)
	go m.durabilityWorker()
	for i := 0; i < 2; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	if m.options.Schedule {
		m.wg.Add(1)
		go m.scheduler()
	}
	go func() { m.wg.Wait(); close(m.done) }()
	return nil
}

// Run persists acceptance before enqueueing. The caller may disconnect after
// return without canceling the job; Cancel and Close own cancellation.
func (m *Manager) Run(ctx context.Context, call application.Call, request Request, key string) (Job, error) {
	if ctx == nil {
		return Job{}, invalid("context is required")
	}
	if err := call.Validate(); err != nil {
		return Job{}, err
	}
	if call.Actor.Kind != application.ActorHuman && call.Actor.Kind != application.ActorController {
		return Job{}, application.NewError(application.CodeForbidden, "background operations require a human or runtime controller")
	}
	automatic := call.Actor.Kind == application.ActorController && call.Actor.Origin == application.OriginController
	if request.DueOnly && !automatic {
		return Job{}, invalid("due_only belongs to the runtime scheduler")
	}
	defaultFrom := request.Kind == DayRebuild && request.Day == "" && request.From == ""
	defaultTo := request.Kind == DayRebuild && request.Day == "" && request.To == ""
	if err := validateRequest(&request, m.options.Now()); err != nil {
		return Job{}, err
	}
	if len(key) > 160 || strings.TrimSpace(key) == "" {
		return Job{}, invalid("an idempotency key of 1–160 characters is required")
	}
	data, _ := json.Marshal(request)
	// An omitted day means today at first acceptance, not today at retry. Keep
	// that request shape in the digest while persisting its resolved execution
	// range, so an overnight retry returns the original job.
	keyRequest := request
	if defaultFrom {
		keyRequest.From = ""
	}
	if defaultTo {
		keyRequest.To = ""
	}
	keyData, _ := json.Marshal(keyRequest)
	hash := sha256.Sum256(keyData)
	digest := hex.EncodeToString(hash[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.closed {
		return Job{}, busy("background runtime is not accepting jobs")
	}
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	record, err := store.GetBackgroundJob(ctx, m.db, "", key)
	if err != nil {
		return Job{}, err
	}
	if record != nil {
		if record.Digest != digest {
			return Job{}, application.NewError(application.CodeConflict, "idempotency key was already used for different job parameters")
		}
		var job Job
		err := json.Unmarshal([]byte(record.ResultJSON), &job)
		return job, err
	}
	// Never queue a duplicate operation behind its currently running copy. An
	// explicit different idempotency key is not evidence that repetition is useful.
	for _, active := range m.active {
		current, _ := json.Marshal(active.request)
		if string(current) == string(data) {
			err := application.NewError(application.CodeBusy, "this operation is already queued or running")
			err.Details = map[string]any{"job_id": active.job.ID}
			return Job{}, err
		}
	}
	if len(m.active) >= 18 || len(m.queue) == cap(m.queue) {
		return Job{}, busy("background queue is full")
	}
	id, err := randomID()
	if err != nil {
		return Job{}, err
	}
	job := Job{ID: id, Kind: request.Kind, TodoID: request.TodoID, Status: "queued", Phase: "等待执行", CreatedAt: m.timestamp()}
	result, _ := json.Marshal(job)
	err = store.InsertBackgroundJob(ctx, m.db, store.BackgroundJobRecord{ID: id, Key: key, Digest: digest, Kind: string(request.Kind), Status: job.Status, RequestJSON: string(data), ResultJSON: string(result), CreatedAt: m.options.Now().UnixNano(), Automatic: automatic})
	if err != nil {
		return Job{}, err
	}
	entry := &queuedJob{job: job, request: request, call: call}
	m.active[id] = entry
	select {
	case m.queue <- entry:
	default:
		delete(m.active, id)
		job.Status = "interrupted"
		job.Phase = "队列已满"
		job.FinishedAt = m.timestamp()
		_ = m.persist(job)
		return Job{}, busy("background queue is full")
	}
	// Listener calls happen outside locks in workers; enqueue also wakes a worker
	// immediately, so a synchronous notification is unnecessary here.
	return job, nil
}

func (m *Manager) Get(ctx context.Context, id string) (Job, error) {
	m.mu.Lock()
	pending, ok := m.pendingFinal[id]
	m.mu.Unlock()
	if ok {
		return pending, nil
	}
	record, err := store.GetBackgroundJob(ctx, m.db, id, "")
	if err != nil {
		return Job{}, err
	}
	if record == nil {
		return Job{}, application.NewError(application.CodeNotFound, "background job not found")
	}
	var job Job
	err = json.Unmarshal([]byte(record.ResultJSON), &job)
	return job, err
}

func (m *Manager) List(ctx context.Context, limit int) ([]Job, error) {
	if limit < 1 || limit > 100 {
		return nil, invalid("limit must be between 1 and 100")
	}
	records, err := store.ListBackgroundJobs(ctx, m.db, limit, false)
	if err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(records))
	m.mu.Lock()
	pending := make(map[string]Job, len(m.pendingFinal))
	for id, job := range m.pendingFinal {
		pending[id] = job
	}
	m.mu.Unlock()
	for _, record := range records {
		if job, ok := pending[record.ID]; ok {
			jobs = append(jobs, job)
			delete(pending, record.ID)
			continue
		}
		var job Job
		if err := json.Unmarshal([]byte(record.ResultJSON), &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	// Pending finalizations are always among the newest bounded active jobs, but
	// include them even if an unusually small list limit excluded their receipt.
	for _, job := range pending {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt == jobs[j].CreatedAt {
			return jobs[i].ID > jobs[j].ID
		}
		return jobs[i].CreatedAt > jobs[j].CreatedAt
	})
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (m *Manager) Cancel(ctx context.Context, id string) (Job, error) {
	m.mu.Lock()
	entry := m.active[id]
	if entry == nil {
		m.mu.Unlock()
		return m.Get(ctx, id)
	}
	entry.job.CancelRequested = true
	entry.job.Phase = "正在取消，等待当前操作收尾"
	if entry.cancel != nil {
		entry.cancel()
	}
	job := entry.job
	err := m.persist(job)
	m.mu.Unlock()
	m.changed(job)
	return job, err
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		if m.cancel != nil {
			m.cancel()
		}
	}
	started := m.started
	m.mu.Unlock()
	if !started {
		return m.db.Close()
	}
	select {
	case <-m.done:
		return m.db.Close()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case entry := <-m.queue:
			m.execute(entry)
		case <-m.ctx.Done():
			for {
				select {
				case entry := <-m.queue:
					m.finish(entry, nil, context.Canceled)
				default:
					return
				}
			}
		}
	}
}

func (m *Manager) execute(entry *queuedJob) {
	ctx, cancel := context.WithTimeout(m.ctx, m.options.JobTimeout)
	m.mu.Lock()
	entry.cancel = cancel
	if entry.job.CancelRequested {
		cancel()
	}
	entry.job.Status = "running"
	entry.job.Phase = "开始执行"
	entry.job.StartedAt = m.timestamp()
	job := entry.job
	err := m.persist(job)
	m.mu.Unlock()
	m.changed(job)
	defer cancel()
	if err != nil {
		m.finish(entry, nil, err)
		return
	}
	var result any
	err = m.options.WithConfig(ctx, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		operationErr, usageErr := runWithUsage(ctx, m.options.DataDir, entry.job.ID, func(ctx context.Context) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = errors.New("background executor panicked")
				}
			}()
			result, err = m.options.Execute(ctx, entry.call, entry.request, func(phase string) { m.progress(entry, phase) })
			return err
		})
		if usageErr != nil {
			if usageJournalExists(m.options.DataDir, entry.job.ID) {
				entry.usagePending = true
				m.retryUsageLater(entry.job.ID)
			} else {
				entry.usageLost = true
			}
		}
		return operationErr
	})
	m.finish(entry, result, err)
}

func (m *Manager) progress(entry *queuedJob, phase string) {
	if len(phase) > 240 {
		phase = phase[:240]
	}
	m.mu.Lock()
	entry.job.Phase = phase
	job := entry.job
	_ = m.persist(job)
	m.mu.Unlock()
	m.changed(job)
}

func (m *Manager) finish(entry *queuedJob, result any, err error) {
	m.mu.Lock()
	entry.job.FinishedAt = m.timestamp()
	entry.job.Status = "succeeded"
	entry.job.Phase = "已完成"
	if entry.usagePending {
		entry.job.Phase = "已完成；用量账本已保留并将在后台补记"
	} else if entry.usageLost {
		entry.job.Phase = "已完成；用量记录未能保存"
	}
	if err != nil {
		entry.job.Status = "failed"
		entry.job.Phase = "执行失败"
		entry.job.Error = &JobError{Code: "unavailable", Message: "操作未完成，请检查相关来源状态后重试"}
		var appErr *application.Error
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case application.CodeConflict:
				entry.job.Error = &JobError{Code: "conflict", Message: "内容已被更新，请刷新后重新操作"}
			case application.CodeNotFound:
				entry.job.Error = &JobError{Code: "not_found", Message: "操作对象已不存在，请刷新页面"}
			case application.CodeInvalidArgument:
				entry.job.Error = &JobError{Code: "invalid_argument", Message: "操作参数不符合要求，请检查后重试"}
			case application.CodeForbidden:
				entry.job.Error = &JobError{Code: "forbidden", Message: "当前操作未获允许"}
			}
		}
		if errors.Is(err, context.Canceled) {
			entry.job.Status = "canceled"
			entry.job.Phase = "已取消"
			entry.job.Error = nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			entry.job.Error = &JobError{Code: "timeout", Message: "操作超时，已请求停止"}
		}
		if m.closed {
			entry.job.Status = "interrupted"
			entry.job.Phase = "服务关闭，操作已中断"
			entry.job.Error = &JobError{Code: "interrupted", Message: "服务关闭前未完成，未自动重试"}
		}
	}
	if result != nil {
		if collection, ok := result.(collectionJobResult); ok {
			entry.job.Collection = collection.completion
		}
		if data, e := json.Marshal(result); e == nil && len(data) <= 32*1024 {
			entry.job.Result = data
		}
	}
	job := entry.job
	if persistErr := m.persist(job); persistErr != nil {
		job.Status = "interrupted"
		job.Phase = "执行结果未能保存"
		job.Error = &JobError{Code: "unavailable", Message: "结果状态未能保存，请先核对实际结果再重试"}
		entry.job = job
		if retryErr := m.persist(job); retryErr != nil {
			m.pendingFinal[job.ID] = job
			m.wakeDurabilityLocked()
		}
	}
	delete(m.active, job.ID)
	m.mu.Unlock()
	_ = store.PruneScheduledBackgroundJobs(context.Background(), m.db)
	m.changed(job)
}

func (m *Manager) persist(job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return m.options.updateJob(context.Background(), m.db, job.ID, job.Status, string(data))
}

// durabilityWorker retries only already-recorded local state. It never calls
// Execute and therefore cannot repeat a provider request, model call, or Todo
// mutation after an ambiguous completion.
func (m *Manager) durabilityWorker() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.options.durabilityRetry)
	defer ticker.Stop()
	for {
		select {
		case <-m.durabilityWake:
			m.retryDurability(m.ctx)
		case <-ticker.C:
			m.retryDurability(m.ctx)
		case <-m.ctx.Done():
			// One last best-effort pass helps a graceful shutdown. Startup recovery
			// remains authoritative if the database itself is unavailable.
			m.retryFinalJobs()
			return
		}
	}
}

func (m *Manager) retryDurability(ctx context.Context) {
	m.retryFinalJobs()
	m.retryUsageJournals(ctx)
}

func (m *Manager) retryFinalJobs() {
	m.mu.Lock()
	pending := make(map[string]Job, len(m.pendingFinal))
	for id, job := range m.pendingFinal {
		pending[id] = job
	}
	m.mu.Unlock()
	for id, job := range pending {
		if err := m.persist(job); err != nil {
			continue
		}
		m.mu.Lock()
		delete(m.pendingFinal, id)
		m.mu.Unlock()
		m.changed(job)
	}
}

func (m *Manager) retryUsageJournals(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.pendingUsage))
	for id := range m.pendingUsage {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	recovered := make([]string, 0, len(ids))
	_ = m.options.WithConfig(ctx, func(ctx context.Context) error {
		var joined error
		for _, id := range ids {
			err := flushUsageFile(ctx, m.options.DataDir, id)
			if err == nil || os.IsNotExist(err) {
				recovered = append(recovered, id)
				continue
			}
			joined = errors.Join(joined, err)
		}
		return joined
	})
	if len(recovered) == 0 {
		return
	}
	m.mu.Lock()
	for _, id := range recovered {
		delete(m.pendingUsage, id)
	}
	m.mu.Unlock()
}

func (m *Manager) retryUsageLater(id string) {
	m.mu.Lock()
	m.pendingUsage[id] = struct{}{}
	m.wakeDurabilityLocked()
	m.mu.Unlock()
}

func (m *Manager) wakeDurabilityLocked() {
	select {
	case m.durabilityWake <- struct{}{}:
	default:
	}
}

func usageJournalExists(dataDir, id string) bool {
	info, err := os.Stat(filepath.Join(dataDir, "model-usage-pending", id+".jsonl"))
	return err == nil && info.Mode().IsRegular()
}

func (m *Manager) changed(job Job) {
	if m.options.OnChange != nil {
		m.options.OnChange(job)
	}
}
func (m *Manager) timestamp() string { return m.options.Now().UTC().Format(time.RFC3339Nano) }
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "job-" + hex.EncodeToString(b[:]), nil
}
func invalid(message string) error {
	return application.NewError(application.CodeInvalidArgument, message)
}
func busy(message string) error { return application.NewError(application.CodeBusy, message) }

func validateRequest(r *Request, now time.Time) error {
	r.Agent = strings.TrimSpace(r.Agent)
	r.SourceID = strings.TrimSpace(r.SourceID)
	r.ItemID = strings.TrimSpace(r.ItemID)
	r.TodoID = strings.TrimSpace(r.TodoID)
	for _, id := range []string{r.Agent, r.SourceID, r.ItemID, r.TodoID} {
		if len(id) > 160 || strings.ContainsAny(id, "\x00\r\n") {
			return invalid("job identifier is invalid")
		}
	}
	if r.Kind != SessionSync && r.Kind != QuotaRefresh && r.Agent != "" {
		return invalid("agent is not valid for this job")
	}
	if r.Kind != CollectionRun && (r.SourceID != "" || r.DueOnly) {
		return invalid("source_id is not valid for this job")
	}
	if r.Kind != CollectionReprocess && r.ItemID != "" {
		return invalid("item_id is not valid for this job")
	}
	if r.Kind != DayRebuild && (r.Day != "" || r.From != "" || r.To != "") {
		return invalid("day range is not valid for this job")
	}
	if r.Kind != TodoRefine && (r.TodoID != "" || r.ExpectedETag != "" || r.Hint != "") {
		return invalid("todo refinement parameters are not valid for this job")
	}
	switch r.Kind {
	case SessionSync, QuotaRefresh:
		if r.Agent != "" {
			normalized := config.NormalizeAgent(r.Agent)
			if normalized == "" {
				return invalid("unknown agent")
			}
			r.Agent = normalized
		}
	case CollectionRun:
	case TodoRefine:
		r.Hint = strings.TrimSpace(r.Hint)
		r.ExpectedETag = strings.TrimSpace(r.ExpectedETag)
		if !store.LooksLikeTodoID(r.TodoID) || r.ExpectedETag == "" || len(r.ExpectedETag) > 160 || utf8.RuneCountInString(r.Hint) > 500 {
			return invalid("valid todo_id and expected_etag are required; hint must not exceed 500 characters")
		}
		r.TodoID = store.NormalizeTodoID(r.TodoID)
	case CollectionReprocess:
		if r.ItemID == "" {
			return invalid("item_id is required")
		}
	case DayRebuild:
		if r.Day != "" {
			if r.From != "" || r.To != "" {
				return invalid("use day or from/to, not both")
			}
			r.From = r.Day
			r.To = r.Day
			r.Day = ""
		}
		if r.From == "" {
			r.From = now.Format(time.DateOnly)
		}
		if r.To == "" {
			r.To = r.From
		}
		from, e1 := time.Parse(time.DateOnly, r.From)
		to, e2 := time.Parse(time.DateOnly, r.To)
		if e1 != nil || e2 != nil || to.Before(from) || to.Sub(from) > 365*24*time.Hour {
			return invalid("day range must contain at most 366 valid calendar days")
		}
	default:
		return invalid(fmt.Sprintf("unsupported background job kind: %s", r.Kind))
	}
	return nil
}
