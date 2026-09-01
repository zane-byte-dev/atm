package guard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Approval is the Guard domain record returned by application use cases. The
// persistence package currently owns the durable representation; the alias
// keeps adapters on the Guard boundary while that representation remains
// shared with the gate process.
type Approval = store.Approval

const (
	ApprovalPending  = store.ApprovalPending
	ApprovalApproved = store.ApprovalApproved
	ApprovalRunning  = store.ApprovalRunning
	ApprovalDone     = store.ApprovalDone
	ApprovalDenied   = store.ApprovalDenied
	ApprovalExpired  = store.ApprovalExpired
)

// DeferredExecutor is the narrow infrastructure port used after a human has
// approved a request whose original gate no longer owns execution. Validate is
// deliberately separate: a row must never be claimed before the executable is
// proven to be one displaced by an installed ATM shim.
type DeferredExecutor interface {
	Validate(context.Context, Approval) error
	Execute(context.Context, Approval) ExecutionResult
}

// ExecutionResult is recorded even when the child exits unsuccessfully. Once a
// command has been claimed, returning it to the approved state would make a
// duplicate external action possible.
type ExecutionResult struct {
	ExitCode int
	Output   string
}

// ServiceOptions are Guard's persistence, clock, and process ports.
type ServiceOptions struct {
	OpenRead  func() (*sql.DB, error)
	OpenWrite func() (*sql.DB, error)
	Sync      func(*sql.DB) (int, error)
	Now       func() time.Time
	Executor  DeferredExecutor
	Shims     ShimInfrastructure
	Config    ManagementRepository
	Runner    string
	PID       func() int
}

// Service owns Guard query validation and the approval state-machine
// orchestration. Cobra and IPC adapters only construct calls and render the
// returned records.
type Service struct {
	openRead  func() (*sql.DB, error)
	openWrite func() (*sql.DB, error)
	sync      func(*sql.DB) (int, error)
	now       func() time.Time
	executor  DeferredExecutor
	shims     ShimInfrastructure
	config    ManagementRepository
	runner    string
	pid       func() int
}

// Default is shared by the CLI today and typed IPC callers as they migrate.
var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.OpenWrite == nil {
		options.OpenWrite = store.Open
	}
	if options.Sync == nil {
		options.Sync = store.SyncAll
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Executor == nil {
		options.Executor = LocalDeferredExecutor{}
	}
	if options.Shims == nil {
		options.Shims = LocalShimInfrastructure{}
	}
	if options.Config == nil {
		options.Config = LocalManagementRepository{}
	}
	if strings.TrimSpace(options.Runner) == "" {
		options.Runner = "app"
	}
	if options.PID == nil {
		options.PID = os.Getpid
	}
	return Service{
		openRead: options.OpenRead, openWrite: options.OpenWrite, sync: options.Sync,
		now: options.Now, executor: options.Executor, shims: options.Shims, config: options.Config,
		runner: options.Runner, pid: options.PID,
	}
}

type ListInput struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Sync   bool   `json:"sync,omitempty"`
}

type ListResult struct {
	Approvals []Approval `json:"approvals"`
}

type ShowInput struct {
	ID   string `json:"id"`
	Sync bool   `json:"sync,omitempty"`
}

type ShowResult struct {
	Approval Approval `json:"approval"`
}

type DecisionInput struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
	Run     bool   `json:"run"`
	Reason  string `json:"reason,omitempty"`
	// DecidedBy is an audit-surface label retained for the current CLI/App
	// contract. It never participates in authorization; authority comes only
	// from Call.Actor, which the trusted adapter constructs.
	DecidedBy string `json:"decided_by,omitempty"`
}

type DecisionOutcome string

const (
	OutcomeDenied          DecisionOutcome = "denied"
	OutcomeApproved        DecisionOutcome = "approved"
	OutcomeApprovedGateRun DecisionOutcome = "approved-gate-runs"
	OutcomeApprovedAndRan  DecisionOutcome = "approved-and-ran"
)

type DecisionResult struct {
	Approval Approval        `json:"approval"`
	Outcome  DecisionOutcome `json:"outcome"`
}

type OperationMeta struct {
	SyncedFiles int
}

func (service Service) List(ctx context.Context, call application.Call, input ListInput) (ListResult, OperationMeta, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return ListResult{}, OperationMeta{}, err
	}
	statuses, err := parseApprovalStatuses(input.Status)
	if err != nil {
		return ListResult{}, OperationMeta{}, err
	}
	db, meta, err := service.openQuery(input.Sync)
	if err != nil {
		return ListResult{}, meta, err
	}
	defer db.Close()

	approvals, err := store.ListApprovals(db, statuses, service.unixNow(), input.Limit)
	if err != nil {
		return ListResult{}, meta, unavailableGuard("list gated actions", err)
	}
	return ListResult{Approvals: approvals}, meta, nil
}

