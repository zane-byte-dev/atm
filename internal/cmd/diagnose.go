package cmd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	diagnoseCmd.Flags().BoolVar(&diagnoseBundleFlag, "bundle", false, "write the report to a file that can be attached to a bug report")
	diagnoseCmd.Flags().StringVarP(&diagnoseOutputFlag, "output", "o", "", "bundle path (default: ./atm-diagnose-<timestamp>.json)")
	rootCmd.AddCommand(diagnoseCmd)
}

var (
	diagnoseBundleFlag bool
	diagnoseOutputFlag string
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Collect the facts needed to debug an ATM problem",
	Long: `Collect the facts needed to debug an ATM problem.

ATM has no telemetry, so nothing about a broken install reaches anyone unless a
person sends it. ` + "`atm diagnose --bundle`" + ` writes a single file holding the
versions, schema versions, doctor findings, data source presence and last sync
error, which can be attached to a bug report as-is.

It reads local state only and never uploads anything. Session text, todo, memory
and knowledge content are never included, and paths under your home directory
are rewritten to ~ so the file does not carry your username.

For the full data-source and coverage tables, use ` + "`atm doctor`" + `.`,
	Args: cobra.NoArgs,
	RunE: runDiagnose,
}

type diagnoseATM struct {
	Version string `json:"version"`
	// SchemaVersion is what this build expects; DatabaseSchemaVersion is what the
	// file on disk actually holds. A mismatch between them is the whole reason
	// both are reported.
	SchemaVersion         int    `json:"schema_version"`
	DatabaseSchemaVersion int    `json:"database_schema_version"`
	DatabasePath          string `json:"database_path"`
	DatabaseExists        bool   `json:"database_exists"`
	DatabaseBytes         int64  `json:"database_bytes"`
	DatabaseError         string `json:"database_error,omitempty"`
	DataDir               string `json:"data_dir"`
	ConfigExists          bool   `json:"config_exists"`
}

type diagnosePlatform struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
	CPUs      int    `json:"cpus"`
}

type diagnoseApp struct {
	Found         bool     `json:"found"`
	Path          string   `json:"path,omitempty"`
	ShortVersion  string   `json:"short_version,omitempty"`
	BundleVersion string   `json:"bundle_version,omitempty"`
	DashboardV    int      `json:"cli_dashboard_schema_version"`
	InspectError  string   `json:"inspect_error,omitempty"`
	SearchedPaths []string `json:"searched_paths"`
}

// diagnoseDataEntry describes one top-level entry under ~/.atm by shape only.
// Names are never recursed into: a knowledge file's name is its title, and a
// support bundle has no business carrying those.
type diagnoseDataEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Bytes   int64  `json:"bytes,omitempty"`
	Entries int    `json:"entries,omitempty"`
}

// diagnoseLog is the tail of one log file. Without this the bundle could only
// describe the present, so an intermittent fault — failing once a day, fine
// otherwise — looked identical to no fault at all.
type diagnoseLog struct {
	Path   string   `json:"path"`
	Exists bool     `json:"exists"`
	Lines  []string `json:"lines"`
	// Truncated says the tail hit its cap, so what is here is the recent end of a
	// longer history rather than all of it.
	Truncated bool `json:"truncated"`
}

type diagnoseBundleReport struct {
	GeneratedAt string                 `json:"generated_at"`
	ATM         diagnoseATM            `json:"atm"`
	Platform    diagnosePlatform       `json:"platform"`
	App         diagnoseApp            `json:"app"`
	Sync        syncStatusReport       `json:"sync"`
	DataDir     []diagnoseDataEntry    `json:"data_dir"`
	Doctor      doctorReport           `json:"doctor"`
	Logs        map[string]diagnoseLog `json:"logs"`
	Redaction   []string               `json:"redaction"`
}

// diagnoseLogTailLines bounds what the bundle carries. Enough to cover a
// recurring fault, small enough that the bundle stays attachable.
const diagnoseLogTailLines = 200

