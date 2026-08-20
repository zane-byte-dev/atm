package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/refine"
	workapp "github.com/zane-byte-dev/atm/internal/work"
	"gopkg.in/yaml.v3"
)

// These structs are the YAML/JSON input grammar of the CLI adapter. Work owns
// their business defaults and validation after the adapter has applied Cobra's
// flag-over-file precedence.
type batchInput struct {
	Project  string      `yaml:"project" json:"project"`
	Source   string      `yaml:"source" json:"source"`
	Creator  string      `yaml:"creator" json:"creator"`
	Priority string      `yaml:"priority" json:"priority"`
	Status   string      `yaml:"status" json:"status"`
	Items    []batchItem `yaml:"items" json:"items"`
}

type batchItem struct {
	Title         string `yaml:"title" json:"title"`
	Desc          string `yaml:"desc" json:"desc"`
	Priority      string `yaml:"priority" json:"priority"`
	Project       string `yaml:"project" json:"project"`
	Source        string `yaml:"source" json:"source"`
	Creator       string `yaml:"creator" json:"creator"`
	Status        string `yaml:"status" json:"status"`
	WakeCondition string `yaml:"wake" json:"wake"`
	ReviewAt      string `yaml:"review_at" json:"review_at"`
}

func runTodoAdd(cmd *cobra.Command, args []string) error {
	if todoBatchFlag {
		if todoDescFileFlag != "" {
			return fmt.Errorf("--batch and --desc-file cannot be used together because both may read stdin")
		}
		return runTodoBatchAdd(cmd)
	}
	if len(args) == 0 {
		return fmt.Errorf("requires at least 1 arg(s), use --batch for batch input")
	}

	title := strings.Join(args, " ")
	if len([]rune(title)) < 8 && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Warning: title is very short (%d chars), consider being more descriptive\n", len([]rune(title)))
	}
	description, err := todoAddDescription(cmd)
	if err != nil {
		return err
	}
	source := todoSourceFlag
	if source == "" {
		source = todoSourceFromSession()
	}
	call := cliApplicationCall("todo-add", "")
	result, err := workapp.Default.Add(cmd.Context(), call, workapp.AddInput{
		Title:         title,
		Description:   description,
		Priority:      todoAddPriorityFlag,
		Status:        todoAddStatusFlag,
		Project:       todoAddProjectFlag,
		WakeCondition: todoAddWakeFlag,
		ReviewAt:      todoAddReviewAtFlag,
		Source:        source,
		Creator:       todoAddCreatorFlag,
		OnDone:        todoOnDoneFlag,
		ImagePaths:    todoAddImageFlags,
	})
	if err != nil {
		return err
	}
	deliverTodoMetadataEffects(result.Effects)

	// Refinement remains a separate adapter action: its own use case may split
	// the just-created Todo, while Add is deliberately one metadata mutation.
	if todoAddRefineFlag {
		if !jsonOutput {
			fmt.Println(result.Todo.ID)
			fmt.Fprintf(cmd.ErrOrStderr(), "Created %s: %s\n", result.Todo.ID, result.Todo.Title)
		}
		if err := refineTodoByID(cmd, result.Todo.ID, refine.Options{AllowSplit: true}, false); err != nil {
			if jsonOutput {
				output.JSON(map[string]any{"todo": result.Todo, "refine_error": err.Error()})
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Refine failed: %v\n", err)
			}
			return err
		}
		return nil
	}

	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	fmt.Println(result.Todo.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created %s: %s\n", result.Todo.ID, result.Todo.Title)
	return nil
}

func todoAddDescription(cmd *cobra.Command) (string, error) {
	description, err := readBodyFlagOrFile(cmd, "desc", todoDescFlag, todoDescFileFlag)
	if err != nil {
		return "", err
	}
	if cmd.Flags().Changed("desc") {
		if err := validateInlineTodoDescription(description); err != nil {
			return "", err
		}
	}
	return description, nil
}

