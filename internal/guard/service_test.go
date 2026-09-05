package guard

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

const guardServiceNow = int64(1_700_000_100)

type fakeDeferredExecutor struct {
	validateErr error
	result      ExecutionResult
	validated   []Approval
	executed    []Approval
}

func (executor *fakeDeferredExecutor) Validate(_ context.Context, approval Approval) error {
	executor.validated = append(executor.validated, approval)
	return executor.validateErr
}

func (executor *fakeDeferredExecutor) Execute(_ context.Context, approval Approval) ExecutionResult {
	executor.executed = append(executor.executed, approval)
	return executor.result
}

func guardServiceCall(kind application.ActorKind) application.Call {
	return application.Call{
		RequestID: "guard-service-test",
		Actor: application.Actor{
			Kind:   kind,
			Origin: application.OriginCLI,
		},
	}
}

func nativeGuardServiceCall(kind application.ActorKind) application.Call {
	call := guardServiceCall(kind)
	call.Actor.Origin = application.OriginNativeControl
	return call
}

func withGuardServiceStore(t *testing.T) {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	directory := t.TempDir()
	config.AtmDir = directory
	config.AtmDB = filepath.Join(directory, "atm.db")
	t.Cleanup(func() {
		config.AtmDir, config.AtmDB = oldDir, oldDB
	})
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open Guard store: %v", err)
	}
	db.Close()
}

func createServiceApproval(t *testing.T, gateDeadline int64) Approval {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open Guard store: %v", err)
	}
	defer db.Close()
	approval, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: "/tmp/dws-atm-real",
		Argv:          []string{"chat", "message", "send", "--text", "hello"},
		Label:         "发送钉钉消息",
		PreviewTarget: "group-1",
		RequestedAt:   guardServiceNow - 5,
		ExpiresAt:     guardServiceNow + 600,
		GateDeadline:  gateDeadline,
	})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	return approval
}

func guardTestService(executor DeferredExecutor) Service {
	return NewService(ServiceOptions{
		Now:      func() time.Time { return time.Unix(guardServiceNow, 0) },
		Executor: executor,
		PID:      func() int { return 4242 },
	})
}

