package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func runTodoDelete(cmd *cobra.Command, args []string) error {
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}

	if todoDeleteProjectFlag != "" {
		deleted := 0
		for _, t := range tf.Items {
			if t.Project == todoDeleteProjectFlag {
				deleted++
			}
		}
		if deleted == 0 {
			return fmt.Errorf("no todos found for project: %s", todoDeleteProjectFlag)
		}
		confirmed, err := confirmDestructive(cmd, todoDeleteYesFlag, fmt.Sprintf("Permanently delete %d todos from project %s?", deleted, todoDeleteProjectFlag))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
		deletedIDs := []string{}
		err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
			keep := []store.Todo{}
			for _, todo := range transaction.Todos().Items {
				if todo.Project == todoDeleteProjectFlag {
					// Comments and session bindings go with the todo via
					// ON DELETE CASCADE.
					deletedIDs = append(deletedIDs, todo.ID)
				} else {
					keep = append(keep, todo)
				}
			}
			transaction.Todos().Items = keep
			return nil
		})
		if err != nil {
			return err
		}
		for _, id := range deletedIDs {
			if store.TodoDocExists(id) {
				_ = os.Remove(store.TodoDocPath(id))
			}
		}
		fmt.Printf("Deleted %d todos from project %s\n", deleted, todoDeleteProjectFlag)
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("provide a todo ID or use --project to batch delete")
	}

	id := args[0]
	found := false
	for _, t := range tf.Items {
		if t.ID == id {
			found = true
		}
	}
	if _, archived := store.ArchivedStatus(tf, id); archived {
		found = true
	}
	if !found {
		return store.TodoNotFoundError(tf, id)
	}
	confirmed, err := confirmDestructive(cmd, todoDeleteYesFlag, fmt.Sprintf("Permanently delete todo %s?", id))
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
		return nil
	}
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		// Comments and session bindings go with the todo via ON DELETE CASCADE.
		_, err := transaction.PermanentlyDeleteTodos([]string{id})
		return err
	})
	if err != nil {
		return err
	}
	if store.TodoDocExists(id) {
		_ = os.Remove(store.TodoDocPath(id))
	}
	fmt.Printf("Deleted %s\n", id)
	return nil
}

func runTodoArchive(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "archived", "Archived",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.ArchiveTodos(ids)
		})
}

func runTodoUnarchive(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "unarchived", "Unarchived",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.UnarchiveTodos(ids)
		})
}

func runTodoTrash(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "trashed", "Trashed",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.TrashTodos(ids)
		})
}

func runTodoRestore(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "restored", "Restored",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.RestoreTodos(ids)
		})
}

