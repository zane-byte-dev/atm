package agentevent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

const hookServiceBinary = "/usr/local/bin/atm"

func hookServiceFixture(t *testing.T) (Service, string) {
	t.Helper()
	home := t.TempDir()
	service := NewService(ServiceOptions{
		Home:       func() string { return home },
		SocketPath: func() string { return "/tmp/atm-hook-service-test.sock" },
		Executable: func() (string, error) { return hookServiceBinary, nil },
	})
	return service, home
}

func hookServiceCall(origin application.Origin) application.Call {
	return application.Call{
		RequestID: "agent-hook-service-test",
		Actor:     application.Actor{Kind: application.ActorHuman, Origin: origin},
	}
}

func registrationFor(t *testing.T, report RegistrationReport, source string) Registration {
	t.Helper()
	for _, entry := range report.Sources {
		if entry.Source == source {
			return entry
		}
	}
	t.Fatalf("report has no entry for %q: %#v", source, report.Sources)
	return Registration{}
}

// The report has to say which verb produced it: `installed` is a finding under
// status and an outcome under install, so a reader that only sees the lists
// cannot tell what it is looking at.
func TestServiceReportsEveryAgentAndTheActionThatProducedIt(t *testing.T) {
	service, _ := hookServiceFixture(t)
	report, err := service.Status(context.Background(), hookServiceCall(application.OriginCLI), RegistrationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionStatus {
		t.Fatalf("action = %q, want %q", report.Action, ActionStatus)
	}
	if report.SocketPath != "/tmp/atm-hook-service-test.sock" {
		t.Fatalf("socket path = %q", report.SocketPath)
	}
	reported := make([]string, 0, len(report.Sources))
	for _, entry := range report.Sources {
		reported = append(reported, entry.Source)
	}
	if strings.Join(reported, ",") != strings.Join(SupportedSources(), ",") {
		t.Fatalf("sources = %v, want %v", reported, SupportedSources())
	}
}

func TestServiceRoundTripsInstallStatusAndUninstall(t *testing.T) {
	service, home := hookServiceFixture(t)
	ctx := context.Background()
	call := hookServiceCall(application.OriginIPC)

	before, err := service.Status(ctx, call, RegistrationInput{Source: SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	missing := registrationFor(t, before, SourceClaude)
	if len(missing.Missing) == 0 {
		t.Fatal("expected hooks to be reported missing before install")
	}
	if len(missing.Installed) != 0 {
		t.Fatalf("nothing should be installed yet: %v", missing.Installed)
	}

	installed, err := service.Install(ctx, call, RegistrationInput{Source: SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	added := registrationFor(t, installed, SourceClaude)
	if len(added.Added) != len(missing.Missing) {
		t.Fatalf("added %v, expected all of %v", added.Added, missing.Missing)
	}
	if added.Missing != nil {
		t.Fatalf("install must not report a missing list: %v", added.Missing)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	if added.Path != settings {
		t.Fatalf("path = %q, want %q", added.Path, settings)
	}
	written, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), hookServiceBinary+" agent hook --source claude") {
		t.Fatalf("settings.json does not invoke the resolved binary:\n%s", written)
	}

	after, err := service.Status(ctx, call, RegistrationInput{Source: SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	if entry := registrationFor(t, after, SourceClaude); len(entry.Missing) != 0 {
		t.Fatalf("still missing after install: %v", entry.Missing)
	}

	removed, err := service.Uninstall(ctx, call, RegistrationInput{Source: SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	if entry := registrationFor(t, removed, SourceClaude); len(entry.Removed) != len(added.Added) {
		t.Fatalf("removed %v, expected all of %v", entry.Removed, added.Added)
	}
}

func TestServiceRoundTripsGrokDedicatedHookFile(t *testing.T) {
	service, home := hookServiceFixture(t)
	ctx := context.Background()
	call := hookServiceCall(application.OriginCLI)

	before, err := service.Status(ctx, call, RegistrationInput{Source: SourceGrokbuild})
	if err != nil {
		t.Fatal(err)
	}
	if len(registrationFor(t, before, SourceGrokbuild).Missing) == 0 {
		t.Fatal("expected Grok hooks to be reported missing before install")
	}

	installed, err := service.Install(ctx, call, RegistrationInput{Source: SourceGrokbuild})
	if err != nil {
		t.Fatal(err)
	}
	entry := registrationFor(t, installed, SourceGrokbuild)
	if len(entry.Added) == 0 {
		t.Fatal("expected Grok install to add hooks")
	}
	if entry.Path != filepath.Join(home, ".grok", "hooks", "atm-notch.json") {
		t.Fatalf("Grok path = %q", entry.Path)
	}

	after, err := service.Status(ctx, call, RegistrationInput{Source: SourceGrokbuild})
	if err != nil {
		t.Fatal(err)
	}
	if entry := registrationFor(t, after, SourceGrokbuild); len(entry.Missing) != 0 || len(entry.Installed) == 0 {
		t.Fatalf("Grok status after install = %#v", entry)
	}

	removed, err := service.Uninstall(ctx, call, RegistrationInput{Source: SourceGrokbuild})
	if err != nil {
		t.Fatal(err)
	}
	if len(registrationFor(t, removed, SourceGrokbuild).Removed) == 0 {
		t.Fatal("expected Grok uninstall to remove hooks")
	}
	if _, err := os.Stat(entry.Path); !os.IsNotExist(err) {
		t.Fatalf("Grok atm-notch.json should be deleted, stat err=%v", err)
	}
}

// Pi loads a TypeScript extension rather than a hooks config, so reporting a
// config-file failure for it would be misleading.
func TestServicePointsPiAtItsExtensionFile(t *testing.T) {
	service, _ := hookServiceFixture(t)
	report, err := service.Status(context.Background(), hookServiceCall(application.OriginCLI), RegistrationInput{Source: SourcePi})
	if err != nil {
		t.Fatal(err)
	}
	entry := registrationFor(t, report, SourcePi)
	if entry.Manual == "" {
		t.Error("expected pi to report manual extension instructions")
	}
	if entry.Error != "" {
		t.Errorf("pi should not report an error: %q", entry.Error)
	}
	if entry.Path != "" {
		t.Errorf("pi should not claim a config path: %q", entry.Path)
	}
}

// One agent's unreadable config is a finding about that agent. Failing the whole
// call would tell the user nothing is installed when four agents are fine.
func TestServiceKeepsGoingWhenOneAgentsConfigIsMalformed(t *testing.T) {
	service, home := hookServiceFixture(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := service.Install(context.Background(), hookServiceCall(application.OriginIPC), RegistrationInput{})
	if err != nil {
		t.Fatalf("one malformed config must not fail the call: %v", err)
	}
	if entry := registrationFor(t, report, SourceClaude); entry.Error == "" {
		t.Error("expected an error rather than a silent overwrite")
	}
	if entry := registrationFor(t, report, SourceCodex); entry.Error != "" || len(entry.Added) == 0 {
		t.Errorf("codex should still be installed: %#v", entry)
	}
}

func TestServiceRejectsAnUnknownAgentAndAnUnresolvableBinary(t *testing.T) {
	service, _ := hookServiceFixture(t)
	_, err := service.Status(context.Background(), hookServiceCall(application.OriginCLI), RegistrationInput{Source: "claudex"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unknown source error = %v, want invalid argument", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Details["field"] != "source" {
		t.Fatalf("unknown source details = %#v", appErr)
	}

	broken := NewService(ServiceOptions{
		Home:       func() string { return t.TempDir() },
		SocketPath: func() string { return "" },
		Executable: func() (string, error) { return "", errors.New("no executable") },
	})
	if _, err := broken.Install(context.Background(), hookServiceCall(application.OriginCLI), RegistrationInput{}); !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("unresolvable binary error = %v, want unavailable", err)
	}
}

func TestServiceRejectsAnUnidentifiedCallAndACanceledContext(t *testing.T) {
	service, _ := hookServiceFixture(t)
	if _, err := service.Status(context.Background(), application.Call{}, RegistrationInput{}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("call without a request id = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Install(ctx, hookServiceCall(application.OriginCLI), RegistrationInput{})
	if !errors.Is(err, application.ErrUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled install error = %v", err)
	}
}
