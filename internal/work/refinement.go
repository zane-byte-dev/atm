package work

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
)

type RefineInput struct {
	TodoID      string        `json:"todo_id"`
	AllowSplit  bool          `json:"allow_split"`
	MaxChildren int           `json:"max_children,omitempty"`
	Hint        string        `json:"hint,omitempty"`
	DryRun      bool          `json:"dry_run,omitempty"`
	Timeout     time.Duration `json:"-"`
}

type RefineResult struct {
	Todo     Todo            `json:"todo"`
	Prepared refine.Prepared `json:"prepared"`
	Proposal refine.Proposal `json:"proposal"`
	Children []Todo          `json:"children"`
	DryRun   bool            `json:"dry_run"`
	Changed  bool            `json:"changed"`
	Effects  []Effect        `json:"-"`
}

type RefinementModelInput struct {
	Todo    Todo
	Card    string
	Options refine.Options
}

type RefinementModelOutput struct {
	Proposal refine.Proposal
	Source   string
}

// RefinementModel is the outbound port for the one schema-constrained model
// call. It returns an untrusted proposal; Work owns all post-model policy and
// persistence decisions.
type RefinementModel interface {
	Propose(context.Context, RefinementModelInput) (RefinementModelOutput, error)
}

type RefinementModelFunc func(context.Context, RefinementModelInput) (RefinementModelOutput, error)

func (propose RefinementModelFunc) Propose(ctx context.Context, input RefinementModelInput) (RefinementModelOutput, error) {
	return propose(ctx, input)
}

type builtInRefinementModel struct{}

func (builtInRefinementModel) Propose(
	ctx context.Context,
	input RefinementModelInput,
) (RefinementModelOutput, error) {
	proposal, source, err := refine.Propose(ctx, input.Todo, input.Card, input.Options)
	return RefinementModelOutput{Proposal: proposal, Source: source}, err
}

func (service Service) refinementModel() RefinementModel {
	if service.RefinementModel != nil {
		return service.RefinementModel
	}
	return builtInRefinementModel{}
}