// runTodoArchiveMove is archive and unarchive, which differ only in the verb:
// both move a set of ids across the archive boundary and report the ones that
// actually moved.
func runTodoArchiveMove(args []string, jsonKey, verb string,
	move func(*workapp.Transaction, []string) ([]string, error)) error {
	var moved []string
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		var err error
		moved, err = move(transaction, uniqueStrings(args))
		return err
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{jsonKey: moved})
		return nil
	}
	fmt.Printf("%s %s\n", verb, strings.Join(moved, ", "))
	return nil
}

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
		return runTodoBatchAdd()
	}

	if len(args) == 0 {
		return fmt.Errorf("requires at least 1 arg(s), use --batch for batch input")
	}

	title := strings.Join(args, " ")
	if len([]rune(title)) < 8 && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Warning: title is very short (%d chars), consider being more descriptive\n", len([]rune(title)))
	}

	status := todoAddStatusFlag
	if err := validateWorkStatus(status); err != nil {
		return err
	}
	if err := validateReviewAt(todoAddReviewAtFlag); err != nil {
		return err
	}
	if status == store.TodoStatusWaiting && todoAddWakeFlag == "" && todoAddReviewAtFlag == "" {
		return fmt.Errorf("waiting todos require --wake or --review-at")
	}

	source := todoSourceFlag
	if source == "" {
		source = todoSourceFromSession()
	}

	creator, err := resolveTodoCreator(todoAddCreatorFlag)
	if err != nil {
		return err
	}

	description, err := todoAddDescription(cmd)
	if err != nil {
		return err
	}

	var tf *store.TodoFile
	var t store.Todo
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		t = store.Todo{
			ID:            store.NextTodoID(tf),
			Title:         title,
			Priority:      todoAddPriorityFlag,
			Status:        status,
			Project:       todoAddProjectFlag,
			WakeCondition: todoAddWakeFlag,
			ReviewAt:      todoAddReviewAtFlag,
			Created:       store.Today(),
			Source:        source,
			Creator:       creator,
			Description:   description,
			OnDone:        todoOnDoneFlag,
		}
		if t.Status != store.TodoStatusWaiting {
			t.WakeCondition = ""
			t.ReviewAt = ""
		}
		tf.Items = append(tf.Items, t)
		return nil
	})
	if err != nil {
		return err
	}
	if err := ensureTodoDocs(tf, t.ID); err != nil {
		return err
	}

	// Humans need to know when work appears — agents often create via --json.
	notifyTodoEvent(&t, notifyEventCreated)
	if t.Status == store.TodoStatusReview {
		notifyTodoEvent(&t, notifyEventReview)
	}

	if todoAddRefineFlag {
		if !jsonOutput {
			fmt.Println(t.ID)
			fmt.Fprintf(cmd.ErrOrStderr(), "Created %s: %s\n", t.ID, t.Title)
		}
		if err := refineTodoByID(cmd, t.ID, refine.Options{AllowSplit: true}, false); err != nil {
			if jsonOutput {
				output.JSON(map[string]any{"todo": t, "refine_error": err.Error()})
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Refine failed: %v\n", err)
			}
			return err
		}
		return nil
	}

	if jsonOutput {
		output.JSON(t)
		return nil
	}
	fmt.Println(t.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created %s: %s\n", t.ID, t.Title)
	return nil
}

// readBodyFlagOrFile resolves a body that may be given inline or read from a
// file, where "-" means stdin. Every parameter carrying multiline prose needs the
// file door: a requirement or an analysis note routinely contains backticks, `$`,
// braces and quotes, and pushing it through a shell argument makes correctness
// depend on the caller quoting a heredoc properly. Getting that wrong fails
// silently — command substitution runs, `$VAR` becomes empty, and the write still
// reports success, so the damage is only visible by reading the text back.
//
// name is the inline flag's name; the file flag is assumed to be name + "-file".
func readBodyFlagOrFile(cmd *cobra.Command, name, inline, path string) (string, error) {
	if path == "" {
		return inline, nil
	}
	if inline != "" {
		return "", fmt.Errorf("--%s and --%s-file cannot be used together", name, name)
	}
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("reading --%s-file from %s: %w", name, path, err)
	}
	return string(data), nil
}

// validateInlineTodoDescription catches the common shell-quoting mistake where
// a caller passes "first\\n- second" to --desc expecting the backslash escape to
// become a newline. Cobra receives those bytes verbatim, and persisting them
// makes the Markdown reader show "\\n" in the task body. Keep this check on the
// inline flag only: --desc-file is the byte-preserving escape hatch for technical
// prose that intentionally discusses encoded newlines.
func validateInlineTodoDescription(description string) error {
	if err := store.ValidateTodoDescription(description); err != nil {
		return err
	}
	if strings.Contains(description, "\n") || strings.Count(description, `\n`) < 2 {
		return nil
	}
	for _, line := range strings.Split(description, `\n`)[1:] {
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") ||
			strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "#") {
			return fmt.Errorf(
				"description contains literal \\n sequences before Markdown structure; " +
					"use real line breaks (for multiline CLI input, use --desc-file)",
			)
		}
	}
	return nil
}