func runTodoBatchAdd(cmd *cobra.Command) error {
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("no input from stdin")
	}

	var batch batchInput
	if err := yaml.Unmarshal(data, &batch); err != nil {
		return fmt.Errorf("parsing batch input: %w", err)
	}
	if len(batch.Items) == 0 {
		return fmt.Errorf("no items in batch input")
	}

	// Cobra flags override batch-level defaults; Work supplies the hard defaults
	// and validates both defaults and item overrides.
	defaultPriority := batch.Priority
	if defaultPriority == "" {
		defaultPriority = "P1"
	}
	if todoAddPriorityFlag != "P1" {
		defaultPriority = todoAddPriorityFlag
	}
	defaultProject := batch.Project
	if todoAddProjectFlag != "" {
		defaultProject = todoAddProjectFlag
	}
	defaultStatus := batch.Status
	if defaultStatus == "" {
		defaultStatus = "open"
	}
	if todoAddStatusFlag != "open" {
		defaultStatus = todoAddStatusFlag
	}
	defaultSource := batch.Source
	if todoSourceFlag != "" {
		defaultSource = todoSourceFlag
	}
	if defaultSource == "" {
		defaultSource = todoSourceFromSession()
	}
	defaultCreator := emptyAs(todoAddCreatorFlag, batch.Creator)

	items := make([]workapp.BatchAddItem, len(batch.Items))
	for index, item := range batch.Items {
		items[index] = workapp.BatchAddItem{
			Title:         item.Title,
			Description:   item.Desc,
			Priority:      item.Priority,
			Project:       item.Project,
			Source:        item.Source,
			Creator:       item.Creator,
			Status:        item.Status,
			WakeCondition: item.WakeCondition,
			ReviewAt:      item.ReviewAt,
		}
	}
	result, err := workapp.Default.BatchAdd(cmd.Context(), cliApplicationCall("todo-batch-add", ""), workapp.BatchAddInput{
		Defaults: workapp.BatchAddDefaults{
			Priority: defaultPriority,
			Status:   defaultStatus,
			Project:  defaultProject,
			Source:   defaultSource,
			Creator:  defaultCreator,
		},
		Items: items,
	})
	if err != nil {
		return err
	}
	deliverTodoMetadataEffects(result.Effects)

	if jsonOutput {
		output.JSON(result.Todos)
		return nil
	}
	for _, todo := range result.Todos {
		fmt.Printf("Added %s: %s\n", todo.ID, todo.Title)
	}
	fmt.Fprintf(os.Stderr, "(%d items added)\n", len(result.Todos))
	return nil
}

func runTodoEdit(cmd *cobra.Command, args []string) error {
	patch := workapp.EditPatch{}
	if cmd.Flags().Changed("title") {
		patch.Title = stringValue(todoEditTitleFlag)
	}
	if cmd.Flags().Changed("desc") || cmd.Flags().Changed("desc-file") {
		description, err := readBodyFlagOrFile(cmd, "desc", todoEditDescFlag, todoEditDescFileFlag)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("desc") {
			if err := validateInlineTodoDescription(description); err != nil {
				return err
			}
		}
		patch.Description = stringValue(description)
	}
	if cmd.Flags().Changed("priority") {
		patch.Priority = stringValue(todoEditPriorityFlag)
	}
	if cmd.Flags().Changed("project") {
		patch.Project = stringValue(todoEditProjectFlag)
	}
	if cmd.Flags().Changed("source") {
		patch.Source = stringValue(todoEditSourceFlag)
	}
	if cmd.Flags().Changed("status") {
		patch.Status = stringValue(todoEditStatusFlag)
	}
	if cmd.Flags().Changed("wake") {
		patch.WakeCondition = stringValue(todoEditWakeFlag)
	}
	if cmd.Flags().Changed("review-at") {
		patch.ReviewAt = stringValue(todoEditReviewAtFlag)
	}

	result, err := workapp.Default.Edit(cmd.Context(), cliApplicationCall("todo-edit", ""), workapp.EditInput{
		TodoID: args[0],
		Patch:  patch,
	})
	if err != nil {
		return err
	}
	deliverTodoMetadataEffects(result.Effects)
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	fmt.Printf("Updated %s: %s\n", result.Todo.ID, result.Todo.Title)
	return nil
}

func runTodoMove(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.Move(cmd.Context(), cliApplicationCall("todo-move", ""), workapp.MoveInput{
		TodoID:  args[0],
		Project: todoMoveProjectFlag,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	if result.PreviousProject != "" {
		fmt.Printf("Moved %s: %s → %s\n", result.Todo.ID, result.PreviousProject, result.Todo.Project)
	} else {
		fmt.Printf("Moved %s → %s\n", result.Todo.ID, result.Todo.Project)
	}
	return nil
}

func deliverTodoMetadataEffects(effects []workapp.MetadataEffect) {
	for _, effect := range effects {
		todo := effect.Todo
		switch effect.Kind {
		case workapp.MetadataEffectCreated:
			notifyTodoEvent(&todo, notifyEventCreated)
		case workapp.MetadataEffectEnteredReview:
			notifyTodoEvent(&todo, notifyEventReview)
		}
	}
}

func stringValue(value string) *string {
	copy := value
	return &copy
}
