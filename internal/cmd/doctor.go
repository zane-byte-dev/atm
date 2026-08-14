package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

type doctorSource struct {
	Agent           string `json:"agent"`
	Path            string `json:"path"`
	Exists          bool   `json:"exists"`
	Files           int    `json:"files"`
	IndexedSessions int    `json:"indexed_sessions"`
	// RetainedSessions is the part of IndexedSessions whose transcript is no
	// longer on disk. ATM keeps those on purpose so rotated logs don't erase
	// spend and history, which also means IndexedSessions counts every session
	// ever seen rather than the files discovered now.
	RetainedSessions int    `json:"retained_sessions"`
	Status           string `json:"status"`
}

type doctorIssue struct {
	Severity   string `json:"severity"`
	Domain     string `json:"domain"`
	Code       string `json:"code"`
	Subject    string `json:"subject"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

// doctorReport is everything a doctor run found, before it is rendered. It is
// split out from printing so `atm diagnose` can embed the same findings in a
// support bundle instead of re-deriving them or shelling out to doctor.
type doctorReport struct {
	Sources      []doctorSource              `json:"sources"`
	Coverage     []store.Coverage            `json:"coverage"`
	ModelPricing []store.ModelPricing        `json:"model_pricing"`
	TodoIssues   []store.TodoDependencyIssue `json:"todo_dependency_issues"`
	Issues       []doctorIssue               `json:"issues"`
}

func init() { rootCmd.AddCommand(doctorCmd) }

var doctorCmd = &cobra.Command{Use: "doctor", Short: "Check data sources and request-level coverage", RunE: runDoctor}

func runDoctor(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(config.AtmDB); os.IsNotExist(err) {
		return runDoctorReport(nil)
	}
	return withDB(true, runDoctorReport)
}

// pricingIssues reports the models whose rate ATM guessed, with what that guess
// is carrying. Both cases are grouped into one issue per severity rather than one
// per model: the useful unit is "this much of your reported spend is estimated",
// and a per-model issue list would bury that under names.
func pricingIssues(pricing []store.ModelPricing) []doctorIssue {
	var total, familyCost, defaultCost float64
	var familyModels, defaultModels []string
	for _, p := range pricing {
		total += p.CostUSD
		switch p.Source {
		case store.PricingDefault:
			defaultCost += p.CostUSD
			defaultModels = append(defaultModels, p.Model)
		case store.PricingFamily:
			familyCost += p.CostUSD
			familyModels = append(familyModels, p.Model)
		}
	}
	share := func(cost float64) string {
		if total <= 0 {
			return "0%"
		}
		return fmt.Sprintf("%.0f%%", cost/total*100)
	}
	var issues []doctorIssue
	if len(defaultModels) > 0 {
		issues = append(issues, doctorIssue{
			Severity: "warning", Domain: "pricing", Code: "pricing_default_rate",
			Subject: strings.Join(defaultModels, ", "),
			Detail: fmt.Sprintf("%d model(s) match no rate and no model family, so they are charged at the conservative Opus-tier default: $%.2f, %s of reported spend",
				len(defaultModels), defaultCost, share(defaultCost)),
			Suggestion: "add the real rates to ~/.atm/pricing.json (per million: [input, output, cache_create, cache_read]); the next `atm sync` reprices history onto them",
		})
	}
	if len(familyModels) > 0 {
		issues = append(issues, doctorIssue{
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
func collectionRetentionIssues(db *sql.DB) []doctorIssue {
	days := config.CollectionMessageRetentionDays
	cutoff := store.RetentionCutoff(days, time.Now())
	if cutoff <= 0 {
		return nil
	}
	stats, err := store.CollectionMessageStatsFor(db)
	if err != nil {
		return []doctorIssue{{Severity: "warning", Domain: "collection", Code: "collection_archive_read_failed",
			Subject: config.AtmDB, Detail: err.Error(),
			Suggestion: "repair or restore the ATM SQLite database; synced chat is queried by `atm collect search`"}}
	}
	if stats.Oldest == 0 || stats.Oldest >= cutoff {
		return nil
	}
	return []doctorIssue{{Severity: "warning", Domain: "collection", Code: "collection_messages_past_retention",
		Subject: "collection_messages",
		Detail: fmt.Sprintf("synced chat reaches back to %s, older than the %d-day retention window",
			time.Unix(stats.Oldest, 0).In(config.Loc).Format("2006-01-02"), days),
		Suggestion: "run `atm collect run` or `atm collect history <source>` to trigger a prune, " +
			"or set collection_message_retention_days to 0 to keep chat on purpose"}}
}

// collectionModelIssues reports collection enabled with no credential for the
// text model that classifies chat. Classification happens in the background and
// fails closed, so a missing key shows up as sources that quietly stop producing
// anything rather than as an error anyone sees. Only the credential is checked —
// doctor stays offline and must not spend a model call.
func collectionModelIssues() []doctorIssue {
	if !config.CollectionEnabled || textmodel.Configured() {
		return nil
	}
	return []doctorIssue{{Severity: "warning", Domain: "collection", Code: "collection_model_unavailable",
		Subject: config.TextModelName,
		Detail:  "the built-in text model has no API Key, so nothing can be classified",
		Suggestion: "save one in ATM.app under Settings > Model or with `atm config credential set`, " +
			"then verify it with `atm config test-text-model`"}}
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
func extractionIssues(agent string, source doctorSource, counts store.ExtractionCounts) []doctorIssue {
	if source.Files == 0 || counts.Sessions == 0 {
		return nil
	}
	claims := parser.CapabilitiesFor(agent)
	var issues []doctorIssue
	if claims.Messages && counts.Messages == 0 {
		issues = append(issues, doctorIssue{
			Severity: "warning", Domain: "parser", Code: "parser_extracts_no_messages",
			Subject: agent,
			Detail: fmt.Sprintf("%d indexed session(s) from %d discovered file(s) contain no messages",
				counts.Sessions, source.Files),
			Suggestion: "the upstream transcript format has probably changed: upgrade atm, and if it is already current, " +
				"report it with `atm diagnose --bundle` attached",
		})
	}
	if claims.Usage && counts.UsageRows == 0 {
		issues = append(issues, doctorIssue{
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

func runDoctorReport(db *sql.DB) error {
	report, err := buildDoctorReport(db)
	if err != nil {
		return err
	}
	printDoctorReport(report)
	return nil
}

func buildDoctorReport(db *sql.DB) (doctorReport, error) {
	paths := map[string]string{"claude": config.ClaudeProjects, "codex": config.CodexSessions, "pi": config.PiSessions, "copilot": config.CopilotWorkspaces, "qoder": config.QoderDB, "qodercli": config.QoderCLIProjects, "qoderwork": config.QoderWorkDB, "grokbuild": config.GrokSessions}
	coverage := []store.Coverage{}
	retained := map[string]int{}
	if db != nil {
		var err error
		coverage, err = store.GetCoverage(db)
		if err != nil {
			return doctorReport{}, err
		}
		retained, err = store.GetRetainedSessionCounts(db)
		if err != nil {
			return doctorReport{}, err
		}
	}
	indexed := map[string]int{}
	for _, item := range coverage {
		indexed[item.Agent] = item.Sessions
	}
	extraction := map[string]store.ExtractionCounts{}
	if db != nil {
		var err error
		extraction, err = store.GetExtractionCounts(db)
		if err != nil {
			return doctorReport{}, err
		}
	}
	var sources []doctorSource
	var issues []doctorIssue
	if db == nil {
		issues = append(issues, doctorIssue{Severity: "info", Domain: "database", Code: "session_index_missing", Subject: config.AtmDB, Detail: "the derived session index does not exist yet", Suggestion: "run `atm sync` to build session history and request coverage; source and todo checks below are still valid"})
	}
	for _, a := range parser.All() {
		p := paths[a.Name()]
		_, statErr := os.Stat(p)
		source := doctorSource{Agent: a.Name(), Path: p, Exists: statErr == nil, Files: len(a.Discover()), IndexedSessions: indexed[a.Name()], RetainedSessions: retained[a.Name()]}
		switch {
		case !source.Exists && source.IndexedSessions > 0:
			source.Status = "source_missing_index_retained"
			issues = append(issues, doctorIssue{Severity: "warning", Domain: "source", Code: "source_missing_index_retained", Subject: a.Name(), Detail: fmt.Sprintf("source path is missing while %d indexed sessions remain", source.IndexedSessions), Suggestion: "restore or reconfigure the source path; keep the index for history, or run sync after intentionally removing the source"})
		case !source.Exists:
			source.Status = "missing"
			issues = append(issues, doctorIssue{Severity: "warning", Domain: "source", Code: "source_missing", Subject: a.Name(), Detail: "configured source path does not exist", Suggestion: "update the path in ~/.atm/config.json or install/use this agent before syncing"})
		case source.Files == 0:
			source.Status = "empty"
			issues = append(issues, doctorIssue{Severity: "info", Domain: "source", Code: "source_empty", Subject: a.Name(), Detail: "source path exists but no supported session files were discovered", Suggestion: "verify the configured path and upstream client data format"})
		default:
			source.Status = "ok"
		}
		if db != nil {
			issues = append(issues, extractionIssues(a.Name(), source, extraction[a.Name()])...)
		}
		sources = append(sources, source)
	}
	for _, item := range coverage {
		if item.CoverageStatus == "inconsistent" {
			issues = append(issues, doctorIssue{Severity: "warning", Domain: "usage", Code: "request_coverage_inconsistent", Subject: item.Agent, Detail: fmt.Sprintf("detailed requests (%d) exceed reported requests (%d) by %d", item.DetailedRequests, item.ReportedRequests, item.DetailedExcess), Suggestion: "resync the agent and inspect parser request-count semantics; coverage is capped at 100% until reconciled"})
		} else if item.CoverageStatus == "partial" {
			issues = append(issues, doctorIssue{Severity: "info", Domain: "usage", Code: "request_coverage_partial", Subject: item.Agent, Detail: fmt.Sprintf("request detail coverage is %.1f%%", item.CoveragePercent), Suggestion: "use aggregate usage for totals and request detail only where coverage is available"})
		}
		if item.UnknownModels > 0 {
			issues = append(issues, doctorIssue{Severity: "warning", Domain: "usage", Code: "unknown_models", Subject: item.Agent, Detail: fmt.Sprintf("%d request events have no model", item.UnknownModels), Suggestion: "update the parser sample or configure model metadata before relying on per-model cost"})
		}
	}
	var pricing []store.ModelPricing
	if db != nil {
		var err error
		pricing, err = store.GetModelPricing(db)
		if err != nil {
			return doctorReport{}, err
		}
		issues = append(issues, pricingIssues(pricing)...)
	}
	if db != nil {
		issues = append(issues, collectionRetentionIssues(db)...)
	}
	issues = append(issues, collectionModelIssues()...)
	var todoIssues []store.TodoDependencyIssue
	if todos, loadErr := store.LoadTodosReadOnly(); loadErr == nil {
		todoIssues = store.AuditTodoDependencies(todos)
		for _, issue := range todoIssues {
			issues = append(issues, doctorIssue{Severity: "warning", Domain: "todo", Code: issue.Code, Subject: issue.TodoID, Detail: issue.Detail, Suggestion: issue.Suggestion})
		}
	} else {
		issues = append(issues, doctorIssue{Severity: "warning", Domain: "todo", Code: "todo_read_failed", Subject: config.AtmDB, Detail: loadErr.Error(), Suggestion: "repair or restore the ATM SQLite database before changing task state"})
	}
	return doctorReport{
		Sources:      sources,
		Coverage:     coverage,
		ModelPricing: pricing,
		TodoIssues:   todoIssues,
		Issues:       issues,
	}, nil
}

func printDoctorReport(report doctorReport) {
	if jsonOutput {
		// Kept as an explicit map rather than marshalling doctorReport so the
		// published shape — including the summary the struct does not carry — does
		// not silently change when a field is added to the struct.
		output.JSON(map[string]any{"sources": report.Sources, "coverage": report.Coverage, "model_pricing": report.ModelPricing, "todo_dependency_issues": report.TodoIssues, "issues": report.Issues, "summary": map[string]int{"issues": len(report.Issues)}})
		return
	}
	fmt.Println("ATM Doctor")
	fmt.Println(strings.Repeat("=", 72))
	fmt.Println("\nData sources")
	for _, s := range report.Sources {
		fmt.Printf("  %-9s %-29s files=%-5d indexed=%-5d retained=%-5d %s\n", s.Agent, s.Status, s.Files, s.IndexedSessions, s.RetainedSessions, s.Path)
	}
	fmt.Println("\nRequest detail coverage")
	for _, c := range report.Coverage {
		fmt.Printf("  %-9s %-12s sessions=%-5d detailed=%-6d reported=%-6d coverage=%6.1f%% unknown_model=%-4d timed=%6.1f%%\n", c.Agent, c.CoverageStatus, c.Sessions, c.DetailedRequests, c.ReportedRequests, c.CoveragePercent, c.UnknownModels, c.TimedPercent)
	}
	fmt.Println("  timed = share of requests whose speed could be measured from the transcript")
	if len(report.ModelPricing) > 0 {
		fmt.Println("\nCost rates")
		for _, p := range report.ModelPricing {
			fmt.Printf("  %-30s %-8s cost=%10.2f requests=%-7d\n", p.Model, p.Source, p.CostUSD, p.Requests)
		}
		fmt.Println("  exact = the model's own rate | family = its family's rate | default = no match, Opus-tier upper bound")
		fmt.Println("  family and default are estimates; pin exact rates in ~/.atm/pricing.json")
	}
	fmt.Printf("\nIssues (%d)\n", len(report.Issues))
	if len(report.Issues) == 0 {
		fmt.Println("  none")
	}
	for _, issue := range report.Issues {
		fmt.Printf("  %-7s %-10s %-32s %s\n", issue.Severity, issue.Domain, issue.Code, issue.Subject)
		fmt.Printf("           %s\n", issue.Detail)
		fmt.Printf("           next: %s\n", issue.Suggestion)
	}
}