func TestServiceListAndShowOwnValidation(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	service := guardTestService(&fakeDeferredExecutor{})

	listed, _, err := service.List(context.Background(), guardServiceCall(application.ActorHuman), ListInput{
		Status: "pending", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed.Approvals) != 1 || listed.Approvals[0].ID != created.ID {
		t.Fatalf("list = %#v, want %s", listed.Approvals, created.ID)
	}
	shown, _, err := service.Show(context.Background(), guardServiceCall(application.ActorHuman), ShowInput{ID: created.ID})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if shown.Approval.ID != created.ID || shown.Approval.Effective != ApprovalPending {
		t.Fatalf("show = %#v", shown.Approval)
	}

	if _, _, err := service.List(context.Background(), guardServiceCall(application.ActorHuman), ListInput{Status: "mystery"}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid status error = %v, want invalid_argument", err)
	}
	if _, _, err := service.Show(context.Background(), guardServiceCall(application.ActorHuman), ShowInput{ID: "ap_missing"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing show error = %v, want not_found", err)
	}
}

func TestServiceDecisionIsHumanOnly(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{}
	service := guardTestService(executor)

	_, err := service.Decide(context.Background(), guardServiceCall(application.ActorAgent), DecisionInput{
		ID: created.ID, Approve: true, Run: true,
	})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent decision error = %v, want forbidden", err)
	}
	shown, _, err := service.Show(context.Background(), guardServiceCall(application.ActorHuman), ShowInput{ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if shown.Approval.Status != ApprovalPending {
		t.Fatalf("status = %s, want pending", shown.Approval.Status)
	}
	if len(executor.validated) != 0 || len(executor.executed) != 0 {
		t.Fatal("an unauthorized decision reached the executor")
	}
}

func TestServiceDecisionRejectsWebAndNativeControl(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{}
	service := guardTestService(executor)
	calls := []application.Call{
		{
			RequestID: "web-human",
			Actor:     application.Actor{Kind: application.ActorHuman, Origin: application.OriginWeb},
		},
		nativeGuardServiceCall(application.ActorHuman),
		nativeGuardServiceCall(application.ActorAgent),
		nativeGuardServiceCall(application.ActorController),
	}
	for _, call := range calls {
		_, err := service.Decide(context.Background(), call, DecisionInput{
			ID: created.ID, Approve: true, Run: true,
		})
		if !errors.Is(err, application.ErrForbidden) {
			t.Fatalf("%s@%s decision error = %v, want forbidden", call.Actor.Kind, call.Actor.Origin, err)
		}
	}
	if len(executor.validated) != 0 || len(executor.executed) != 0 {
		t.Fatal("an unauthorized decision reached the executor")
	}
}

func TestServiceDenyPersistsAuditWithoutExecutor(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{}
	service := guardTestService(executor)

	result, err := service.Decide(context.Background(), guardServiceCall(application.ActorHuman), DecisionInput{
		ID: created.ID, Approve: false, Run: true, Reason: "内容不对", DecidedBy: "panel",
	})
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	if result.Outcome != OutcomeDenied || result.Approval.Status != ApprovalDenied {
		t.Fatalf("decision = %#v", result)
	}
	if result.Approval.DecidedBy != "panel" || result.Approval.Reason != "内容不对" {
		t.Fatalf("audit = by %q reason %q", result.Approval.DecidedBy, result.Approval.Reason)
	}
	if len(executor.validated) != 0 || len(executor.executed) != 0 {
		t.Fatal("a denial reached the executor")
	}
}

func TestServiceApprovalPreservesGateOwnership(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, guardServiceNow+30)
	executor := &fakeDeferredExecutor{}
	service := guardTestService(executor)

	result, err := service.Decide(context.Background(), guardServiceCall(application.ActorHuman), DecisionInput{
		ID: created.ID, Approve: true, Run: true,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if result.Outcome != OutcomeApprovedGateRun || result.Approval.Status != ApprovalApproved {
		t.Fatalf("decision = %#v", result)
	}
	if len(executor.validated) != 0 || len(executor.executed) != 0 {
		t.Fatal("the service raced a gate that still owned execution")
	}
}

func TestServiceApprovalClaimsAndRecordsFakeExecution(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{result: ExecutionResult{Output: "sent"}}
	service := guardTestService(executor)

	result, err := service.Decide(context.Background(), guardServiceCall(application.ActorHuman), DecisionInput{
		ID: created.ID, Approve: true, Run: true,
	})
	if err != nil {
		t.Fatalf("approve and execute: %v", err)
	}
	if result.Outcome != OutcomeApprovedAndRan || result.Approval.Status != ApprovalDone {
		t.Fatalf("decision = %#v", result)
	}
	if result.Approval.RanBy != "app" || result.Approval.ExitCode == nil || *result.Approval.ExitCode != 0 ||
		result.Approval.Output != "sent" {
		t.Fatalf("recorded execution = %#v", result.Approval)
	}
	if len(executor.validated) != 1 || len(executor.executed) != 1 {
		t.Fatalf("executor calls validate=%d execute=%d", len(executor.validated), len(executor.executed))
	}
}

func TestServiceApprovalRetryAcrossRestartRunsExactlyOnce(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{result: ExecutionResult{Output: "sent"}}
	input := DecisionInput{ID: created.ID, Approve: true, Run: true, DecidedBy: "panel"}

	first, err := guardTestService(executor).Decide(
		context.Background(), guardServiceCall(application.ActorHuman), input,
	)
	if err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if first.Replayed || first.Outcome != OutcomeApprovedAndRan || first.Approval.Status != ApprovalDone {
		t.Fatalf("first decision = %#v", first)
	}

	// A new Service value models a process restart: idempotency comes entirely
	// from the stored decision and execution claim.
	retry, err := guardTestService(executor).Decide(
		context.Background(), guardServiceCall(application.ActorHuman), input,
	)
	if err != nil {
		t.Fatalf("retry approve: %v", err)
	}
	if !retry.Replayed || retry.Outcome != OutcomeApprovedAndRan || retry.Approval.Status != ApprovalDone {
		t.Fatalf("retry decision = %#v", retry)
	}
	if len(executor.validated) != 1 || len(executor.executed) != 1 {
		t.Fatalf("executor calls after retry validate=%d execute=%d, want 1/1", len(executor.validated), len(executor.executed))
	}

	_, err = guardTestService(executor).Decide(
		context.Background(), guardServiceCall(application.ActorHuman),
		DecisionInput{ID: created.ID, Approve: false},
	)
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("opposite denial error = %v, want conflict", err)
	}
}

func TestServiceDenialRetryAcrossRestartPreservesOriginalAudit(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	executor := &fakeDeferredExecutor{}
	call := guardServiceCall(application.ActorHuman)

	first, err := guardTestService(executor).Decide(context.Background(), call, DecisionInput{
		ID: created.ID, Approve: false, Reason: "内容不对", DecidedBy: "panel",
	})
	if err != nil {
		t.Fatalf("first denial: %v", err)
	}
	if first.Replayed || first.Outcome != OutcomeDenied {
		t.Fatalf("first denial = %#v", first)
	}

	retry, err := guardTestService(executor).Decide(context.Background(), call, DecisionInput{
		ID: created.ID, Approve: false, Reason: "retry must not replace audit", DecidedBy: "other",
	})
	if err != nil {
		t.Fatalf("retry denial: %v", err)
	}
	if !retry.Replayed || retry.Outcome != OutcomeDenied || retry.Approval.Status != ApprovalDenied {
		t.Fatalf("retry denial = %#v", retry)
	}
	if retry.Approval.Reason != "内容不对" || retry.Approval.DecidedBy != "panel" {
		t.Fatalf("retry replaced audit: by=%q reason=%q", retry.Approval.DecidedBy, retry.Approval.Reason)
	}
	if len(executor.validated) != 0 || len(executor.executed) != 0 {
		t.Fatal("a denial reached the executor")
	}

	_, err = guardTestService(executor).Decide(context.Background(), call, DecisionInput{
		ID: created.ID, Approve: true, Run: true,
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("opposite approval error = %v, want conflict", err)
	}
}

func TestServiceClaimInfrastructureFailureIsNotReportedAsGateRace(t *testing.T) {
	withGuardServiceStore(t)
	created := createServiceApproval(t, 0)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_guard_execution_claim
		BEFORE UPDATE OF status ON approvals
		WHEN NEW.status = 'running'
		BEGIN SELECT RAISE(ABORT, 'forced execution claim failure'); END`); err != nil {
		db.Close()
		t.Fatalf("install claim failure trigger: %v", err)
	}
	db.Close()

	executor := &fakeDeferredExecutor{}
	service := guardTestService(executor)
	_, err = service.Decide(context.Background(), guardServiceCall(application.ActorHuman), DecisionInput{
		ID: created.ID, Approve: true, Run: true,
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("claim infrastructure error = %v, want unavailable", err)
	}
	if len(executor.executed) != 0 {
		t.Fatal("executor ran after the durable claim failed")
	}
	shown, _, showErr := service.Show(
		context.Background(), guardServiceCall(application.ActorHuman), ShowInput{ID: created.ID},
	)
	if showErr != nil {
		t.Fatal(showErr)
	}
	if shown.Approval.Status != ApprovalApproved {
		t.Fatalf("status = %s, want approved for explicit retry/recovery", shown.Approval.Status)
	}
}
