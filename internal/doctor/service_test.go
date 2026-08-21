package doctor

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

func doctorTestCall() application.Call {
	return application.Call{
		RequestID: "doctor-service-test",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginCLI,
		},
	}
}

// withTempAtmDir points the data root at an empty temporary directory, so the
// check reports on nothing rather than on the developer's own install.
func withTempAtmDir(t *testing.T) string {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
	return dir
}

// noGuard stands in for a machine with no gate installed, so a finding in these
// tests is always about the thing under test.
type noGuard struct{}

func (noGuard) Diagnose(context.Context, application.Call) ([]guard.DiagnosticIssue, error) {
	return nil, nil
}

type failingGuard struct{ err error }

func (g failingGuard) Diagnose(context.Context, application.Call) ([]guard.DiagnosticIssue, error) {
	return nil, g.err
}

type fixedGuard struct{ issues []guard.DiagnosticIssue }

func (g fixedGuard) Diagnose(context.Context, application.Call) ([]guard.DiagnosticIssue, error) {
	return g.issues, nil
}

func emptyTodos() (*store.TodoFile, error) { return &store.TodoFile{}, nil }

func offlineService(t *testing.T, options ServiceOptions) Service {
	t.Helper()
	if options.Guard == nil {
		options.Guard = noGuard{}
	}
	if options.LoadTodos == nil {
		options.LoadTodos = emptyTodos
	}
	if options.TextModelReady == nil {
		options.TextModelReady = func() bool { return true }
	}
	return NewService(options)
}

func check(t *testing.T, service Service) Report {
	t.Helper()
	report, err := service.Check(context.Background(), doctorTestCall(), Input{})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return report
}

func findingByCode(report Report, code string) (Issue, bool) {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return issue, true
		}
	}
	return Issue{}, false
}

// A fresh install has no index. That is the state this command most needs to be
// usable in, so it reports the gap and keeps going rather than failing.
func TestCheckReportsAMissingIndexWithoutFailing(t *testing.T) {
	withTempAtmDir(t)
	report := check(t, offlineService(t, ServiceOptions{}))

	found, ok := findingByCode(report, "session_index_missing")
	if !ok {
		t.Fatalf("missing index was not reported: %+v", report.Issues)
	}
	if found.Severity != "info" {
		t.Errorf("severity = %q, want info: an index nobody has built yet is not a fault", found.Severity)
	}
	if !strings.Contains(found.Suggestion, "atm sync") {
		t.Errorf("suggestion does not say how to build it: %q", found.Suggestion)
	}
	// Empty rather than null so a consumer decoding this does not have to
	// distinguish "no coverage yet" from "field missing".
	if report.Coverage == nil {
		t.Error("coverage is null with no index; want an empty list")
	}
	if report.Summary.Issues != len(report.Issues) {
		t.Errorf("summary %d disagrees with %d issues", report.Summary.Issues, len(report.Issues))
	}
}

// The check must never build or migrate the index it reports on: doing so would
// make running a diagnostic change the thing being diagnosed.
func TestCheckDoesNotOpenTheIndexWhenItDoesNotExist(t *testing.T) {
	withTempAtmDir(t)
	opened := 0
	check(t, offlineService(t, ServiceOptions{
		OpenRead: func() (*sql.DB, error) { opened++; return nil, errors.New("unreachable") },
	}))
	if opened != 0 {
		t.Fatalf("opened the index %d times when the file is absent", opened)
	}
}

// A guard that cannot be inspected must not take the report down with it: every
// other finding here is still worth showing.
func TestCheckKeepsGoingWhenTheGateCannotBeInspected(t *testing.T) {
	withTempAtmDir(t)
	report := check(t, offlineService(t, ServiceOptions{
		Guard: failingGuard{err: errors.New("guard store unreadable")},
	}))
	if _, ok := findingByCode(report, "session_index_missing"); !ok {
		t.Fatalf("a failing gate suppressed the rest of the report: %+v", report.Issues)
	}
	for _, issue := range report.Issues {
		if issue.Domain == "guard" {
			t.Errorf("reported a gate finding from a failed inspection: %+v", issue)
		}
	}
}

// Guard owns the wording of its own findings; the check only carries them.
func TestCheckCarriesGateFindingsThrough(t *testing.T) {
	withTempAtmDir(t)
	report := check(t, offlineService(t, ServiceOptions{Guard: fixedGuard{issues: []guard.DiagnosticIssue{{
		Severity: "warning", Domain: "guard", Code: "guard_shim_clobbered",
		Subject: "/usr/local/bin/dws", Detail: "闸门已经不在位了", Suggestion: "atm guard install dws",
	}}}}))
	found, ok := findingByCode(report, "guard_shim_clobbered")
	if !ok {
		t.Fatalf("gate finding was dropped: %+v", report.Issues)
	}
	if found.Detail != "闸门已经不在位了" || found.Subject != "/usr/local/bin/dws" {
		t.Errorf("gate finding was rewritten: %+v", found)
	}
}

