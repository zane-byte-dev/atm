package cmd

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

var (
	sessionReviewOutcome string
	sessionReviewNote    string
)

func init() {
	sessionReviewCmd.Flags().StringVar(&sessionReviewOutcome, "outcome", "", "review outcome: none, memory, knowledge, or mixed")
	sessionReviewCmd.Flags().StringVar(&sessionReviewNote, "note", "", "short review note")
	_ = sessionReviewCmd.MarkFlagRequired("outcome")
	sessionCmd.AddCommand(sessionReviewCmd)
}

var sessionReviewCmd = &cobra.Command{
	Use:   "review <session-id>",
	Short: "Mark a session as reviewed for durable memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionReview,
}

func runSessionReview(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		session, err := store.GetSession(db, args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		review, err := knowledge.MarkSessionReviewed(session.FullID, sessionReviewOutcome, sessionReviewNote)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(review)
		} else {
			fmt.Printf("%s  %s\n", review.SessionID, review.Outcome)
		}
		return nil
	})
}
