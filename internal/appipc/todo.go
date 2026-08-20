package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/work"
)

// TodoListRequest is the desktop's bounded browse/search intent. Priority,
// creator and project filters remain available through the public CLI, but are
// not parameters of an App screen today.
type TodoListRequest struct {
	Status string `json:"status,omitempty"`
	Query  string `json:"query,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type TodoIDRequest struct {
	TodoID string `json:"todo_id"`
}

type TodoShowResponse struct {
	Todo     work.Todo                 `json:"todo"`
	Bindings []work.TodoSessionBinding `json:"bindings,omitempty"`
	Sessions []work.TodoBoundSession   `json:"sessions,omitempty"`
}

type TodoDocumentResponse struct {
	Exists  bool   `json:"exists"`
	Content string `json:"content,omitempty"`
}

// TodoCreateRequest deliberately fixes new App Todos to the normal open/human
// workflow. Status, creator, source and on-done commands are not accepted over
// this method merely because the broader CLI AddInput supports them.
type TodoCreateRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Project     string   `json:"project,omitempty"`
	ImagePaths  []string `json:"image_paths,omitempty"`
}

// TodoUpdateRequest is a sparse metadata patch. Nil means omitted; a pointer to
// an empty string explicitly clears that field. Creator and destructive or
// retention operations are outside this method.
type TodoUpdateRequest struct {
	TodoID        string  `json:"todo_id"`
	Title         *string `json:"title,omitempty"`
	Description   *string `json:"description,omitempty"`
	Priority      *string `json:"priority,omitempty"`
	Project       *string `json:"project,omitempty"`
	Status        *string `json:"status,omitempty"`
	WakeCondition *string `json:"wake_condition,omitempty"`
	ReviewAt      *string `json:"review_at,omitempty"`
	Source        *string `json:"source,omitempty"`
}

// TodoRefineRequest is the complete, bounded refinement policy available to
// the App. It cannot choose a model endpoint, timeout, arbitrary argv or an
// executor; Work owns those details and validates the graph fan-out.
type TodoRefineRequest struct {
	TodoID      string `json:"todo_id"`
	AllowSplit  bool   `json:"allow_split"`
	MaxChildren int    `json:"max_children,omitempty"`
	Hint        string `json:"hint,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

type TodoRefineChild struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	DependsOnIndexes []int  `json:"depends_on_indexes"`
}

type TodoRefineProposal struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Complexity  string            `json:"complexity"`
	Plan        string            `json:"plan"`
	Reason      string            `json:"reason"`
	Children    []TodoRefineChild `json:"children"`
}

// TodoRefineResponse keeps the established useful result vocabulary without
// leaking Work effects or model transport details onto the wire. Proposal
// fields are present only for an explicit dry run.
type TodoRefineResponse struct {
	Todo                work.Todo           `json:"todo"`
	Complexity          string              `json:"complexity"`
	Reason              string              `json:"reason,omitempty"`
	TitleChanged        bool                `json:"title_changed"`
	DescriptionChanged  bool                `json:"description_changed"`
	Split               bool                `json:"split"`
	SplitSkip           string              `json:"split_skip,omitempty"`
	Plan                string              `json:"plan,omitempty"`
	Children            []work.Todo         `json:"children"`
	DryRun              bool                `json:"dry_run"`
	Changed             bool                `json:"changed"`
	Source              string              `json:"source,omitempty"`
	Proposal            *TodoRefineProposal `json:"proposal,omitempty"`
	ProposedTitle       string              `json:"proposed_title,omitempty"`
	ProposedDescription string              `json:"proposed_description,omitempty"`
	ProposedChildren    []TodoRefineChild   `json:"proposed_children,omitempty"`
}

