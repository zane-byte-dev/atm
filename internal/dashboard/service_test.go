package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/store"
)

func dashboardTestCall() application.Call {
	return application.Call{
		RequestID: "test-dashboard-1",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginIPC,
		},
	}
}

func TestBuildSnapshotOrchestratesWorkAndReturnsStableEmptyCollections(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local)
	service := Service{
		now: func() time.Time { return now },
		live: func(context.Context, string) (LiveStatus, error) {
			return LiveStatus{Time: "10:30:00"}, nil
		},
		loadTodos: func() (*store.TodoFile, error) {
			return &store.TodoFile{Items: []store.Todo{{
				ID: "t2", Title: "Dashboard service", Priority: "P1",
				Status: store.TodoStatusOpen, Created: "2026-08-20",
			}}}, nil
		},
		loadCurrent: func(_ context.Context, _ application.Call, sessionID string) (*CurrentSession, error) {
			return &CurrentSession{SessionID: sessionID, State: "unbound"}, nil
		},
		query: func(_ context.Context, _ time.Time, _ string, sections sectionSet, sync bool) (queryResult, error) {
			if !sections.work || sections.stats || sync {
				t.Fatalf("query options = %+v, sync=%v", sections, sync)
			}
			return queryResult{ranges: map[string]Range{}}, nil
		},
	}

	result, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{
		Sections: []string{"work"}, SessionID: "s9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != contract.DashboardSchemaVersion {
		t.Fatalf("schema version = %d", result.SchemaVersion)
	}
	if len(result.Todos) != 1 || result.Todos[0].ID != "t2" || result.Work.Summary.Open != 1 {
		t.Fatalf("work result = %+v, todos=%+v", result.Work, result.Todos)
	}
	if result.CurrentSession == nil || result.CurrentSession.SessionID != "s9" {
		t.Fatalf("current session = %+v", result.CurrentSession)
	}
	if result.DayStats == nil || result.HourStats == nil || result.LiveStatus.Sessions == nil ||
		result.LiveStatus.Bindings == nil || result.Ranges == nil {
		t.Fatal("optional collections must encode as []/{} rather than null")
	}
}

func TestBuildSnapshotRejectsUnknownSectionBeforeCallingDependencies(t *testing.T) {
	service := Service{now: time.Now}
	_, err := service.BuildSnapshot(
		context.Background(), dashboardTestCall(), Request{Sections: []string{"todos"}},
	)
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid_argument", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Details["field"] != "sections" {
		t.Fatalf("typed details = %#v", err)
	}
}

func TestStatsOnlySnapshotDoesNotReadWorkOrLiveState(t *testing.T) {
	service := Service{
		now: time.Now,
		loadTodos: func() (*store.TodoFile, error) {
			t.Fatal("stats-only snapshot loaded todos")
			return nil, nil
		},
		loadCurrent: func(context.Context, application.Call, string) (*CurrentSession, error) {
			t.Fatal("stats-only snapshot loaded a current binding")
			return nil, nil
		},
		live: func(context.Context, string) (LiveStatus, error) {
			t.Fatal("stats-only snapshot loaded live state")
			return LiveStatus{}, nil
		},
		query: func(_ context.Context, _ time.Time, _ string, sections sectionSet, _ bool) (queryResult, error) {
			if sections.work || !sections.stats {
				t.Fatalf("sections = %+v", sections)
			}
			return queryResult{}, nil
		},
	}
	result, err := service.BuildSnapshot(
		context.Background(), dashboardTestCall(), Request{Sections: []string{"stats"}, SessionID: "ignored"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ranges == nil || result.Todos == nil || result.LiveStatus.Sessions == nil {
		t.Fatal("stats-only response must retain stable empty collection shapes")
	}
}

func TestBuildStatsQualityCarriesCoveragePricingAndSpeedSamples(t *testing.T) {
	quality := buildStatsQuality(
		[]store.StatsResult{
			{Agent: "codex", Sessions: 2, TokenSessions: 1, Requests: 10, DetailedRequests: 8, AggregateRequests: 2, CostUSD: 100},
			{Agent: "qoderwork", Sessions: 3},
		},
		[]store.ModelStatsResult{
			{Model: "gpt-5.5", CostUSD: 1, PricingSource: store.PricingExact},
			{Model: "gpt-5.6-sol", CostUSD: 99, CostEstimated: true, PricingSource: store.PricingFamily},
		},
		store.SpeedReport{Models: []store.SpeedStatsResult{{Requests: 8, Sampled: 6}}, Untimed: 1, OutOfWindow: 1},
	)
	if quality.ActiveSessions != 5 || quality.TokenSessions != 1 || quality.SessionCoveragePct != 20 ||
		quality.ActiveAgents != 2 || quality.TokenAgents != 1 || quality.AgentCoveragePct != 50 ||
		quality.RequestCoveragePct != 80 || quality.SpeedRequests != 8 ||
		quality.SpeedSampledRequests != 6 || quality.SpeedSamplePct != 75 ||
		quality.EstimatedCostUSD != 99 || quality.EstimatedCostShare != 0.99 {
		t.Fatalf("quality = %+v", quality)
	}
	if len(quality.PricingSources) != 2 || quality.PricingSources[0] != "exact" || quality.PricingSources[1] != "family" {
		t.Fatalf("pricing sources = %#v", quality.PricingSources)
	}
}

func TestBuildSnapshotRejectsNilContextAsTypedInputError(t *testing.T) {
	service := Service{now: time.Now}
	_, err := service.BuildSnapshot(nil, dashboardTestCall(), Request{})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid_argument", err)
	}
}

func TestBuildSnapshotWrapsInfrastructureFailureWithoutExposingCause(t *testing.T) {
	secret := errors.New("/Users/private/atm.db: secret sqlite detail")
	service := Service{
		now:  time.Now,
		live: func(context.Context, string) (LiveStatus, error) { return LiveStatus{}, nil },
		loadTodos: func() (*store.TodoFile, error) {
			return nil, secret
		},
	}
	_, err := service.BuildSnapshot(
		context.Background(), dashboardTestCall(), Request{Sections: []string{"work"}},
	)
	if !errors.Is(err, application.ErrUnavailable) || !errors.Is(err, secret) {
		t.Fatalf("error = %v, want unavailable retaining cause", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Message != "load dashboard work failed" {
		t.Fatalf("safe application error = %#v", err)
	}
}

func TestBuildWorkClassifiesDueAndSortsByPriority(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.Local)
	file := &store.TodoFile{Items: []store.Todo{
		{ID: "t3", Priority: "P2", Status: store.TodoStatusOpen, Created: "2026-08-19"},
		{ID: "t1", Priority: "P0", Status: store.TodoStatusOpen, Created: "2026-08-20", Tags: []string{store.TodoTagMaintenance}},
		{ID: "t2", Priority: "P1", Status: store.TodoStatusInProgress, ReviewAt: "2026-08-20"},
	}}
	view := buildWork(file, now)
	if len(view.Open) != 2 || view.Open[0].ID != "t1" || len(view.Working) != 1 ||
		len(view.Due) != 1 || view.Due[0].ID != "t2" {
		t.Fatalf("work view = %+v", view)
	}
	if view.Summary.Maintenance != 1 || view.Summary.Due != 1 {
		t.Fatalf("summary = %+v", view.Summary)
	}
}
