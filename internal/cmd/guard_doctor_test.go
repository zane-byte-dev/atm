package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

// issuesByCode keeps the whole finding, because these assertions are about the
// wording as much as the presence: a stuck request whose suggestion invites a
// retry would be worse than no finding at all.
func issuesByCode(issues []doctorIssue) map[string]doctorIssue {
	byCode := map[string]doctorIssue{}
	for _, issue := range issues {
		byCode[issue.Code] = issue
	}
	return byCode
}

// guardOnlyTool narrows the rule set to one tool so a test does not depend on
// which of the real tools happen to be installed on the machine running it.
func guardOnlyTool(t *testing.T, tool, bin string) {
	t.Helper()
	original := config.Guard
	t.Cleanup(func() { config.Guard = original })
	config.Guard = config.GuardConfig{Tools: map[string]config.GuardToolConfig{
		tool: {Bin: bin, Rules: []config.GuardRule{{
			ID: "send", Label: "发消息", Path: []string{"chat", "message", "send"},
		}}},
	}}
	// Keep the built-in tools out of the way: they resolve against the real PATH
	// and would add findings this test says nothing about.
	t.Setenv("PATH", filepath.Dir(bin))
}

// Every finding here describes a gate that is present but not working. None of
// them make any command fail, so without doctor the user goes on believing sends
// are being reviewed.
func TestDoctorReportsAGateThatWasOverwritten(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "faketool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	guardOnlyTool(t, "faketool", bin)

	if _, err := guard.Install("faketool", bin, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The tool upgrades itself over the shim.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n# new version\n"), 0o755); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	issues := issuesByCode(guardIssues(nil))
	found, ok := issues["guard_shim_clobbered"]
	if !ok {
		t.Fatalf("no clobber finding; issues = %v", issues)
	}
	if !strings.Contains(found.Suggestion, "atm guard install faketool") {
		t.Errorf("suggestion does not say how to repair it: %q", found.Suggestion)
	}
}

func TestDoctorReportsAGateThatPathWalksAround(t *testing.T) {
	shadowDir := t.TempDir()
	gatedDir := t.TempDir()
	for _, dir := range []string{shadowDir, gatedDir} {
		if err := os.WriteFile(filepath.Join(dir, "faketool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	gated := filepath.Join(gatedDir, "faketool")
	guardOnlyTool(t, "faketool", gated)
	// PATH finds the other copy first, so invocations by bare name miss the gate.
	t.Setenv("PATH", shadowDir+string(os.PathListSeparator)+gatedDir)

	if _, err := guard.Install("faketool", gated, "/usr/local/bin/atm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	issues := issuesByCode(guardIssues(nil))
	found, ok := issues["guard_bin_shadowed"]
	if !ok {
		t.Fatalf("no shadowing finding; issues = %v", issues)
	}
	if !strings.Contains(found.Subject, "faketool") {
		t.Errorf("subject does not name the bypassing copy: %q", found.Subject)
	}
}

// A request stuck in running is the one state nothing can resolve automatically,
// so doctor's job is to say so and point at the target, never to offer a retry.
func TestDoctorReportsAStuckRequestWithoutSuggestingARetry(t *testing.T) {
	guardTestEnv(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	old := time.Now().In(config.Loc).Add(-30 * time.Minute).Unix()
	approval, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: "/tmp/x-atm-real",
		Argv:          []string{"chat", "message", "send"},
		Label:         "发送钉钉消息",
		PreviewTarget: "cid1",
		RequestedAt:   old, ExpiresAt: old + 1800,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.ApproveApproval(db, approval.ID, old+10, "panel", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := store.ClaimApprovalRun(db, approval.ID, "gate", 999999); err != nil {
		t.Fatalf("claim: %v", err)
	}

	issues := issuesByCode(guardStuckIssues(db))
	found, ok := issues["guard_stuck_running"]
	if !ok {
		t.Fatalf("no stuck finding; issues = %v", issues)
	}
	if found.Subject != approval.ID {
		t.Errorf("subject = %q, want %q", found.Subject, approval.ID)
	}
	if !strings.Contains(found.Suggestion, "不要重跑") {
		t.Errorf("suggestion does not rule out a retry: %q", found.Suggestion)
	}
	for _, forbidden := range []string{"重试", "retry", "rerun"} {
		if strings.Contains(strings.ToLower(found.Suggestion), forbidden) &&
			!strings.Contains(found.Suggestion, "不要重跑") {
			t.Errorf("suggestion invites a retry: %q", found.Suggestion)
		}
	}
}

// A request that only just started running is not stuck; warning about it would
// make doctor noisy on every normal send.
func TestDoctorIgnoresARequestThatOnlyJustStarted(t *testing.T) {
	guardTestEnv(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	now := time.Now().In(config.Loc).Unix()
	approval, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: "/tmp/x-atm-real", Argv: []string{"chat", "message", "send"},
		RequestedAt: now, ExpiresAt: now + 1800,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.ApproveApproval(db, approval.ID, now, "panel", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := store.ClaimApprovalRun(db, approval.ID, "gate", os.Getpid()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if issues := guardStuckIssues(db); len(issues) != 0 {
		t.Fatalf("warned about a send that is still in progress: %v", issues)
	}
}

// The MCP blind spot is only worth raising once a gate exists: it is not that MCP
// is a problem, it is that somebody who installed a gate has stopped watching
// sends, and a channel the gate cannot see is worse for them than for someone who
// never installed one.
func TestDoctorSaysNothingAboutMCPWhenNoGateIsInstalled(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "faketool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	guardOnlyTool(t, "faketool", bin)

	for _, issue := range guardIssues(nil) {
		if issue.Code == "guard_mcp_uncovered" {
			t.Fatalf("raised the MCP gap with no gate installed: %+v", issue)
		}
	}
}
