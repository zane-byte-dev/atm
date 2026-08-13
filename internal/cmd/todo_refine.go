package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoAddRefineFlag     bool
	todoRefineDryRunFlag  bool
	todoRefineNoSplitFlag bool
	todoRefineMaxChildren int
)

func init() {
	todoAddCmd.Flags().BoolVar(&todoAddRefineFlag, "refine", false, "after creating the todo, polish it with the collection model and split it if it is complex")
	todoRefineCmd.Flags().BoolVar(&todoRefineDryRunFlag, "dry-run", false, "print the proposal without writing")
	todoRefineCmd.Flags().BoolVar(&todoRefineNoSplitFlag, "no-split", false, "polish title and description only; never create child todos")
	todoRefineCmd.Flags().IntVar(&todoRefineMaxChildren, "max-children", refine.DefaultMaxChildren, "maximum child todos to create when splitting")
	todoCmd.AddCommand(todoRefineCmd)
}

var todoRefineCmd = &cobra.Command{
	Use:   "refine [id]",
	Short: "Polish a todo and split it when the work is complex",
	Long: `Ask the collection model to rewrite a todo so it is ready to start.

The same isolated, schema-constrained CLI chain as collect (collection_model_command)
rewrites the title and the 需求 section. Complex work also gets a plan in 分析.
Independently trackable pieces become child todos the parent waits on.

This is one model call, not an Agent loop, and it never dispatches work.
in_progress todos are polished but not split, so an active session is not
unbound. Re-running refine will not mint a second set of children.

CLI todo add is never implicit — pass --refine. The desktop app runs this
automatically after a human files a todo when todo_refine_on_add is true.`,
	Example: `  atm todo refine t270
  atm todo refine t270 --dry-run
  atm todo refine t270 --no-split
  atm todo add "把发布检查修一下" --project atm --refine`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTodoRefine,
}

func runTodoRefine(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	return refineTodoByID(cmd, id, refine.Options{
		AllowSplit:  !todoRefineNoSplitFlag,
		MaxChildren: todoRefineMaxChildren,
	}, todoRefineDryRunFlag)
}

func refineTodoByID(cmd *cobra.Command, id string, opts refine.Options, dryRun bool) error {
	tf, todo, err := loadTodoByID(id)
	if err != nil {
		return err
	}
	if err := refine.CanRefine(*todo); err != nil {
		return err
	}

	card := ""
	if raw, err := store.ReadTodoDoc(todo.ID); err == nil {
		card = raw
	}

	prepared, proposal, err := refine.Analyze(context.Background(), *todo, card, len(refine.ExistingChildren(tf, todo.ID)), opts)
	if err != nil {
		return err
	}

	if dryRun {
		return reportRefine(cmd, *todo, prepared, nil, proposal, true)
	}
	if !refine.Changed(prepared) {
		return reportRefine(cmd, *todo, prepared, nil, proposal, false)
	}

	updated, children, err := applyTodoRefine(*todo, prepared)
	if err != nil {
		return err
	}
	return reportRefine(cmd, updated, prepared, children, proposal, false)
}

