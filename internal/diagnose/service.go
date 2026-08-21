package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
	syncapp "github.com/zane-byte-dev/atm/internal/sync"
)

// logTailLines bounds what the bundle carries. Enough to cover a recurring fault,
// small enough that the bundle stays attachable.
const logTailLines = 200

// bundleMode keeps a support bundle owner-readable. It carries log tails and
// paths from the reporter's machine, so it must not land world-readable in a
// directory they may then share wholesale.
const bundleMode = 0o600

// Checker is the self-check whose findings the bundle carries. It is a port so
// the bundle embeds doctor's own findings rather than re-deriving them.
type Checker interface {
	Check(context.Context, application.Call, doctorapp.Input) (doctorapp.Report, error)
}

// FreshnessReader reports how stale the session index is.
type FreshnessReader interface {
	Status(context.Context, application.Call, syncapp.StatusInput) (syncapp.StatusReport, error)
}

// ServiceOptions are the bundle's clock, cross-domain ports, and the build
// identity it reports.
type ServiceOptions struct {
	// Version is this binary's version string. It is injected because only the
	// executable knows it — the value is stamped in at link time and set from main.
	Version   string
	Now       func() time.Time
	Doctor    Checker
	Freshness FreshnessReader
}

// Service owns the bundle: what it collects, what it never collects, how paths
// are redacted, and the rules for writing it to disk.
type Service struct {
	version   string
	now       func() time.Time
	doctor    Checker
	freshness FreshnessReader
}

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Doctor == nil {
		options.Doctor = doctorapp.Default
	}
	if options.Freshness == nil {
		options.Freshness = syncapp.Default
	}
	return Service{
		version: options.Version, now: options.Now,
		doctor: options.Doctor, freshness: options.Freshness,
	}
}

