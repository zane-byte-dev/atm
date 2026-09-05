package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/refine"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoAddRefineFlag     bool
	todoRefineDryRunFlag  bool
	todoRefineNoSplitFlag bool
	todoRefineMaxChildren int
	todoRefineHintFlag    string
)

func init() {
	todoAddCmd.Flags().BoolVar(&todoAddRefineFlag, "refine", false, "after creating the todo, polish it with ATM's built-in DeepSeek text model and split it if it is complex")
	todoRefineCmd.Flags().BoolVar(&todoRefineDryRunFlag, "dry-run", false, "print the proposal without writing")
	todoRefineCmd.Flags().BoolVar(&todoRefineNoSplitFlag, "no-split", false, "polish title and description only; never create child todos")
	todoRefineCmd.Flags().IntVar(&todoRefineMaxChildren, "max-children", refine.DefaultMaxChildren, "maximum child todos to create when splitting")
	todoRefineCmd.Flags().StringVar(&todoRefineHintFlag, "hint", "", "one-shot request for this pass, e.g. \"拆细一点\" or \"补上验收标准\"")
	todoCmd.AddCommand(todoRefineCmd)
}

var todoRefineCmd = &cobra.Command{
	Use:   "refine [id]",
	Short: "Polish a todo and split it when the work is complex",
	Long: `Ask ATM's built-in DeepSeek text-model service to rewrite a todo so it is ready to start.

The dedicated schema-constrained model rewrites the title and the 需求 section.
Complex work also gets a plan in 分析.
Independently trackable pieces become child todos the parent waits on.

This is one API call, not an Agent loop, and it never dispatches work. It shares
its credential, model and endpoint with collection classification and digests.
In the browser workspace, configure Settings > Model; CLI users can set
DEEPSEEK_API_KEY. The default model is deepseek-v4-flash with
thinking disabled. Config or ATM_TEXT_MODEL_* can override model and endpoint.
Every written analysis records "from <text_model_source>". Optional
todo_refine_prompt guidance is appended after ATM's fixed safety and JSON rules.
in_progress todos are polished but not split, so an active session is not
unbound. Re-running refine will not mint a second set of children.

A bare second pass usually reports "already clear": the card is already
structured, so the model returns the same text. Pass --hint to say what this
pass should change instead.

Refining is always asked for. CLI todo add needs --refine; the browser workspace
only runs this when a human triggers it, or on add when the opt-in
todo_refine_on_add is turned on.`,
	Example: `  atm todo refine t270
  atm todo refine t270 --dry-run
  atm todo refine t270 --no-split
  atm todo refine t270 --hint "把验收标准写成可观察行为"
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
		Hint:        todoRefineHintFlag,
	}, todoRefineDryRunFlag)
}

func refineTodoByID(cmd *cobra.Command, id string, opts refine.Options, dryRun bool) error {
	call := todoWorkflowCLICall("refine")
	result, err := workapp.Default.Refine(cmd.Context(), call, workapp.RefineInput{
		TodoID: id, AllowSplit: opts.AllowSplit, MaxChildren: opts.MaxChildren,
		Hint: opts.Hint, DryRun: dryRun, Timeout: opts.Timeout,
	})
	if err != nil {
		return err
	}
	if !dryRun {
		if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
			return err
		}
	}
	return reportRefine(cmd, result)
}

func reportRefine(cmd *cobra.Command, result workapp.RefineResult) error {
	prepared := result.Prepared
	payload := map[string]any{
		"todo":                result.Todo,
		"complexity":          prepared.Complexity,
		"reason":              prepared.Reason,
		"title_changed":       prepared.TitleChanged,
		"description_changed": prepared.DescChanged,
		"split":               prepared.Split,
		"split_skip":          prepared.SplitSkip,
		"plan":                prepared.Plan,
		"children":            result.Children,
		"dry_run":             result.DryRun,
		"changed":             result.Changed,
		"source":              prepared.Source,
	}
	if result.DryRun {
		payload["proposal"] = result.Proposal
		payload["proposed_title"] = prepared.Title
		payload["proposed_description"] = prepared.Description
		payload["proposed_children"] = prepared.Children
	}
	if jsonOutput {
		output.JSON(payload)
		return nil
	}

	if result.DryRun {
		fmt.Fprintf(cmd.ErrOrStderr(), "Dry-run refine %s (%s)\n", result.Todo.ID, prepared.Complexity)
	} else if !result.Changed {
		fmt.Fprintf(cmd.ErrOrStderr(), "Refined %s: already clear\n", result.Todo.ID)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Refined %s: %s\n", result.Todo.ID, result.Todo.Title)
	}
	if prepared.TitleChanged {
		fmt.Fprintf(cmd.ErrOrStderr(), "  Title: %s\n", prepared.Title)
	}
	if prepared.SplitSkip != "" && !prepared.Split {
		fmt.Fprintf(cmd.ErrOrStderr(), "  Split skipped: %s\n", prepared.SplitSkip)
	}
	if prepared.Split {
		ids := make([]string, 0, len(result.Children))
		if result.DryRun {
			for i, child := range prepared.Children {
				ids = append(ids, fmt.Sprintf("#%d %s", i+1, child.Title))
			}
		} else {
			for _, child := range result.Children {
				ids = append(ids, child.ID)
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "  Split into %s\n", strings.Join(ids, ", "))
	}
	return nil
}
