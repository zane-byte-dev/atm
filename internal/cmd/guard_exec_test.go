package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

// fakeTool writes its own argv to a file and exits with the given code, so a test
// can tell "the command ran" from "the command was gated" without sending
// anything anywhere.
func fakeTool(t *testing.T, exitCode int) (bin, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	// The name must look like a displaced binary, because that is what a shim
	// hands to the guard and what `approve` will consent to run.
	bin = filepath.Join(dir, "faketool-atm-real")
	argvFile = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n" +
		"echo fake-stdout\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	return bin, argvFile
}

// installFakeShim goes through the real installer, so a test exercising deferred
// execution is subject to the same "only what a shim displaced" check that
// production is. Returns the displaced binary's path, which is what a shim hands
// to the guard.
func installFakeShim(t *testing.T, tool string) (realBin, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, tool)
	argvFile = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\necho fake-stdout\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	if _, _, _, err := runGuard(t, "install", tool, "--bin", binPath); err != nil {
		t.Fatalf("install shim: %v", err)
	}
	t.Cleanup(func() { runGuard(t, "uninstall", tool, "--bin", binPath) })
	return guard.RealBinPath(binPath), argvFile
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func toolRan(t *testing.T, argvFile string) bool {
	t.Helper()
	_, err := os.Stat(argvFile)
	return err == nil
}

// runGuard drives `atm guard ...` through real argument parsing, with the
// pass-through stubbed: the real one replaces this process, which would take the
// test binary with it.
func runGuard(t *testing.T, args ...string) (stdout, stderr string, passedThrough bool, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	replaced := false

	// The refusal text goes to the process's real stderr, not cobra's stream,
	// because that is what the calling agent reads.
	originalStderr := guardStderr
	guardStderr = &errOut
	originalExec := execReplace
	execReplace = func(string, []string, []string) error {
		replaced = true
		return nil
	}
	guardExecTool, guardExecWait, guardExecExpire = "", 0, 0
	guardListStatus, guardListLimit = "pending", 50
	guardDecideReason, guardDecideBy, guardApproveRun = "", "cli", true
	guardInstallBin = ""
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(append([]string{"guard"}, args...))
	t.Cleanup(func() {
		guardStderr = originalStderr
		execReplace = originalExec
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		guardExecTool, guardExecWait, guardExecExpire = "", 0, 0
		guardApproveRun = true
	})
	err = rootCmd.Execute()
	return out.String(), errOut.String(), replaced, err
}

func guardTestEnv(t *testing.T) {
	t.Helper()
	withTempAtmDir(t)
	// Decision provenance is derived best-effort from the process environment. These
	// CLI integration tests stand in for a human terminal unless a test opts into
	// an Agent environment explicitly.
	withHumanCLI(t)
	// No app listening, and no banner: the notifier would otherwise spawn
	// osascript on every test run.
	t.Setenv(agentevent.SocketEnvVar, filepath.Join(shortSocketDir(t), "absent.sock"))
	t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1")
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var coded exitError
	if !errors.As(err, &coded) {
		t.Fatalf("error %v does not carry an exit code", err)
	}
	return coded.ExitCode()
}

// The pass-through is the contract that keeps this feature installed: a read must
// reach the tool with nothing in the way, and must not even create a database.
func TestUnmatchedCommandRunsAndTouchesNoDatabase(t *testing.T) {
	guardTestEnv(t)
	bin, _ := fakeTool(t, 0)

	_, stderr, passedThrough, err := runGuard(t, "exec", "--tool", "dws", "--",
		bin, "chat", "message", "list", "--group", "g1", "-f", "json")
	if err != nil {
		t.Fatalf("read command errored: %v (stderr %q)", err, stderr)
	}
	if !passedThrough {
		t.Fatal("a read did not reach the real binary")
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("a read opened the database (%v); the fast path must not", err)
	}
	if stderr != "" {
		t.Fatalf("a read produced stderr: %q", stderr)
	}
}

