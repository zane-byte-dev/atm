package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

const maxTodoPlanFileBytes = 1 << 20

var todoPlanFile string

var todoPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Update the bound Todo's structured execution plan",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var todoPlanSetCmd = &cobra.Command{
	Use:   "set [todo-id]",
	Short: "Atomically replace the latest execution-plan snapshot",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoPlanSet,
}

func init() {
	todoPlanSetCmd.Flags().StringVar(&todoPlanFile, "file", "", "read the JSON plan snapshot from a file (use - for stdin)")
	_ = todoPlanSetCmd.MarkFlagRequired("file")
	todoPlanCmd.AddCommand(todoPlanSetCmd)
	todoCmd.AddCommand(todoPlanCmd)
}

type todoPlanFilePayload struct {
	BaseRevision int64              `json:"base_revision"`
	Explanation  string             `json:"explanation,omitempty"`
	Items        []workapp.PlanItem `json:"items"`
}

func runTodoPlanSet(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(todoPlanFile) == "" {
		return fmt.Errorf("--file is required (use --file - for stdin)")
	}
	payload, err := readTodoPlanFile(cmd, todoPlanFile)
	if err != nil {
		return err
	}
	todoID := ""
	if len(args) == 1 {
		todoID = args[0]
	}
	result, err := workapp.Default.SetPlan(cmd.Context(), cliApplicationCall("todo-plan-set", ""), workapp.SetPlanInput{
		TodoID: todoID, BaseRevision: payload.BaseRevision,
		Explanation: payload.Explanation, Items: payload.Items,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	state := "updated"
	if !result.Changed {
		state = "unchanged"
	}
	fmt.Printf("Plan %s revision %d %s\n", result.Plan.TodoID, result.Plan.Revision, state)
	return nil
}

func readTodoPlanFile(cmd *cobra.Command, path string) (todoPlanFilePayload, error) {
	var reader io.Reader
	var closeFile func() error
	if path == "-" {
		reader = cmd.InOrStdin()
	} else {
		file, err := os.Open(path)
		if err != nil {
			return todoPlanFilePayload{}, fmt.Errorf("open plan file %s: %w", path, err)
		}
		reader = file
		closeFile = file.Close
	}
	if closeFile != nil {
		defer closeFile()
	}

	limited := io.LimitReader(reader, maxTodoPlanFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return todoPlanFilePayload{}, fmt.Errorf("read plan file %s: %w", path, err)
	}
	if len(data) > maxTodoPlanFileBytes {
		return todoPlanFilePayload{}, fmt.Errorf("plan file exceeds %d bytes", maxTodoPlanFileBytes)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return todoPlanFilePayload{}, fmt.Errorf("plan file is empty")
	}
	var payload todoPlanFilePayload
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return todoPlanFilePayload{}, fmt.Errorf("decode plan file %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return todoPlanFilePayload{}, fmt.Errorf("decode plan file %s: multiple JSON values", path)
		}
		return todoPlanFilePayload{}, fmt.Errorf("decode plan file %s: %w", path, err)
	}
	return payload, nil
}
