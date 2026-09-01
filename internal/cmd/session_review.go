package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
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
	result, err := currentSessionService().Review(cmd.Context(), sessionapp.ReviewInput{
		SessionID: args[0], Outcome: sessionReviewOutcome, Note: sessionReviewNote,
		SyncBeforeRead: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	renderSessionReadMeta(result.Meta)
	if jsonOutput {
		output.JSON(result.Review)
		return nil
	}
	fmt.Printf("%s  %s\n", result.Review.SessionID, result.Review.Outcome)
	return nil
}
