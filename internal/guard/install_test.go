package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestInstallAndUninstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")

	state, err := Install("dws", binPath, "/usr/local/bin/atm")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !state.Installed || state.Clobbered {
		t.Fatalf("state = %+v", state)
	}

	shim, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	if !strings.Contains(string(shim), "guard exec --tool 'dws'") {
		t.Fatalf("shim does not call the gate:\n%s", shim)
	}
	if !strings.Contains(string(shim), RealBinPath(binPath)) {
		t.Fatalf("shim does not point at the displaced binary:\n%s", shim)
	}
	displaced, err := os.ReadFile(RealBinPath(binPath))
	if err != nil {
		t.Fatalf("read displaced binary: %v", err)
	}
	if string(displaced) != "#!/bin/sh\necho real\n" {
		t.Fatalf("displaced binary changed: %q", displaced)
	}
	if info, err := os.Stat(binPath); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("shim is not executable: %v %v", info, err)
	}

	// Installing again is a no-op rather than a second displacement.
	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(RealBinPath(RealBinPath(binPath))); err == nil {
		t.Fatal("reinstall displaced the shim on top of itself")
	}

	restored, err := Uninstall("dws", binPath)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if restored.Installed {
		t.Fatalf("state = %+v", restored)
	}
	back, err := os.ReadFile(binPath)
	if err != nil || string(back) != "#!/bin/sh\necho real\n" {
		t.Fatalf("binary not restored: %q %v", back, err)
	}
	if _, err := os.Stat(RealBinPath(binPath)); !os.IsNotExist(err) {
		t.Fatalf("displaced copy left behind: %v", err)
	}
}

func TestUninstallIsIdempotentOnAToolThatWasNeverGated(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")

	if _, err := Uninstall("dws", binPath); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if body, _ := os.ReadFile(binPath); string(body) != "#!/bin/sh\necho real\n" {
		t.Fatalf("untouched binary was modified: %q", body)
	}
}

// A tool that upgrades itself overwrites the shim, leaving the previous version
// sitting in the displaced slot. Reinstalling has to adopt the *new* file as the
// real binary; keeping the old one would downgrade the user's CLI every time they
// repaired the gate.
func TestReinstallAfterAToolUpgradeAdoptsTheNewBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho v1\n")
	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The tool upgrades itself over the shim.
	writeExecutable(t, binPath, "#!/bin/sh\necho v2\n")

	state, err := Status("dws", binPath)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Installed {
		t.Fatal("an overwritten shim still reports installed")
	}
	if !state.Clobbered {
		t.Fatal("an overwritten shim is not reported as clobbered, so nobody would know to repair it")
	}

	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	displaced, err := os.ReadFile(RealBinPath(binPath))
	if err != nil {
		t.Fatalf("read displaced binary: %v", err)
	}
	if !strings.Contains(string(displaced), "v2") {
		t.Fatalf("reinstall resurrected the old binary: %q", displaced)
	}
}

// A shim whose displaced binary has gone cannot run anything, and must not be
// overwritten either — that would lose the tool entirely.
func TestShimWithNoRealBinaryIsReportedAndRefusesRepair(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")
	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.Remove(RealBinPath(binPath)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	state, err := Status("dws", binPath)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !state.Installed || !state.Clobbered || state.Healthy() {
		t.Fatalf("state = %+v", state)
	}
	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err == nil {
		t.Fatal("repair overwrote the shim with itself, losing the tool")
	}
	if _, err := Uninstall("dws", binPath); err == nil {
		t.Fatal("uninstall claimed to restore a binary that is gone")
	}
}

// The check that matters most. Two copies of a tool exist on this machine and
// PATH picks one; gating the other gates nothing while reporting success.
func TestPathShadowingIsReported(t *testing.T) {
	shadowDir := t.TempDir()
	gatedDir := t.TempDir()
	writeExecutable(t, filepath.Join(shadowDir, "a1"), "#!/bin/sh\necho shadow\n")
	gated := filepath.Join(gatedDir, "a1")
	writeExecutable(t, gated, "#!/bin/sh\necho gated\n")
	t.Setenv("PATH", shadowDir+string(os.PathListSeparator)+gatedDir)

	state, err := Install("a1", gated, "/usr/local/bin/atm")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !state.Installed {
		t.Fatal("not installed")
	}
	if state.ShadowedBy != filepath.Join(shadowDir, "a1") {
		t.Fatalf("shadowed_by = %q, want the copy PATH finds first", state.ShadowedBy)
	}
	if state.Healthy() {
		t.Fatal("a bypassed gate reports itself healthy")
	}
}

func TestNoShadowingWhenPathResolvesToTheGatedCopy(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "a1")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")
	t.Setenv("PATH", dir)

	state, err := Install("a1", binPath, "/usr/local/bin/atm")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if state.ShadowedBy != "" {
		t.Fatalf("shadowed_by = %q, want empty", state.ShadowedBy)
	}
	if !state.Healthy() {
		t.Fatalf("state = %+v", state)
	}
}