func collectDiagnoseLogs() map[string]diagnoseLog {
	out := map[string]diagnoseLog{}
	for name, path := range map[string]string{
		"cli": logging.Path(),
		"app": filepath.Join(logging.Dir(), "app.log"),
	} {
		entry := diagnoseLog{Path: path}
		if _, err := os.Stat(path); err == nil {
			entry.Exists = true
		}
		lines, err := logging.Tail(path, diagnoseLogTailLines)
		if err != nil {
			entry.Lines = []string{fmt.Sprintf("could not read log: %v", err)}
			out[name] = entry
			continue
		}
		// Empty rather than null: a consumer reading this bundle should not have to
		// distinguish "no failures logged" from "field missing".
		entry.Lines = lines
		if entry.Lines == nil {
			entry.Lines = []string{}
		}
		// Say so rather than let a capped view read as the whole history.
		entry.Truncated = len(lines) == diagnoseLogTailLines
		out[name] = entry
	}
	return out
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	report, err := buildDiagnoseReport()
	if err != nil {
		return err
	}

	if !diagnoseBundleFlag {
		if jsonOutput {
			redacted, err := redactedJSON(report)
			if err != nil {
				return err
			}
			// Already-marshalled JSON, so it is written directly rather than
			// re-encoded through output.JSON.
			fmt.Println(string(redacted))
			return nil
		}
		printDiagnoseSummary(report)
		return nil
	}

	target := diagnoseOutputFlag
	if target == "" {
		target = fmt.Sprintf("atm-diagnose-%s.json", time.Now().In(config.Loc).Format("20060102-150405"))
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("refusing to overwrite %s: pass --output to choose another path", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	redacted, err := redactedJSON(report)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, append(redacted, '\n'), 0600); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"bundle": target, "bytes": len(redacted) + 1})
		return nil
	}
	fmt.Printf("Wrote %s (%s)\n", target, formatBytes(int64(len(redacted)+1)))
	fmt.Println("  contains: versions, schema versions, doctor findings, data source presence, last sync error")
	fmt.Println("  excludes: session text, todo/memory/knowledge content, credentials")
	fmt.Println("  attach it to the bug report as-is; nothing was uploaded")
	return nil
}

func buildDiagnoseReport() (diagnoseBundleReport, error) {
	report := diagnoseBundleReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ATM: diagnoseATM{
			Version:       rootCmd.Version,
			SchemaVersion: store.SchemaVersion,
			DatabasePath:  config.AtmDB,
			DataDir:       config.AtmDir,
		},
		Platform: diagnosePlatform{
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
			GoVersion: runtime.Version(),
			CPUs:      runtime.NumCPU(),
		},
		App:  inspectATMApp(),
		Logs: collectDiagnoseLogs(),
		Redaction: []string{
			"paths under $HOME are rewritten to ~",
			"session text, todo/memory/knowledge content and credentials are never collected",
			"directories are reported by entry count and size only, never by file name",
			fmt.Sprintf("logs are the last %d lines and record failures only, never command arguments", diagnoseLogTailLines),
			`quoted values inside logged error messages are replaced with "…", because that is how an error embeds a title, a path or any other argument`,
		},
	}

	if info, err := os.Stat(config.AtmDB); err == nil {
		report.ATM.DatabaseExists = true
		report.ATM.DatabaseBytes = info.Size()
		version, versionErr := store.ReadSchemaVersionAt(config.AtmDB)
		if versionErr != nil {
			// Worth reporting rather than failing: an unreadable database is
			// precisely the kind of problem this bundle exists to describe.
			report.ATM.DatabaseError = versionErr.Error()
		} else {
			report.ATM.DatabaseSchemaVersion = version
		}
	}
	if _, err := os.Stat(config.ConfigPath); err == nil {
		report.ATM.ConfigExists = true
	}

	entries, err := diagnoseDataDir()
	if err != nil {
		return diagnoseBundleReport{}, err
	}
	report.DataDir = entries

	scope := store.SyncScopeAll
	syncReport, syncErr := buildSyncStatusReport(scope)
	if syncErr != nil {
		report.Sync = syncStatusReport{
			GeneratedAt: report.GeneratedAt,
			Sync:        syncStatusState{Scope: scope, Status: "unreadable", LastError: syncErr.Error()},
		}
	} else {
		report.Sync = syncReport
	}

	// doctor's own findings, not a re-derivation of them. A missing database is a
	// valid state for it, so the nil-db path is kept.
	doctor, doctorErr := diagnoseDoctorReport()
	if doctorErr != nil {
		report.Doctor.Issues = append(report.Doctor.Issues, doctorIssue{
			Severity: "warning", Domain: "diagnose", Code: "doctor_failed",
			Subject: config.AtmDB, Detail: doctorErr.Error(),
			Suggestion: "include this bundle in the report; the doctor section is incomplete",
		})
	} else {
		report.Doctor = doctor
	}
	return report, nil
}

func diagnoseDoctorReport() (doctorReport, error) {
	if _, err := os.Stat(config.AtmDB); os.IsNotExist(err) {
		return buildDoctorReport(nil)
	}
	var report doctorReport
	err := withDB(true, func(db *sql.DB) error {
		var buildErr error
		report, buildErr = buildDoctorReport(db)
		return buildErr
	})
	return report, err
}

