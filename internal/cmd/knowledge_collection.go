package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/output"
)

type collectionFlagValues struct {
	name         string
	description  string
	role         string
	topics       []string
	useWhen      []string
	avoidWhen    []string
	instructions []string
}

var (
	collectionCreateFlags collectionFlagValues
	collectionEditFlags   collectionFlagValues
	collectionDeleteForce bool
	collectionDeleteMove  string
)

var knowledgeCollectionCmd = &cobra.Command{
	Use:   "collection",
	Short: "Manage directory-backed knowledge collections",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var knowledgeCollectionCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Create an empty knowledge collection and manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeCollectionCreate,
}

var knowledgeCollectionEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit or create a collection manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeCollectionEdit,
}

var knowledgeCollectionRenameCmd = &cobra.Command{
	Use:   "rename <id> <new-id>",
	Short: "Rename a collection directory and keep all documents",
	Args:  cobra.ExactArgs(2),
	RunE:  runKnowledgeCollectionRename,
}

var knowledgeCollectionDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a collection, optionally moving its documents first",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeCollectionDelete,
}

var knowledgeCollectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List collections (alias of knowledge catalog)",
	Args:  cobra.NoArgs,
	RunE:  runKnowledgeCollectionList,
}

func init() {
	bindCollectionFlags(knowledgeCollectionCreateCmd, &collectionCreateFlags)
	bindCollectionFlags(knowledgeCollectionEditCmd, &collectionEditFlags)
	knowledgeCollectionDeleteCmd.Flags().BoolVar(&collectionDeleteForce, "force", false, "delete a non-empty collection and its documents")
	knowledgeCollectionDeleteCmd.Flags().StringVar(&collectionDeleteMove, "move-to", "", "move documents to another collection before deleting")
	knowledgeCollectionCmd.AddCommand(knowledgeCollectionCreateCmd, knowledgeCollectionEditCmd, knowledgeCollectionRenameCmd, knowledgeCollectionDeleteCmd, knowledgeCollectionListCmd)
	knowledgeCmd.AddCommand(knowledgeCollectionCmd)
}

func bindCollectionFlags(command *cobra.Command, values *collectionFlagValues) {
	command.Flags().StringVar(&values.name, "name", "", "collection display name (empty clears on edit)")
	command.Flags().StringVar(&values.description, "description", "", "collection description (empty clears on edit)")
	command.Flags().StringVar(&values.role, "role", "", "collection routing role (empty clears on edit)")
	command.Flags().StringArrayVar(&values.topics, "topic", nil, "collection topic; repeat for multiple values")
	command.Flags().StringArrayVar(&values.useWhen, "use-when", nil, "when agents should use this collection; repeatable")
	command.Flags().StringArrayVar(&values.avoidWhen, "avoid-when", nil, "when agents should avoid this collection; repeatable")
	command.Flags().StringArrayVar(&values.instructions, "instruction", nil, "collection routing instruction; repeatable")
}

func collectionEditInput(command *cobra.Command, values collectionFlagValues) knowledge.EditCollectionInput {
	input := knowledge.EditCollectionInput{}
	if command.Flags().Changed("name") {
		input.Name = &values.name
	}
	if command.Flags().Changed("description") {
		input.Description = &values.description
	}
	if command.Flags().Changed("role") {
		input.Role = &values.role
	}
	if command.Flags().Changed("topic") {
		input.Topics = &values.topics
	}
	if command.Flags().Changed("use-when") {
		input.UseWhen = &values.useWhen
	}
	if command.Flags().Changed("avoid-when") {
		input.AvoidWhen = &values.avoidWhen
	}
	if command.Flags().Changed("instruction") {
		input.Instructions = &values.instructions
	}
	return input
}

func runKnowledgeCollectionCreate(cmd *cobra.Command, args []string) error {
	collection, err := knowledge.CreateCollection(config.AtmDir, args[0], collectionEditInput(cmd, collectionCreateFlags))
	if err != nil {
		return err
	}
	return printCollectionResult(collection, "Created")
}

func runKnowledgeCollectionEdit(cmd *cobra.Command, args []string) error {
	input := collectionEditInput(cmd, collectionEditFlags)
	if input.Name == nil && input.Description == nil && input.Role == nil && input.Topics == nil && input.UseWhen == nil && input.AvoidWhen == nil && input.Instructions == nil {
		return fmt.Errorf("nothing to update; use --name, --description, --role, --topic, --use-when, --avoid-when, or --instruction")
	}
	collection, err := knowledge.EditCollection(config.AtmDir, args[0], input)
	if err != nil {
		return err
	}
	return printCollectionResult(collection, "Updated")
}

func runKnowledgeCollectionRename(cmd *cobra.Command, args []string) error {
	collection, err := knowledge.RenameCollection(config.AtmDir, args[0], args[1])
	if err != nil {
		return err
	}
	return printCollectionResult(collection, "Renamed")
}

func runKnowledgeCollectionDelete(cmd *cobra.Command, args []string) error {
	if collectionDeleteForce && strings.TrimSpace(collectionDeleteMove) != "" {
		return fmt.Errorf("use either --force or --move-to, not both")
	}
	result, err := knowledge.DeleteCollection(config.AtmDir, args[0], knowledge.DeleteCollectionOptions{Force: collectionDeleteForce, MoveTo: collectionDeleteMove})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	if result.MovedTo != "" {
		fmt.Printf("Deleted collection %s after moving %d documents to %s\n", result.ID, result.MovedDocuments, result.MovedTo)
	} else {
		fmt.Printf("Deleted collection %s\n", result.ID)
	}
	return nil
}

func runKnowledgeCollectionList(cmd *cobra.Command, args []string) error {
	return printCollectionCatalog(true)
}

func printCollectionResult(collection *knowledge.CollectionInfo, action string) error {
	if jsonOutput {
		output.JSON(collection)
		return nil
	}
	fmt.Printf("%s collection %s: %s\n", action, collection.ID, collection.Name)
	return nil
}
