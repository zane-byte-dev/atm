package cmd

import (
	"bytes"
	"errors"
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
	Items    []batchItem `yaml:"items" json:"items"`
}

type batchItem struct {
	Title    string `yaml:"title" json:"title"`
	Desc     string `yaml:"desc" json:"desc"`
	Priority string `yaml:"priority" json:"priority"`
	Project  string `yaml:"project" json:"project"`
	Source   string `yaml:"source" json:"source"`
	Creator  string `yaml:"creator" json:"creator"`
}

// batchGrammarMessage keeps the decoder's line numbers, which are the useful
// part of a rejected batch, while naming the grammar the way the file's author
// sees it. Unmodified, KnownFields reports the Go type — "field status not found
// in type cmd.batchItem" tells a reader nothing they can act on.
func batchGrammarMessage(err error) string {
	message := strings.TrimPrefix(err.Error(), "yaml: unmarshal errors:\n")
	for typeName, grammarName := range map[string]string{
		"cmd.batchItem":  "a batch item (title, desc, priority, project, source, creator)",
		"cmd.batchInput": "batch input (project, source, creator, priority, items)",
	} {
		message = strings.ReplaceAll(message, "in type "+typeName, "in "+grammarName)
	}
	return strings.TrimSpace(message)
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
		Title:       title,
		Description: description,
		Priority:    todoAddPriorityFlag,
		Project:     todoAddProjectFlag,
		Source:      source,
		Creator:     todoAddCreatorFlag,
		OnDone:      todoOnDoneFlag,
		ImagePaths:  todoAddImageFlags,
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

	// KnownFields, so a key this grammar does not have is an error rather than a
	// silent omission. `status:` and `wake:` used to be accepted here; creation is
	// fixed to open now, and quietly dropping them would turn an old batch file
	// into a pile of plain open Todos that looks like it worked.
	var batch batchInput
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&batch); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("no input from stdin")
		}
		return fmt.Errorf("parsing batch input: %s", batchGrammarMessage(err))
	}
	if len(batch.Items) == 0 {
		return fmt.Errorf("no items in batch input")
	}

	// Cobra flags override batch-level defaults; Work supplies the hard defaults
	// and validates both defaults and item overrides.
	defaultPriority := batch.Priority
	if defaultPriority == "" {
		defaultPriority = "P2"
	}
	if cmd.Flags().Changed("priority") {
		defaultPriority = todoAddPriorityFlag
	}
	defaultProject := batch.Project
	if todoAddProjectFlag != "" {
		defaultProject = todoAddProjectFlag
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
			Title:       item.Title,
			Description: item.Desc,
			Priority:    item.Priority,
			Project:     item.Project,
			Source:      item.Source,
			Creator:     item.Creator,
		}
	}
	result, err := workapp.Default.BatchAdd(cmd.Context(), cliApplicationCall("todo-batch-add", ""), workapp.BatchAddInput{
		Defaults: workapp.BatchAddDefaults{
			Priority: defaultPriority,
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
	if cmd.Flags().Changed("status") {
		return runTodoReturnToOpen(cmd, args[0])
	}
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
	if cmd.Flags().Changed("creator") {
		patch.Creator = stringValue(todoEditCreatorFlag)
	}
	if cmd.Flags().Changed("wake") {
		patch.WakeCondition = stringValue(todoEditWakeFlag)
	}
	if cmd.Flags().Changed("review-at") {
		patch.ReviewAt = stringValue(todoEditReviewAtFlag)
	}
	if cmd.Flags().Changed("maintenance-limit") {
		patch.MaintenanceLimit = intValue(todoEditMaintenanceLimitFlag)
	}

	result, err := workapp.Default.Edit(cmd.Context(), cliApplicationCall("todo-edit", ""), workapp.EditInput{
		TodoID: args[0],
		Patch:  patch,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	fmt.Printf("Updated %s: %s\n", result.Todo.ID, result.Todo.Title)
	return nil
}

func runTodoReturnToOpen(cmd *cobra.Command, rawID string) error {
	id := canonicalTodoIDForHint(rawID)
	status := strings.ToLower(strings.TrimSpace(todoEditStatusFlag))
	switch status {
	case "open":
	case "in_progress":
		return fmt.Errorf("--status cannot start work; run `atm todo start %s` (add --reopen-reason when reopening review/done)", id)
	case "review":
		return fmt.Errorf("--status cannot submit work; run `atm todo submit %s --reason \"<result and evidence>\"`", id)
	case "done":
		return fmt.Errorf("--status cannot accept work; a human must run `atm todo done %s --reason \"<acceptance evidence>\"`; Agents use `atm todo submit %s --reason \"<result and evidence>\"`", id, id)
	case "archived":
		return fmt.Errorf("--status cannot archive work; run `atm todo archive %s`", id)
	case "waiting", "blocked":
		return fmt.Errorf("%s is not a lifecycle status; keep the Todo in_progress and run `atm todo edit %s --wake \"<observable condition>\"`", status, id)
	default:
		return fmt.Errorf("invalid --status %q; only open is accepted", todoEditStatusFlag)
	}
	for _, name := range []string{"title", "desc", "desc-file", "priority", "project", "source", "creator", "wake", "review-at", "maintenance-limit"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--status cannot be combined with --%s; return the Todo to open first, then edit its metadata", name)
		}
	}

	call := todoWorkflowCLICall("return-open")
	result, err := workapp.Default.ReturnToOpen(cmd.Context(), call, workapp.ReturnToOpenInput{TodoID: rawID})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	if result.AlreadyOpen {
		fmt.Printf("%s already open: %s\n", result.Todo.ID, result.Todo.Title)
	} else {
		fmt.Printf("Returned %s to open: %s\n", result.Todo.ID, result.Todo.Title)
	}
	return nil
}

func deliverTodoMetadataEffects(effects []workapp.MetadataEffect) {
	for _, effect := range effects {
		todo := effect.Todo
		switch effect.Kind {
		case workapp.MetadataEffectCreated:
			notifyTodoEvent(&todo, notifyEventCreated)
		}
	}
}

func stringValue(value string) *string {
	copy := value
	return &copy
}

func intValue(value int) *int {
	copy := value
	return &copy
}
