package aiday_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestApplicationServiceDashboardSyncsAndAggregates(t *testing.T) {
	opener := newApplicationServiceDatabase(t)
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, location)
	var openWriteCalls, syncCalls int

	service := aiday.NewService(aiday.ServiceOptions{
		OpenRead: opener,
		OpenWrite: func() (*sql.DB, error) {
			openWriteCalls++
			return opener()
		},
		Sync: func(db *sql.DB) (int, error) {
			syncCalls++
			serviceMustExec(t, db, `INSERT INTO sessions
				(id,short_id,agent,project,file_path,created_ts,last_ts)
				VALUES ('service-dashboard','svc','codex','atm','/tmp/service-dashboard',?,?)`,
				now.Add(-2*time.Hour).Unix(), now.Add(-time.Hour).Unix())
			serviceMustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts)
				VALUES ('service-dashboard',0,'user','帮我实现 dashboard service',?)`,
				now.Add(-2*time.Hour).Unix())
			serviceMustExec(t, db, `INSERT INTO usage_events
				(session_id,ts,input_tokens,output_tokens,duration_ms)
				VALUES ('service-dashboard',?,1200,300,2500)`, now.Add(-time.Hour).Unix())
			return 7, nil
		},
		Now:      func() time.Time { return now },
		Location: func() *time.Location { return location },
	})

	dashboard, meta, err := service.Dashboard(context.Background(), aiday.DashboardInput{
		Days: 3,
		Sync: true,
	})
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if openWriteCalls != 1 {
		t.Errorf("OpenWrite calls = %d, want 1", openWriteCalls)
	}
	if syncCalls != 1 {
		t.Errorf("Sync calls = %d, want 1", syncCalls)
	}
	if meta.SyncedFiles != 7 {
		t.Errorf("OperationMeta.SyncedFiles = %d, want 7", meta.SyncedFiles)
	}
	if dashboard.SchemaVersion != aiday.ContractVersion {
		t.Errorf("dashboard schema = %d, want %d", dashboard.SchemaVersion, aiday.ContractVersion)
	}
	if dashboard.Today.Day != "2026-08-20" {
		t.Errorf("dashboard today = %q, want 2026-08-20", dashboard.Today.Day)
	}
	if dashboard.Today.Features.SessionCount != 1 || dashboard.Today.Features.TurnCount != 1 {
		t.Errorf("dashboard today features = %+v, want one synced session and turn", dashboard.Today.Features)
	}
	if dashboard.Today.Features.InputTokens != 1200 || dashboard.Today.Features.OutputTokens != 300 {
		t.Errorf("dashboard today tokens = %d/%d, want 1200/300",
			dashboard.Today.Features.InputTokens, dashboard.Today.Features.OutputTokens)
	}
	if dashboard.Atlas.Total == 0 || len(dashboard.Atlas.Badges) == 0 {
		t.Errorf("dashboard atlas was not aggregated: %+v", dashboard.Atlas)
	}
	if len(dashboard.History.Days) != 3 {
		t.Errorf("dashboard history days = %d, want 3", len(dashboard.History.Days))
	}
	if len(dashboard.Privacy.Sources) != 1 || dashboard.Privacy.Sources[0].Source != "codex" {
		t.Errorf("dashboard privacy sources = %+v, want synced codex source", dashboard.Privacy.Sources)
	}
}

func TestApplicationServiceDashboardRejectsInvalidDays(t *testing.T) {
	for _, days := range []int{0, 3651} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			service := aiday.NewService(aiday.ServiceOptions{
				OpenWrite: unexpectedApplicationServiceOpen(t),
			})
			_, _, err := service.Dashboard(context.Background(), aiday.DashboardInput{Days: days})
			assertApplicationInvalidArgument(t, err, "days")
		})
	}
}

func TestApplicationServiceFeedbackRejectsClearWithValues(t *testing.T) {
	service := aiday.NewService(aiday.ServiceOptions{
		OpenWrite: unexpectedApplicationServiceOpen(t),
		Location:  func() *time.Location { return time.UTC },
	})
	_, err := service.Feedback(context.Background(), aiday.FeedbackInput{
		Day:            "2026-08-20",
		Clear:          true,
		Verdict:        "corrected",
		CorrectedBadge: "code_architect",
		SemanticLabels: []string{"directive"},
	})
	assertApplicationInvalidArgument(t, err, "clear")
}

func TestApplicationServiceFeedbackValidatesWireValuesBeforePersistence(t *testing.T) {
	tests := []struct {
		name  string
		input aiday.FeedbackInput
		field string
	}{
		{name: "verdict", input: aiday.FeedbackInput{Day: "2026-08-20", Verdict: "maybe"}, field: "verdict"},
		{name: "badge", input: aiday.FeedbackInput{Day: "2026-08-20", Verdict: "corrected", CorrectedBadge: "missing"}, field: "corrected_badge_id"},
		{name: "semantic label", input: aiday.FeedbackInput{Day: "2026-08-20", Verdict: "accurate", SemanticLabels: []string{"invented"}}, field: "semantic_labels"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := aiday.NewService(aiday.ServiceOptions{
				OpenWrite: unexpectedApplicationServiceOpen(t),
				Location:  func() *time.Location { return time.UTC },
			})
			_, err := service.Feedback(context.Background(), testCase.input)
			assertApplicationInvalidArgument(t, err, testCase.field)
		})
	}
}

func TestApplicationServicePrivacyValidatesRetentionBeforePersistence(t *testing.T) {
	service := aiday.NewService(aiday.ServiceOptions{OpenWrite: unexpectedApplicationServiceOpen(t)})
	invalid := 0
	_, err := service.SetPrivacy(context.Background(), aiday.PrivacyPatch{RetentionDays: &invalid})
	assertApplicationInvalidArgument(t, err, "retention_days")
}

func TestApplicationServiceBadgeReturnsTypedNotFoundBeforePersistence(t *testing.T) {
	service := aiday.NewService(aiday.ServiceOptions{OpenRead: unexpectedApplicationServiceOpen(t)})
	_, err := service.Badge(context.Background(), "missing")
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("error = %v, want not_found", err)
	}
}

func TestApplicationServiceInfrastructureErrorHasSafeTypedMessage(t *testing.T) {
	service := aiday.NewService(aiday.ServiceOptions{
		OpenRead: func() (*sql.DB, error) {
			return nil, errors.New("open /private/secret/path: token=secret")
		},
	})
	_, err := service.Privacy(context.Background())
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Message != "open ATM database failed" || !appErr.Retryable {
		t.Fatalf("application error = %+v", appErr)
	}
}

func TestApplicationServiceDeleteRequiresConfirmation(t *testing.T) {
	service := aiday.NewService(aiday.ServiceOptions{
		OpenWrite: unexpectedApplicationServiceOpen(t),
	})
	_, err := service.Delete(context.Background(), aiday.DeleteInput{
		From: "2026-08-20",
		To:   "2026-08-20",
	})
	assertApplicationInvalidArgument(t, err, "confirmed")
}

func unexpectedApplicationServiceOpen(t *testing.T) func() (*sql.DB, error) {
	t.Helper()
	return func() (*sql.DB, error) {
		t.Helper()
		t.Fatal("database opener called before application validation")
		return nil, nil
	}
}

func assertApplicationInvalidArgument(t *testing.T, err error, field string) {
	t.Helper()
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("error = %v, want application.ErrInvalidArgument", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error type = %T, want *application.Error", err)
	}
	if got := appErr.Details["field"]; got != field {
		t.Errorf("error details field = %v, want %q", got, field)
	}
}

var applicationServiceConfigMu sync.Mutex

// newApplicationServiceDatabase uses store.Open only to initialize a complete
// schema. It restores config immediately, then every service call opens the
// captured database path directly; no test leaves process-wide ATM paths active.
func newApplicationServiceDatabase(t *testing.T) func() (*sql.DB, error) {
	t.Helper()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "atm.db")

	applicationServiceConfigMu.Lock()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	config.AtmDir, config.AtmDB = directory, databasePath
	initial, err := store.Open()
	config.AtmDir, config.AtmDB = oldDir, oldDB
	applicationServiceConfigMu.Unlock()
	if err != nil {
		t.Fatalf("initialize service database: %v", err)
	}
	if err := initial.Close(); err != nil {
		t.Fatalf("close initial service database: %v", err)
	}

	dsn := (&url.URL{Scheme: "file", Path: databasePath}).String() +
		"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	return func() (*sql.DB, error) {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		if err := db.Ping(); err != nil {
			db.Close()
			return nil, err
		}
		return db, nil
	}
}

func serviceMustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seed service database: %v", err)
	}
}
