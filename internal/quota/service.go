package quota

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Trend is the rate a window is filling at. The persistence package owns the
// durable representation and its published field names; the alias keeps adapters
// on the quota boundary without restating that shape.
type Trend = store.QuotaTrend

// reader is one agent's live reading, named so the service can filter by agent
// before paying for the read rather than reading everything and discarding it.
type reader struct {
	agent       string
	displayName string
	read        func(live bool) *parser.QuotaInfo
}

// readers is ATM's fixed reporting order. It is a table rather than three
// branches so the JSON, the text view, and history sampling cannot drift on
// which agents exist or what they are called.
var readers = []reader{
	{
		agent:       "codex",
		displayName: "Codex",
		read:        func(bool) *parser.QuotaInfo { return parser.CodexQuota() },
	},
	{
		agent:       "grokbuild",
		displayName: "Grok Build",
		// Live is opt-in (grok_live_quota / ATM_GROK_LIVE_QUOTA); the default stays
		// a local log read with no network traffic.
		read: func(live bool) *parser.QuotaInfo { return parser.GrokQuotaAuto(live) },
	},
	{
		agent: "antigravity",
		// The scope is in the name because the reading is one of the account's two
		// model groups; see the Antigravity parser for why the other cannot be
		// reported alongside it.
		displayName: "Antigravity (Gemini models)",
		read:        func(bool) *parser.QuotaInfo { return parser.AntigravityQuota() },
	},
}

// ServiceOptions are the quota domain's clock, persistence, and provider ports.
type ServiceOptions struct {
	Now           func() time.Time
	OpenRead      func() (*sql.DB, error)
	ProviderCards func(context.Context) (map[string][]ProviderCard, []error)
}

// Service owns the whole quota read: which agents to ask, the live-billing
// opt-in, trend lookup and its degradation, provider card collection, and the
// reset-aware percentage. Adapters render what it returns.
type Service struct {
	now           func() time.Time
	openRead      func() (*sql.DB, error)
	providerCards func(context.Context) (map[string][]ProviderCard, []error)
}

var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.ProviderCards == nil {
		options.ProviderCards = loadProviderCards
	}
	return Service{
		now:           options.Now,
		openRead:      options.OpenRead,
		providerCards: options.ProviderCards,
	}
}

// Snapshot reads every quota source the caller asked for. It returns an error
// only for a bad request: an agent that will not resolve, or a cancelled call.
// Every source failure degrades — a missing reading, a missing trend, or a
// provider warning — because this command answered from agent logs long before
// any history or provider existed and must keep working when they are absent.
func (service Service) Snapshot(
	ctx context.Context,
	call application.Call,
	input Input,
) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, invalid("context", nil, "quota context is required")
	}
	if err := call.Validate(); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, unavailable("read quota", err)
	}
	agent, err := normalizeAgent(input.Agent)
	if err != nil {
		return Snapshot{}, err
	}
	want := func(name string) bool { return agent == "" || agent == name }

	now := service.now()
	cards, providerErrs := service.providerCards(ctx)
	snapshot := Snapshot{Agents: map[string]*AgentQuota{}}
	for _, err := range providerErrs {
		snapshot.Warnings = append(snapshot.Warnings, err.Error())
	}

	for _, source := range readers {
		if !want(source.agent) {
			continue
		}
		reading := source.read(input.Live)
		entry := service.agentQuota(source, reading, now)
		// A requested agent is present even when it reported nothing, as an
		// explicit null: "asked and got nothing" is not the same answer as
		// "not asked", and consumers key off the difference.
		snapshot.Agents[source.agent] = entry
		if entry != nil && len(entry.Windows()) > 0 {
			snapshot.Order = append(snapshot.Order, source.agent)
		}
	}
	service.mergeProviderCards(&snapshot, cards, want)
	return snapshot, nil
}

// agentQuota converts one live reading. It returns nil for a reading that says
// nothing about a limit: source and products only decorate an agent that already
// reported a window or a plan, so a bare source label never presents itself as a
// quota. Provider cards are merged afterwards and may revive a nil entry.
func (service Service) agentQuota(source reader, reading *parser.QuotaInfo, now time.Time) *AgentQuota {
	if reading == nil {
		return nil
	}
	entry := &AgentQuota{DisplayName: source.displayName, Plan: reading.Plan}
	trends := service.trends(source.agent, reading, now)
	entry.Primary = window(reading.Primary, now, trends)
	entry.Secondary = window(reading.Secondary, now, trends)
	if entry.Plan == "" && entry.Primary == nil && entry.Secondary == nil {
		return nil
	}
	entry.Source = reading.Source
	for _, product := range reading.Products {
		entry.Products = append(entry.Products, Product{
			Name: product.Name, UsedPercent: product.UsedPercent,
		})
	}
	return entry
}

