package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/quota"
	"github.com/zane-byte-dev/atm/internal/store"
)

// QuotaSampler appends the current rate-limit readings to history. It takes the
// refresh's own connection: sampling rides on a sync that already holds a
// writable one, and a second writer would contend with it for no gain.
type QuotaSampler interface {
	RecordSamples(*sql.DB, time.Time) error
}

// ServiceOptions are the refresh's clock, persistence, and cross-domain ports.
type ServiceOptions struct {
	Now       func() time.Time
	OpenRead  func() (*sql.DB, error)
	OpenWrite func() (*sql.DB, error)
	SyncAll   func(*sql.DB) (int, error)
	SyncAgent func(*sql.DB, string) (int, error)
	Sampler   QuotaSampler
	// IndexExists reports whether the index file is on disk. Separate from
	// OpenRead because the answer decides whether to open at all, and separate
	// from a plain bool because "not there yet" and "there but unreadable" are
	// different answers: the first is a fresh install, the second is a fault.
	IndexExists func() (bool, error)
}

// Service owns the refresh and the freshness read: which sources to scan, the
// side jobs that ride along, and whether a status read is allowed to create the
// index it is describing.
type Service struct {
	now         func() time.Time
	openRead    func() (*sql.DB, error)
	openWrite   func() (*sql.DB, error)
	syncAll     func(*sql.DB) (int, error)
	syncAgent   func(*sql.DB, string) (int, error)
	sampler     QuotaSampler
	indexExists func() (bool, error)
}

var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.OpenWrite == nil {
		options.OpenWrite = store.Open
	}
	if options.SyncAll == nil {
		options.SyncAll = store.SyncAll
	}
	if options.SyncAgent == nil {
		options.SyncAgent = store.SyncAgent
	}
	if options.Sampler == nil {
		options.Sampler = quota.Default
	}
	if options.IndexExists == nil {
		options.IndexExists = func() (bool, error) {
			if _, err := os.Stat(config.AtmDB); err != nil {
				if os.IsNotExist(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		}
	}
	return Service{
		now: options.Now, openRead: options.OpenRead, openWrite: options.OpenWrite,
		syncAll: options.SyncAll, syncAgent: options.SyncAgent,
		sampler: options.Sampler, indexExists: options.IndexExists,
	}
}

// Run refreshes the session index and samples quota history alongside it.
//
// Sampling rides on the refresh rather than a timer of its own: the desktop app
// already syncs every few minutes, which is resolution enough for an hourly rate.
func (service Service) Run(
	ctx context.Context,
	call application.Call,
	input RunInput,
) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, invalid("context", nil, "sync context is required")
	}
	if err := call.Validate(); err != nil {
		return RunResult{}, err
	}
	agent, err := normalizeAgent(input.Agent)
	if err != nil {
		return RunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RunResult{}, unavailable("sync sessions", err)
	}
	db, err := service.openWrite()
	if err != nil {
		return RunResult{}, unavailable("open session index", err)
	}
	if db == nil {
		return RunResult{}, unavailable("open session index", errors.New("database opener returned nil"))
	}
	defer db.Close()

	var result RunResult
	if agent != "" {
		result.SyncedFiles, err = service.syncAgent(db, agent)
	} else {
		result.SyncedFiles, err = service.syncAll(db)
	}
	if err != nil {
		return RunResult{}, unavailable("sync sessions", err)
	}
	if warning := service.sample(db); warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}
	return result, nil
}

// sample records quota history. A failure here must not fail the refresh —
// history is a convenience, the session index is the point — but it must not
// vanish either: nobody reads stderr when the App runs this on a timer, and
// quota history silently never accumulating is exactly the kind of fault that
// needs a record.
func (service Service) sample(db *sql.DB) string {
	if service.sampler == nil {
		return ""
	}
	err := service.sampler.RecordSamples(db, service.now())
	if err == nil {
		return ""
	}
	logging.Failure("quota_history_not_recorded", "atm sync", err, nil)
	return fmt.Sprintf("quota history not recorded: %v", err)
}

