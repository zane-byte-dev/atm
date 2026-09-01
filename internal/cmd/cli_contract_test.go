package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A flag-only leaf that forgets Args silently accepts every positional token.
// Besides hiding typos, this made commands such as `quota disable` and
// `version anything` exit successfully while doing something unrelated. Keep
// the contract mechanical: commands whose Use declares no positional grammar
// must install an argument validator.
func TestFlagOnlyRunnableCommandsRejectPositionalArguments(t *testing.T) {
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		fields := strings.Fields(command.Use)
		if (command.Run != nil || command.RunE != nil) && len(fields) == 1 {
			if command.Args == nil {
				t.Errorf("%s is runnable with no positional grammar but has no Args validator", command.CommandPath())
			} else if err := command.Args(command, []string{"unexpected"}); err == nil {
				t.Errorf("%s accepts a positional argument its Use does not declare", command.CommandPath())
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(rootCmd)
}

func TestQuotaEnableDisableTyposReturnCopyableConfigCommand(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		err := quotaCmd.Args(quotaCmd, []string{action})
		if err == nil || !strings.Contains(err.Error(), "atm config set grok_live_quota") {
			t.Fatalf("quota %s error = %v", action, err)
		}
	}
}

func TestTodoEditStatusErrorsPointToExplicitLifecycleCommands(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "in_progress", want: "atm todo start t42"},
		{status: "review", want: "atm todo submit t42"},
		{status: "done", want: "atm todo done t42"},
		{status: "archived", want: "atm todo archive t42"},
		{status: "waiting", want: "atm todo edit t42 --wake"},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			setCommandFlagForTest(t, todoEditCmd, "status", test.status)
			err := runTodoEdit(todoEditCmd, []string{"#T42"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("edit --status %s error = %v, want copyable %q", test.status, err, test.want)
			}
		})
	}
}

func TestTodoBulkEditStatusErrorsPointToExplicitLifecycleCommands(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "in_progress", want: "atm todo start t41"},
		{status: "review", want: "atm todo submit t41"},
		{status: "done", want: "atm todo bulk done t41 t42"},
		{status: "archived", want: "atm todo archive t41 t42"},
		{status: "waiting", want: "atm todo edit t41 --wake"},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			err := validateBulkEditStatusCLI(test.status, []string{"#T41", "t42"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("bulk edit --status %s error = %v, want copyable %q", test.status, err, test.want)
			}
		})
	}
}
