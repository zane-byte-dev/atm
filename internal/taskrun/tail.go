package taskrun

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type TailInput struct {
	TodoID   string `json:"todo_id"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
	Follow   bool   `json:"follow,omitempty"`
}

type TailResult struct {
	RunID string `json:"run_id"`
}

const logTruncatedNotice = "[... earlier log truncated ...]\n"

func (service Service) Tail(
	ctx context.Context,
	call application.Call,
	input TailInput,
	out io.Writer,
) (TailResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateCall(ctx, call); err != nil {
		return TailResult{}, err
	}
	if input.MaxBytes < 0 {
		return TailResult{}, invalidArgument("tail bytes must be zero or greater", "max_bytes", input.MaxBytes)
	}
	if out == nil {
		return TailResult{}, invalidArgument("tail output writer is required", "output", nil)
	}
	todoID, err := requireTodo(input.TodoID)
	if err != nil {
		return TailResult{}, err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return TailResult{}, unavailable("open latest task run", err)
	}
	run, queryErr := store.LatestTaskRun(db, todoID)
	_ = db.Close()
	if queryErr != nil {
		return TailResult{}, unavailable("find latest task run", queryErr)
	}
	if run == nil {
		return TailResult{}, notFound(fmt.Sprintf("todo %s has no agent runs", todoID), "todo_id", todoID, nil)
	}
	file, err := service.logs.Open(run.LogPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return TailResult{}, notFound("run log not found", "run_id", run.ID, err)
		}
		return TailResult{}, unavailable("open run log", err)
	}
	defer file.Close()
	if err := copyLogTail(out, file, input.MaxBytes); err != nil {
		return TailResult{}, unavailable("read run log", err)
	}
	result := TailResult{RunID: run.ID}
	for input.Follow {
		active, err := taskRunActive(run.ID)
		if err != nil {
			return TailResult{}, err
		}
		if !active {
			if _, err := io.Copy(out, file); err != nil {
				return TailResult{}, unavailable("read final run log", err)
			}
			return result, nil
		}
		if err := service.clock.Wait(ctx, 500*time.Millisecond); err != nil {
			return TailResult{}, unavailable("follow run log", err)
		}
		if _, err := io.Copy(out, file); err != nil {
			return TailResult{}, unavailable("follow run log", err)
		}
	}
	return result, nil
}

func taskRunActive(runID string) (bool, error) {
	db, err := store.OpenReadOnly()
	if err != nil {
		return false, unavailable("open task run status", err)
	}
	run, queryErr := store.GetTaskRun(db, runID)
	_ = db.Close()
	if queryErr != nil {
		return false, unavailable("read task run status", queryErr)
	}
	return run != nil && (run.Status == store.TaskRunStarting || run.Status == store.TaskRunRunning), nil
}

func copyLogTail(out io.Writer, file LogReader, maxBytes int64) error {
	if maxBytes <= 0 {
		_, err := io.Copy(out, file)
		return err
	}
	size, err := file.Size()
	if err != nil {
		return err
	}
	if size <= maxBytes {
		_, err = io.Copy(out, file)
		return err
	}
	if _, err := file.Seek(size-maxBytes, io.SeekStart); err != nil {
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
	if _, err := io.WriteString(out, logTruncatedNotice); err != nil {
		return err
	}
	_, err = io.Copy(out, reader)
	return err
}