func (service Service) Show(ctx context.Context, call application.Call, input ShowInput) (ShowResult, OperationMeta, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return ShowResult{}, OperationMeta{}, err
	}
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		return ShowResult{}, OperationMeta{}, invalidGuardArgument("approval ID is required", "id", input.ID)
	}
	db, meta, err := service.openQuery(input.Sync)
	if err != nil {
		return ShowResult{}, meta, err
	}
	defer db.Close()

	approval, err := store.GetApproval(db, input.ID, service.unixNow())
	if err != nil {
		return ShowResult{}, meta, unavailableGuard("show gated action", err)
	}
	if approval == nil {
		return ShowResult{}, meta, notFoundApproval(input.ID)
	}
	return ShowResult{Approval: *approval}, meta, nil
}

// Decide applies the human-only approval policy and owns the entire persisted
// transition. It preserves the gate ownership boundary: an original caller
// still inside its declared deadline runs the approved command, while the
// service may claim and execute it exactly once after that deadline.
func (service Service) Decide(ctx context.Context, call application.Call, input DecisionInput) (DecisionResult, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return DecisionResult{}, err
	}
	// `_ipc` is intentionally replayable from a terminal and currently carries
	// no proof that ATM.app, rather than an Agent, launched it. Never turn its
	// convenient human@ipc presentation identity into Guard authority. A future
	// App decision method needs an authenticated local capability before this
	// policy can admit OriginIPC.
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginCLI {
		err := application.NewError(application.CodeForbidden, "only a human at the Guard CLI edge may approve or deny a gated action")
		err.Details = map[string]any{
			"actor_kind": call.Actor.Kind,
			"origin":     call.Actor.Origin,
		}
		return DecisionResult{}, err
	}
	input.ID = strings.TrimSpace(input.ID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.DecidedBy = strings.TrimSpace(input.DecidedBy)
	if input.ID == "" {
		return DecisionResult{}, invalidGuardArgument("approval ID is required", "id", input.ID)
	}
	if input.DecidedBy == "" {
		input.DecidedBy = string(call.Actor.Origin)
	}
	if err := guardContextError(ctx); err != nil {
		return DecisionResult{}, err
	}
	db, err := service.openWrite()
	if err != nil {
		return DecisionResult{}, unavailableGuard("open Guard store", err)
	}
	defer db.Close()

	now := service.unixNow()
	approval, err := store.GetApproval(db, input.ID, now)
	if err != nil {
		return DecisionResult{}, unavailableGuard("read gated action", err)
	}
	if approval == nil {
		return DecisionResult{}, notFoundApproval(input.ID)
	}
	if approval.Status == ApprovalRunning {
		return DecisionResult{}, conflictGuard(fmt.Sprintf(
			"approval %s is already executing; check the target yourself with `atm guard show %s`",
			input.ID, input.ID), input.ID, approval.Status, nil)
	}

	if !input.Approve {
		if err := store.DenyApproval(db, input.ID, now, input.DecidedBy, input.Reason); err != nil {
			return DecisionResult{}, decisionStoreError(input.ID, approval.Status, err)
		}
		return service.loadDecision(db, input.ID, OutcomeDenied, now)
	}

	if err := store.ApproveApproval(db, input.ID, now, input.DecidedBy, input.Reason); err != nil {
		return DecisionResult{}, decisionStoreError(input.ID, approval.Status, err)
	}
	if !input.Run {
		return service.loadDecision(db, input.ID, OutcomeApproved, now)
	}
	if approval.GateOwnsExecution(now) {
		return service.loadDecision(db, input.ID, OutcomeApprovedGateRun, now)
	}
	if approval.StdinPiped {
		return DecisionResult{}, conflictGuard(fmt.Sprintf(
			"approval %s was given input on a pipe, which cannot be reproduced; it is approved but must be re-run by whoever composed it",
			input.ID), input.ID, ApprovalApproved, nil)
	}
	if err := service.executor.Validate(ctx, *approval); err != nil {
		return DecisionResult{}, forbiddenDeferredExecution(input.ID, err)
	}
	if err := guardContextError(ctx); err != nil {
		return DecisionResult{}, err
	}
	if err := store.ClaimApprovalRun(db, input.ID, service.runner, service.pid()); err != nil {
		if errors.Is(err, store.ErrApprovalRunClaimLost) {
			// The waiting gate may wake and win the only execution claim between the
			// ownership check and this update. Its result is then authoritative.
			return service.loadDecision(db, input.ID, OutcomeApprovedGateRun, now)
		}
		return DecisionResult{}, unavailableGuard("claim gated action execution", err)
	}

	execution := service.executor.Execute(ctx, *approval)
	if err := store.FinishApproval(db, input.ID, execution.ExitCode, execution.Output); err != nil {
		return DecisionResult{}, unavailableGuard("record gated action outcome", err)
	}
	result, err := service.loadDecision(db, input.ID, OutcomeApprovedAndRan, now)
	if err != nil {
		return DecisionResult{}, err
	}
	if execution.ExitCode != 0 {
		appErr := application.NewError(application.CodeUnavailable, fmt.Sprintf(
			"approved, but %s exited %d", approval.Tool, execution.ExitCode))
		appErr.Details = map[string]any{
			"approval_id": input.ID,
			"tool":        approval.Tool,
			"exit_code":   execution.ExitCode,
		}
		return result, appErr
	}
	return result, nil
}