func todoAddDescription(cmd *cobra.Command) (string, error) {
	description, err := readBodyFlagOrFile(cmd, "desc", todoDescFlag, todoDescFileFlag)
	if err != nil {
		return "", err
	}
	if cmd.Flags().Changed("desc") {
		return description, validateInlineTodoDescription(description)
	}
	return description, store.ValidateTodoDescription(description)
}

func runTodoBatchAdd() error {
	data, err := io.ReadAll(os.Stdin)
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

	// Defaults: CLI flags > batch-level > hardcoded
	defaultPriority := "P1"
	if batch.Priority != "" {
		defaultPriority = batch.Priority
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
		defaultStatus = store.TodoStatusOpen
	}
	if todoAddStatusFlag != store.TodoStatusOpen {
		defaultStatus = todoAddStatusFlag
	}

	defaultSource := batch.Source
	if todoSourceFlag != "" {
		defaultSource = todoSourceFlag
	}
	if defaultSource == "" {
		defaultSource = todoSourceFromSession()
	}

	// One batch is filed by one caller, so the creator is resolved once. Items
	// may still name their own, which is what a batch assembled from several
	// intake paths needs.
	defaultCreator, err := resolveTodoCreator(emptyAs(todoAddCreatorFlag, batch.Creator))
	if err != nil {
		return err
	}

	var tf *store.TodoFile
	var added []store.Todo
	var changedDocIDs []string
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		for _, item := range batch.Items {
			if item.Title == "" {
				continue
			}
			priority := defaultPriority
			if item.Priority != "" {
				priority = item.Priority
			}
			project := defaultProject
			if item.Project != "" {
				project = item.Project
			}
			source := defaultSource
			if item.Source != "" {
				source = item.Source
			}
			creator := defaultCreator
			if item.Creator != "" {
				normalized, err := store.NormalizeTodoCreator(item.Creator)
				if err != nil {
					return fmt.Errorf("item %q: %w", item.Title, err)
				}
				creator = normalized
			}
			status := defaultStatus
			if item.Status != "" {
				status = item.Status
			}
			if err := validateWorkStatus(status); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}
			if err := validateReviewAt(item.ReviewAt); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}
			if err := store.ValidateTodoDescription(item.Desc); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}

			t := store.Todo{
				ID:            store.NextTodoID(tf),
				Title:         item.Title,
				Description:   item.Desc,
				Priority:      priority,
				Status:        status,
				Project:       project,
				WakeCondition: item.WakeCondition,
				ReviewAt:      item.ReviewAt,
				Created:       store.Today(),
				Source:        source,
				Creator:       creator,
			}
			if t.Status == store.TodoStatusWaiting && t.WakeCondition == "" && t.ReviewAt == "" {
				return fmt.Errorf("item %q: waiting todos require wake or review_at", item.Title)
			}
			if t.Status != store.TodoStatusWaiting {
				t.WakeCondition = ""
				t.ReviewAt = ""
			}
			tf.Items = append(tf.Items, t)
			added = append(added, t)
			changedDocIDs = append(changedDocIDs, t.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := ensureTodoDocs(tf, changedDocIDs...); err != nil {
		return err
	}

	for i := range added {
		notifyTodoEvent(&added[i], notifyEventCreated)
		if added[i].Status == store.TodoStatusReview {
			notifyTodoEvent(&added[i], notifyEventReview)
		}
	}

	if jsonOutput {
		output.JSON(added)
		return nil
	}
	for _, t := range added {
		fmt.Printf("Added %s: %s\n", t.ID, t.Title)
	}
	fmt.Fprintf(os.Stderr, "(%d items added)\n", len(added))
	return nil
}

