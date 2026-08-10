package cmd

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	resolveTaskRunAgentBinary = exec.LookPath
	launchTaskRunController   = startTaskRunController
	buildTaskRunAgentCommand  = buildCodexTaskRunCommand
)

func runTodoRun(cmd *cobra.Command, args []string) error {
	const agent = "codex"
	policy := strings.TrimSpace(todoRunPolicyFlag)
	if policy != "guarded" && policy != "trusted" {
		return fmt.Errorf("invalid run policy %q (use guarded or trusted)", policy)
	}
	if _, err := resolveTaskRunAgentBinary(agent); err != nil {
		return fmt.Errorf("%s not found in PATH", agent)
	}

	_, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	workDir, source, err := resolveTaskRunCWD(todo, todoRunCWDFlag)
	if err != nil {
		return err
	}
	todoFile, todo, err := startTodo(todo.ID)
	if err != nil {
		return err
	}
	if err := ensureTodoDocs(todoFile, todo.ID); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(config.AtmDir, "exec"), 0755); err != nil {
		return fmt.Errorf("create task run log directory: %w", err)
	}
	now := time.Now().In(config.Loc)
	runID := fmt.Sprintf("%s-%d", todo.ID, now.UnixNano())
	logPath := filepath.Join(config.AtmDir, "exec", runID+".log")
	run := store.TaskRun{
		ID: runID, TodoID: todo.ID, Agent: agent, Project: todo.Project, WorkDir: workDir,
		Prompt: buildTaskRunPrompt(todo), Policy: policy, LogPath: logPath,
		Status: store.TaskRunStarting, StartTS: now.Unix(),
	}
	if err := withDB(false, func(db *sql.DB) error {
		if err := reconcileStaleTaskRun(db, todo.ID, now); err != nil {
			return err
		}
		return store.CreateTaskRun(db, run)
	}); err != nil {
		return fmt.Errorf("claim agent run: %w", err)
	}

	if policy == "trusted" {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: trusted policy bypasses Codex approvals and sandboxing")
	}
	pid, err := launchTaskRunController(run)
	if err != nil {
		message := "controller failed to start: " + err.Error()
		_ = withDB(false, func(db *sql.DB) error {
			return store.FinishTaskRun(db, run.ID, 1, time.Now().In(config.Loc).Unix(), message)
		})
		return fmt.Errorf("start task run controller: %w", err)
	}
	run.PID = pid
	if jsonOutput {
		output.JSON(map[string]any{"run": run, "workspace_source": source})
		return nil
	}
	fmt.Printf("Dispatched %s to %s\n", todo.ID, agent)
	fmt.Printf("  Run:    %s\n", run.ID)
	fmt.Printf("  Policy: %s\n", policy)
	fmt.Printf("  Dir:    %s (%s)\n", workDir, source)
	fmt.Printf("  PID:    %d\n", pid)
	fmt.Printf("  Log:    atm todo tail %s -f\n", todo.ID)
	return nil
}

func reconcileStaleTaskRun(db *sql.DB, todoID string, now time.Time) error {
	active, err := store.ActiveTaskRun(db, todoID)
	if err != nil || active == nil {
		return err
	}
	if processIsRunning(active.PID) || (active.PID == 0 && now.Unix()-active.StartTS < 30) {
		return fmt.Errorf("todo %s already has active run %s", todoID, active.ID)
	}
	return store.FinishTaskRun(db, active.ID, -1, now.Unix(), "controller no longer running; reconciled before retry")
}

func startTaskRunController(run store.TaskRun) (int, error) {
	executable, err := atmExecutablePath()
	if err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(executable, "todo", "run-controller", run.ID)
	configureBackgroundProcess(command)
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = append(os.Environ(), "ATM_RUN_ID="+run.ID, "ATM_TODO_ID="+run.TodoID)
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		return 0, err
	}
	return pid, nil
}

func runTodoRunController(cmd *cobra.Command, args []string) error {
	return executeTaskRun(args[0])
}

