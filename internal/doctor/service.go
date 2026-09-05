package doctor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

// GuardDiagnoser is the outbound action gate's own health check. Guard owns those
// findings and its own store access, so this is an injected port rather than
// something the doctor derives.
type GuardDiagnoser interface {
	Diagnose(context.Context, application.Call) ([]guard.DiagnosticIssue, error)
}

// ServiceOptions are the check's clock, persistence, and cross-domain ports.
type ServiceOptions struct {
	Now            func() time.Time
	OpenRead       func() (*sql.DB, error)
	Guard          GuardDiagnoser
	TextModelReady func() bool
	LoadTodos      func() (*store.TodoFile, error)
	DatabaseExists func() bool
}

// Service owns the whole self-check: whether there is an index to read, which
// sources exist, how much of each agent ATM understood, what it had to guess a
// price for, and every finding derived from those. Adapters render the report.
type Service struct {
	now            func() time.Time
	openRead       func() (*sql.DB, error)
	guard          GuardDiagnoser
	textModelReady func() bool
	loadTodos      func() (*store.TodoFile, error)
	databaseExists func() bool
}

var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.Guard == nil {
		options.Guard = guard.Default
	}
	if options.TextModelReady == nil {
		options.TextModelReady = textmodel.Configured
	}
	if options.LoadTodos == nil {
		options.LoadTodos = store.LoadTodosReadOnly
	}
	if options.DatabaseExists == nil {
		options.DatabaseExists = func() bool {
			_, err := os.Stat(config.AtmDB)
			return err == nil
		}
	}
	return Service{
		now: options.Now, openRead: options.OpenRead, guard: options.Guard,
		textModelReady: options.TextModelReady, loadTodos: options.LoadTodos,
		databaseExists: options.DatabaseExists,
	}
}

// Check runs every source, coverage, pricing, collection, gate, and todo check.
//
// A missing session index is a valid state, not an error: the source and todo
// checks are still meaningful, and a fresh install has to be able to run this and
// be told to sync rather than be handed a failure.
func (service Service) Check(
	ctx context.Context,
	call application.Call,
	input Input,
) (Report, error) {
	if ctx == nil {
		return Report{}, invalid("context", nil, "doctor context is required")
	}
	if err := call.Validate(); err != nil {
		return Report{}, err
	}
	if input.Days < 0 {
		return Report{}, invalid("days", input.Days, "doctor days must be zero (all history) or positive")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, unavailable("run doctor", err)
	}
	if !service.databaseExists() {
		return service.check(ctx, call, nil, input)
	}
	// Read-only: a self-check must never create or migrate the index it is
	// reporting on.
	db, err := service.openRead()
	if err != nil {
		return Report{}, unavailable("open session index", err)
	}
	if db == nil {
		return Report{}, unavailable("open session index", errors.New("database opener returned nil"))
	}
	defer db.Close()
	return service.check(ctx, call, db, input)
}

func (service Service) check(ctx context.Context, call application.Call, db *sql.DB, input Input) (Report, error) {
	report := Report{Coverage: []store.Coverage{}, CoverageWindow: CoverageWindow{Mode: "all_time"}}
	var issues []Issue
	var coverageStart, coverageEnd time.Time
	if input.Days > 0 {
		coverageEnd = service.now()
		coverageStart = coverageEnd.Add(-time.Duration(input.Days) * 24 * time.Hour)
		report.CoverageWindow = CoverageWindow{
			Mode: "rolling", Days: input.Days,
			Start: coverageStart.Format(time.RFC3339), End: coverageEnd.Format(time.RFC3339),
		}
	}

	retained := map[string]int{}
	extraction := map[string]store.ExtractionCounts{}
	if db != nil {
		var err error
		if input.Days > 0 {
			report.Coverage, err = store.GetCoverageWindow(db, coverageStart.Unix(), coverageEnd.Unix())
		} else {
			report.Coverage, err = store.GetCoverage(db)
		}
		if err != nil {
			return Report{}, unavailable("read request coverage", err)
		}
		if retained, err = store.GetRetainedSessionCounts(db); err != nil {
			return Report{}, unavailable("read retained session counts", err)
		}
		if extraction, err = store.GetExtractionCounts(db); err != nil {
			return Report{}, unavailable("read extraction counts", err)
		}
	} else {
		issues = append(issues, Issue{
			Severity: "info", Domain: "database", Code: "session_index_missing",
			Subject: config.AtmDB,
			Detail:  "the derived session index does not exist yet",
			Suggestion: "run `atm sync` to build session history and request coverage; " +
				"source and todo checks below are still valid",
		})
	}

	indexed := map[string]int{}
	for _, item := range report.Coverage {
		indexed[item.Agent] = item.Sessions
	}
	report.Sources, issues = appendSourceIssues(issues, indexed, retained, extraction, db != nil)
	issues = append(issues, coverageIssues(report.Coverage)...)

	if db != nil {
		pricing, err := store.GetModelPricing(db)
		if err != nil {
			return Report{}, unavailable("read model pricing", err)
		}
		report.ModelPricing = pricing
		issues = append(issues, pricingIssues(pricing)...)
		issues = append(issues, service.collectionRetentionIssues(db)...)
	}
	issues = append(issues, service.collectionModelIssues()...)
	issues = append(issues, service.guardIssues(ctx, call)...)

	todoIssues, todoFindings := service.todoIssues()
	report.TodoIssues = todoIssues
	issues = append(issues, todoFindings...)

	report.Issues = issues
	report.Summary = Summary{Issues: len(issues)}
	return report, nil
}

