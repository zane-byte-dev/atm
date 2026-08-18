package store

import (
	"errors"
	"testing"
)

// sendRequest is a valid minimal gated request: a DingTalk group message, which
// is the case the gate was built for.
func sendRequest(now int64) Approval {
	return Approval{
		Tool:          "dws",
		RuleID:        "chat-send",
		RealBin:       "/Users/x/.qoderwork/bin/dws-atm-real",
		Argv:          []string{"chat", "message", "send", "--group", "g1", "--text", "上线了", "-y"},
		CWD:           "/Users/x/mox/atm",
		Label:         "发送钉钉消息",
		PreviewTarget: "g1",
		PreviewBody:   "上线了",
		RequestedAt:   now,
		ExpiresAt:     now + 1800,
		GatePID:       4242,
		GateDeadline:  now + 25,
	}
}

func TestPendingDedupIndexRefusesASecondRequest(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	first, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := CreateApproval(db, sendRequest(now+1)); !errors.Is(err, ErrApprovalPending) {
		t.Fatalf("second create = %v, want ErrApprovalPending", err)
	}

	if err := DenyApproval(db, first.ID, now+5, "cli", "手动发"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	// Once the first is terminal the same command may be requested again: sending
	// the same message twice on purpose is normal.
	if _, err := CreateApproval(db, sendRequest(now+10)); err != nil {
		t.Fatalf("create after denial: %v", err)
	}
}

func TestDenyThenApproveKeepsBothRecords(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	denied, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DenyApproval(db, denied.ID, now+5, "cli", "内容不对"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	approved, err := CreateApproval(db, sendRequest(now+3600))
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := ApproveApproval(db, approved.ID, now+3605, "cli", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if denied.ID == approved.ID {
		t.Fatalf("both requests share id %s; the denial record was overwritten", denied.ID)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM approvals WHERE dedup_key=?`, denied.DedupKey).
		Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("rows for dedup key = %d, want 2 (the denial must survive)", count)
	}
}

func TestOnlyOneClaimWins(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ApproveApproval(db, approval.ID, now+5, "panel", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := ClaimApprovalRun(db, approval.ID, "gate", 4242); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := ClaimApprovalRun(db, approval.ID, "app", 99); err == nil {
		t.Fatal("second claim succeeded; one approved command could run twice")
	}

	stored, err := GetApproval(db, approval.ID, now+6)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != ApprovalRunning || stored.RanBy != "gate" {
		t.Fatalf("status=%s ran_by=%s, want running/gate", stored.Status, stored.RanBy)
	}
}

func TestRunningIsTerminalForAutomation(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ApproveApproval(db, approval.ID, now+5, "panel", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := ClaimApprovalRun(db, approval.ID, "gate", 4242); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// The gate is presumed dead here. Nothing may move the row back to a state
	// something else would execute from, because whether the message went out is
	// not recoverable.
	if err := ApproveApproval(db, approval.ID, now+10, "panel", ""); err == nil {
		t.Fatal("approving a running request succeeded; the send could be repeated")
	}
	if err := ClaimApprovalRun(db, approval.ID, "app", 99); err == nil {
		t.Fatal("claiming a running request succeeded; the send could be repeated")
	}
}

func TestApproveRefusesAfterExpiryAndSettlesTheRow(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	late := approval.ExpiresAt + 1
	if err := ApproveApproval(db, approval.ID, late, "panel", ""); err == nil {
		t.Fatal("approve past expiry succeeded")
	}
	stored, err := GetApproval(db, approval.ID, late)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != ApprovalExpired {
		t.Fatalf("status = %s, want expired to be settled by the refusal", stored.Status)
	}
}

func TestDenyIsAcceptedPastExpiry(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DenyApproval(db, approval.ID, approval.ExpiresAt+60, "panel", "不发"); err != nil {
		t.Fatalf("deny past expiry: %v", err)
	}
	stored, err := GetApproval(db, approval.ID, approval.ExpiresAt+61)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != ApprovalDenied {
		t.Fatalf("status = %s, want denied", stored.Status)
	}
}

// Read commands open the database query_only, so listing must never need a write
// to report that a pending request has expired.
func TestEffectiveStatusNeedsNoWrite(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	readOnly, err := OpenReadOnly()
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer readOnly.Close()

	late := approval.ExpiresAt + 1
	pending, err := ListApprovals(readOnly, []string{ApprovalPending}, late, 50)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %d, want 0: an expired request must not read as pending", len(pending))
	}
	expired, err := ListApprovals(readOnly, []string{ApprovalExpired}, late, 50)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(expired) != 1 || expired[0].Effective != ApprovalExpired || expired[0].Status != ApprovalPending {
		t.Fatalf("expired = %+v, want one row still stored pending but reading expired", expired)
	}

	// The same row is still attachable-by-dedup before expiry and not after.
	live, err := PendingApprovalByDedup(readOnly, approval.DedupKey, now+5)
	if err != nil || live == nil {
		t.Fatalf("pending by dedup before expiry = %v, %v", live, err)
	}
	dead, err := PendingApprovalByDedup(readOnly, approval.DedupKey, late)
	if err != nil {
		t.Fatalf("pending by dedup after expiry: %v", err)
	}
	if dead != nil {
		t.Fatalf("expired request offered for attachment: %+v", dead)
	}
}

func TestGateOwnershipIsAClockNotAPID(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !approval.GateOwnsExecution(now + 1) {
		t.Fatal("gate does not own execution inside its own deadline")
	}
	if approval.GateOwnsExecution(approval.GateDeadline + 1) {
		t.Fatal("gate still owns execution past its deadline")
	}

	if err := ReleaseApprovalGate(db, approval.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	released, err := GetApproval(db, approval.ID, now+2)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if released.GateOwnsExecution(now + 2) {
		t.Fatal("released gate still owns execution; a later approval would not run")
	}
	// Releasing is not conditional on rows affected: losing the race to a decision
	// is expected, not an error.
	if err := ApproveApproval(db, approval.ID, now+3, "cli", ""); err != nil {
		t.Fatalf("approve after release: %v", err)
	}
	if err := ReleaseApprovalGate(db, approval.ID); err != nil {
		t.Fatalf("release after decision: %v", err)
	}
}

func TestAttachCountsRetriesWithoutRaisingASecondRequest(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := AttachApproval(db, approval.ID, 5151, now+130); err != nil {
		t.Fatalf("attach: %v", err)
	}
	stored, err := GetApproval(db, approval.ID, now+131)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.AttachCount != 2 {
		t.Fatalf("attach_count = %d, want 2", stored.AttachCount)
	}
	if stored.GatePID != 5151 || stored.GateDeadline != now+130 {
		t.Fatalf("ownership not handed over: pid=%d deadline=%d", stored.GatePID, stored.GateDeadline)
	}
	if err := DenyApproval(db, approval.ID, now+140, "cli", ""); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := AttachApproval(db, approval.ID, 6262, now+200); err == nil {
		t.Fatal("attached to a decided request")
	}
}

func TestDenialCooldownExpires(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DenyApproval(db, approval.ID, now+5, "cli", "别发"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	fresh, err := RecentDeniedApproval(db, approval.DedupKey, now+5-600)
	if err != nil || fresh == nil {
		t.Fatalf("denial inside cooldown = %v, %v; a retry would re-raise the banner", fresh, err)
	}
	if fresh.Reason != "别发" {
		t.Fatalf("reason = %q, want the recorded refusal so the agent can be told why", fresh.Reason)
	}
	stale, err := RecentDeniedApproval(db, approval.DedupKey, now+5+1)
	if err != nil {
		t.Fatalf("stale lookup: %v", err)
	}
	if stale != nil {
		t.Fatal("denial never expires; the same message could never be sent again")
	}
}

func TestFinishRecordsOutcomeOnlyFromRunning(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	approval, err := CreateApproval(db, sendRequest(now))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := FinishApproval(db, approval.ID, 0, "sent"); err == nil {
		t.Fatal("finished a request that never ran")
	}
	if err := ApproveApproval(db, approval.ID, now+5, "banner", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := ClaimApprovalRun(db, approval.ID, "app", 77); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := FinishApproval(db, approval.ID, 3, "auth failed"); err != nil {
		t.Fatalf("finish: %v", err)
	}
	stored, err := GetApproval(db, approval.ID, now+6)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != ApprovalDone {
		t.Fatalf("status = %s, want done", stored.Status)
	}
	if stored.ExitCode == nil || *stored.ExitCode != 3 {
		t.Fatalf("exit_code = %v, want 3: a failed send must not read as success", stored.ExitCode)
	}
	if stored.Output != "auth failed" {
		t.Fatalf("output = %q", stored.Output)
	}
}

func TestApprovalRoundTripPreservesArgvAndCapsPreview(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	request := sendRequest(now)
	request.Argv = []string{"chat", "message", "send", "--group", "g 1", "--text", "带 空格\n和换行", "-y"}
	long := make([]rune, ApprovalTextLimit+50)
	for i := range long {
		long[i] = '字'
	}
	request.PreviewBody = string(long)
	request.StdinPiped = true

	created, err := CreateApproval(db, request)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored, err := GetApproval(db, created.ID, now+1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(stored.Argv) != len(request.Argv) {
		t.Fatalf("argv = %q, want %q", stored.Argv, request.Argv)
	}
	for i := range request.Argv {
		if stored.Argv[i] != request.Argv[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, stored.Argv[i], request.Argv[i])
		}
	}
	if !stored.StdinPiped {
		t.Fatal("stdin_piped lost; deferred execution would replay the wrong content")
	}
	if runes := []rune(stored.PreviewBody); len(runes) != ApprovalTextLimit+1 {
		t.Fatalf("preview body runes = %d, want %d plus the ellipsis", len(runes), ApprovalTextLimit)
	}
}

func TestCreateApprovalRejectsIncompleteRequests(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const now = int64(1_700_000_000)
	cases := map[string]func(Approval) Approval{
		"no tool":         func(a Approval) Approval { a.Tool = " "; return a },
		"no real binary":  func(a Approval) Approval { a.RealBin = ""; return a },
		"no argv":         func(a Approval) Approval { a.Argv = nil; return a },
		"expiry in past":  func(a Approval) Approval { a.ExpiresAt = a.RequestedAt - 1; return a },
		"expiry same sec": func(a Approval) Approval { a.ExpiresAt = a.RequestedAt; return a },
	}
	for name, mutate := range cases {
		if _, err := CreateApproval(db, mutate(sendRequest(now))); err == nil {
			t.Fatalf("%s: create succeeded", name)
		}
	}
}

func TestApprovalStatusCheckRejectsUnknownStatus(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT INTO approvals
		(id,dedup_key,tool,real_bin,argv,status,requested_at,expires_at)
		VALUES ('ap_x','k','dws','/bin/true','[]','bogus',1,2)`)
	if err == nil {
		t.Fatal("inserted an unknown status; the lifecycle is not enforced by the database")
	}
}