func executeTaskRun(runID string) error {
	var run *store.TaskRun
	if err := withDB(false, func(db *sql.DB) error {
		var err error
		run, err = store.GetTaskRun(db, runID)
		if err != nil {
			return err
		}
		if run == nil {
			return fmt.Errorf("task run not found: %s", runID)
		}
		return store.MarkTaskRunRunning(db, runID, os.Getpid())
	}); err != nil {
		return err
	}

	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return finishTaskRunAfterControllerError(run, 1, "open run log: "+err.Error())
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "ATM task run %s\ntodo=%s agent=%s policy=%s cwd=%s\n\n",
		run.ID, run.TodoID, run.Agent, run.Policy, run.WorkDir)

	agentCommand, err := buildTaskRunAgentCommand(*run)
	if err != nil {
		fmt.Fprintln(logFile, err)
		return finishTaskRunAfterControllerError(run, 1, err.Error())
	}
	agentCommand.Stdout = logFile
	agentCommand.Stderr = logFile
	agentCommand.Dir = run.WorkDir
	started := time.Now().In(config.Loc)
	err = agentCommand.Run()
	exitCode := 0
	message := "agent exited successfully"
	if err != nil {
		exitCode = 1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
		message = fmt.Sprintf("agent exited with code %d", exitCode)
	}

	if exitCode == 0 {
		_, _, _, submitErr := submitTodo(run.TodoID, fmt.Sprintf("agent run %s completed", run.ID))
		if submitErr != nil {
			message += "; todo not submitted: " + submitErr.Error()
		}
	}
	finished := time.Now().In(config.Loc)
	fmt.Fprintf(logFile, "\nATM run finished in %s: %s\n", finished.Sub(started).Round(time.Second), message)
	return withDB(false, func(db *sql.DB) error {
		return store.FinishTaskRun(db, run.ID, exitCode, finished.Unix(), message)
	})
}

func finishTaskRunAfterControllerError(run *store.TaskRun, exitCode int, message string) error {
	return withDB(false, func(db *sql.DB) error {
		return store.FinishTaskRun(db, run.ID, exitCode, time.Now().In(config.Loc).Unix(), message)
	})
}

func buildCodexTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	binary, err := resolveTaskRunAgentBinary("codex")
	if err != nil {
		return nil, fmt.Errorf("codex not found in PATH")
	}
	args := []string{"exec", "--json", "--color", "never", "-C", run.WorkDir}
	if run.Policy == "trusted" {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "--sandbox", "workspace-write", "--add-dir", config.AtmDir)
	}
	args = append(args, "-")
	command := exec.Command(binary, args...)
	command.Stdin = strings.NewReader(run.Prompt)
	command.Env = append(os.Environ(), "ATM_RUN_ID="+run.ID, "ATM_TODO_ID="+run.TodoID)
	return command, nil
}

func buildTaskRunPrompt(todo *store.Todo) string {
	return fmt.Sprintf(`You are the unattended Agent run for ATM Todo %s: %s.

First run `+"`atm todo doc %s`"+` to load the current requirements, then run
`+"`atm session bind %s`"+` so the session is explicitly associated with the Todo.
Implement the task completely in the current repository, verify the result, and
record only meaningful milestones with ATM. Do not mark the Todo done; successful
process completion is submitted to review by the run controller.`,
		todo.ID, todo.Title, todo.ID, todo.ID)
}