func applyTodoRefine(original store.Todo, prepared refine.Prepared) (store.Todo, []store.Todo, error) {
	var updated store.Todo
	var children []store.Todo
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf := transaction.Todos()
		parent, err := transaction.Todo(original.ID)
		if err != nil {
			return err
		}
		if prepared.TitleChanged {
			parent.Title = prepared.Title
		}
		if prepared.DescChanged {
			parent.Description = prepared.Description
		}

		if prepared.Split {
			creator, err := resolveTodoCreator("")
			if err != nil {
				return err
			}
			if creator == "" {
				creator = parent.Creator
			}
			created := make([]store.Todo, 0, len(prepared.Children))
			for _, spec := range prepared.Children {
				child := store.Todo{
					ID:          store.NextTodoID(tf),
					Title:       spec.Title,
					Description: spec.Description,
					Priority:    parent.Priority,
					Status:      store.TodoStatusOpen,
					Project:     parent.Project,
					Created:     store.Today(),
					Source:      refine.ChildSource(parent.ID),
					Creator:     creator,
				}
				tf.Items = append(tf.Items, child)
				created = append(created, child)
			}
			// Re-find after append: Items may have been reallocated.
			parent = store.FindTodo(tf, original.ID)
			if parent == nil {
				return fmt.Errorf("todo %s disappeared while creating refine children", original.ID)
			}
			for i, spec := range prepared.Children {
				for _, index := range spec.DependsOnIndexes {
					if index < 0 || index >= len(created) {
						continue
					}
					if err := store.AddTodoDependency(tf, created[i].ID, created[index].ID); err != nil {
						return err
					}
				}
				if err := store.AddTodoDependency(tf, parent.ID, created[i].ID); err != nil {
					return err
				}
			}
			if store.TodoIsActive(*parent) && len(store.UnmetTodoDependencies(tf, *parent)) > 0 {
				parent.Status = store.TodoStatusWaiting
				parent.WakeCondition = store.TodoDependencyWakeCondition(*parent)
			}
			// Refresh created rows from the file so DependsOn is what was stored.
			children = make([]store.Todo, 0, len(created))
			for _, child := range created {
				if latest := store.FindTodo(tf, child.ID); latest != nil {
					children = append(children, *latest)
				}
			}
		}

		updated = *parent
		return nil
	})
	if err != nil {
		return store.Todo{}, nil, err
	}

	ids := []string{updated.ID}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return store.Todo{}, nil, err
	}
	if err := ensureTodoDocs(tf, ids...); err != nil {
		return store.Todo{}, nil, err
	}
	if latest := store.FindTodo(tf, updated.ID); latest != nil {
		updated = *latest
	}

	if note := refine.FormatAnalysis(prepared, children); strings.TrimSpace(note) != "" {
		if err := validateTodoLogReferences(tf, note); err != nil {
			return store.Todo{}, nil, err
		}
		if _, err := store.AppendTodoLog(&updated, note, "分析"); err != nil {
			return store.Todo{}, nil, err
		}
	}
	return updated, children, nil
}

func reportRefine(cmd *cobra.Command, todo store.Todo, prepared refine.Prepared, children []store.Todo, proposal refine.Proposal, dryRun bool) error {
	payload := map[string]any{
		"todo":                todo,
		"complexity":          prepared.Complexity,
		"reason":              prepared.Reason,
		"title_changed":       prepared.TitleChanged,
		"description_changed": prepared.DescChanged,
		"split":               prepared.Split,
		"split_skip":          prepared.SplitSkip,
		"plan":                prepared.Plan,
		"children":            children,
		"dry_run":             dryRun,
		"changed":             refine.Changed(prepared),
	}
	if dryRun {
		payload["proposal"] = proposal
		payload["proposed_title"] = prepared.Title
		payload["proposed_description"] = prepared.Description
		payload["proposed_children"] = prepared.Children
	}
	if jsonOutput {
		output.JSON(payload)
		return nil
	}

	if dryRun {
		fmt.Fprintf(cmd.ErrOrStderr(), "Dry-run refine %s (%s)\n", todo.ID, prepared.Complexity)
	} else if !refine.Changed(prepared) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Refined %s: already clear\n", todo.ID)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Refined %s: %s\n", todo.ID, todo.Title)
	}
	if prepared.TitleChanged {
		fmt.Fprintf(cmd.ErrOrStderr(), "  Title: %s\n", prepared.Title)
	}
	if prepared.SplitSkip != "" && !prepared.Split {
		fmt.Fprintf(cmd.ErrOrStderr(), "  Split skipped: %s\n", prepared.SplitSkip)
	}
	if prepared.Split {
		ids := make([]string, 0, len(children))
		if dryRun {
			for i, child := range prepared.Children {
				ids = append(ids, fmt.Sprintf("#%d %s", i+1, child.Title))
			}
		} else {
			for _, child := range children {
				ids = append(ids, child.ID)
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "  Split into %s\n", strings.Join(ids, ", "))
	}
	return nil
}