// sourcePaths is where each agent's transcripts live. Read through config on
// every call rather than captured once, because a test — and a user editing
// config.json — changes these underneath a long-lived service value.
func sourcePaths() map[string]string {
	return map[string]string{
		"claude": config.ClaudeProjects, "codex": config.CodexSessions,
		"pi": config.PiSessions, "copilot": config.CopilotWorkspaces,
		"qoder": config.QoderDB, "qodercli": config.QoderCLIProjects,
		"qoderwork": config.QoderWorkDB, "grokbuild": config.GrokSessions,
		"antigravity": config.AntigravityConversations(),
	}
}

func appendSourceIssues(
	issues []Issue,
	indexed, retained map[string]int,
	extraction map[string]store.ExtractionCounts,
	haveIndex bool,
) ([]Source, []Issue) {
	paths := sourcePaths()
	var sources []Source
	for _, agent := range parser.All() {
		name := agent.Name()
		path := paths[name]
		_, statErr := os.Stat(path)
		source := Source{
			Agent: name, Path: path, Exists: statErr == nil,
			Files: len(agent.Discover()), IndexedSessions: indexed[name],
			RetainedSessions: retained[name],
		}
		switch {
		case !source.Exists && source.IndexedSessions > 0:
			source.Status = "source_missing_index_retained"
			issues = append(issues, Issue{
				Severity: "warning", Domain: "source", Code: "source_missing_index_retained",
				Subject: name,
				Detail: fmt.Sprintf("source path is missing while %d indexed sessions remain",
					source.IndexedSessions),
				Suggestion: "restore or reconfigure the source path; keep the index for history, " +
					"or run sync after intentionally removing the source",
			})
		case !source.Exists:
			source.Status = "missing"
			issues = append(issues, Issue{
				Severity: "warning", Domain: "source", Code: "source_missing", Subject: name,
				Detail:     "configured source path does not exist",
				Suggestion: "update the path in ~/.atm/config.json or install/use this agent before syncing",
			})
		case source.Files == 0:
			source.Status = "empty"
			issues = append(issues, Issue{
				Severity: "info", Domain: "source", Code: "source_empty", Subject: name,
				Detail:     "source path exists but no supported session files were discovered",
				Suggestion: "verify the configured path and upstream client data format",
			})
		default:
			source.Status = "ok"
		}
		if haveIndex {
			issues = append(issues, extractionIssues(name, source, extraction[name])...)
		}
		sources = append(sources, source)
	}
	return sources, issues
}

