package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/parser"
)

const quotaTestNow = int64(1_700_000_000)

func quotaTestClock() func() time.Time {
	return func() time.Time { return time.Unix(quotaTestNow, 0) }
}

// withReaders replaces the live agent readings so a test does not depend on
// which agents happen to have logs on the machine running it.
func withReaders(t *testing.T, replacement []reader) {
	t.Helper()
	previous := readers
	readers = replacement
	t.Cleanup(func() { readers = previous })
}

func fixedReader(agent, name string, reading *parser.QuotaInfo) reader {
	return reader{
		agent:       agent,
		displayName: name,
		read:        func(bool) *parser.QuotaInfo { return reading },
	}
}

func noProviderCards(context.Context) (map[string][]ProviderCard, []error) { return nil, nil }

func snapshotOf(t *testing.T, service Service, input Input) Snapshot {
	t.Helper()
	snapshot, err := service.Snapshot(context.Background(), quotaTestCall(), input)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot
}

// The two percentages answer different questions and must not be collapsed. A
// window whose reset has passed has already refilled, so what a person is shown
// is 0 — but the published number stays the agent's own reading, because every
// consumer of this payload already interprets resets_at itself.
func TestSnapshotKeepsTheRawPercentAndZeroesOnlyTheDisplayedOne(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{fixedReader("codex", "Codex", &parser.QuotaInfo{
		Primary:   &parser.QuotaLimit{UsedPercent: 87, WindowMinutes: 10080, ResetsAt: quotaTestNow - 60},
		Secondary: &parser.QuotaLimit{UsedPercent: 12, WindowMinutes: 300, ResetsAt: quotaTestNow + 3600},
	})})
	service := NewService(ServiceOptions{Now: quotaTestClock(), ProviderCards: noProviderCards})

	snapshot := snapshotOf(t, service, Input{})
	primary := snapshot.Agents["codex"].Primary
	if primary.UsedPercent != 87 {
		t.Errorf("published used_percent = %v, want the agent's own 87", primary.UsedPercent)
	}
	if primary.DisplayPercent != 0 {
		t.Errorf("displayed percent = %v, want 0 for a window that already refilled", primary.DisplayPercent)
	}
	if primary.ResetPending || primary.ResetsIn != "" {
		t.Errorf("an elapsed reset still counts down: pending=%v in=%q", primary.ResetPending, primary.ResetsIn)
	}

	secondary := snapshot.Agents["codex"].Secondary
	if secondary.DisplayPercent != 12 {
		t.Errorf("displayed percent = %v, want the reading 12 while the reset is ahead", secondary.DisplayPercent)
	}
	if secondary.ResetsIn != "1h0m" {
		t.Errorf("resets_in = %q, want 1h0m", secondary.ResetsIn)
	}
}