// Report collects everything the bundle carries.
//
// Nothing here fails on a broken install: an unreadable database, an
// uninspectable app bundle, and a failing self-check all become fields, because
// a broken install is exactly what this is for.
func (service Service) Report(
	ctx context.Context,
	call application.Call,
	_ Input,
) (Report, error) {
	if ctx == nil {
		return Report{}, invalid("context", nil, "diagnose context is required")
	}
	if err := call.Validate(); err != nil {
		return Report{}, err
	}
	if err := ctx.Err(); err != nil {
		return Report{}, unavailable("collect diagnostics", err)
	}

	generatedAt := service.now().UTC().Format(time.RFC3339)
	report := Report{
		GeneratedAt: generatedAt,
		ATM: ATM{
			Version:       service.version,
			SchemaVersion: store.SchemaVersion,
			DatabasePath:  config.AtmDB,
			DataDir:       config.AtmDir,
		},
		Platform: Platform{
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
			GoVersion: runtime.Version(),
			CPUs:      runtime.NumCPU(),
		},
		App:  inspectApp(),
		Logs: collectLogs(),
		Redaction: []string{
			"paths under $HOME are rewritten to ~",
			"session text, todo/memory/knowledge content and credentials are never collected",
			"directories are reported by entry count and size only, never by file name",
			fmt.Sprintf("logs are the last %d lines and record failures only, never command arguments", logTailLines),
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

	entries, err := dataDir()
	if err != nil {
		return Report{}, unavailable("inspect data directory", err)
	}
	report.DataDir = entries

	scope := store.SyncScopeAll
	freshness, freshnessErr := service.freshness.Status(ctx, call, syncapp.StatusInput{Scope: scope})
	if freshnessErr != nil {
		// An index this build cannot describe is exactly what the bundle exists to
		// report, so the failure becomes a field rather than an error.
		report.Sync = syncapp.StatusReport{
			GeneratedAt: generatedAt,
			Sync: syncapp.StatusState{
				Scope: scope, Status: "unreadable", LastError: freshnessErr.Error(),
			},
		}
	} else {
		report.Sync = freshness
	}

	// doctor's own findings, not a re-derivation of them. A missing database is a
	// valid state for it, which is why this can fail only for a real fault.
	doctor, doctorErr := service.doctor.Check(ctx, call, doctorapp.Input{})
	if doctorErr != nil {
		report.Doctor.Issues = append(report.Doctor.Issues, doctorapp.Issue{
			Severity: "warning", Domain: "diagnose", Code: "doctor_failed",
			Subject: config.AtmDB, Detail: doctorErr.Error(),
			Suggestion: "include this bundle in the report; the doctor section is incomplete",
		})
	} else {
		report.Doctor = doctor
	}
	return report, nil
}

// RedactedJSON is the bundle as it may leave the machine. Redaction runs on the
// encoded bytes rather than field by field, deliberately: paths turn up inside
// doctor issue details and sync error strings too, and a per-field pass would miss
// exactly the places nobody thought about.
func (service Service) RedactedJSON(report Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, internalError("encode diagnostics", err)
	}
	// An empty or "/" home would match everything; leaving the paths intact is
	// the lesser problem, and the bundle says what redaction ran.
	home := strings.TrimRight(config.Home, "/")
	if len(home) > 1 {
		data = bytes.ReplaceAll(data, []byte(home), []byte("~"))
	}
	return data, nil
}

// WriteBundle writes the redacted report to a file.
//
// It refuses to overwrite. The default name is timestamped, so a collision means
// the caller named a path — and silently replacing a file a person chose, in a
// directory they are working in, is not a trade this command gets to make.
func (service Service) WriteBundle(
	ctx context.Context,
	call application.Call,
	input BundleInput,
) (BundleResult, error) {
	report, err := service.Report(ctx, call, Input{})
	if err != nil {
		return BundleResult{}, err
	}
	target := strings.TrimSpace(input.Path)
	if target == "" {
		target = service.defaultBundleName()
	}
	if _, err := os.Stat(target); err == nil {
		appErr := application.NewError(application.CodeConflict,
			fmt.Sprintf("refusing to overwrite %s: pass --output to choose another path", target))
		appErr.Details = map[string]any{"path": target}
		return BundleResult{}, appErr
	} else if !os.IsNotExist(err) {
		return BundleResult{}, unavailable("inspect bundle path", err)
	}
	data, err := service.RedactedJSON(report)
	if err != nil {
		return BundleResult{}, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(target, data, bundleMode); err != nil {
		return BundleResult{}, unavailable("write bundle", err)
	}
	return BundleResult{Path: target, Bytes: len(data)}, nil
}

func (service Service) defaultBundleName() string {
	return fmt.Sprintf("atm-diagnose-%s.json",
		service.now().In(config.Loc).Format("20060102-150405"))
}

func collectLogs() map[string]Log {
	out := map[string]Log{}
	for name, path := range map[string]string{
		"cli": logging.Path(),
		"app": filepath.Join(logging.Dir(), "app.log"),
	} {
		entry := Log{Path: path}
		if _, err := os.Stat(path); err == nil {
			entry.Exists = true
		}
		lines, err := logging.Tail(path, logTailLines)
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
		entry.Truncated = len(lines) == logTailLines
		out[name] = entry
	}
	return out
}

func dataDir() ([]DataEntry, error) {
	items, err := os.ReadDir(config.AtmDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []DataEntry
	for _, item := range items {
		if item.Name() == config.CredentialsFileName {
			continue
		}
		entry := DataEntry{Name: item.Name(), Kind: "file"}
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

// appSearchPaths are where an installed ATM.app would be. The bundle reports
// which were tried, so "app: not found" can be told apart from "app installed
// somewhere ATM does not look".
func appSearchPaths() []string {
	paths := []string{"/Applications/ATM.app"}
	if config.Home != "" {
		paths = append(paths, filepath.Join(config.Home, "Applications", "ATM.app"))
	}
	if override := strings.TrimSpace(os.Getenv("ATM_APP_PATH")); override != "" {
		paths = append([]string{override}, paths...)
	}
	return paths
}

func inspectApp() App {
	app := App{
		DashboardV:    contract.DashboardSchemaVersion,
		SearchedPaths: appSearchPaths(),
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
// inspectApp reports rather than hides.
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

func internalError(action string, cause error) *application.Error {
	return application.WrapError(application.CodeInternal, action+" failed", cause)
}