func coverageIssues(coverage []store.Coverage) []Issue {
	var issues []Issue
	for _, item := range coverage {
		switch item.CoverageStatus {
		case "inconsistent":
			issues = append(issues, Issue{
				Severity: "warning", Domain: "usage", Code: "request_coverage_inconsistent",
				Subject: item.Agent,
				Detail: fmt.Sprintf("detailed requests (%d) exceed reported requests (%d) by %d",
					item.DetailedRequests, item.ReportedRequests, item.DetailedExcess),
				Suggestion: "resync the agent and inspect parser request-count semantics; " +
					"coverage is capped at 100% until reconciled",
			})
		case "partial":
			issues = append(issues, Issue{
				Severity: "info", Domain: "usage", Code: "request_coverage_partial",
				Subject:    item.Agent,
				Detail:     fmt.Sprintf("request detail coverage is %.1f%%", item.CoveragePercent),
				Suggestion: "use aggregate usage for totals and request detail only where coverage is available",
			})
		}
		if item.UnknownModels > 0 {
			issues = append(issues, Issue{
				Severity: "warning", Domain: "usage", Code: "unknown_models", Subject: item.Agent,
				Detail: fmt.Sprintf("%d request events have no model", item.UnknownModels),
				Suggestion: "update the parser sample or configure model metadata before relying " +
					"on per-model cost",
			})
		}
	}
	return issues
}

// pricingIssues reports the models whose rate ATM guessed, with what that guess
// is carrying. Both cases are grouped into one issue per severity rather than one
// per model: the useful unit is "this much of your reported spend is estimated",
// and a per-model issue list would bury that under names.
func pricingIssues(pricing []store.ModelPricing) []Issue {
	var total, familyCost, defaultCost float64
	var familyModels, defaultModels []string
	for _, entry := range pricing {
		total += entry.CostUSD
		switch entry.Source {
		case store.PricingDefault:
			defaultCost += entry.CostUSD
			defaultModels = append(defaultModels, entry.Model)
		case store.PricingFamily:
			familyCost += entry.CostUSD
			familyModels = append(familyModels, entry.Model)
		}
	}
	share := func(cost float64) string {
		if total <= 0 {
			return "0%"
		}
		return fmt.Sprintf("%.0f%%", cost/total*100)
	}
	var issues []Issue
	if len(defaultModels) > 0 {
		issues = append(issues, Issue{
			Severity: "warning", Domain: "pricing", Code: "pricing_default_rate",
			Subject: strings.Join(defaultModels, ", "),
			Detail: fmt.Sprintf("%d model(s) match no rate and no model family, so they are charged at the conservative Opus-tier default: $%.2f, %s of reported spend",
				len(defaultModels), defaultCost, share(defaultCost)),
			Suggestion: "add the real rates to ~/.atm/pricing.json (per million: [input, output, cache_create, cache_read]); the next `atm sync` reprices history onto them",
		})
	}
	if len(familyModels) > 0 {
		issues = append(issues, Issue{
			Severity: "info", Domain: "pricing", Code: "pricing_family_rate",
			Subject: strings.Join(familyModels, ", "),
			Detail: fmt.Sprintf("%d model(s) are priced at their family's representative rate rather than their own: $%.2f, %s of reported spend",
				len(familyModels), familyCost, share(familyCost)),
			Suggestion: "treat these totals as estimates (`atm stats` marks them with ~), or pin exact rates in ~/.atm/pricing.json",
		})
	}
	return issues
}

// collectionRetentionIssues reports synced chat that outlived its retention
// window. Pruning runs after every sync, so anything still here means it has
// been failing quietly — the one thing about this archive worth diagnosing.
func (service Service) collectionRetentionIssues(db *sql.DB) []Issue {
	days := config.CollectionMessageRetentionDays
	cutoff := store.RetentionCutoff(days, service.now())
	if cutoff <= 0 {
		return nil
	}
	stats, err := store.CollectionMessageStatsFor(db)
	if err != nil {
		return []Issue{{
			Severity: "warning", Domain: "collection", Code: "collection_archive_read_failed",
			Subject: config.AtmDB, Detail: err.Error(),
			Suggestion: "repair or restore the ATM SQLite database; synced chat is queried by `atm collect search`",
		}}
	}
	if stats.Oldest == 0 || stats.Oldest >= cutoff {
		return nil
	}
	return []Issue{{
		Severity: "warning", Domain: "collection", Code: "collection_messages_past_retention",
		Subject: "collection_messages",
		Detail: fmt.Sprintf("synced chat reaches back to %s, older than the %d-day retention window",
			time.Unix(stats.Oldest, 0).In(config.Loc).Format("2006-01-02"), days),
		Suggestion: "run `atm collect run` or `atm collect history <source>` to trigger a prune, " +
			"or set collection_message_retention_days to 0 to keep chat on purpose",
	}}
}

