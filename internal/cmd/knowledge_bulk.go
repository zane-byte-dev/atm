package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/output"
)

var (
	knowledgeBulkCollection string
	knowledgeBulkStatus     string
)

var knowledgeBulkEditCmd = &cobra.Command{
	Use:   "bulk-edit <document-id>...",
	Short: "Move or change the status of multiple knowledge documents",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runKnowledgeBulkEdit,
}

func init() {
	knowledgeBulkEditCmd.Flags().StringVar(&knowledgeBulkCollection, "collection", "", "move documents to this collection")
	knowledgeBulkEditCmd.Flags().StringVar(&knowledgeBulkStatus, "status", "", "set status: active, draft, or archived")
	knowledgeCmd.AddCommand(knowledgeBulkEditCmd)
}

func runKnowledgeBulkEdit(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("collection") && !cmd.Flags().Changed("status") {
		return fmt.Errorf("bulk-edit requires --collection or --status")
	}
	if cmd.Flags().Changed("collection") && strings.TrimSpace(knowledgeBulkCollection) == "" {
		return fmt.Errorf("bulk-edit collection must not be empty")
	}
	if cmd.Flags().Changed("status") && knowledgeBulkStatus != "active" && knowledgeBulkStatus != "draft" && knowledgeBulkStatus != "archived" {
		return fmt.Errorf("invalid knowledge status %q", knowledgeBulkStatus)
	}
	ids := uniqueStrings(args)
	for _, id := range ids {
		if _, err := currentKnowledgeService().Get(cmd.Context(), knowledge.GetInput{DocumentID: id}); err != nil {
			return fmt.Errorf("preflight %s: %w", id, err)
		}
	}
	input := knowledge.SetDocumentMetadataInput{}
	if cmd.Flags().Changed("collection") {
		input.Collection = &knowledgeBulkCollection
	}
	if cmd.Flags().Changed("status") {
		input.Status = &knowledgeBulkStatus
	}
	updated := make([]knowledge.Document, 0, len(ids))
	for _, id := range ids {
		input.DocumentID = id
		document, err := currentKnowledgeService().SaveDocument(cmd.Context(), knowledge.SaveDocumentInput{Metadata: &input})
		if err != nil {
			return fmt.Errorf("bulk edit %s after %d updates: %w", id, len(updated), err)
		}
		updated = append(updated, document)
	}
	if jsonOutput {
		output.JSON(updated)
		return nil
	}
	fmt.Printf("Updated %d knowledge documents\n", len(updated))
	return nil
}
