package cmd

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	resolveTaskRunAgentBinary = exec.LookPath
	launchTaskRunController   = startTaskRunController
	buildTaskRunAgentCommand  = buildTaskRunCommand
	interruptTaskRunProcess   = terminateTaskRunProcess
)

func runTodoRun(cmd *cobra.Command, args []string) error {
	agent, err := resolveTaskRunDispatchAgent()
	if err != nil {
		return err
	}
	policy := strings.TrimSpace(todoRunPolicyFlag)
	if policy != "guarded" && policy != "trusted" {
		return fmt.Errorf("invalid run policy %q (use guarded or trusted)", policy)
	}
	if _, err := resolveTaskRunAgentBinary(agent.Binary); err != nil {
		return fmt.Errorf("%s not found in PATH", agent.Binary)
	}

	_, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	followUp := strings.TrimSpace(todoRunContinueFlag)
	var resumedRun *store.TaskRun
	if followUp != "" {
		if err := withDB(true, func(db *sql.DB) error {
			var err error
			resumedRun, err = store.LatestResumableTaskRunForAgent(db, todo.ID, agent.ID)
			return err
		}); err != nil {
			return fmt.Errorf("find %s session to continue: %w", agent.Name, err)
		}
		if resumedRun == nil {
			return fmt.Errorf("todo %s has no finished %s run with a session to continue", todo.ID, agent.Name)
		}
		if store.ResumableThreadID(resumedRun.SessionID) == "" {
			// Refusing beats guessing: Codex would read an unrecognizable id as a
			// thread name and quietly begin a fresh session instead of failing.
			// LatestResumableTaskRunForAgent already guarantees a non-blank id.
			return fmt.Errorf("run %s has no resumable %s thread id (session %q); dispatch a fresh run instead",
				resumedRun.ID, agent.Name, *resumedRun.SessionID)
		}
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
	prompt := buildTaskRunPrompt(todo)
	var resumeSessionID *string
	if resumedRun != nil {
		prompt = buildTaskRunContinuationPrompt(todo, followUp)
		// Validated above, so this cannot be empty. Codex only accepts the bare
		// thread UUID; the stored id may be the rollout form.
		value := store.ResumableThreadID(resumedRun.SessionID)
		// Let this run discover its own session identity from Codex's
		// `thread.started` event (with `session bind` as a fallback): copying the
		// previous id into session_id would present intent as evidence and hide
		// the row from transcript sync's unlinked-run pass.
		resumeSessionID = &value
	}
	run := store.TaskRun{
		ID: runID, TodoID: todo.ID, Agent: agent.ID, Project: todo.Project, WorkDir: workDir,
		Prompt: prompt, Policy: policy, LogPath: logPath,
		Status: store.TaskRunStarting, StartTS: now.Unix(),
		ResumeSessionID: resumeSessionID,
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
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: trusted policy bypasses %s safety prompts and filesystem sandboxing\n", agent.Name)
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
	if resumedRun != nil {
		fmt.Printf("Continued %s in its previous %s session\n", todo.ID, agent.Name)
	} else {
		fmt.Printf("Dispatched %s to %s\n", todo.ID, agent.Name)
	}
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
	if err := withDB(false, func(db *sql.DB) error {
		return store.RecordTaskRunControllerPID(db, run.ID, pid)
	}); err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return 0, fmt.Errorf("record task run controller pid: %w", err)
	}
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
	if run.Agent != "" && run.Agent != taskRunDispatchAgentID {
		return finishTaskRunAfterControllerError(run, 1, fmt.Sprintf("todo run only dispatches Codex (got %q)", run.Agent))
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
	codexOutput := newCodexThreadStartedWriter(logFile, func(sessionID string) error {
		return attachTaskRunSession(*run, sessionID)
	})
	agentCommand.Stdout = codexOutput
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
	if codexOutput.linkErr != nil {
		// A short SQLite lock while the Agent starts should not permanently lose
		// the authoritative id. Retry once after the child exits, still before a
		// successful run moves the Todo to review and closes its binding.
		codexOutput.retryLink()
		if codexOutput.linkErr != nil {
			fmt.Fprintf(logFile, "\nwarning: could not attach Codex thread to task run: %v\n", codexOutput.linkErr)
		}
	}
	reportTaskRunSessionEnd(*run, codexOutput.sessionID)

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

// reportTaskRunSessionEnd tells the App that this thread is gone.
//
// The controller is the only thing that knows this for certain: it owns the
// child process. Hooks are best-effort and a killed or crashed Agent never
// sends its own `SessionEnd`, which left the App's attention overlay holding a
// 等待授权 signal until the safety TTL expired — a banner for a process that no
// longer exists. Best-effort by design: if the App is not running there is
// nobody to tell, and that must never affect the run's outcome.
func reportTaskRunSessionEnd(run store.TaskRun, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		// Nothing to retire: without a thread id from `thread.started` the App
		// never joined a signal to this run in the first place.
		return
	}
	_ = agentevent.Deliver(agentevent.Envelope{
		Version:   agentevent.Version,
		Source:    agentevent.SourceCodex,
		Event:     agentevent.KindSessionEnd,
		SessionID: sessionID,
		CWD:       run.WorkDir,
		Reason:    "task_run_finished",
		At:        time.Now().In(config.Loc).Format(time.RFC3339),
	})
}

// codexThreadStartedWriter tees Codex's JSONL stream into the durable run log
// and observes the one event that provides an authoritative thread id. Parsing
// the controller-owned stream avoids depending on the child Agent to invoke an
// installed `atm` binary, which may be missing, stale, or simply never called.
type codexThreadStartedWriter struct {
	destination io.Writer
	onThread    func(string) error
	pending     []byte
	sessionID   string
	linked      bool
	linkErr     error
}

func newCodexThreadStartedWriter(destination io.Writer, onThread func(string) error) *codexThreadStartedWriter {
	return &codexThreadStartedWriter{destination: destination, onThread: onThread}
}

func (writer *codexThreadStartedWriter) Write(data []byte) (int, error) {
	written, err := writer.destination.Write(data)
	if err != nil {
		return written, err
	}
	if written != len(data) {
		return written, io.ErrShortWrite
	}
	if writer.linked || writer.sessionID != "" {
		return written, nil
	}

	writer.pending = append(writer.pending, data...)
	for {
		newline := bytes.IndexByte(writer.pending, '\n')
		if newline < 0 {
			break
		}
		writer.observe(writer.pending[:newline])
		writer.pending = writer.pending[newline+1:]
		if writer.linked || writer.sessionID != "" {
			writer.pending = nil
			return written, nil
		}
	}
	// Codex normally terminates JSON events with a newline. Trying the partial
	// buffer as well makes the association robust to a final complete event that
	// reaches os/exec without one; incomplete JSON simply fails to decode and is
	// retried after the next Write.
	writer.observe(writer.pending)
	return written, nil
}

func (writer *codexThreadStartedWriter) observe(line []byte) {
	if writer.linked || writer.sessionID != "" || len(bytes.TrimSpace(line)) == 0 {
		return
	}
	var event struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal(line, &event); err != nil || event.Type != "thread.started" {
		return
	}
	sessionID := store.ResumableThreadID(&event.ThreadID)
	if sessionID == "" {
		return
	}
	writer.sessionID = sessionID
	writer.retryLink()
}

func (writer *codexThreadStartedWriter) retryLink() {
	if writer.linked || writer.sessionID == "" {
		return
	}
	writer.linkErr = nil
	if writer.onThread != nil {
		writer.linkErr = writer.onThread(writer.sessionID)
		if writer.linkErr != nil {
			return
		}
	}
	writer.linked = true
}

// attachTaskRunSession records both execution evidence (run -> session) and
// work ownership (session -> Todo). The run link is written first so the Todo
// detail page remains accurate even if the binding ledger is temporarily
// unavailable; a later explicit `atm session bind` is idempotent.
func attachTaskRunSession(run store.TaskRun, sessionID string) error {
	if err := withDB(false, func(db *sql.DB) error {
		return store.LinkTaskRunSession(db, run.ID, run.TodoID, sessionID)
	}); err != nil {
		return err
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: sessionID,
		TodoID:    run.TodoID,
		Agent:     run.Agent,
		Project:   run.Project,
		CWD:       run.WorkDir,
		BoundAt:   time.Now().In(config.Loc).Unix(),
	}); err != nil {
		return fmt.Errorf("bind task run session: %w", err)
	}
	return nil
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
	// Only an explicit `--continue` dispatch fills resume_session_id, so a fresh
	// run whose session_id was filled in by transcript-sync routing can never be
	// turned into a resume of some unrelated thread. Codex needs the bare thread
	// UUID: it reads anything else as a thread name and silently starts over.
	if resumeID := store.ResumableThreadID(run.ResumeSessionID); resumeID != "" {
		args = append(args, "resume", resumeID)
	}
	args = append(args, "-")
	command := exec.Command(binary, args...)
	command.Stdin = strings.NewReader(run.Prompt)
	command.Env = append(os.Environ(), "ATM_RUN_ID="+run.ID, "ATM_TODO_ID="+run.TodoID)
	return command, nil
}

func buildTaskRunContinuationPrompt(todo *store.Todo, followUp string) string {
	return fmt.Sprintf(`%s (ATM Todo %s)

Continue the existing Codex work for this task.

The user wants these follow-up changes:

%s

First run `+"`atm todo doc %s`"+` to load the latest requirements, then run
`+"`atm session bind %s`"+` so this resumed turn is explicitly associated with the Todo.
Inspect the existing work before editing, implement the follow-up completely, and verify the result.
%s`,
		todo.Title, todo.ID, strings.TrimSpace(followUp), todo.ID, todo.ID, taskRunOutcomeInstruction)
}

// taskRunOutcomeInstruction closes the prompt for both a fresh dispatch and a
// continuation. It asks for no ATM writes: this run's session is indexed with
// every reply in full, and the Todo reads the Agent's own closing message from
// that index, so a progress entry would be a second copy of text ATM already
// owns — one the Agent has to spend turns producing, and cannot write at all
// under a sandbox that keeps ~/.atm read-only.
const taskRunOutcomeInstruction = `End with what you did, the evidence you verified it with, and anything left open:
that closing message is what the person reviewing this Todo reads.
Do not record progress with ATM and do not mark the Todo done;
successful process completion is submitted to review by the run controller.`

func buildTaskRunPrompt(todo *store.Todo) string {
	return fmt.Sprintf(`%s (ATM Todo %s)

This is an unattended Codex task run managed by ATM.

First run `+"`atm todo doc %s`"+` to load the current requirements, then run
`+"`atm session bind %s`"+` so the session is explicitly associated with the Todo.
Implement the task completely in the current repository and verify the result.
%s`,
		todo.Title, todo.ID, todo.ID, todo.ID, taskRunOutcomeInstruction)
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

func runTodoRunInterrupt(cmd *cobra.Command, args []string) error {
	if _, _, err := loadTodoByID(args[0]); err != nil {
		return err
	}
	var run *store.TaskRun
	if err := withDB(true, func(db *sql.DB) error {
		var err error
		run, err = store.ActiveTaskRun(db, args[0])
		return err
	}); err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("todo %s has no active agent run", args[0])
	}
	if run.PID <= 0 {
		return fmt.Errorf("agent run %s is still starting; try again shortly", run.ID)
	}
	if err := interruptTaskRunProcess(run.PID); err != nil {
		return fmt.Errorf("interrupt agent run %s: %w", run.ID, err)
	}

	finished := time.Now().In(config.Loc)
	message := "interrupted by user"
	if err := withDB(false, func(db *sql.DB) error {
		return store.InterruptTaskRun(db, run.ID, finished.Unix(), message)
	}); err != nil {
		return err
	}
	if logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
		fmt.Fprintf(logFile, "\nATM run interrupted by user at %s\n", finished.Format(time.RFC3339))
		_ = logFile.Close()
	}
	// A killed Agent cannot report its own end, and interrupting one that was
	// blocked on approval is exactly when a stale 等待授权 banner would outlive
	// the process.
	reportTaskRunSessionEnd(*run, store.ResumableThreadID(run.SessionID))
	if err := withDB(true, func(db *sql.DB) error {
		var err error
		run, err = store.GetTaskRun(db, run.ID)
		return err
	}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"run": run})
		return nil
	}
	fmt.Printf("Interrupted %s agent run %s\n", args[0], run.ID)
	return nil
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