func runTodoEdit(cmd *cobra.Command, args []string) error {
	// Resolved before the mutation opens, so a missing file or an unreadable
	// stdin fails without having touched the todo.
	editedDescription, err := readBodyFlagOrFile(cmd, "desc", todoEditDescFlag, todoEditDescFileFlag)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("desc") || cmd.Flags().Changed("desc-file") {
		validate := store.ValidateTodoDescription
		if cmd.Flags().Changed("desc") {
			validate = validateInlineTodoDescription
		}
		if err := validate(editedDescription); err != nil {
			return err
		}
	}
	var enteredReview bool
	tf, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, transaction *workapp.Transaction) error {
		changed := false
		prevStatus := t.Status
		if cmd.Flags().Changed("title") {
			t.Title = todoEditTitleFlag
			changed = true
		}
		if cmd.Flags().Changed("desc") || cmd.Flags().Changed("desc-file") {
			t.Description = editedDescription
			changed = true
		}
		if cmd.Flags().Changed("priority") {
			t.Priority = todoEditPriorityFlag
			changed = true
		}
		if cmd.Flags().Changed("project") {
			t.Project = todoEditProjectFlag
			changed = true
		}
		if cmd.Flags().Changed("source") {
			t.Source = todoEditSourceFlag
			changed = true
		}
		if cmd.Flags().Changed("status") {
			if err := validateWorkStatus(todoEditStatusFlag); err != nil {
				return err
			}
			t.Status = todoEditStatusFlag
			changed = true
		}
		if cmd.Flags().Changed("wake") {
			t.WakeCondition = todoEditWakeFlag
			changed = true
		}
		if cmd.Flags().Changed("review-at") {
			if err := validateReviewAt(todoEditReviewAtFlag); err != nil {
				return err
			}
			t.ReviewAt = todoEditReviewAtFlag
			changed = true
		}
		if !changed {
			return fmt.Errorf("nothing to update, use --title, --desc, --priority, --project, --source, --status, --wake, or --review-at")
		}
		if t.Status == store.TodoStatusWaiting && t.WakeCondition == "" && t.ReviewAt == "" && len(t.DependsOn) == 0 {
			return fmt.Errorf("waiting todos require --wake or --review-at")
		}
		if t.Status != store.TodoStatusWaiting {
			t.WakeCondition = ""
			t.ReviewAt = ""
		}
		if cmd.Flags().Changed("status") && t.Status != store.TodoStatusInProgress {
			if _, err := transaction.UnbindTodoSessions(t.ID, "status:"+t.Status); err != nil {
				return fmt.Errorf("unbind todo sessions before status change: %w", err)
			}
		}
		enteredReview = prevStatus != store.TodoStatusReview && t.Status == store.TodoStatusReview
		return nil
	})
	if err != nil {
		return err
	}
	if enteredReview {
		notifyTodoEvent(t, notifyEventReview)
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Updated %s: %s", t.ID, t.Title))
}

func runTodoMove(cmd *cobra.Command, args []string) error {
	var old string
	tf, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		old = t.Project
		t.Project = todoMoveProjectFlag
		return nil
	})
	if err != nil {
		return err
	}
	if err := syncExistingTodoDocs(tf, t.ID); err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(t)
		return nil
	}
	if old != "" {
		fmt.Printf("Moved %s: %s → %s\n", t.ID, old, t.Project)
	} else {
		fmt.Printf("Moved %s → %s\n", t.ID, t.Project)
	}
	return nil
}

