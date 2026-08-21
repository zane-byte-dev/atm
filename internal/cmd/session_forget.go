package cmd

import (
	"fmt"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"

	"github.com/spf13/cobra"
)

var sessionForgetYesFlag bool

func init() {
	forgetCmd.Flags().BoolVarP(&sessionForgetYesFlag, "yes", "y", false, "skip the confirmation prompt")
	sessionCmd.AddCommand(forgetCmd)
}

var forgetCmd = &cobra.Command{
	Use:   "forget <session-id>",
	Short: "Permanently drop a retained session from the index",
	Long: `Permanently drop a session whose transcript is already gone from disk.

ATM keeps sessions after the agent rotates its own logs away, so history and
spend survive. This is the escape hatch for the cases where that is not what you
want — a junk session, or a duplicate left behind by a renamed transcript.

Only retained sessions can be forgotten. While the last sync still found the
transcript, forgetting is pointless: the next sync indexes it again. Delete the
transcript first, run ` + "`atm sync`" + `, then forget it here.

Forgetting takes the session's messages, tool counts and token usage with it, so
its tokens and cost leave every total. ` + "`atm doctor`" + ` reports how many retained
sessions each agent has.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionForget,
}

func runSessionForget(cmd *cobra.Command, args []string) error {
	service := currentSessionService()
	plan, err := service.PlanForget(cmd.Context(), sessionapp.PlanForgetInput{SessionID: args[0]})
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("Permanently forget session %s (%s | %s | %s)? %d messages, %d requests, $%.2f leave every total.",
		plan.ShortID, plan.Agent, plan.Project, plan.CreatedAt, plan.Messages, plan.Requests, plan.CostUSD)
	confirmed, err := confirmDestructive(cmd, sessionForgetYesFlag, prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
		return nil
	}

	result, err := service.Forget(cmd.Context(), sessionapp.ForgetInput{Plan: plan, Confirmed: true})
	if err != nil {
		return err
	}
	forgotten := result.Session
	if jsonOutput {
		output.JSON(map[string]any{
			"forgotten": forgotten.SessionID,
			"short_id":  forgotten.ShortID,
			"agent":     forgotten.Agent,
			"messages":  forgotten.Messages,
			"requests":  forgotten.Requests,
			"cost_usd":  forgotten.CostUSD,
		})
		return nil
	}
	fmt.Printf("Forgot session %s (%s): %d messages, %d requests, $%.2f removed.\n",
		forgotten.ShortID, forgotten.Agent, forgotten.Messages, forgotten.Requests, forgotten.CostUSD)
	return nil
}
