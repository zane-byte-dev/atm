package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestBuildSnapshotSelectsAndDeduplicatesRanges(t *testing.T) {
	for _, test := range []struct {
		name        string
		ranges      []string
		compact     bool
		wantRanges  []config.MetricsRange
		wantCompact bool
	}{
		{name: "default", wantRanges: config.MetricsRanges},
		{name: "compact without selection retains full snapshot", compact: true, wantRanges: config.MetricsRanges},
		{
			name: "explicit normalized selection", ranges: []string{" Today ", "last-7-days", "today"}, compact: true,
			wantRanges: []config.MetricsRange{config.RangeToday, config.RangeLast7Days}, wantCompact: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			service := Service{
				now: time.Now,
				query: func(_ context.Context, _ time.Time, _ string, sections sectionSet, ranges []config.MetricsRange, compact, sync bool) (queryResult, error) {
					called = true
					if !sections.stats || sections.work || sync || compact != test.wantCompact || !reflect.DeepEqual(ranges, test.wantRanges) {
						t.Fatalf("query options = %+v, %v, compact=%v, sync=%v", sections, ranges, compact, sync)
					}
					return queryResult{}, nil
				},
			}
			_, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{
				Sections: []string{"stats"}, Ranges: test.ranges, Compact: test.compact,
			})
			if err != nil || !called {
				t.Fatalf("BuildSnapshot: called=%v, err=%v", called, err)
			}
		})
	}
}

func TestBuildSnapshotRejectsUnknownRangeBeforeDependencies(t *testing.T) {
	service := Service{}
	_, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{
		Sections: []string{"stats"}, Ranges: []string{"today", "all_time"},
	})
	var appErr *application.Error
	if !errors.Is(err, application.ErrInvalidArgument) || !errors.As(err, &appErr) || appErr.Details["field"] != "ranges" {
		t.Fatalf("error = %#v, want invalid_argument identifying ranges", err)
	}
}

func TestCompactSnapshotMatchesFullRangeWithoutUnselectedHistory(t *testing.T) {
	db := dashboardTestDB(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, config.Loc)
	for _, entry := range []struct {
		id  string
		day time.Time
	}{
		{"today", now.Add(-time.Hour)},
		{"yesterday", now.AddDate(0, 0, -1)},
		{"older", now.AddDate(0, 0, -20)},
	} {
		if _, err := db.Exec(`INSERT INTO sessions
			(id, short_id, agent, project, file_path, created_at, created_ts, last_ts)
			VALUES (?, ?, 'codex', 'atm', ?, ?, ?, ?)`,
			entry.id, entry.id, entry.id+".jsonl", entry.day.Format(time.RFC3339), entry.day.Unix(), entry.day.Unix()+1); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO messages (session_id, seq, role, content, ts)
			VALUES (?, 0, 'user', 'dashboard fixture', ?)`, entry.id, entry.day.Unix()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO usage_events
			(session_id, model, ts, input_tokens, output_tokens, request_count, duration_ms)
			VALUES (?, 'gpt-5.5', ?, 100, 50, 1, 1000)`, entry.id, entry.day.Unix()+1); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(nil)
	service.now = func() time.Time { return now }
	full, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{Sections: []string{"stats"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []config.MetricsRange{config.RangeToday, config.RangeYesterday, config.RangeLast30Days} {
		t.Run(string(name), func(t *testing.T) {
			selected, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{
				Sections: []string{"stats"}, Ranges: []string{string(name)}, Compact: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(selected.Ranges) != 1 || !reflect.DeepEqual(selected.Ranges[string(name)], full.Ranges[string(name)]) {
				t.Fatal("selected range must preserve the complete snapshot's counts, sessions, speed and quality")
			}
			start, _ := compactSeriesBounds(now, []config.MetricsRange{name})
			startDate := time.Unix(start, 0).In(config.Loc).Format("2006-01-02")
			wantDays := []store.DayStatsResult{}
			for _, row := range full.DayStats {
				if row.Date >= startDate {
					wantDays = append(wantDays, row)
				}
			}
			if !reflect.DeepEqual(selected.DayStats, wantDays) {
				t.Fatalf("compact days = %+v, want %+v", selected.DayStats, wantDays)
			}
			if len(selected.DayStats) == 0 || selected.DayStats[len(selected.DayStats)-1].Date != "2026-09-03" {
				t.Fatal("compact historical selection must retain today's summary bucket")
			}
			for _, row := range selected.ModelDayStats {
				if row.Date < startDate {
					t.Fatalf("unselected model bucket retained: %+v", row)
				}
			}
			for _, row := range selected.ProjectDayStats {
				if row.Date < startDate {
					t.Fatalf("unselected project bucket retained: %+v", row)
				}
			}
			if !reflect.DeepEqual(selected.TodoCompletions, full.TodoCompletions) || selected.Todos == nil || selected.Work.Open == nil || selected.LiveStatus.Sessions == nil {
				t.Fatal("selected stats must retain completion history and empty work/live collection shapes")
			}
		})
	}
	t.Run("lightweight summary", func(t *testing.T) {
		// A summary must not need any of the tables used only by range
		// breakdowns. This catches accidental restoration of full stats work.
		if _, err := db.Exec(`DROP TABLE tools`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DROP TABLE skill_events`); err != nil {
			t.Fatal(err)
		}
		summary, err := service.BuildSnapshot(context.Background(), dashboardTestCall(), Request{
			Sections: []string{"summary"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(summary.DayStats, full.DayStats[len(full.DayStats)-1:]) {
			t.Fatalf("summary today = %+v, want complete snapshot's latest day", summary.DayStats)
		}
		if len(summary.Ranges) != 0 || len(summary.HourStats) != 0 || len(summary.ModelDayStats) != 0 ||
			len(summary.ProjectDayStats) != 0 || len(summary.TodoCompletions) != 0 {
			t.Fatal("summary must not carry range breakdowns, charts, or completion history")
		}
	})
}

func TestCompactSeriesUsesCalendarMonthRatherThanThirtyDays(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, config.Loc)
	start, end := compactSeriesBounds(now, []config.MetricsRange{config.RangeThisMonth})
	if got := time.Unix(start, 0).In(config.Loc).Format("2006-01-02"); got != "2026-08-01" || end != now.Unix() {
		t.Fatalf("month bounds = %s..%d, want August 1 through now", got, end)
	}
}

func dashboardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	config.AtmDir = t.TempDir()
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