// A todo store that cannot be read is reported rather than silently audited as
// empty: "no dependency problems" and "could not look" are different answers.
func TestCheckReportsAnUnreadableTodoStore(t *testing.T) {
	withTempAtmDir(t)
	report := check(t, offlineService(t, ServiceOptions{
		LoadTodos: func() (*store.TodoFile, error) { return nil, errors.New("database is locked") },
	}))
	found, ok := findingByCode(report, "todo_read_failed")
	if !ok {
		t.Fatalf("unreadable todo store was not reported: %+v", report.Issues)
	}
	if !strings.Contains(found.Detail, "database is locked") {
		t.Errorf("detail does not carry the cause: %q", found.Detail)
	}
}

// The useful unit is "this much of your reported spend is estimated". A per-model
// issue list would bury that under names, so the share is what these assert.
func TestPricingIssuesReportTheShareOfSpendThatWasGuessed(t *testing.T) {
	issues := pricingIssues([]store.ModelPricing{
		{Model: "exact-1", Source: store.PricingExact, CostUSD: 50},
		{Model: "guessy-1", Source: store.PricingDefault, CostUSD: 30},
		{Model: "family-1", Source: store.PricingFamily, CostUSD: 20},
	})
	byCode := map[string]Issue{}
	for _, issue := range issues {
		byCode[issue.Code] = issue
	}
	def, ok := byCode["pricing_default_rate"]
	if !ok {
		t.Fatalf("no default-rate issue: %+v", issues)
	}
	if def.Severity != "warning" || !strings.Contains(def.Detail, "30%") || !strings.Contains(def.Detail, "$30.00") {
		t.Errorf("default-rate issue does not carry cost and share: %+v", def)
	}
	family, ok := byCode["pricing_family_rate"]
	if !ok {
		t.Fatalf("no family-rate issue: %+v", issues)
	}
	if family.Severity != "info" || !strings.Contains(family.Detail, "20%") {
		t.Errorf("family-rate issue does not carry its share: %+v", family)
	}
}

// Every rate exact means nothing to report. A legend nobody needs is noise.
func TestPricingIssuesStayQuietWhenEveryRateIsExact(t *testing.T) {
	if issues := pricingIssues([]store.ModelPricing{
		{Model: "exact-1", Source: store.PricingExact, CostUSD: 10},
	}); len(issues) != 0 {
		t.Fatalf("reported an issue with no guessed rates: %+v", issues)
	}
}

// Pruning runs after every sync, so chat older than the window means the prune
// has been failing quietly — the one thing about this archive worth diagnosing.
func TestCollectionRetentionIssuesReportAStuckPrune(t *testing.T) {
	withTempAtmDir(t)
	oldRetention := config.CollectionMessageRetentionDays
	t.Cleanup(func() { config.CollectionMessageRetentionDays = oldRetention })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ancient := time.Now().In(config.Loc).AddDate(0, 0, -200).Unix()
	if _, err := store.PutCollectionMessages(db, []store.CollectionMessage{{
		Connector: "test", ConversationID: "cid-1", MessageID: "m1",
		CreatedAt: ancient, Content: "两百天前",
	}}); err != nil {
		t.Fatal(err)
	}
	service := offlineService(t, ServiceOptions{})

	config.CollectionMessageRetentionDays = 90
	issues := service.collectionRetentionIssues(db)
	if len(issues) != 1 || issues[0].Code != "collection_messages_past_retention" {
		t.Fatalf("stuck prune was not reported: %+v", issues)
	}
	// Keeping chat on purpose is not a problem to report.
	config.CollectionMessageRetentionDays = 0
	if issues := service.collectionRetentionIssues(db); len(issues) != 0 {
		t.Fatalf("retention 0 reported an issue: %+v", issues)
	}
}

// Classification fails closed in the background, so a missing credential shows up
// as sources that quietly produce nothing rather than as an error anyone sees.
func TestCollectionModelIssueFiresOnlyWhenCollectionIsOnAndTheKeyIsMissing(t *testing.T) {
	oldEnabled := config.CollectionEnabled
	t.Cleanup(func() { config.CollectionEnabled = oldEnabled })

	config.CollectionEnabled = true
	service := offlineService(t, ServiceOptions{TextModelReady: func() bool { return false }})
	if issues := service.collectionModelIssues(); len(issues) != 1 ||
		issues[0].Code != "collection_model_unavailable" {
		t.Fatalf("missing credential was not reported: %+v", issues)
	}

	configured := offlineService(t, ServiceOptions{TextModelReady: func() bool { return true }})
	if issues := configured.collectionModelIssues(); len(issues) != 0 {
		t.Fatalf("reported a missing key that is present: %+v", issues)
	}

	config.CollectionEnabled = false
	if issues := service.collectionModelIssues(); len(issues) != 0 {
		t.Fatalf("reported a classifier nobody asked to run: %+v", issues)
	}
}