// collectionModelIssues reports collection enabled with no credential for the
// text model that classifies chat. Classification happens in the background and
// fails closed, so a missing key shows up as sources that quietly stop producing
// anything rather than as an error anyone sees. Only the credential is checked —
// this check stays offline and must not spend a model call.
func (service Service) collectionModelIssues() []Issue {
	if !config.CollectionEnabled || service.textModelReady() {
		return nil
	}
	return []Issue{{
		Severity: "warning", Domain: "collection", Code: "collection_model_unavailable",
		Subject: config.TextModelName,
		Detail:  "the built-in text model has no API Key, so nothing can be classified",
		Suggestion: "save one under Settings > Model in the browser workspace or with `atm config credential set`, " +
			"then verify it with `atm config test-text-model`",
	}}
}

// guardIssues degrades to no findings on failure. A gate ATM cannot inspect must
// not take down the report that would have told the user about everything else.
func (service Service) guardIssues(ctx context.Context, call application.Call) []Issue {
	if service.guard == nil {
		return nil
	}
	found, err := service.guard.Diagnose(ctx, call)
	if err != nil {
		return nil
	}
	issues := make([]Issue, 0, len(found))
	for _, issue := range found {
		issues = append(issues, Issue{
			Severity: issue.Severity, Domain: issue.Domain, Code: issue.Code,
			Subject: issue.Subject, Detail: issue.Detail, Suggestion: issue.Suggestion,
		})
	}
	return issues
}

func (service Service) todoIssues() ([]store.TodoDependencyIssue, []Issue) {
	todos, err := service.loadTodos()
	if err != nil {
		return nil, []Issue{{
			Severity: "warning", Domain: "todo", Code: "todo_read_failed",
			Subject: config.AtmDB, Detail: err.Error(),
			Suggestion: "repair or restore the ATM SQLite database before changing task state",
		}}
	}
	audited := store.AuditTodoDependencies(todos)
	issues := make([]Issue, 0, len(audited))
	for _, issue := range audited {
		issues = append(issues, Issue{
			Severity: "warning", Domain: "todo", Code: issue.Code,
			Subject: issue.TodoID, Detail: issue.Detail, Suggestion: issue.Suggestion,
		})
	}
	return audited, issues
}

// extractionIssues turns "the parser read the file and got nothing out of it"
// into something a person sees.
//
// Every agent owns its own transcript format and changes it whenever it likes.
// When that happens ATM does not error: discovery still finds the files, the
// session row is still created, and the fields the parser looked for are simply
// absent — so history quietly stops growing and spend quietly reads as zero. The
// numbers are the only evidence, and they are only damning in combination:
// indexed sessions with no messages behind them, or an agent that reports tokens
// upstream but none here.
//
// Each check requires an indexed session first. An agent that was never used has
// zero of everything and must not be reported as broken, and each check is gated
// on the parser claiming that capability at all — an upstream that never reported
// tokens is a documented limitation, not a regression.
func extractionIssues(agent string, source Source, counts store.ExtractionCounts) []Issue {
	if source.Files == 0 || counts.Sessions == 0 {
		return nil
	}
	claims := parser.CapabilitiesFor(agent)
	var issues []Issue
	if claims.Messages && counts.Messages == 0 {
		issues = append(issues, Issue{
			Severity: "warning", Domain: "parser", Code: "parser_extracts_no_messages",
			Subject: agent,
			Detail: fmt.Sprintf("%d indexed session(s) from %d discovered file(s) contain no messages",
				counts.Sessions, source.Files),
			Suggestion: "the upstream transcript format has probably changed: upgrade atm, and if it is already current, " +
				"report it with `atm diagnose --bundle` attached",
		})
	}
	if claims.Usage && counts.UsageRows == 0 {
		issues = append(issues, Issue{
			Severity: "warning", Domain: "parser", Code: "parser_extracts_no_usage",
			Subject: agent,
			Detail: fmt.Sprintf("%d indexed session(s) carry no token accounting, so this agent's spend reads as zero",
				counts.Sessions),
			Suggestion: "check whether this agent reports usage at all (`atm doctor` coverage row); if it used to, " +
				"upgrade atm and report it with `atm diagnose --bundle` attached",
		})
	}
	return issues
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