// The published shape is what the browser decodes, so the keys are part of the
// contract: a top-level object per agent, null for an agent that reported
// nothing, and no trend key when history is too thin to divide.
func TestSnapshotPublishesOneNullableObjectPerAgent(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{
		fixedReader("codex", "Codex", &parser.QuotaInfo{
			Plan:    "pro",
			Primary: &parser.QuotaLimit{UsedPercent: 34, WindowMinutes: 10080, ResetsAt: quotaTestNow + 7200},
		}),
		fixedReader("grokbuild", "Grok Build", nil),
	})
	service := NewService(ServiceOptions{Now: quotaTestClock(), ProviderCards: noProviderCards})

	encoded, err := json.Marshal(snapshotOf(t, service, Input{}).Agents)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]*struct {
		Plan    string `json:"plan"`
		Primary *struct {
			UsedPercent   float64          `json:"used_percent"`
			WindowMinutes int              `json:"window_minutes"`
			ResetsAt      int64            `json:"resets_at"`
			ResetsIn      string           `json:"resets_in"`
			Trend         *json.RawMessage `json:"trend"`
		} `json:"primary"`
		Secondary *json.RawMessage `json:"secondary"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode published shape: %v — %s", err, encoded)
	}
	entry, ok := decoded["grokbuild"]
	if !ok {
		t.Error("an agent that was asked for is missing entirely; null and absent are different answers")
	}
	if entry != nil {
		t.Errorf("grokbuild reported nothing but is not null: %+v", entry)
	}
	codex := decoded["codex"]
	if codex == nil || codex.Primary == nil {
		t.Fatalf("codex window missing: %s", encoded)
	}
	if codex.Plan != "pro" || codex.Primary.UsedPercent != 34 || codex.Primary.WindowMinutes != 10080 {
		t.Errorf("codex window did not round-trip: %+v", codex)
	}
	if codex.Primary.Trend != nil {
		t.Error("trend present with no history; a consumer cannot then tell 'no data yet' from 'not moving'")
	}
	if codex.Secondary != nil {
		t.Error("absent secondary window serialized as a key")
	}
}

// A source label alone says nothing about a limit. Reporting it as a quota would
// put an agent on screen with a badge and no reading behind it.
func TestSnapshotTreatsASourceWithNoWindowOrPlanAsNoData(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{fixedReader("grokbuild", "Grok Build", &parser.QuotaInfo{Source: "cache"})})
	service := NewService(ServiceOptions{Now: quotaTestClock(), ProviderCards: noProviderCards})

	snapshot := snapshotOf(t, service, Input{})
	if entry := snapshot.Agents["grokbuild"]; entry != nil {
		t.Errorf("a bare source label became a quota: %+v", entry)
	}
	if len(snapshot.Order) != 0 {
		t.Errorf("reporting order includes an agent with no window: %v", snapshot.Order)
	}
}

// `atm quota` answered from agent logs long before any history existed, so an
// absent or unreadable index has to leave the plain percentage intact rather than
// fail the read.
func TestSnapshotDegradesToNoTrendWhenTheIndexCannotBeRead(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{fixedReader("codex", "Codex", &parser.QuotaInfo{
		Primary: &parser.QuotaLimit{UsedPercent: 50, WindowMinutes: 300, ResetsAt: quotaTestNow + 600},
	})})
	opened := 0
	service := NewService(ServiceOptions{
		Now:           quotaTestClock(),
		ProviderCards: noProviderCards,
		OpenRead: func() (*sql.DB, error) {
			opened++
			return nil, errors.New("index is not readable by this build")
		},
	})

	snapshot := snapshotOf(t, service, Input{})
	window := snapshot.Agents["codex"].Primary
	if window == nil || window.UsedPercent != 50 {
		t.Fatalf("reading was lost with the trend: %+v", window)
	}
	if window.Trend != nil {
		t.Errorf("trend survived an unreadable index: %+v", window.Trend)
	}
	// The database file does not exist in a temp data dir, so the pre-check should
	// have spared the opener entirely: showing a quota must never create one.
	if opened != 0 {
		t.Errorf("opened the index %d times when the file does not exist", opened)
	}
}

// A provider failing is a warning, not an error: the agent-log readings in the
// same snapshot are still worth returning.
func TestSnapshotReportsProviderFailuresAsWarnings(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{fixedReader("codex", "Codex", &parser.QuotaInfo{
		Primary: &parser.QuotaLimit{UsedPercent: 20, WindowMinutes: 300, ResetsAt: quotaTestNow + 600},
	})})
	service := NewService(ServiceOptions{
		Now: quotaTestClock(),
		ProviderCards: func(context.Context) (map[string][]ProviderCard, []error) {
			return nil, []error{errors.New("quota provider idealab failed: browser bridge is not running")}
		},
	})

	snapshot := snapshotOf(t, service, Input{})
	if len(snapshot.Warnings) != 1 || snapshot.Agents["codex"] == nil {
		t.Fatalf("provider failure did not degrade: warnings=%v agents=%v", snapshot.Warnings, snapshot.Agents)
	}
}

// A provider can report for an agent ATM has no reader for at all, which is the
// only way that agent appears on screen.
func TestSnapshotAddsAnAgentKnownOnlyThroughAProvider(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{fixedReader("codex", "Codex", nil)})
	card := ProviderCard{ID: "daily", Agent: "claude", Provider: "idealab", Title: "专项AK"}
	service := NewService(ServiceOptions{
		Now: quotaTestClock(),
		ProviderCards: func(context.Context) (map[string][]ProviderCard, []error) {
			return map[string][]ProviderCard{"claude": {card}}, nil
		},
	})

	snapshot := snapshotOf(t, service, Input{})
	entry := snapshot.Agents["claude"]
	if entry == nil || len(entry.ProviderCards) != 1 {
		t.Fatalf("provider-only agent missing: %+v", snapshot.Agents)
	}
	if len(snapshot.Order) != 0 {
		t.Errorf("a provider-only agent is in the rate-limit reporting order: %v", snapshot.Order)
	}
}

// --agent narrows every source, provider cards included; an agent that was not
// asked for must not arrive through the provider path.
func TestSnapshotAgentFilterAlsoNarrowsProviderCards(t *testing.T) {
	withTempAtmDir(t)
	withReaders(t, []reader{
		fixedReader("codex", "Codex", &parser.QuotaInfo{
			Primary: &parser.QuotaLimit{UsedPercent: 5, WindowMinutes: 300, ResetsAt: quotaTestNow + 600},
		}),
		fixedReader("grokbuild", "Grok Build", &parser.QuotaInfo{
			Primary: &parser.QuotaLimit{UsedPercent: 9, WindowMinutes: 300, ResetsAt: quotaTestNow + 600},
		}),
	})
	service := NewService(ServiceOptions{
		Now: quotaTestClock(),
		ProviderCards: func(context.Context) (map[string][]ProviderCard, []error) {
			return map[string][]ProviderCard{
				"claude": {{ID: "daily", Agent: "claude", Provider: "idealab", Title: "专项AK"}},
			}, nil
		},
	})

	snapshot := snapshotOf(t, service, Input{Agent: "codex"})
	if _, ok := snapshot.Agents["grokbuild"]; ok {
		t.Error("an agent that was not asked for is in the snapshot")
	}
	if _, ok := snapshot.Agents["claude"]; ok {
		t.Error("a provider card escaped the --agent filter")
	}
	if len(snapshot.Order) != 1 || snapshot.Order[0] != "codex" {
		t.Errorf("order = %v, want just codex", snapshot.Order)
	}
}

func TestSnapshotRejectsAnUnknownAgent(t *testing.T) {
	service := NewService(ServiceOptions{Now: quotaTestClock(), ProviderCards: noProviderCards})
	_, err := service.Snapshot(context.Background(), quotaTestCall(), Input{Agent: "nope"})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
		t.Fatalf("err = %v, want invalid_argument", err)
	}
}

// Live billing is opt-in, and the path that runs unattended on a timer must never
// take it: RecordSamples reads local sources only.
func TestRecordSamplesNeverAsksForALiveReading(t *testing.T) {
	var liveRequested []bool
	withReaders(t, []reader{{
		agent: "grokbuild", displayName: "Grok Build",
		read: func(live bool) *parser.QuotaInfo {
			liveRequested = append(liveRequested, live)
			return nil
		},
	}})
	service := NewService(ServiceOptions{Now: quotaTestClock(), ProviderCards: noProviderCards})

	// No samples to write, so no connection is touched and nil is safe here.
	if err := service.RecordSamples(nil, time.Unix(quotaTestNow, 0)); err != nil {
		t.Fatalf("record: %v", err)
	}
	for _, live := range liveRequested {
		if live {
			t.Fatal("the unattended sampling path asked for a live billing reading")
		}
	}
}

// Storing the stale percentage of a refilled window would read as usage that
// never drained, which is exactly what a trend is computed from.
func TestSnapshotAndSamplesAgreeOnARefilledWindow(t *testing.T) {
	withTempAtmDir(t)
	limit := &parser.QuotaLimit{UsedPercent: 91, WindowMinutes: 10080, ResetsAt: quotaTestNow - 1}
	now := time.Unix(quotaTestNow, 0)
	if got := displayPercent(limit, now); got != 0 {
		t.Fatalf("displayPercent = %v, want 0 once the reset has passed", got)
	}
}