// Refine analyzes one active Todo outside the write lock, then atomically
// verifies the analyzed snapshot and applies the rewrite, split graph, binding
// policy and durable document projection.
func (service Service) Refine(
	ctx context.Context,
	call application.Call,
	input RefineInput,
) (RefineResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateLifecycleCall(ctx, call); err != nil {
		return RefineResult{}, err
	}
	if err := validateLifecycleTodoID(input.TodoID); err != nil {
		return RefineResult{}, err
	}
	// The fixed model contract returns at most five children. Rejecting values
	// outside that boundary keeps transports from presenting an arbitrary fan-out
	// knob that the model prompt and Work graph policy do not actually support.
	// Zero means the documented default.
	if input.MaxChildren < 0 || input.MaxChildren > refine.DefaultMaxChildren {
		return RefineResult{}, lifecycleInvalidArgument(
			fmt.Sprintf("max_children must be between 0 and %d", refine.DefaultMaxChildren),
			"max_children", input.MaxChildren,
		)
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return RefineResult{}, refinementUnavailable("load todo for refinement", err)
	}
	parent := store.FindTodo(todos, input.TodoID)
	if parent == nil {
		return RefineResult{}, lifecycleTodoNotFound(input.TodoID, store.TodoNotFoundError(todos, input.TodoID))
	}
	if err := refine.CanRefine(*parent); err != nil {
		return RefineResult{}, lifecycleConflict(err.Error(), parent.ID, parent.Status)
	}
	baseParent := cloneTodo(*parent)
	baseChildIDs := refineChildIDs(todos, parent.ID)
	card := ""
	if raw, readErr := store.ReadTodoDoc(parent.ID); readErr == nil {
		card = raw
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return RefineResult{}, refinementUnavailable("read todo card for refinement", readErr)
	}
	options := refine.NormalizeOptions(refine.Options{
		AllowSplit:  input.AllowSplit,
		MaxChildren: input.MaxChildren,
		Hint:        input.Hint,
		Timeout:     input.Timeout,
	})
	modelOutput, err := service.refinementModel().Propose(ctx, RefinementModelInput{
		Todo: cloneTodo(baseParent), Card: card, Options: options,
	})
	if err != nil {
		return RefineResult{}, refinementUnavailable("analyze todo refinement", err)
	}
	prepared, err := refine.Prepare(baseParent, len(baseChildIDs), modelOutput.Proposal, options)
	if err != nil {
		return RefineResult{}, refinementUnavailable("model returned an invalid todo refinement", err)
	}
	prepared.Source = modelOutput.Source
	result := RefineResult{
		Todo: cloneTodo(baseParent), Prepared: prepared, Proposal: modelOutput.Proposal,
		DryRun: input.DryRun, Changed: refine.Changed(prepared),
	}
	if input.DryRun {
		return result, nil
	}
	if !result.Changed {
		result.Effects, err = pendingRefinementEffects(baseParent.ID)
		if err != nil {
			return RefineResult{}, refinementUnavailable("load pending refinement projection", err)
		}
		return result, nil
	}

	err = service.Mutate(func(transaction *Transaction) error {
		current, findErr := transaction.Todo(baseParent.ID)
		if findErr != nil {
			return lifecycleTodoNotFound(baseParent.ID, findErr)
		}
		if !reflect.DeepEqual(cloneTodo(*current), baseParent) ||
			!reflect.DeepEqual(refineChildIDs(transaction.Todos(), current.ID), baseChildIDs) {
			conflict := application.NewError(
				application.CodeConflict,
				fmt.Sprintf("todo %s changed while its refinement was being generated; retry against the current version", current.ID),
			)
			conflict.Details = map[string]any{"todo_id": current.ID}
			return conflict
		}
		if err := refine.CanRefine(*current); err != nil {
			return lifecycleConflict(err.Error(), current.ID, current.Status)
		}

		if prepared.TitleChanged {
			current.Title = prepared.Title
		}
		if prepared.DescChanged {
			current.Description = prepared.Description
		}

		children := make([]Todo, 0, len(prepared.Children))
		if prepared.Split {
			creator, creatorErr := normalizeCreator(call, "")
			if creatorErr != nil {
				return creatorErr
			}
			for _, spec := range prepared.Children {
				child := Todo{
					ID: store.NextTodoID(transaction.Todos()), Title: spec.Title, Description: spec.Description,
					Priority: current.Priority, Status: store.TodoStatusOpen, Project: current.Project,
					Created: store.Today(), Source: refine.ChildSource(current.ID), Creator: creator,
				}
				transaction.Todos().Items = append(transaction.Todos().Items, child)
				children = append(children, child)
			}
			current = store.FindTodo(transaction.Todos(), baseParent.ID)
			if current == nil {
				return fmt.Errorf("todo %s disappeared while creating refine children", baseParent.ID)
			}
			for childIndex, spec := range prepared.Children {
				for _, dependencyIndex := range spec.DependsOnIndexes {
					if dependencyIndex < 0 || dependencyIndex >= len(children) {
						continue
					}
					if err := store.AddTodoDependency(
						transaction.Todos(), children[childIndex].ID, children[dependencyIndex].ID,
					); err != nil {
						return err
					}
				}
				if err := store.AddTodoDependency(transaction.Todos(), current.ID, children[childIndex].ID); err != nil {
					return err
				}
			}
			if current.Status == store.TodoStatusInProgress && len(store.UnmetTodoDependencies(transaction.Todos(), *current)) > 0 {
				current.WakeCondition = store.TodoDependencyWakeCondition(*current)
				current.ReviewAt = ""
				if _, err := transaction.UnbindTodoSessions(current.ID, "refine:waiting"); err != nil {
					return fmt.Errorf("unbind refined todo sessions: %w", err)
				}
			}
			for index := range children {
				if latest := store.FindTodo(transaction.Todos(), children[index].ID); latest != nil {
					children[index] = cloneTodo(*latest)
				}
			}
		}

		analysis := refine.FormatAnalysis(prepared, children)
		if strings.TrimSpace(analysis) != "" {
			if err := store.ValidateTodoLogMessage(analysis, "分析"); err != nil {
				return lifecycleInvalidArgument(err.Error(), "proposal", modelOutput.Proposal)
			}
			if unknown := store.UnknownTodoReferences(transaction.Todos(), analysis); len(unknown) > 0 {
				err := lifecycleInvalidArgument(
					"refinement analysis references missing todos: "+strings.Join(unknown, ", "),
					"proposal", modelOutput.Proposal,
				)
				err.Details["unknown_todo_ids"] = unknown
				return err
			}
		}
		result.Todo = cloneTodo(*current)
		result.Children = append([]Todo(nil), children...)
		projectionChildren := refine.ExistingChildren(transaction.Todos(), current.ID)
		if err := transaction.enqueueOrReplaceRefinementEffect(call, *current, projectionChildren, analysis); err != nil {
			return fmt.Errorf("enqueue refinement projection: %w", err)
		}
		pending, pendingErr := transaction.pendingEffects(current.ID)
		if pendingErr != nil {
			return pendingErr
		}
		result.Effects = filterRefinementEffects(pending)
		return nil
	})
	if err != nil {
		return RefineResult{}, lifecycleApplicationError("apply todo refinement", err)
	}
	return result, nil
}

func pendingRefinementEffects(todoID string) ([]Effect, error) {
	records, err := store.ListPendingWorkEffects(todoID)
	if err != nil {
		return nil, err
	}
	effects, err := decodeEffects(records)
	if err != nil {
		return nil, err
	}
	return filterRefinementEffects(effects), nil
}

func filterRefinementEffects(effects []Effect) []Effect {
	result := make([]Effect, 0, len(effects))
	for _, effect := range effects {
		if effect.Kind == EffectTodoRefined {
			result = append(result, effect)
		}
	}
	return result
}

func refineChildIDs(todos *store.TodoFile, parentID string) []string {
	children := refine.ExistingChildren(todos, parentID)
	ids := make([]string, 0, len(children))
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	return ids
}

func refinementUnavailable(message string, cause error) *application.Error {
	var appErr *application.Error
	if errors.As(cause, &appErr) {
		return appErr
	}
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}