func (service Service) mergeProviderCards(
	snapshot *Snapshot,
	cardsByAgent map[string][]ProviderCard,
	want func(string) bool,
) {
	agents := make([]string, 0, len(cardsByAgent))
	for agent := range cardsByAgent {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		cards := cardsByAgent[agent]
		if !want(agent) || len(cards) == 0 {
			continue
		}
		entry := snapshot.Agents[agent]
		if entry == nil {
			// A provider can report for an agent that has no reader of its own, so
			// the entry is created here rather than skipped. It gets no DisplayName:
			// there are no windows to head, and a card names its own provider.
			entry = &AgentQuota{}
			snapshot.Agents[agent] = entry
		}
		entry.ProviderCards = cards
	}
}

// trends reads the trend for each window the agent currently reports. Every
// failure degrades to no trend rather than an error: a missing database, a
// database this build cannot open read-only, or a run before the first two
// samples all have to leave the plain percentage intact.
func (service Service) trends(agent string, reading *parser.QuotaInfo, now time.Time) map[int]Trend {
	windows := make([]int, 0, 2)
	for _, limit := range []*parser.QuotaLimit{reading.Primary, reading.Secondary} {
		if limit != nil && limit.WindowMinutes > 0 {
			windows = append(windows, limit.WindowMinutes)
		}
	}
	if len(windows) == 0 {
		return nil
	}
	// Showing a quota reading must never create or migrate a database.
	if _, err := os.Stat(config.AtmDB); err != nil {
		return nil
	}
	db, err := service.openRead()
	if err != nil || db == nil {
		return nil
	}
	defer db.Close()
	trends := map[int]Trend{}
	for _, windowMinutes := range windows {
		trend, ok, err := store.QuotaTrendFor(db, agent, windowMinutes, now)
		if err != nil {
			return nil
		}
		if ok {
			trends[windowMinutes] = trend
		}
	}
	return trends
}

// RecordSamples appends the current rate-limit readings to history. It reads only
// local sources — never the opt-in Grok live billing endpoint — because its
// caller runs unattended on a timer and must not make network calls the user did
// not ask for on that path.
//
// Antigravity is included even though its reading arrives over HTTP, because the
// peer is loopback: a process already running on this machine, answering from its
// own cache, with nothing leaving the host. That is the same category as reading
// another client's log file, not the same category as the Grok billing API. It has
// to be sampled here or not at all — Antigravity persists no quota anywhere, so
// skipping this path would mean the trend can never be computed.
//
// It takes the caller's connection rather than opening its own: sampling rides on
// a sync that already holds a writable one, and a second writer would contend
// with it for no gain.
func (service Service) RecordSamples(db *sql.DB, now time.Time) error {
	var samples []store.QuotaSample
	for _, source := range readers {
		reading := source.read(false)
		if reading == nil {
			continue
		}
		for _, limit := range []*parser.QuotaLimit{reading.Primary, reading.Secondary} {
			if limit == nil || limit.WindowMinutes <= 0 {
				continue
			}
			samples = append(samples, store.QuotaSample{
				Agent: source.agent, WindowMinutes: limit.WindowMinutes,
				// Record what is true now rather than what the log still says: a
				// window whose reset has passed has already refilled, and storing
				// the stale percentage would look like usage that never drained.
				UsedPercent: displayPercent(limit, now),
				ResetsAt:    limit.ResetsAt, TS: now.Unix(),
			})
		}
	}
	return store.RecordQuotaSamples(db, samples, now)
}

func window(limit *parser.QuotaLimit, now time.Time, trends map[int]Trend) *Window {
	if limit == nil {
		return nil
	}
	converted := &Window{
		UsedPercent:    limit.UsedPercent,
		WindowMinutes:  limit.WindowMinutes,
		ResetsAt:       limit.ResetsAt,
		DisplayPercent: displayPercent(limit, now),
		ResetPending:   resetPending(limit, now),
	}
	if converted.ResetPending {
		converted.ResetsIn = Countdown(limit.ResetsAt, now)
	}
	// Absent when history is too thin to divide, so a consumer can tell "not
	// enough data yet" from "not moving".
	if trend, ok := trends[limit.WindowMinutes]; ok {
		converted.Trend = &trend
	}
	return converted
}

// displayPercent is the reset-aware reading: a window whose reset time has passed
// has already refilled, so it is empty now regardless of what the log carries.
func displayPercent(limit *parser.QuotaLimit, now time.Time) float64 {
	if limit.ResetsAt > 0 && time.Unix(limit.ResetsAt, 0).Before(now) {
		return 0
	}
	return limit.UsedPercent
}

func resetPending(limit *parser.QuotaLimit, now time.Time) bool {
	return limit.ResetsAt > 0 && time.Unix(limit.ResetsAt, 0).After(now)
}

// Countdown renders how long until epoch as a coarse span. Quota keeps its own
// formatter rather than borrowing the elapsed-time one: this counts toward a
// future reset, so it needs a day tier and no seconds tier. Exported because the
// same string is both a payload field and what the text view prints.
func Countdown(epoch int64, now time.Time) string {
	remaining := time.Unix(epoch, 0).Sub(now)
	if remaining <= 0 {
		return "resetting"
	}
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	if hours >= 24 {
		return fmt.Sprintf("%dd%dh", hours/24, hours%24)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func normalizeAgent(raw string) (string, error) {
	if raw == "" {
		return "", nil
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
