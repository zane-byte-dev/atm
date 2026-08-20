package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatsJSONShapesAcrossGroups(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	oldRange, oldSession := statsRangeFlag, statsSessionFlag
	t.Cleanup(func() {
		statsRangeFlag = oldRange
		statsSessionFlag = oldSession
	})
	jsonOutput = true
	statsDaysFlag = 2

	for _, group := range []string{
		"", "model", "model-day", "model-hour", "skill", "session",
		"session-usage", "request", "speed", "day", "hour", "wrapped",
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

func groupName(group string) string {
	if group == "" {
		return "project"
	}
	return group
}