func TestMatchedCommandWithNoDecisionDoesNotRun(t *testing.T) {
	guardTestEnv(t)
	bin, argvFile := fakeTool(t, 0)

	_, stderr, passedThrough, err := runGuard(t, "exec", "--tool", "dws",
		"--wait", "50ms", "--", bin, "chat", "message", "send",
		"--group", "g1", "--title", "上线", "--text", "已发布到预发", "-y")
	if passedThrough {
		t.Fatal("a send was handed straight to the tool")
	}
	if toolRan(t, argvFile) {
		t.Fatal("the tool ran without a decision")
	}
	if code := exitCodeOf(t, err); code != guardExitPending {
		t.Fatalf("exit code = %d, want %d", code, guardExitPending)
	}
	// The stderr text is the whole agent-facing UX; these are the sentences that
	// stop a retry and a workaround.
	for _, want := range []string{"不要重试", "不要换用其他命令或工具绕过",
		"重试会让同一条消息发两遍", "已发布到预发", "上线"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestDeniedCommandExitsSeventySevenAndDoesNotRun(t *testing.T) {
	guardTestEnv(t)
	bin, argvFile := fakeTool(t, 0)
	send := []string{"exec", "--tool", "dws", "--wait", "50ms", "--", bin,
		"chat", "message", "send", "--group", "g1", "--text", "别发", "-y"}

	if _, _, _, err := runGuard(t, send...); exitCodeOf(t, err) != guardExitPending {
		t.Fatalf("first attempt: %v", err)
	}
	id := onlyApprovalID(t)
	if _, _, _, err := runGuard(t, "deny", id, "--reason", "内容不对"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	// The retry must be answered from the record, not raise the request again.
	_, stderr, _, err := runGuard(t, send...)
	if code := exitCodeOf(t, err); code != guardExitDenied {
		t.Fatalf("exit code = %d, want %d", code, guardExitDenied)
	}
	if toolRan(t, argvFile) {
		t.Fatal("a denied command ran")
	}
	if !strings.Contains(stderr, "内容不对") {
		t.Errorf("stderr does not tell the model why it was refused:\n%s", stderr)
	}
	if count := approvalCount(t); count != 1 {
		t.Fatalf("approvals = %d, want 1: a denial must not be re-raised as a new request", count)
	}
}

func TestApprovalWhileWaitingRunsInlineAndPassesTheExitCodeThrough(t *testing.T) {
	guardTestEnv(t)
	bin, argvFile := fakeTool(t, 3)

	// Approve from another goroutine while the gate is polling.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if id := firstPendingApprovalID(); id != "" {
				db, err := store.Open()
				if err != nil {
					return
				}
				defer db.Close()
				store.ApproveApproval(db, id, time.Now().In(config.Loc).Unix(), "panel", "")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	_, _, _, err := runGuard(t, "exec", "--tool", "dws", "--wait", "6s", "--", bin,
		"chat", "message", "send", "--group", "g1", "--text", "hi", "-y")
	wg.Wait()

	if !toolRan(t, argvFile) {
		t.Fatal("an approved command did not run")
	}
	// The gate stands in front of another program, so that program's own status is
	// what the caller must see.
	if code := exitCodeOf(t, err); code != 3 {
		t.Fatalf("exit code = %d, want the tool's own 3", code)
	}

	approval := loadOnlyApproval(t)
	if approval.Status != store.ApprovalDone {
		t.Fatalf("status = %s, want done", approval.Status)
	}
	if approval.RanBy != "gate" {
		t.Fatalf("ran_by = %q, want gate", approval.RanBy)
	}
	if approval.ExitCode == nil || *approval.ExitCode != 3 {
		t.Fatalf("exit_code = %v, want 3 recorded", approval.ExitCode)
	}
	if !strings.Contains(approval.Output, "fake-stdout") {
		t.Fatalf("output = %q, want the tool's output kept for the record", approval.Output)
	}
}

// A retry must attach to the pending request: one row, one banner, and a count of
// how many times the agent came back.
func TestRetryAttachesInsteadOfRaisingASecondRequest(t *testing.T) {
	guardTestEnv(t)
	socket, delivered := guardSocketRecorder(t)
	t.Setenv(agentevent.SocketEnvVar, socket)
	t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1")

	bin, _ := fakeTool(t, 0)
	send := []string{"exec", "--tool", "dws", "--wait", "50ms", "--", bin,
		"chat", "message", "send", "--group", "g1", "--text", "hi", "-y"}

	for attempt := 0; attempt < 3; attempt++ {
		if _, _, _, err := runGuard(t, send...); exitCodeOf(t, err) != guardExitPending {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if count := approvalCount(t); count != 1 {
		t.Fatalf("approvals = %d, want 1", count)
	}
	approval := loadOnlyApproval(t)
	if approval.AttachCount != 3 {
		t.Fatalf("attach_count = %d, want 3", approval.AttachCount)
	}
	if got := len(delivered()); got != 1 {
		t.Fatalf("socket envelopes = %d, want 1: retries must not re-notify", got)
	}
}

// The gate hands ownership back when it stops waiting, so a later approval runs
// the command at once instead of waiting out a deadline nobody is honouring.
func TestGivingUpReleasesOwnershipSoALaterApprovalRuns(t *testing.T) {
	guardTestEnv(t)
	// A real shim, because ATM refuses to run anything a shim did not displace.
	bin, argvFile := installFakeShim(t, "dws")

	if _, _, _, err := runGuard(t, "exec", "--tool", "dws", "--wait", "50ms", "--", bin,
		"chat", "message", "send", "--group", "g1", "--text", "hi", "-y"); exitCodeOf(t, err) != guardExitPending {
		t.Fatalf("unexpected: %v", err)
	}
	approval := loadOnlyApproval(t)
	if approval.GateDeadline != 0 || approval.GatePID != 0 {
		t.Fatalf("ownership not released: pid=%d deadline=%d", approval.GatePID, approval.GateDeadline)
	}

	if _, _, _, err := runGuard(t, "approve", approval.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !toolRan(t, argvFile) {
		t.Fatal("ATM did not run the approved command; nothing would ever complete")
	}
	ran := loadOnlyApproval(t)
	if ran.RanBy != "app" || ran.Status != store.ApprovalDone {
		t.Fatalf("ran_by=%q status=%q", ran.RanBy, ran.Status)
	}
}

// Failing open here would send the message silently while ATM believed it was
// reviewing sends — worse than no gate, because the user would have stopped
// watching. Reads are unaffected, which is what makes that acceptable.
func TestFailsClosedOnlyForMatchedCommands(t *testing.T) {
	guardTestEnv(t)
	bin, argvFile := fakeTool(t, 0)
	// A directory where the database file should be makes Open fail.
	if err := os.MkdirAll(config.AtmDB, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, stderr, _, err := runGuard(t, "exec", "--tool", "dws", "--wait", "50ms", "--", bin,
		"chat", "message", "send", "--group", "g1", "--text", "hi", "-y")
	if code := exitCodeOf(t, err); code != guardExitBlocked {
		t.Fatalf("exit code = %d, want %d", code, guardExitBlocked)
	}
	if toolRan(t, argvFile) {
		t.Fatal("a send went through while ATM could not record it")
	}
	if !strings.Contains(stderr, "不要重试") {
		t.Errorf("stderr does not stop a retry:\n%s", stderr)
	}

	// The same broken database must not stop a read.
	if _, _, passedThrough, err := runGuard(t, "exec", "--tool", "dws", "--", bin,
		"chat", "message", "list", "--group", "g1"); err != nil || !passedThrough {
		t.Fatalf("a read was blocked by an ATM failure: %v", err)
	}
}

func TestBrokenRuleBlocksOnlyItsOwnTool(t *testing.T) {
	guardTestEnv(t)
	original := config.Guard
	t.Cleanup(func() { config.Guard = original })
	config.Guard = config.GuardConfig{Tools: map[string]config.GuardToolConfig{
		"dws": {Rules: []config.GuardRule{{ID: "broken", ArgvPattern: `^ata::(`}}},
	}}

	bin, argvFile := fakeTool(t, 0)
	_, _, _, err := runGuard(t, "exec", "--tool", "dws", "--", bin, "chat", "message", "list")
	if code := exitCodeOf(t, err); code != guardExitBlocked {
		t.Fatalf("exit code = %d, want %d: an unevaluable rule means 'cannot tell'", code, guardExitBlocked)
	}
	if toolRan(t, argvFile) {
		t.Fatal("ran a command whose rules could not be evaluated")
	}
	// Another tool's commands are untouched by dws's broken rule.
	if _, _, passedThrough, err := runGuard(t, "exec", "--tool", "a1", "--", bin,
		"repo", "mr", "list"); err != nil || !passedThrough {
		t.Fatalf("a1 blocked by a broken dws rule: %v", err)
	}
}

// Regression for the one that would have silently broken the whole feature:
// agents do not run commands on a terminal, so "stdin is not a tty" is true for
// every real invocation and must not be read as "content arrived on a pipe".
func TestStdinDetectionIgnoresWhatAgentHarnessesActuallyHandOver(t *testing.T) {
	cases := []struct {
		name string
		open func(t *testing.T) *os.File
		want bool
	}{
		{"socket, which is what Claude Code hands a child", func(t *testing.T) *os.File {
			left, right, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			t.Cleanup(func() { left.Close(); right.Close() })
			// An empty pipe stands in for the no-content case; the socket a harness
			// supplies reports the same "nothing readable".
			return left
		}, false},
		{"/dev/null", func(t *testing.T) *os.File {
			file, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { file.Close() })
			return file
		}, false},
		{"empty regular file", func(t *testing.T) *os.File {
			path := filepath.Join(t.TempDir(), "empty")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { file.Close() })
			return file
		}, false},
		{"regular file with content", func(t *testing.T) *os.File {
			path := filepath.Join(t.TempDir(), "body")
			if err := os.WriteFile(path, []byte("消息正文"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { file.Close() })
			return file
		}, true},
	}
	original := os.Stdin
	t.Cleanup(func() { os.Stdin = original })
	for _, test := range cases {
		os.Stdin = test.open(t)
		if got := guardStdinCarriesContent(); got != test.want {
			t.Errorf("%s: guardStdinCarriesContent() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestApproveRefusesABinaryNoShimDisplaced(t *testing.T) {
	guardTestEnv(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().In(config.Loc).Unix()
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	approval, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: "/bin/sh",
		Argv:        []string{"-c", "touch " + marker},
		RequestedAt: now, ExpiresAt: now + 600,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	db.Close()

	if _, _, _, err := runGuard(t, "approve", approval.ID); err == nil {
		t.Fatal("approved a command ATM had no business executing")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("ATM executed an arbitrary recorded command")
	}
}

func TestApproveRefusesARunningRequest(t *testing.T) {
	guardTestEnv(t)
	bin, _ := fakeTool(t, 0)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().In(config.Loc).Unix()
	approval, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: bin, Argv: []string{"chat", "message", "send"},
		RequestedAt: now, ExpiresAt: now + 600,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.ApproveApproval(db, approval.ID, now, "cli", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := store.ClaimApprovalRun(db, approval.ID, "gate", 999999); err != nil {
		t.Fatalf("claim: %v", err)
	}
	db.Close()

	// Whether the message went out is not recorded anywhere, so nothing may act.
	_, _, _, err = runGuard(t, "approve", approval.ID)
	if err == nil {
		t.Fatal("acted on a request whose outcome is unknown")
	}
	if !strings.Contains(err.Error(), "executing") {
		t.Fatalf("error %q does not explain why", err)
	}
}

// --- helpers over the database ---------------------------------------------

func openTestApprovals(t *testing.T) []store.Approval {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	approvals, err := store.ListApprovals(db, nil, time.Now().In(config.Loc).Unix(), 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return approvals
}

func approvalCount(t *testing.T) int {
	t.Helper()
	return len(openTestApprovals(t))
}

func loadOnlyApproval(t *testing.T) store.Approval {
	t.Helper()
	approvals := openTestApprovals(t)
	if len(approvals) != 1 {
		t.Fatalf("approvals = %d, want exactly 1", len(approvals))
	}
	return approvals[0]
}

func onlyApprovalID(t *testing.T) string {
	t.Helper()
	return loadOnlyApproval(t).ID
}

// firstPendingApprovalID is used from a goroutine, so it reports nothing rather
// than failing a test from the wrong stack.
func firstPendingApprovalID() string {
	db, err := store.Open()
	if err != nil {
		return ""
	}
	defer db.Close()
	approvals, err := store.ListApprovals(db, []string{store.ApprovalPending},
		time.Now().In(config.Loc).Unix(), 1)
	if err != nil || len(approvals) == 0 {
		return ""
	}
	return approvals[0].ID
}

// guardSocketRecorder stands in for the app, so a test can assert how many
// notifications one decision produced.
func guardSocketRecorder(t *testing.T) (string, func() []string) {
	t.Helper()
	path := filepath.Join(shortSocketDir(t), "notch.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mutex sync.Mutex
	lines := []string{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				mutex.Lock()
				lines = append(lines, scanner.Text())
				mutex.Unlock()
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return path, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string{}, lines...)
	}
}

// The gate records where it installed a shim, which means `guard install` writes
// config. A test whose config path is not redirected therefore writes the *user's*
// config — and did: it left the real file pointing at a t.TempDir() that ceased to
// exist with the test, after which the gate reported itself off for a tool it was
// in fact guarding. This pins the isolation rather than the symptom.
func TestInstallingInATestNeverWritesTheRealConfig(t *testing.T) {
	realConfig := config.ConfigPath
	guardTestEnv(t)
	if config.ConfigPath == realConfig {
		t.Fatal("guardTestEnv did not redirect ConfigPath; installing would write the user's own config")
	}
	if _, err := os.Stat(config.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("test config already exists at %s", config.ConfigPath)
	}

	bin, _ := installFakeShim(t, "dws")
	if bin == "" {
		t.Fatal("no shim installed")
	}
	// The install must have recorded its path somewhere — just not in the real file.
	data, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatalf("install recorded nothing: %v", err)
	}
	if !strings.Contains(string(data), "dws-atm-real") && !strings.Contains(string(data), "\"bin\"") {
		t.Fatalf("install did not record where it went:\n%s", data)
	}
}
