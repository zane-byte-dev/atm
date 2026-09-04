package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeWorkspaceLaunchctl struct {
	loaded bool
	calls  [][]string
	fail   string
}

func (fake *fakeWorkspaceLaunchctl) run(_ context.Context, args ...string) ([]byte, error) {
	fake.calls = append(fake.calls, append([]string{}, args...))
	if args[0] == fake.fail {
		return []byte("fixture failure"), errors.New("failed")
	}
	switch args[0] {
	case "print":
		if !fake.loaded {
			return []byte("Could not find service fixture in domain for user"), errors.New("not found")
		}
	case "bootstrap":
		fake.loaded = true
	case "bootout":
		fake.loaded = false
	}
	return nil, nil
}

func workspaceServiceFixture(t *testing.T) (workspaceServiceOptions, *fakeWorkspaceLaunchctl) {
	t.Helper()
	root, err := canonicalServicePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home & owner's Mac")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "atm & web")
	if err := os.WriteFile(executable, []byte("fixture executable, never run"), 0755); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWorkspaceLaunchctl{}
	return workspaceServiceOptions{Home: home, DataDir: filepath.Join(root, "data & notes"), Executable: executable, Path: ".:/fixture/bin:/fixture/bin:relative:/usr/bin", GOOS: "darwin", UID: 501, Port: 47321,
		CheckAssets: func() error { return nil }, Run: fake.run, WorkspaceRunning: func(context.Context, string) (bool, error) { return false, nil }}, fake
}

func requireServicePlan(t *testing.T, opts workspaceServiceOptions) (workspaceServicePlan, []byte) {
	t.Helper()
	plan, err := makeWorkspaceServicePlan(opts, true)
	if err != nil {
		t.Fatal(err)
	}
	plist, err := renderWorkspaceService(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, plist
}

func TestServeServicePlistUsesEscapedAbsoluteArgumentsWithoutSideEffects(t *testing.T) {
	opts, fake := workspaceServiceFixture(t)
	plan, plist := requireServicePlan(t, opts)
	if !ownedWorkspaceService(plist, plan) {
		t.Fatalf("rendered plist does not validate: %s", plist)
	}
	if !bytes.Contains(plist, []byte("<key>ExitTimeOut</key><integer>45</integer>")) {
		t.Fatal("launchd shutdown deadline must cover HTTP drain and background cancellation budgets")
	}
	want := []string{opts.Executable, "serve", "--background", "--data-dir", opts.DataDir, "--port", "47321"}
	if !reflect.DeepEqual(plan.Arguments, want) {
		t.Fatalf("argv=%q", plan.Arguments)
	}
	if !bytes.Contains(plist, []byte("&amp;")) || bytes.Contains(plist, []byte("data & notes")) {
		t.Fatalf("XML not escaped: %s", plist)
	}
	if strings.Contains(plan.Path, "relative") || strings.HasPrefix(plan.Path, ".") || strings.Count(plan.Path, "/fixture/bin") != 1 {
		t.Fatalf("unsafe PATH: %q", plan.Path)
	}
	if _, err := os.Stat(opts.DataDir); !os.IsNotExist(err) {
		t.Fatalf("preview created data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Home, "Library")); !os.IsNotExist(err) {
		t.Fatalf("preview created LaunchAgents: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatal("preview invoked launchctl")
	}
	opts.CheckAssets = func() error { return errors.New("no Web assets") }
	if _, err := makeWorkspaceServicePlan(opts, true); err == nil {
		t.Fatal("CLI-only executable accepted")
	}
	if _, err := makeWorkspaceServicePlan(opts, false); err != nil {
		t.Fatalf("uninstall should not require Web assets: %v", err)
	}
}

func TestServeServiceInstallIsIdempotentAndUpdatesOnlyOwnedJob(t *testing.T) {
	opts, fake := workspaceServiceFixture(t)
	plan, plist := requireServicePlan(t, opts)
	ctx := context.Background()
	if err := installWorkspaceService(ctx, opts, plan, plist); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"print", plan.Domain + "/" + plan.Label}, {"bootstrap", plan.Domain, plan.PlistPath}, {"kickstart", plan.Domain + "/" + plan.Label}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls=%v", fake.calls)
	}
	for _, path := range []string{plan.PlistPath, plan.Stdout, plan.Stderr} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %s: %v %v", path, info, err)
		}
	}
	before, _ := os.Stat(plan.PlistPath)
	fake.calls = nil
	if err := installWorkspaceService(ctx, opts, plan, plist); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(plan.PlistPath)
	if !os.SameFile(before, after) || len(fake.calls) != 1 || fake.calls[0][0] != "print" {
		t.Fatalf("identical reinstall restarted/replaced: %v", fake.calls)
	}
	opts.Port = 47322
	updated, updatedPlist := requireServicePlan(t, opts)
	fake.calls = nil
	if err := installWorkspaceService(ctx, opts, updated, updatedPlist); err != nil {
		t.Fatal(err)
	}
	if plan.Label != updated.Label || len(fake.calls) != 4 || fake.calls[1][0] != "bootout" || fake.calls[2][0] != "bootstrap" || fake.calls[3][0] != "kickstart" {
		t.Fatalf("update did not replace same job: %v", fake.calls)
	}
}