// Status reports how fresh the index is.
//
// A read must not build what it is describing: with no index on disk and no
// explicit request to sync, it answers "missing" rather than creating an empty
// database as a side effect of being asked about it.
func (service Service) Status(
	ctx context.Context,
	call application.Call,
	input StatusInput,
) (StatusReport, error) {
	if ctx == nil {
		return StatusReport{}, invalid("context", nil, "sync status context is required")
	}
	if err := call.Validate(); err != nil {
		return StatusReport{}, err
	}
	scope, err := normalizeAgent(input.Scope)
	if err != nil {
		return StatusReport{}, err
	}
	if scope == "" {
		scope = store.SyncScopeAll
	}
	if err := ctx.Err(); err != nil {
		return StatusReport{}, unavailable("read session index freshness", err)
	}

	exists, err := service.indexExists()
	if err != nil {
		// The cause is spelled into the message here rather than only wrapped. This
		// is a filesystem fault on a path the user can fix — a permission, or a
		// path component that turned out to be a file — and adapters print the code
		// and message only. "inspect session index failed" alone sends nobody
		// anywhere, and syncing cannot repair it.
		appErr := application.WrapError(application.CodeUnavailable,
			fmt.Sprintf("inspect session index: %v", err), err)
		appErr.Retryable = true
		return StatusReport{}, appErr
	}
	if !input.Sync && !exists {
		return StatusReport{
			GeneratedAt: service.generatedAt(),
			Index:       StatusIndex{Path: config.AtmDB, Exists: false},
			Sync: StatusState{
				Scope: scope, Status: "missing", RunStatus: "never",
				StaleAfterSeconds: int64(store.DefaultSyncStaleAfter.Seconds()),
			},
		}, nil
	}

	db, err := service.openStatus(input.Sync)
	if err != nil {
		return StatusReport{}, err
	}
	defer db.Close()
	if input.Sync {
		if _, err := service.syncAll(db); err != nil {
			return StatusReport{}, unavailable("sync before reading freshness", err)
		}
	}
	health, err := store.ReadSyncHealth(db, scope, service.now(), store.DefaultSyncStaleAfter)
	if err != nil {
		return StatusReport{}, unavailable("read session index freshness", err)
	}
	return StatusReport{
		GeneratedAt: service.generatedAt(),
		Index: StatusIndex{
			Path:             config.AtmDB,
			Exists:           true,
			SchemaVersion:    health.SchemaVersion,
			IndexedSessions:  health.IndexedSessions,
			RetainedSessions: health.RetainedSessions,
		},
		Sync: StatusState{
			Scope:             health.Scope,
			Status:            health.Status,
			RunStatus:         health.RunStatus,
			LastAttemptAt:     health.LastAttemptAt,
			LastSuccessAt:     health.LastSuccessAt,
			AgeSeconds:        health.AgeSeconds,
			StaleAfterSeconds: health.StaleAfterSeconds,
			LastError:         health.LastError,
			LastSyncedFiles:   health.LastSyncedFiles,
		},
	}, nil
}

func (service Service) openStatus(syncBeforeRead bool) (*sql.DB, error) {
	open := service.openRead
	if syncBeforeRead {
		open = service.openWrite
	}
	db, err := open()
	if err != nil {
		return nil, unavailable("open session index", err)
	}
	if db == nil {
		return nil, unavailable("open session index", errors.New("database opener returned nil"))
	}
	return db, nil
}

func (service Service) generatedAt() string {
	return service.now().UTC().Format(time.RFC3339)
}

func normalizeAgent(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	// The scope an already-recorded sync run was filed under is one of these
	// names, so status and run normalize the same way.
	if raw == store.SyncScopeAll {
		return raw, nil
	}
	agent := config.NormalizeAgent(raw)
	if agent == "" {
		return "", invalid(
			"agent",
			raw,
			fmt.Sprintf("unknown agent: %s (use claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, or antigravity)", raw),
		)
	}
	return agent, nil
}

func invalid(field string, value any, message string) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func unavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action+" failed", cause)
	err.Retryable = true
	return err
}