func diagnoseDataDir() ([]diagnoseDataEntry, error) {
	items, err := os.ReadDir(config.AtmDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []diagnoseDataEntry
	for _, item := range items {
		if item.Name() == config.CredentialsFileName {
			continue
		}
		entry := diagnoseDataEntry{Name: item.Name(), Kind: "file"}
		if item.IsDir() {
			entry.Kind = "dir"
			children, err := os.ReadDir(filepath.Join(config.AtmDir, item.Name()))
			if err == nil {
				entry.Entries = len(children)
			}
		} else if info, err := item.Info(); err == nil {
			entry.Bytes = info.Size()
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// atmAppSearchPaths are where an installed ATM.app would be. The bundle reports
// which were tried, so "app: not found" can be told apart from "app installed
// somewhere ATM does not look".
func atmAppSearchPaths() []string {
	paths := []string{"/Applications/ATM.app"}
	if config.Home != "" {
		paths = append(paths, filepath.Join(config.Home, "Applications", "ATM.app"))
	}
	if override := strings.TrimSpace(os.Getenv("ATM_APP_PATH")); override != "" {
		paths = append([]string{override}, paths...)
	}
	return paths
}

func inspectATMApp() diagnoseApp {
	app := diagnoseApp{
		DashboardV:    contract.DashboardSchemaVersion,
		SearchedPaths: atmAppSearchPaths(),
	}
	for _, path := range app.SearchedPaths {
		plist := filepath.Join(path, "Contents", "Info.plist")
		data, err := os.ReadFile(plist)
		if err != nil {
			continue
		}
		app.Found = true
		app.Path = path
		app.ShortVersion = plistString(data, "CFBundleShortVersionString")
		app.BundleVersion = plistString(data, "CFBundleVersion")
		if app.ShortVersion == "" && app.BundleVersion == "" {
			// A bundle whose Info.plist is binary rather than XML lands here. Say
			// so instead of reporting the app as versionless.
			app.InspectError = "could not read a version from Info.plist (binary plist?)"
		}
		return app
	}
	return app
}

// plistString pulls one value out of an XML plist without a plist parser: the
// only consumer is a version string from a bundle ATM builds itself, and a
// dependency for two fields is not worth it. A binary plist yields "", which
// inspectATMApp reports rather than hides.
func plistString(data []byte, key string) string {
	marker := []byte("<key>" + key + "</key>")
	index := bytes.Index(data, marker)
	if index < 0 {
		return ""
	}
	rest := data[index+len(marker):]
	start := bytes.Index(rest, []byte("<string>"))
	if start < 0 {
		return ""
	}
	rest = rest[start+len("<string>"):]
	end := bytes.Index(rest, []byte("</string>"))
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

// redactedJSON marshals the report and rewrites the home directory prefix out of
// it. Doing it on the encoded bytes rather than field by field is deliberate:
// paths turn up inside doctor issue details and sync error strings too, and a
// per-field pass would miss exactly the places nobody thought about.
func redactedJSON(report diagnoseBundleReport) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	// An empty or "/" home would match everything; leaving the paths intact is
	// the lesser problem, and the bundle says what redaction ran.
	home := strings.TrimRight(config.Home, "/")
	if len(home) > 1 {
		data = bytes.ReplaceAll(data, []byte(home), []byte("~"))
	}
	return data, nil
}

func printDiagnoseSummary(report diagnoseBundleReport) {
	fmt.Println("ATM Diagnose")
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  atm %s (schema v%d)\n", report.ATM.Version, report.ATM.SchemaVersion)
	if report.ATM.DatabaseExists {
		fmt.Printf("  database: v%d, %s\n", report.ATM.DatabaseSchemaVersion, formatBytes(report.ATM.DatabaseBytes))
		if report.ATM.DatabaseSchemaVersion > report.ATM.SchemaVersion {
			fmt.Println("            newer than this build — upgrade atm")
		}
		if report.ATM.DatabaseError != "" {
			fmt.Printf("            unreadable: %s\n", report.ATM.DatabaseError)
		}
	} else {
		fmt.Println("  database: absent — run `atm sync`")
	}
	if report.App.Found {
		fmt.Printf("  app: %s (bundle %s)\n", report.App.ShortVersion, report.App.BundleVersion)
	} else {
		fmt.Println("  app: not installed where ATM looks")
	}
	fmt.Printf("  platform: %s/%s, %s\n", report.Platform.OS, report.Platform.Arch, report.Platform.GoVersion)
	fmt.Printf("  sync: %s", report.Sync.Sync.Status)
	if report.Sync.Sync.LastError != "" {
		fmt.Printf(" — last error: %s", report.Sync.Sync.LastError)
	}
	fmt.Println()

	counts := map[string]int{}
	for _, issue := range report.Doctor.Issues {
		counts[issue.Severity]++
	}
	severities := make([]string, 0, len(counts))
	for severity := range counts {
		severities = append(severities, severity)
	}
	sort.Strings(severities)
	if len(severities) == 0 {
		fmt.Println("  doctor: no issues")
	} else {
		parts := make([]string, 0, len(severities))
		for _, severity := range severities {
			parts = append(parts, fmt.Sprintf("%d %s", counts[severity], severity))
		}
		fmt.Printf("  doctor: %s (run `atm doctor` for detail)\n", strings.Join(parts, ", "))
	}
	fmt.Println("\nRun `atm diagnose --bundle` to write a file you can attach to a bug report.")
}
