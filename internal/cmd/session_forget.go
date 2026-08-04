package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

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
	target := args[0]
	return withDB(false, func(db *sql.DB) error {
		s, err := store.FindForgettableSession(db, target)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session not found: %s", target)
		}
		if err != nil {
			return err
		}
		if s.SourceTracked {
			return fmt.Errorf("session %s is still backed by %s: delete the transcript and run `atm sync` first, or the next sync will index it again",
				s.ShortID, s.FilePath)
		}

		prompt := fmt.Sprintf("Permanently forget session %s (%s | %s | %s)? %d messages, %d requests, $%.2f leave every total.",
			s.ShortID, s.Agent, s.Project, s.CreatedAt, s.Messages, s.Requests, s.CostUSD)
		confirmed, err := confirmDestructive(cmd, sessionForgetYesFlag, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}

		if err := store.ForgetSession(db, s.ID); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{
				"forgotten": s.ID,
				"short_id":  s.ShortID,
				"agent":     s.Agent,
				"messages":  s.Messages,
				"requests":  s.Requests,
				"cost_usd":  s.CostUSD,
			})
			return nil
		}
		fmt.Printf("Forgot session %s (%s): %d messages, %d requests, $%.2f removed.\n",
			s.ShortID, s.Agent, s.Messages, s.Requests, s.CostUSD)
		return nil
	})
}
