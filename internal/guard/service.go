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
	// Replayed reports that the requested decision had already been durably
	// recorded. It is derived from the approval state, so it survives process
	// restarts and does not depend on an in-memory request cache.
	Replayed bool `json:"replayed"`
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
// service may claim and execute it exactly once after that deadline. The
// persisted decision itself is also the idempotency record: retrying the same
// approve/deny is safe across restarts, while reversing it remains a conflict.
func (service Service) Decide(ctx context.Context, call application.Call, input DecisionInput) (DecisionResult, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return DecisionResult{}, err
	}
	// `_ipc`, browser, and native-control identities do not prove that a person is
	// present at the Guard prompt. Decisions remain restricted to the CLI edge.
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
	if approval.Status != ApprovalPending {
		return service.replayDecision(ctx, db, *approval, input, now)
	}

	if !input.Approve {
		if err := store.DenyApproval(db, input.ID, now, input.DecidedBy, input.Reason); err != nil {
			return service.recoverDecisionRace(ctx, db, input, now, approval.Status, err)
		}
		return service.loadDecision(db, input.ID, OutcomeDenied, now, false)
	}

	if err := store.ApproveApproval(db, input.ID, now, input.DecidedBy, input.Reason); err != nil {
		return service.recoverDecisionRace(ctx, db, input, now, approval.Status, err)
	}
	approved, err := store.GetApproval(db, input.ID, now)
	if err != nil {
		return DecisionResult{}, unavailableGuard("read Guard decision", err)
	}
	if approved == nil {
		return DecisionResult{}, notFoundApproval(input.ID)
	}
	return service.continueApproved(ctx, db, *approved, input.Run, now, false)
}

// replayDecision interprets a durable state reached by an earlier equivalent
// request. A matching decision succeeds without replacing its audit fields. An
// opposite decision is rejected even if the earlier execution is still running
// or has already completed.
func (service Service) replayDecision(
	ctx context.Context,
	db *sql.DB,
	approval Approval,
	input DecisionInput,
	now int64,
) (DecisionResult, error) {
	switch approval.Status {
	case ApprovalDenied:
		if !input.Approve {
			return service.loadDecision(db, approval.ID, OutcomeDenied, now, true)
		}
	case ApprovalApproved:
		if input.Approve {
			return service.continueApproved(ctx, db, approval, input.Run, now, true)
		}
	case ApprovalRunning:
		if input.Approve {
			// The durable claim prevents a second execution, but it cannot prove
			// whether the external effect finished. Keep the long-standing explicit
			// conflict instead of turning an unknown outcome into reported success.
			return DecisionResult{}, conflictGuard(fmt.Sprintf(
				"approval %s is already executing; check the target yourself with `atm guard show %s`",
				approval.ID, approval.ID), approval.ID, approval.Status, nil)
		}
	case ApprovalDone:
		if input.Approve {
			return service.loadCompletedDecision(db, approval.ID, now, true)
		}
	}

	desired := ApprovalDenied
	if input.Approve {
		desired = ApprovalApproved
	}
	return DecisionResult{}, conflictGuard(fmt.Sprintf(
		"approval %s is already %s and cannot be changed to %s",
		approval.ID, approval.Status, desired), approval.ID, approval.Status, nil)
}

// recoverDecisionRace distinguishes a concurrent equivalent decision from a
// persistence failure. This matters when two authenticated clients retry at
// nearly the same time: one transition wins, and the other observes it as a
// replay instead of receiving a false conflict.
func (service Service) recoverDecisionRace(
	ctx context.Context,
	db *sql.DB,
	input DecisionInput,
	now int64,
	previousStatus string,
	cause error,
) (DecisionResult, error) {
	approval, err := store.GetApproval(db, input.ID, now)
	if err != nil {
		return DecisionResult{}, unavailableGuard("read concurrent Guard decision", err)
	}
	if approval != nil && approval.Status != ApprovalPending {
		return service.replayDecision(ctx, db, *approval, input, now)
	}
	return DecisionResult{}, decisionStoreError(input.ID, previousStatus, cause)
}

func (service Service) continueApproved(
	ctx context.Context,
	db *sql.DB,
	approval Approval,
	run bool,
	now int64,
	replayed bool,
) (DecisionResult, error) {
	if !run {
		return service.loadDecision(db, approval.ID, OutcomeApproved, now, replayed)
	}
	if approval.GateOwnsExecution(now) {
		return service.loadDecision(db, approval.ID, OutcomeApprovedGateRun, now, replayed)
	}
	if approval.StdinPiped {
		return DecisionResult{}, conflictGuard(fmt.Sprintf(
			"approval %s was given input on a pipe, which cannot be reproduced; it is approved but must be re-run by whoever composed it",
			approval.ID), approval.ID, ApprovalApproved, nil)
	}
	if err := service.executor.Validate(ctx, approval); err != nil {
		return DecisionResult{}, forbiddenDeferredExecution(approval.ID, err)
	}
	if err := guardContextError(ctx); err != nil {
		return DecisionResult{}, err
	}
	if err := store.ClaimApprovalRun(db, approval.ID, service.runner, service.pid()); err != nil {
		if errors.Is(err, store.ErrApprovalRunClaimLost) {
			// Another owner won the one durable execution claim. Reloading the row
			// makes a concurrent retry return running/done without re-executing.
			current, readErr := store.GetApproval(db, approval.ID, now)
			if readErr != nil {
				return DecisionResult{}, unavailableGuard("read claimed Guard execution", readErr)
			}
			if current == nil {
				return DecisionResult{}, notFoundApproval(approval.ID)
			}
			return service.replayDecision(ctx, db, *current, DecisionInput{
				ID: approval.ID, Approve: true, Run: run,
			}, now)
		}
		return DecisionResult{}, unavailableGuard("claim gated action execution", err)
	}

	execution := service.executor.Execute(ctx, approval)
	if err := store.FinishApproval(db, approval.ID, execution.ExitCode, execution.Output); err != nil {
		return DecisionResult{}, unavailableGuard("record gated action outcome", err)
	}
	return service.loadCompletedDecision(db, approval.ID, now, replayed)
}

func (service Service) loadCompletedDecision(
	db *sql.DB,
	id string,
	now int64,
	replayed bool,
) (DecisionResult, error) {
	result, err := service.loadDecision(db, id, OutcomeApprovedAndRan, now, replayed)
	if err != nil {
		return DecisionResult{}, err
	}
	if result.Approval.ExitCode != nil && *result.Approval.ExitCode != 0 {
		appErr := application.NewError(application.CodeUnavailable, fmt.Sprintf(
			"approved, but %s exited %d", result.Approval.Tool, *result.Approval.ExitCode))
		appErr.Details = map[string]any{
			"approval_id": id,
			"tool":        result.Approval.Tool,
			"exit_code":   *result.Approval.ExitCode,
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

func (service Service) loadDecision(
	db *sql.DB,
	id string,
	outcome DecisionOutcome,
	now int64,
	replayed bool,
) (DecisionResult, error) {
	approval, err := store.GetApproval(db, id, now)
	if err != nil {
		return DecisionResult{}, unavailableGuard("read Guard decision", err)
	}
	if approval == nil {
		return DecisionResult{}, notFoundApproval(id)
	}
	return DecisionResult{Approval: *approval, Outcome: outcome, Replayed: replayed}, nil
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
