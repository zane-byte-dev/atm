package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
	syncapp "github.com/zane-byte-dev/atm/internal/sync"
)

const diagnoseTestVersion = "1.2.3-test"

func diagnoseTestCall() application.Call {
	return application.Call{
		RequestID: "diagnose-service-test",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginCLI,
		},
	}
}

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

func diagnoseService() Service {
	return NewService(ServiceOptions{
		Version: diagnoseTestVersion,
		Now:     func() time.Time { return time.Date(2026, 8, 21, 15, 4, 5, 0, time.UTC) },
	})
}

// writeBundle produces the file the way the command does and hands back what it
// contains, so these assertions read the artifact rather than the struct.
func writeBundle(t *testing.T, service Service, target string) Report {
	t.Helper()
	result, err := service.WriteBundle(context.Background(), diagnoseTestCall(), BundleInput{Path: target})
	if err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if result.Path != target {
		t.Fatalf("bundle path = %q, want %q", result.Path, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if result.Bytes != len(data) {
		t.Errorf("reported %d bytes, file has %d", result.Bytes, len(data))
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	return report
}

// The acceptance list: versions, schema versions, doctor findings, data source
// presence and the last sync error all have to be in there, or the bundle does not
// save a round trip with the person reporting the bug.
func TestBundleCollectsRequiredFields(t *testing.T) {
	withFakeHome(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()

	report := writeBundle(t, diagnoseService(), filepath.Join(t.TempDir(), "bundle.json"))

	if report.ATM.Version != diagnoseTestVersion {
		t.Errorf("version = %q, want the injected build version", report.ATM.Version)
	}
	if report.ATM.SchemaVersion != store.SchemaVersion {
		t.Errorf("schema version = %d, want %d", report.ATM.SchemaVersion, store.SchemaVersion)
	}
	if !report.ATM.DatabaseExists || report.ATM.DatabaseSchemaVersion != store.SchemaVersion {
		t.Errorf("database not described: %+v", report.ATM)
	}
	if report.Platform.OS == "" || report.Platform.GoVersion == "" {
		t.Errorf("platform not described: %+v", report.Platform)
	}
	if len(report.Doctor.Sources) == 0 {
		t.Error("doctor findings are missing; the bundle exists to carry them")
	}
	if report.Sync.Sync.Scope != store.SyncScopeAll {
		t.Errorf("sync scope = %q, want %q", report.Sync.Sync.Scope, store.SyncScopeAll)
	}
	if len(report.Redaction) == 0 {
		t.Error("the bundle does not say what was redacted, so a reader cannot tell what is missing")
	}
	if report.GeneratedAt != "2026-08-21T15:04:05Z" {
		t.Errorf("generated_at = %q, not the injected clock", report.GeneratedAt)
	}
}

func TestBundleRedactsHomePaths(t *testing.T) {
	home := withFakeHome(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()

	target := filepath.Join(t.TempDir(), "bundle.json")
	writeBundle(t, diagnoseService(), target)
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

// Guards the substitution itself: an empty or root home would match everywhere
// and shred the document.
func TestRedactionLeavesUnsafeHomeAlone(t *testing.T) {
	withFakeHome(t)
	service := diagnoseService()
	for _, home := range []string{"", "/"} {
		config.Home = home
		data, err := service.RedactedJSON(Report{ATM: ATM{DatabasePath: "/var/db/atm.db"}})
		if err != nil {
			t.Fatalf("redact with home %q: %v", home, err)
		}
		if !strings.Contains(string(data), "/var/db/atm.db") {
			t.Fatalf("home %q mangled unrelated paths: %s", home, data)
		}
	}
}

// Nothing from the second brain rides along — not knowledge titles, which are
// file names, and not todo text.
func TestBundleExcludesRecordContent(t *testing.T) {
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
	writeBundle(t, diagnoseService(), target)
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
func TestBundleCarriesLogTails(t *testing.T) {
	withFakeHome(t)
	logging.Failure("command_failed", "atm sync", errors.New("SEEDED_LOG_FAILURE"), nil)

	report := writeBundle(t, diagnoseService(), filepath.Join(t.TempDir(), "bundle.json"))

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
}

func TestWriteBundleRefusesToOverwrite(t *testing.T) {
	withFakeHome(t)
	target := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(target, []byte("previous bundle"), 0600); err != nil {
		t.Fatalf("seed bundle: %v", err)
	}

	_, err := diagnoseService().WriteBundle(context.Background(), diagnoseTestCall(), BundleInput{Path: target})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeConflict {
		t.Fatalf("err = %v, want conflict", err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read bundle: %v", readErr)
	}
	if string(data) != "previous bundle" {
		t.Fatal("an existing file was overwritten")
	}
}

// A bundle carries log tails and paths from the reporter's machine, so it must not
// land world-readable in a directory they may then share wholesale.
func TestWriteBundleIsOwnerReadableOnly(t *testing.T) {
	withFakeHome(t)
	target := filepath.Join(t.TempDir(), "bundle.json")
	writeBundle(t, diagnoseService(), target)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if mode := info.Mode().Perm(); mode != bundleMode {
		t.Errorf("bundle mode = %04o, want %04o", mode, bundleMode)
	}
}

// The default name is timestamped so a repeat run does not collide with itself.
func TestDefaultBundleNameIsTimestamped(t *testing.T) {
	withFakeHome(t)
	directory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	result, err := diagnoseService().WriteBundle(context.Background(), diagnoseTestCall(), BundleInput{})
	if err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if !strings.HasPrefix(result.Path, "atm-diagnose-") || !strings.HasSuffix(result.Path, ".json") {
		t.Fatalf("default bundle name = %q", result.Path)
	}
	if _, err := os.Stat(filepath.Join(directory, result.Path)); err != nil {
		t.Fatalf("default bundle was not written: %v", err)
	}
}

// Covers the state a new or broken install is in: the bundle still has to be
// produced, because that is when it is needed.
func TestBundleWithoutDatabase(t *testing.T) {
	withFakeHome(t)
	report := writeBundle(t, diagnoseService(), filepath.Join(t.TempDir(), "bundle.json"))
	if report.ATM.DatabaseExists {
		t.Error("database reported as present when none exists")
	}
	if len(report.Doctor.Sources) == 0 {
		t.Error("source checks were skipped along with the database; they are independent")
	}
}

type failingChecker struct{ err error }

func (checker failingChecker) Check(
	context.Context, application.Call, doctorapp.Input,
) (doctorapp.Report, error) {
	return doctorapp.Report{}, checker.err
}

type failingFreshness struct{ err error }

func (reader failingFreshness) Status(
	context.Context, application.Call, syncapp.StatusInput,
) (syncapp.StatusReport, error) {
	return syncapp.StatusReport{}, reader.err
}

// A self-check or freshness read that fails is exactly the fault this bundle is
// for, so each becomes a field rather than taking the bundle down.
func TestReportDegradesWhenItsSourcesFail(t *testing.T) {
	withFakeHome(t)
	service := NewService(ServiceOptions{
		Version:   diagnoseTestVersion,
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		Doctor:    failingChecker{err: errors.New("index is corrupt")},
		Freshness: failingFreshness{err: errors.New("index is unreadable")},
	})

	report, err := service.Report(context.Background(), diagnoseTestCall(), Input{})
	if err != nil {
		t.Fatalf("report failed instead of degrading: %v", err)
	}
	if report.Sync.Sync.Status != "unreadable" ||
		!strings.Contains(report.Sync.Sync.LastError, "index is unreadable") {
		t.Errorf("freshness failure did not become a field: %+v", report.Sync.Sync)
	}
	if len(report.Doctor.Issues) != 1 || report.Doctor.Issues[0].Code != "doctor_failed" {
		t.Fatalf("self-check failure did not become a finding: %+v", report.Doctor.Issues)
	}
	if !strings.Contains(report.Doctor.Issues[0].Detail, "index is corrupt") {
		t.Errorf("finding does not carry the cause: %q", report.Doctor.Issues[0].Detail)
	}
}
