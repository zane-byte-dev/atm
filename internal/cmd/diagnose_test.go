package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
)

// withFakeHome points config.Home at a temporary directory as well as the data
// dir, so redaction has something to redact that is not the real user's path.
func withFakeHome(t *testing.T) string {
	t.Helper()
	oldHome, oldDir, oldDB, oldConfig := config.Home, config.AtmDir, config.AtmDB, config.ConfigPath
	home := t.TempDir()
	config.Home = home
	config.AtmDir = filepath.Join(home, ".atm")
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	config.ConfigPath = filepath.Join(config.AtmDir, "config.json")
	if err := os.MkdirAll(config.AtmDir, 0700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Cleanup(func() {
		config.Home, config.AtmDir, config.AtmDB, config.ConfigPath = oldHome, oldDir, oldDB, oldConfig
	})
	return home
}

func writeDiagnoseBundle(t *testing.T, target string) diagnoseBundleReport {
	t.Helper()
	oldBundle, oldOutput := diagnoseBundleFlag, diagnoseOutputFlag
	diagnoseBundleFlag, diagnoseOutputFlag = true, target
	t.Cleanup(func() { diagnoseBundleFlag, diagnoseOutputFlag = oldBundle, oldOutput })
	captureStdout(t, func() {
		if err := runDiagnose(diagnoseCmd, nil); err != nil {
			t.Fatalf("diagnose: %v", err)
		}
	})
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var report diagnoseBundleReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	return report
}

// TestDiagnoseBundleCollectsRequiredFields checks the acceptance list: versions,
// schema versions, doctor findings, data source presence and the last sync error
// all have to be in there, or the bundle does not save a round trip with the
// person reporting the bug.
func TestDiagnoseBundleCollectsRequiredFields(t *testing.T) {
	withFakeHome(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()

	target := filepath.Join(t.TempDir(), "bundle.json")
	report := writeDiagnoseBundle(t, target)

	if report.GeneratedAt == "" {
		t.Error("generated_at is empty")
	}
	if report.ATM.SchemaVersion != store.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", report.ATM.SchemaVersion, store.SchemaVersion)
	}
	if report.ATM.DatabaseSchemaVersion != store.SchemaVersion {
		t.Errorf("database_schema_version = %d, want %d", report.ATM.DatabaseSchemaVersion, store.SchemaVersion)
	}
	if !report.ATM.DatabaseExists || report.ATM.DatabaseBytes == 0 {
		t.Errorf("database presence not reported: %+v", report.ATM)
	}
	if report.Platform.OS == "" || report.Platform.Arch == "" || report.Platform.GoVersion == "" {
		t.Errorf("platform incomplete: %+v", report.Platform)
	}
	if report.App.DashboardV != contract.DashboardSchemaVersion {
		t.Errorf("dashboard schema version = %d, want %d", report.App.DashboardV, contract.DashboardSchemaVersion)
	}
	if len(report.App.SearchedPaths) == 0 {
		t.Error("app searched_paths is empty, so 'not found' cannot be interpreted")
	}
	if report.Sync.Sync.Scope == "" {
		t.Errorf("sync state not reported: %+v", report.Sync)
	}
	// Every agent must appear with its path and existence, which is what makes a
	// misconfigured source visible without asking for more information.
	if len(report.Doctor.Sources) == 0 {
		t.Fatal("doctor sources missing")
	}
	for _, source := range report.Doctor.Sources {
		if source.Agent == "" || source.Path == "" {
			t.Errorf("source without agent or path: %+v", source)
		}
	}
	if len(report.DataDir) == 0 {
		t.Error("data_dir is empty even though the database was created in it")
	}
	if len(report.Redaction) == 0 {
		t.Error("redaction notes are empty, so a reader cannot tell what was withheld")
	}
}

// TestDiagnoseRedactsHomePaths is the privacy boundary: a file meant to be
// attached to a public bug report must not carry the reporter's username.
func TestDiagnoseRedactsHomePaths(t *testing.T) {
	home := withFakeHome(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()

	target := filepath.Join(t.TempDir(), "bundle.json")
	writeDiagnoseBundle(t, target)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if strings.Contains(string(data), home) {
		t.Fatalf("bundle leaks the home directory %q", home)
	}
	if !strings.Contains(string(data), "~/.atm") {
		t.Error("paths were not rewritten to ~, so the bundle lost its diagnostic value")
	}
}

// TestDiagnoseRedactionLeavesUnsafeHomeAlone guards the substitution itself: an
// empty or root home would match everywhere and shred the document.
func TestDiagnoseRedactionLeavesUnsafeHomeAlone(t *testing.T) {
	withFakeHome(t)
	for _, home := range []string{"", "/"} {
		config.Home = home
		data, err := redactedJSON(diagnoseBundleReport{ATM: diagnoseATM{DatabasePath: "/var/db/atm.db"}})
		if err != nil {
			t.Fatalf("redact with home %q: %v", home, err)
		}
		if !strings.Contains(string(data), "/var/db/atm.db") {
			t.Fatalf("home %q mangled unrelated paths: %s", home, data)
		}
	}
}

// TestDiagnoseExcludesRecordContent checks that nothing from the second brain
// rides along — not knowledge titles, which are file names, and not todo text.
func TestDiagnoseExcludesRecordContent(t *testing.T) {
	withFakeHome(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO todos (id,position,title,description,priority,status,created)
		VALUES ('t1',1,'SECRETTODOTITLE','SECRETTODOBODY','P1','open','2026-08-05')`); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	db.Close()

	secretDoc := filepath.Join(config.AtmDir, "knowledge", "personal", "SECRETKNOWLEDGETITLE.md")
	if err := os.MkdirAll(filepath.Dir(secretDoc), 0700); err != nil {
		t.Fatalf("mkdir knowledge: %v", err)
	}
	if err := os.WriteFile(secretDoc, []byte("SECRETKNOWLEDGEBODY\n"), 0600); err != nil {
		t.Fatalf("write knowledge: %v", err)
	}
	if err := config.SaveTextModelAPIKey("SECRETDEEPSEEKKEY"); err != nil {
		t.Fatalf("write credential: %v", err)
	}

	target := filepath.Join(t.TempDir(), "bundle.json")
	writeDiagnoseBundle(t, target)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	for _, secret := range []string{
		"SECRETTODOTITLE", "SECRETTODOBODY",
		"SECRETKNOWLEDGETITLE", "SECRETKNOWLEDGEBODY",
		"SECRETDEEPSEEKKEY", config.CredentialsFileName,
	} {
		if strings.Contains(string(data), secret) {
			t.Errorf("bundle leaked %s", secret)
		}
	}
}

// The bundle has to carry the log tails, or an intermittent fault — failing once
// a day, fine otherwise — reads as no fault at all.
func TestDiagnoseBundleCarriesLogTails(t *testing.T) {
	withFakeHome(t)
	logging.Failure("command_failed", "atm sync", errors.New("SEEDED_LOG_FAILURE"), nil)

	target := filepath.Join(t.TempDir(), "bundle.json")
	report := writeDiagnoseBundle(t, target)

	cli, ok := report.Logs["cli"]
	if !ok {
		t.Fatal("bundle has no cli log section")
	}
	if !cli.Exists {
		t.Error("cli log reported as absent after a failure was logged")
	}
	if len(cli.Lines) == 0 || !strings.Contains(strings.Join(cli.Lines, "\n"), "SEEDED_LOG_FAILURE") {
		t.Errorf("logged failure did not reach the bundle: %v", cli.Lines)
	}
	// The app section must be present even with no App installed, so "no app log"
	// is distinguishable from "we forgot to look".
	if _, ok := report.Logs["app"]; !ok {
		t.Error("bundle has no app log section")
	}
}

// The log is attached to public bug reports, and `atm todo add "<title>"` and
// `atm knowledge import <path>` put content directly in argv. Recording the
// subcommand without its arguments is what keeps that out.
func TestCommandLoggingExcludesArguments(t *testing.T) {
	withFakeHome(t)
	oldArgs := os.Args
	os.Args = []string{"atm", "todo", "add", "SECRET_TODO_TITLE_IN_ARGV"}
	t.Cleanup(func() { os.Args = oldArgs })

	logging.Failure("command_failed", failedCommandPath(), errors.New("boom"), nil)

	lines, err := logging.Tail(logging.Path(), 0)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "SECRET_TODO_TITLE_IN_ARGV") {
		t.Fatalf("log captured a command argument: %s", joined)
	}
	if !strings.Contains(joined, "atm todo add") {
		t.Errorf("log lost the command path, so the failure is not locatable: %s", joined)
	}
}

func TestDiagnoseBundleRefusesToOverwrite(t *testing.T) {
	withFakeHome(t)
	target := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(target, []byte("previous bundle"), 0600); err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	oldBundle, oldOutput := diagnoseBundleFlag, diagnoseOutputFlag
	diagnoseBundleFlag, diagnoseOutputFlag = true, target
	t.Cleanup(func() { diagnoseBundleFlag, diagnoseOutputFlag = oldBundle, oldOutput })

	if err := runDiagnose(diagnoseCmd, nil); err == nil {
		t.Fatal("expected diagnose to refuse an existing bundle path")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if string(data) != "previous bundle" {
		t.Fatal("diagnose overwrote an existing file")
	}
}

// TestDiagnoseWithoutDatabase covers the state a new or broken install is in:
// the bundle still has to be produced, because that is when it is needed.
func TestDiagnoseWithoutDatabase(t *testing.T) {
	withFakeHome(t)
	target := filepath.Join(t.TempDir(), "bundle.json")
	report := writeDiagnoseBundle(t, target)
	if report.ATM.DatabaseExists {
		t.Error("database reported as present when none exists")
	}
	if len(report.Doctor.Sources) == 0 {
		t.Error("source checks were skipped along with the database; they are independent")
	}
}

func TestPlistStringReadsVersionsAndToleratesBinary(t *testing.T) {
	xml := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>ATM</string>
	<key>CFBundleShortVersionString</key>
	<string>1.2.3</string>
	<key>CFBundleVersion</key>
	<string>42</string>
</dict>
</plist>`)
	if got := plistString(xml, "CFBundleShortVersionString"); got != "1.2.3" {
		t.Errorf("short version = %q, want 1.2.3", got)
	}
	if got := plistString(xml, "CFBundleVersion"); got != "42" {
		t.Errorf("bundle version = %q, want 42", got)
	}
	if got := plistString(xml, "CFBundleAbsent"); got != "" {
		t.Errorf("missing key returned %q", got)
	}
	if got := plistString([]byte("bplist00\x00\x01\x02"), "CFBundleVersion"); got != "" {
		t.Errorf("binary plist returned %q, want empty", got)
	}
}

func TestInspectATMAppReportsWhereItLooked(t *testing.T) {
	withFakeHome(t)
	appDir := filepath.Join(t.TempDir(), "ATM.app", "Contents")
	if err := os.MkdirAll(appDir, 0700); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	plist := `<plist version="1.0"><dict>
	<key>CFBundleShortVersionString</key><string>9.9.9</string>
	<key>CFBundleVersion</key><string>77</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(appDir, "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	t.Setenv("ATM_APP_PATH", filepath.Dir(appDir))

	app := inspectATMApp()
	if !app.Found {
		t.Fatalf("app not found via ATM_APP_PATH: %+v", app)
	}
	if app.ShortVersion != "9.9.9" || app.BundleVersion != "77" {
		t.Errorf("versions = %q/%q, want 9.9.9/77", app.ShortVersion, app.BundleVersion)
	}
	if len(app.SearchedPaths) == 0 {
		t.Error("searched paths not reported")
	}
}