func resolveTaskRunCWD(todo *store.Todo, override string) (string, string, error) {
	if value := strings.TrimSpace(override); value != "" {
		return validateTaskRunCWD(value, "flag")
	}
	bindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		return "", "", err
	}
	active := map[string]struct{}{}
	for _, binding := range bindings {
		if binding.UnboundAt == nil {
			if value := cleanReviewContextCWD(binding.CWD); value != "" {
				active[value] = struct{}{}
			}
		}
	}
	if len(active) > 1 {
		values := make([]string, 0, len(active))
		for value := range active {
			values = append(values, value)
		}
		sort.Strings(values)
		return "", "", fmt.Errorf("todo has active bindings in multiple worktrees; pass --cwd explicitly: %s", strings.Join(values, ", "))
	}
	for value := range active {
		return validateTaskRunCWD(value, "active_binding")
	}
	for index := len(bindings) - 1; index >= 0; index-- {
		if value := strings.TrimSpace(bindings[index].CWD); value != "" {
			if cwd, source, err := validateTaskRunCWD(value, "latest_binding"); err == nil {
				return cwd, source, nil
			}
		}
	}
	project := strings.TrimSpace(todo.Project)
	if project != "" {
		home, homeErr := os.UserHomeDir()
		candidates := []string{}
		if filepath.IsAbs(project) {
			candidates = append(candidates, project)
		}
		if homeErr == nil {
			candidates = append(candidates,
				filepath.Join(home, "mox", project),
				filepath.Join(home, "work", project),
				filepath.Join(home, project),
			)
		}
		for _, candidate := range candidates {
			if cwd, source, err := validateTaskRunCWD(candidate, "project"); err == nil {
				return cwd, source, nil
			}
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return validateTaskRunCWD(cwd, "current_directory")
}

func validateTaskRunCWD(value, source string) (string, string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve run cwd %s: %w", value, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", "", fmt.Errorf("inspect run cwd %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("run cwd is not a directory: %s", absolute)
	}
	return absolute, source, nil
}

func runTodoRuns(cmd *cobra.Command, args []string) error {
	if _, _, err := loadTodoByID(args[0]); err != nil {
		return err
	}
	return withDB(true, func(db *sql.DB) error {
		runs, err := store.ListTaskRuns(db, args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(runs)
			return nil
		}
		if len(runs) == 0 {
			fmt.Println("No runs recorded.")
			return nil
		}
		for _, run := range runs {
			age := time.Unix(run.StartTS, 0).In(config.Loc).Format("01-02 15:04:05")
			session := "-"
			if run.SessionID != nil {
				session = *run.SessionID
				if len(session) > 12 {
					session = session[:12]
				}
			}
			fmt.Printf("%-20s %-9s %-9s pid=%-7d session=%-12s %s\n",
				age, run.Agent, run.Status, run.PID, session, run.Message)
		}
		return nil
	})
}

func runTodoRunTail(cmd *cobra.Command, args []string) error {
	if todoRunTailBytesFlag < 0 {
		return fmt.Errorf("tail bytes must be zero or greater")
	}
	if _, _, err := loadTodoByID(args[0]); err != nil {
		return err
	}
	var run *store.TaskRun
	if err := withDB(true, func(db *sql.DB) error {
		var err error
		run, err = store.LatestTaskRun(db, args[0])
		return err
	}); err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("todo %s has no agent runs", args[0])
	}
	file, err := os.Open(run.LogPath)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	defer file.Close()
	if err := copyTaskRunLogTail(cmd.OutOrStdout(), file, todoRunTailBytesFlag); err != nil {
		return err
	}
	for {
		if !todoRunTailFollowFlag {
			return nil
		}
		var active bool
		if err := withDB(true, func(db *sql.DB) error {
			current, err := store.GetTaskRun(db, run.ID)
			if err != nil {
				return err
			}
			active = current != nil && (current.Status == store.TaskRunStarting || current.Status == store.TaskRunRunning)
			return nil
		}); err != nil {
			return err
		}
		if !active {
			_, err := io.Copy(cmd.OutOrStdout(), file)
			return err
		}
		time.Sleep(500 * time.Millisecond)
		if _, err := io.Copy(cmd.OutOrStdout(), file); err != nil {
			return err
		}
	}
}

const taskRunLogTruncatedNotice = "[... earlier log truncated ...]\n"

// copyTaskRunLogTail keeps App refreshes bounded without changing the CLI's
// traditional full-log default. Seeking is constant-memory and skipping UTF-8
// continuation bytes prevents a byte cap from producing invalid text.
func copyTaskRunLogTail(out io.Writer, file *os.File, maxBytes int64) error {
	if maxBytes <= 0 {
		_, err := io.Copy(out, file)
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= maxBytes {
		_, err = io.Copy(out, file)
		return err
	}
	if _, err := file.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		value, readErr := reader.ReadByte()
		if readErr != nil {
			return readErr
		}
		if value&0xc0 != 0x80 {
			if err := reader.UnreadByte(); err != nil {
				return err
			}
			break
		}
	}
	if _, err := io.WriteString(out, taskRunLogTruncatedNotice); err != nil {
		return err
	}
	_, err = io.Copy(out, reader)
	return err
}