func registerTodo(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "todo.list", func(
		ctx context.Context,
		call application.Call,
		input TodoListRequest,
	) ([]work.Todo, error) {
		result, err := dependencies.Work.List(ctx, call, work.ListInput{
			Status: input.Status, Query: input.Query, Limit: input.Limit, Offset: input.Offset,
		})
		if err != nil {
			return nil, err
		}
		if result.Kind != work.ListKindArchived {
			return result.Todos, nil
		}
		todos := make([]work.Todo, 0, len(result.Archived))
		for _, archived := range result.Archived {
			todos = append(todos, archived.Todo)
		}
		return todos, nil
	})
	bind(registry, "todo.show", func(
		ctx context.Context,
		call application.Call,
		input TodoIDRequest,
	) (TodoShowResponse, error) {
		result, err := dependencies.Work.Show(ctx, call, work.ShowInput{TodoID: input.TodoID})
		if err != nil {
			return TodoShowResponse{}, err
		}
		return TodoShowResponse{Todo: result.Todo, Bindings: result.Bindings, Sessions: result.Sessions}, nil
	})
	bind(registry, "todo.doc", func(
		ctx context.Context,
		call application.Call,
		input TodoIDRequest,
	) (TodoDocumentResponse, error) {
		// The App method is a document read. It cannot opt into Doc's explicit
		// initialize mutation by smuggling an extra flag into the request.
		result, err := dependencies.Work.Doc(ctx, call, work.DocInput{TodoID: input.TodoID})
		if err != nil {
			return TodoDocumentResponse{}, err
		}
		return TodoDocumentResponse{Exists: result.Exists, Content: result.Content}, nil
	})
	bind(registry, "todo.create", func(
		ctx context.Context,
		call application.Call,
		input TodoCreateRequest,
	) (work.Todo, error) {
		result, err := dependencies.Work.Add(ctx, call, work.AddInput{
			Title: input.Title, Description: input.Description, Priority: input.Priority,
			Project: input.Project, ImagePaths: input.ImagePaths,
		})
		if err != nil {
			return work.Todo{}, err
		}
		// ATMCommandRunner disables child-process notifications for App calls;
		// the desktop's own optimistic state/refresh diff owns their presentation.
		return result.Todo, nil
	})
	bind(registry, "todo.update", func(
		ctx context.Context,
		call application.Call,
		input TodoUpdateRequest,
	) (work.Todo, error) {
		result, err := dependencies.Work.Edit(ctx, call, work.EditInput{
			TodoID: input.TodoID,
			Patch: work.EditPatch{
				Title: input.Title, Description: input.Description, Priority: input.Priority,
				Project: input.Project, Status: input.Status, WakeCondition: input.WakeCondition,
				ReviewAt: input.ReviewAt, Source: input.Source,
			},
		})
		if err != nil {
			return work.Todo{}, err
		}
		return result.Todo, nil
	})
	bind(registry, "todo.refine", func(
		ctx context.Context,
		call application.Call,
		input TodoRefineRequest,
	) (TodoRefineResponse, error) {
		result, err := dependencies.Work.Refine(ctx, call, work.RefineInput{
			TodoID: input.TodoID, AllowSplit: input.AllowSplit, MaxChildren: input.MaxChildren,
			Hint: input.Hint, DryRun: input.DryRun,
		})
		if err != nil {
			return TodoRefineResponse{}, err
		}
		if !result.DryRun && len(result.Effects) > 0 {
			if err := dependencies.Work.DeliverEffects(ctx, call, result.Effects, dependencies.WorkEffects); err != nil {
				return TodoRefineResponse{}, err
			}
		}
		response := TodoRefineResponse{
			Todo: result.Todo, Complexity: result.Prepared.Complexity, Reason: result.Prepared.Reason,
			TitleChanged: result.Prepared.TitleChanged, DescriptionChanged: result.Prepared.DescChanged,
			Split: result.Prepared.Split, SplitSkip: result.Prepared.SplitSkip, Plan: result.Prepared.Plan,
			// Work deliberately leaves Children nil when a pass is dry-run or does
			// not split. The wire contract is an array in every response so Swift
			// callers do not have to treat "no children" as a schema variant.
			Children: append([]work.Todo{}, result.Children...),
			DryRun:   result.DryRun, Changed: result.Changed,
			Source: result.Prepared.Source,
		}
		if result.DryRun {
			proposal := todoRefineProposal(result.Proposal)
			response.Proposal = &proposal
			response.ProposedTitle = result.Prepared.Title
			response.ProposedDescription = result.Prepared.Description
			if len(result.Prepared.Children) > 0 {
				response.ProposedChildren = todoRefineChildren(result.Prepared.Children)
			}
		}
		return response, nil
	})
}

func todoRefineProposal(proposal refine.Proposal) TodoRefineProposal {
	return TodoRefineProposal{
		Title: proposal.Title, Description: proposal.Description, Complexity: proposal.Complexity,
		Plan: proposal.Plan, Reason: proposal.Reason, Children: todoRefineChildren(proposal.Children),
	}
}

func todoRefineChildren(children []refine.Child) []TodoRefineChild {
	result := make([]TodoRefineChild, 0, len(children))
	for _, child := range children {
		result = append(result, TodoRefineChild{
			Title: child.Title, Description: child.Description,
			DependsOnIndexes: append([]int{}, child.DependsOnIndexes...),
		})
	}
	return result
}