func (service Service) openQuery(syncBeforeRead bool) (*sql.DB, OperationMeta, error) {
	if !syncBeforeRead {
		db, err := service.openRead()
		if err != nil {
			return nil, OperationMeta{}, unavailableGuard("open Guard store", err)
		}
		return db, OperationMeta{}, nil
	}
	db, err := service.openWrite()
	if err != nil {
		return nil, OperationMeta{}, unavailableGuard("open Guard store", err)
	}
	count, err := service.sync(db)
	if err != nil {
		db.Close()
		return nil, OperationMeta{}, unavailableGuard("sync before Guard query", err)
	}
	return db, OperationMeta{SyncedFiles: count}, nil
}

func (service Service) unixNow() int64 {
	return service.now().In(config.Loc).Unix()
}

func (service Service) loadDecision(db *sql.DB, id string, outcome DecisionOutcome, now int64) (DecisionResult, error) {
	approval, err := store.GetApproval(db, id, now)
	if err != nil {
		return DecisionResult{}, unavailableGuard("read Guard decision", err)
	}
	if approval == nil {
		return DecisionResult{}, notFoundApproval(id)
	}
	return DecisionResult{Approval: *approval, Outcome: outcome}, nil
}

func parseApprovalStatuses(value string) ([]string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "all" {
		return nil, nil
	}
	valid := map[string]bool{
		ApprovalPending: true, ApprovalApproved: true, ApprovalRunning: true,
		ApprovalDone: true, ApprovalDenied: true, ApprovalExpired: true,
	}
	statuses := []string{}
	for _, status := range strings.Split(value, ",") {
		status = strings.TrimSpace(status)
		if status == "" {
			continue
		}
		if !valid[status] {
			return nil, invalidGuardArgument(fmt.Sprintf("unknown status: %s", status), "status", status)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func validateGuardCall(ctx context.Context, call application.Call) error {
	if err := guardContextError(ctx); err != nil {
		return err
	}
	return call.Validate()
}

func guardContextError(ctx context.Context) error {
	if ctx == nil {
		return application.NewError(application.CodeInvalidArgument, "context is required")
	}
	if err := ctx.Err(); err != nil {
		appErr := application.WrapError(application.CodeUnavailable, "Guard request was cancelled", err)
		appErr.Retryable = errors.Is(err, context.DeadlineExceeded)
		return appErr
	}
	return nil
}

func invalidGuardArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func notFoundApproval(id string) *application.Error {
	err := application.NewError(application.CodeNotFound, "approval not found: "+id)
	err.Details = map[string]any{"approval_id": id}
	return err
}

func conflictGuard(message, id, status string, cause error) *application.Error {
	err := application.WrapError(application.CodeConflict, message, cause)
	err.Details = map[string]any{"approval_id": id, "status": status}
	return err
}

func decisionStoreError(id, status string, cause error) error {
	text := cause.Error()
	if strings.Contains(text, "already ") || strings.Contains(text, "no longer pending") ||
		strings.Contains(text, "expired at") {
		return conflictGuard(text, id, status, cause)
	}
	if strings.Contains(text, "not found") {
		return notFoundApproval(id)
	}
	return unavailableGuard("persist Guard decision", cause)
}

func unavailableGuard(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action+" failed", cause)
	err.Retryable = true
	return err
}

func forbiddenDeferredExecution(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeForbidden, "deferred execution is no longer valid", cause)
	err.Details = map[string]any{"approval_id": id}
	return err
}
