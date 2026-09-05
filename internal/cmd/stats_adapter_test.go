package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestStatsJSONShapesAcrossGroups(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	createdTS := seedCommandSession(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (
		session_id, model, ts, input_tokens, output_tokens, cache_create_tokens,
		cache_read_tokens, cost_usd, fingerprint, request_count, duration_ms
	) VALUES ('cmd-session-full', 'gpt-5.5', ?, 10, 2, 3, 4, 0.1, 'json-shape', 1, 1000)`, createdTS+30); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	oldRange, oldSession := statsRangeFlag, statsSessionFlag
	t.Cleanup(func() {
		statsRangeFlag = oldRange
		statsSessionFlag = oldSession
	})
	jsonOutput = true
	statsDaysFlag = 2

	for _, group := range []string{
		"", "model", "model-day", "model-hour", "skill", "session",
		"request", "speed", "day", "hour", "wrapped",
	} {
		t.Run(groupName(group), func(t *testing.T) {
			statsByFlag = group
			raw := captureStdout(t, func() {
				if err := runStats(statsCmd, nil); err != nil {
					t.Fatalf("runStats(%q): %v", group, err)
				}
			})
			var decoded any
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				t.Fatalf("decode stats %q: %v\n%s", group, err, raw)
			}
			if group == "speed" || group == "wrapped" {
				if _, ok := decoded.(map[string]any); !ok {
					t.Fatalf("stats %q JSON root = %T, want object", group, decoded)
				}
				return
			}
			if _, ok := decoded.([]any); !ok {
				t.Fatalf("stats %q JSON root = %T, want array", group, decoded)
			}
		})
	}
}

func TestStatsNamedRangeUsesCalendarLabel(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	oldRange, oldSession := statsRangeFlag, statsSessionFlag
	t.Cleanup(func() {
		statsRangeFlag = oldRange
		statsSessionFlag = oldSession
	})
	statsRangeFlag = "yesterday"
	statsByFlag = "skill"

	out := captureStdout(t, func() {
		if err := runStats(statsCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(out, "Statistics by Skill (yesterday)\n") {
		t.Fatalf("named range heading = %q", out)
	}
}

func TestStatsJSONRowsExposeCacheSafeTokenBreakdown(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	createdTS := seedCommandSession(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (
		session_id, model, ts, input_tokens, output_tokens, cache_create_tokens,
		cache_read_tokens, cost_usd, fingerprint, request_count, duration_ms
	) VALUES ('cmd-session-full', 'gpt-5.5', ?, 10, 2, 3, 4, 0.1, 'json-fields', 1, 1000)`, createdTS+30); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	jsonOutput = true
	statsDaysFlag = 2
	statsByFlag = "request"

	raw := captureStdout(t, func() {
		if err := runStats(statsCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var rows []map[string]any
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("decode request stats: %v\n%s", err, raw)
	}
	if len(rows) == 0 {
		t.Fatal("request stats are empty")
	}
	for _, key := range []string{
		"fresh_input_tokens", "cache_create_tokens", "cache_read_tokens",
		"total_input_tokens", "total_tokens", "cost_estimated", "pricing_source",
		"sampled_requests", "untimed_requests", "out_of_window_requests",
	} {
		if _, ok := rows[0][key]; !ok {
			t.Fatalf("request JSON missing %q: %#v", key, rows[0])
		}
	}
}

func groupName(group string) string {
	if group == "" {
		return "project"
	}
	return group
}