// A tool that is never invoked by bare name cannot be shadowed, and must not be
// warned about.
func TestToolAbsentFromPathIsNotReportedAsShadowed(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")
	t.Setenv("PATH", t.TempDir())

	state, err := Status("dws", binPath)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.ShadowedBy != "" {
		t.Fatalf("shadowed_by = %q, want empty for a tool only called by absolute path",
			state.ShadowedBy)
	}
}

func TestResolvePrefersOverrideThenConfigThenPath(t *testing.T) {
	original := Tools()
	_ = original
	dir := t.TempDir()
	onPath := filepath.Join(dir, "dws")
	writeExecutable(t, onPath, "#!/bin/sh\n")
	t.Setenv("PATH", dir)

	resolved, err := Resolve("dws", "/explicit/path")
	if err != nil || resolved != "/explicit/path" {
		t.Fatalf("override ignored: %q %v", resolved, err)
	}
	resolved, err = Resolve("dws", "")
	if err != nil || resolved != onPath {
		t.Fatalf("PATH fallback = %q %v", resolved, err)
	}
	if _, err := Resolve("definitely-not-installed", ""); err == nil {
		t.Fatal("resolved a tool that is nowhere; install would have created a stray file")
	}
}

func TestInstallRefusesAMissingBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "dws")
	if _, err := Install("dws", missing, "/usr/local/bin/atm"); err == nil {
		t.Fatal("installed a shim in front of nothing")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("left a shim behind for a tool that does not exist")
	}
}

func TestRealBinPathRoundTrips(t *testing.T) {
	const bin = "/usr/local/bin/dws"
	displaced := RealBinPath(bin)
	if !IsRealBinPath(displaced) {
		t.Fatalf("IsRealBinPath(%q) = false", displaced)
	}
	if IsRealBinPath(bin) {
		t.Fatalf("IsRealBinPath(%q) = true", bin)
	}
	if got := BinPathFromReal(displaced); got != bin {
		t.Fatalf("BinPathFromReal(%q) = %q, want %q", displaced, got, bin)
	}
}

// The shim carries no prose about what was moved where. The redirect target is
// unavoidably visible, but there is no reason to also leave a how-to beside it.
func TestShimCarriesNoExplanation(t *testing.T) {
	script := shimScript("dws", "/usr/local/bin/atm", "/usr/local/bin/dws-atm-real")
	for _, unwanted := range []string{"real binary", "moved", "bypass", "原始", "真身"} {
		if strings.Contains(strings.ToLower(script), unwanted) {
			t.Errorf("shim explains itself (%q):\n%s", unwanted, script)
		}
	}
	if strings.Count(script, "\n") > 3 {
		t.Errorf("shim is longer than it needs to be:\n%s", script)
	}
}

func TestShimQuotingSurvivesPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir with spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	binPath := filepath.Join(dir, "dws")
	writeExecutable(t, binPath, "#!/bin/sh\necho real\n")
	if _, err := Install("dws", binPath, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	shim, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(shim), "'"+RealBinPath(binPath)+"'") {
		t.Fatalf("path with spaces is not quoted:\n%s", shim)
	}
}