func runTodoLog(cmd *cobra.Command, args []string) error {
	id := ""
	messageArgs := args
	// With --message-file the body comes from the file, so the only positional
	// left is the optional id. A 分析 entry is the longest prose ATM accepts and
	// the one most likely to carry code, which is exactly what a shell argument
	// mangles; see readBodyFlagOrFile.
	if todoLogMessageFileFlag != "" {
		if len(args) > 1 {
			return fmt.Errorf("--message-file takes the entry text, so at most an id may be given as an argument")
		}
		id = strings.Join(args, "")
		messageArgs = nil
	} else if len(args) > 1 {
		id = args[0]
		messageArgs = args[1:]
	}
	if id == "" || id == "current" {
		var err error
		id, err = resolveCurrentTodoID()
		if err != nil {
			return err
		}
	}
	tf, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	msg, err := readBodyFlagOrFile(cmd, "message", strings.Join(messageArgs, " "), todoLogMessageFileFlag)
	if err != nil {
		return err
	}
	msg = strings.TrimRight(msg, "\n")
	if err := store.ValidateTodoLogMessage(msg, todoLogSectionFlag); err != nil {
		return err
	}
	if err := validateTodoLogReferences(tf, msg); err != nil {
		return err
	}
	if store.TodoDocExists(t.ID) {
		if err := store.SyncTodoDocMetadata(t); err != nil {
			return fmt.Errorf("sync todo doc: %w", err)
		}
	}
	entry, err := store.AppendTodoLog(t, msg, todoLogSectionFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"success": true,
			"path":    store.TodoDocPath(t.ID),
			"entry":   strings.TrimSpace(entry),
		})
		return nil
	}
	fmt.Printf("Logged to %s: %s", t.ID, entry)
	return nil
}

func runTodoDoc(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	_, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	if todoDocInitFlag {
		path, err := store.InitTodoDoc(t)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"success": true, "path": path})
			return nil
		}
		fmt.Printf("Created %s\n", path)
		return nil
	}

	// Agent handoff always starts with `todo doc`. GUI-created todos may lack a
	// card even though the structured row exists; materialize one so bind+read
	// never looks like a missing task.
	if !store.TodoDocExists(t.ID) {
		if _, err := store.EnsureTodoDoc(t); err != nil {
			return err
		}
	}

	content, err := store.ReadTodoDoc(t.ID)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"path":    store.TodoDocPath(t.ID),
			"exists":  true,
			"content": content,
		})
		return nil
	}
	fmt.Print(content)
	return nil
}

var todoCaptureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture todos from Claude TodoWrite hook",
	Long:  "Read TodoWrite JSON from $TOOL_INPUT (set by Claude Code hook) or stdin, deduplicate against existing open todos, and create new ATM todos.",
	RunE:  runTodoCapture,
}

type todoWriteInput struct {
	Todos []todoWriteItem `json:"todos"`
}

type todoWriteItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

func runTodoCapture(cmd *cobra.Command, args []string) error {
	var data []byte
	if toolInput := os.Getenv("TOOL_INPUT"); toolInput != "" {
		data = []byte(toolInput)
	} else {
		var err error
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
	}

	if len(data) == 0 {
		return nil
	}

	var input todoWriteInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parsing TodoWrite input: %w", err)
	}

	var candidates []todoWriteItem
	for _, item := range input.Todos {
		if item.Status != "completed" && item.Content != "" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	project := todoCaptureProjectFlag
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = filepath.Base(cwd)
		}
	}

	source := todoSourceFromSession()
	// This command only runs from Claude's TodoWrite hook, so the creator is
	// known even when the hook process carries no session ID to detect it from.
	creator := todoCreatorFromEnvironment()
	if creator == store.TodoCreatorMe {
		creator = "claude"
	}

	var tf *store.TodoFile
	var added []store.Todo
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		existing := make(map[string]bool)
		for _, todo := range tf.Items {
			if store.TodoIsActive(todo) && todo.Project == project {
				existing[todo.Title] = true
			}
		}
		for _, item := range candidates {
			if existing[item.Content] {
				continue
			}
			t := store.Todo{
				ID:       store.NextTodoID(tf),
				Title:    item.Content,
				Priority: "P1",
				Status:   store.TodoStatusOpen,
				Project:  project,
				Created:  store.Today(),
				Source:   source,
				Creator:  creator,
			}
			tf.Items = append(tf.Items, t)
			added = append(added, t)
			existing[item.Content] = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(added) == 0 {
		return nil
	}

	if jsonOutput {
		output.JSON(added)
		return nil
	}
	for _, t := range added {
		fmt.Printf("Captured %s: %s\n", t.ID, t.Title)
	}
	return nil
}