func TestServeServiceUninstallPreservesDataOwnerMarkerAndFailedRemoval(t *testing.T) {
	opts, fake := workspaceServiceFixture(t)
	plan, plist := requireServicePlan(t, opts)
	ctx := context.Background()
	if err := installWorkspaceService(ctx, opts, plan, plist); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(plan.DataDir, "runtime", "presence-owner.json")
	data := filepath.Join(plan.DataDir, "atm.db")
	for _, path := range []string{owner, data} {
		if err := os.WriteFile(path, []byte("preserve exactly"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fake.fail = "bootout"
	if err := uninstallWorkspaceService(ctx, opts, plan); err == nil {
		t.Fatal("bootout failure ignored")
	}
	if _, err := os.Stat(plan.PlistPath); err != nil {
		t.Fatal("failed bootout removed plist")
	}
	fake.fail = ""
	fake.calls = nil
	if err := uninstallWorkspaceService(ctx, opts, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.PlistPath); !os.IsNotExist(err) {
		t.Fatal("managed plist not removed")
	}
	for _, path := range []string{owner, data} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "preserve exactly" {
			t.Fatalf("uninstall changed %s", path)
		}
	}
	for _, path := range []string{plan.Stdout, plan.Stderr} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("log removed: %v", err)
		}
	}
	fake.calls = nil
	if err := uninstallWorkspaceService(ctx, opts, plan); err != nil || len(fake.calls) != 0 {
		t.Fatalf("repeat uninstall=%v calls=%v", err, fake.calls)
	}
}

func TestServeServiceStopUnloadsJobAndPreservesLoginConfiguration(t *testing.T) {
	opts, fake := workspaceServiceFixture(t)
	plan, plist := requireServicePlan(t, opts)
	ctx := context.Background()
	if err := installWorkspaceService(ctx, opts, plan, plist); err != nil {
		t.Fatal(err)
	}
	fake.calls = nil
	managed, err := stopWorkspaceService(ctx, opts, plan)
	if err != nil || !managed || fake.loaded || len(fake.calls) != 2 || fake.calls[1][0] != "bootout" {
		t.Fatalf("stop=%v err=%v calls=%v", managed, err, fake.calls)
	}
	content, err := os.ReadFile(plan.PlistPath)
	if err != nil || !bytes.Equal(content, plist) {
		t.Fatal("stop removed or changed login configuration")
	}
	managed, err = stopWorkspaceService(ctx, opts, plan)
	if err != nil || managed {
		t.Fatalf("inactive job claimed a manual server: %v %v", managed, err)
	}
}

func TestServeServiceRefusesForeignFilesSymlinksAndUnmanagedServer(t *testing.T) {
	for _, scenario := range []string{"foreign", "plist-symlink", "directory-symlink", "log-symlink", "lock-symlink", "live-server", "unknown-job"} {
		t.Run(scenario, func(t *testing.T) {
			opts, fake := workspaceServiceFixture(t)
			plan, plist := requireServicePlan(t, opts)
			outside := filepath.Join(filepath.Dir(opts.Home), "outside")
			if err := os.WriteFile(outside, []byte("do not alter"), 0644); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "foreign", "plist-symlink":
				if err := os.MkdirAll(filepath.Dir(plan.PlistPath), 0700); err != nil {
					t.Fatal(err)
				}
				if scenario == "foreign" {
					if err := os.WriteFile(plan.PlistPath, []byte(`<plist version="1.0"><dict><key>Label</key><string>other</string></dict></plist>`), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(outside, plan.PlistPath); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				if err := os.Symlink(filepath.Dir(outside), filepath.Join(opts.Home, "Library")); err != nil {
					t.Fatal(err)
				}
			case "log-symlink":
				if err := os.MkdirAll(filepath.Dir(plan.Stdout), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, plan.Stdout); err != nil {
					t.Fatal(err)
				}
			case "lock-symlink":
				directory := filepath.Join(plan.DataDir, "runtime", "locks")
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(directory, "workspace-service.lock")); err != nil {
					t.Fatal(err)
				}
			case "live-server":
				opts.WorkspaceRunning = func(context.Context, string) (bool, error) { return true, nil }
			case "unknown-job":
				fake.loaded = true
			}
			if err := installWorkspaceService(context.Background(), opts, plan, plist); err == nil {
				t.Fatalf("accepted %s", scenario)
			}
			for _, call := range fake.calls {
				if call[0] != "print" {
					t.Fatalf("unsafe install invoked %v", call)
				}
			}
			content, err := os.ReadFile(outside)
			info, _ := os.Stat(outside)
			if err != nil || string(content) != "do not alter" || info.Mode().Perm() != 0644 {
				t.Fatal("outside target modified")
			}
		})
	}
}
