package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/output"
)

var (
	knowledgeLimit             int
	knowledgeCatalogVerbose    bool
	knowledgeSearchCollections []string
	knowledgeSearchDomains     []string
	knowledgeSearchTags        []string
	knowledgeSearchProjects    []string
	knowledgeSearchStatuses    []string
	knowledgeSearchSession     string
	knowledgeListCollections   []string
	knowledgeListLimit         int
	knowledgeListOffset        int
	knowledgeQualityLimit      int
	knowledgeQualityOffset     int
	knowledgeQualityIssuesOnly bool
	knowledgeQualitySummary    bool
	knowledgeAuditStaleDays    int
	knowledgeFeedbackSession   string
	knowledgeFeedbackQuery     string
	knowledgeFeedbackOutcome   string
	knowledgeFeedbackNote      string
	knowledgeAddFile           string
	knowledgeAddCollection     string
	knowledgeAddDomains        []string
	knowledgeAddTags           []string
	knowledgeAddProjects       []string
	knowledgeAddProducer       string
	knowledgeUpdateFile        string
	knowledgeDeleteYes         bool
	knowledgeEditTitle         string
	knowledgeEditCollection    string
	knowledgeEditStatus        string
	knowledgeEditDomains       []string
	knowledgeEditTags          []string
	knowledgeEditProjects      []string
	knowledgeImportDomains     []string
	knowledgeImportCollection  string
	knowledgeImportTags        []string
	knowledgeImportProjects    []string
	knowledgeImportProducer    string
	memoryRecallScope          string
	memoryWriteScope           string
	memoryTags                 []string
	memorySource               string
	memoryWriteFile            string
	memoryLimit                int
	artifactFile               string
	artifactProducer           string
	artifactSourceRaw          []string
)

func init() {
	knowledgeCatalogCmd.Flags().BoolVar(&knowledgeCatalogVerbose, "verbose", false, "show collection descriptions and all routing topics")
	knowledgeSearchCmd.Flags().IntVar(&knowledgeLimit, "limit", 10, "maximum number of chunks")
	knowledgeSearchCmd.Flags().StringSliceVar(&knowledgeSearchCollections, "collection", nil, "filter by knowledge collection")
	knowledgeSearchCmd.Flags().StringSliceVar(&knowledgeSearchDomains, "domain", nil, "filter by knowledge domain")
	knowledgeSearchCmd.Flags().StringSliceVar(&knowledgeSearchTags, "tag", nil, "filter by tag")
	knowledgeSearchCmd.Flags().StringSliceVar(&knowledgeSearchProjects, "project", nil, "filter by project metadata (case-insensitive exact match, repeatable)")
	knowledgeSearchCmd.Flags().StringSliceVar(&knowledgeSearchStatuses, "status", nil, "filter by document status")
	knowledgeSearchCmd.Flags().StringVar(&knowledgeSearchSession, "session", "", "session id for recording retrieval feedback")
	knowledgeListCmd.Flags().StringSliceVar(&knowledgeListCollections, "collection", nil, "filter by knowledge collection")
	knowledgeListCmd.Flags().IntVar(&knowledgeListLimit, "limit", 0, "maximum number of documents (0 means all)")
	knowledgeListCmd.Flags().IntVar(&knowledgeListOffset, "offset", 0, "number of documents to skip")
	knowledgeQualityCmd.Flags().IntVar(&knowledgeQualityLimit, "limit", 0, "maximum number of quality rows (0 means all)")
	knowledgeQualityCmd.Flags().IntVar(&knowledgeQualityOffset, "offset", 0, "number of quality rows to skip")
	knowledgeQualityCmd.Flags().BoolVar(&knowledgeQualityIssuesOnly, "issues-only", false, "show documents with corrections, rejections, or below-neutral quality")
	knowledgeQualityCmd.Flags().BoolVar(&knowledgeQualitySummary, "summary", false, "output aggregate quality counts")
	knowledgeAuditCmd.Flags().IntVar(&knowledgeAuditStaleDays, "stale-days", 180, "report active documents not updated within this many days")
	knowledgeFeedbackCmd.Flags().StringVar(&knowledgeFeedbackSession, "session", "", "session that used the knowledge (required)")
	knowledgeFeedbackCmd.Flags().StringVar(&knowledgeFeedbackQuery, "query", "", "original retrieval query")
	knowledgeFeedbackCmd.Flags().StringVar(&knowledgeFeedbackOutcome, "outcome", "", "usage result: adopted, corrected, or rejected")
	knowledgeFeedbackCmd.Flags().StringVar(&knowledgeFeedbackNote, "note", "", "optional correction or rejection note")

	knowledgeAddCmd.Flags().StringVar(&knowledgeAddFile, "file", "", "read Markdown body from file (use - for stdin)")
	knowledgeAddCmd.Flags().StringVar(&knowledgeAddCollection, "collection", "inbox", "target knowledge collection")
	knowledgeAddCmd.Flags().StringSliceVar(&knowledgeAddDomains, "domain", nil, "knowledge domain")
	knowledgeAddCmd.Flags().StringSliceVar(&knowledgeAddTags, "tag", nil, "knowledge tag")
	knowledgeAddCmd.Flags().StringSliceVar(&knowledgeAddProjects, "project", nil, "related project metadata")
	knowledgeAddCmd.Flags().StringVar(&knowledgeAddProducer, "producer", "human", "knowledge producer")
	knowledgeUpdateCmd.Flags().StringVar(&knowledgeUpdateFile, "file", "", "read Markdown body from file (use - for stdin)")
	knowledgeDeleteCmd.Flags().BoolVarP(&knowledgeDeleteYes, "yes", "y", false, "skip the permanent deletion confirmation")
	knowledgeEditCmd.Flags().StringVar(&knowledgeEditTitle, "title", "", "replace document title")
	knowledgeEditCmd.Flags().StringVar(&knowledgeEditCollection, "collection", "", "move document to collection")
	knowledgeEditCmd.Flags().StringVar(&knowledgeEditStatus, "status", "", "replace document status")
	knowledgeEditCmd.Flags().StringSliceVar(&knowledgeEditDomains, "domain", nil, "replace knowledge domains")
	knowledgeEditCmd.Flags().StringSliceVar(&knowledgeEditTags, "tag", nil, "replace knowledge tags")
	knowledgeEditCmd.Flags().StringSliceVar(&knowledgeEditProjects, "project", nil, "replace related projects")

	knowledgeImportCmd.Flags().StringSliceVar(&knowledgeImportDomains, "domain", nil, "knowledge domain")
	knowledgeImportCmd.Flags().StringVar(&knowledgeImportCollection, "collection", "", "target knowledge collection")
	knowledgeImportCmd.Flags().StringSliceVar(&knowledgeImportTags, "tag", nil, "knowledge tag")
	knowledgeImportCmd.Flags().StringSliceVar(&knowledgeImportProjects, "project", nil, "related project metadata")
	knowledgeImportCmd.Flags().StringVar(&knowledgeImportProducer, "producer", "atm-import", "import producer")
	knowledgeCmd.AddCommand(knowledgeCatalogCmd, knowledgeListCmd, knowledgeSearchCmd, knowledgeGetCmd, knowledgeAddCmd, knowledgeUpdateCmd, knowledgeEditCmd, knowledgeDeleteCmd, knowledgeImportCmd, knowledgeAuditCmd, knowledgeFeedbackCmd, knowledgeQualityCmd, knowledgeDoctorCmd)

	memoryRecallCmd.Flags().StringVar(&memoryRecallScope, "scope", "", "scope filter")
	memoryRecallCmd.Flags().IntVar(&memoryLimit, "limit", 10, "maximum number of memories")
	for _, command := range []*cobra.Command{memoryRememberCmd, memorySupersedeCmd, memoryForgetCmd} {
		command.Flags().StringVar(&memoryWriteScope, "scope", "global", "memory scope")
		command.Flags().StringVar(&memorySource, "source", "", "source reference, for example session:<id>#turn:<n>")
	}
	memoryRememberCmd.Flags().StringSliceVar(&memoryTags, "tag", nil, "memory tag")
	memorySupersedeCmd.Flags().StringSliceVar(&memoryTags, "tag", nil, "memory tag")
	for _, command := range []*cobra.Command{memoryRememberCmd, memorySupersedeCmd} {
		command.Flags().StringVar(&memoryWriteFile, "file", "", "read memory content from file (use - for stdin)")
	}
	memoryCmd.AddCommand(memoryRecallCmd, memoryRememberCmd, memorySupersedeCmd, memoryForgetCmd)

	artifactSaveCmd.Flags().StringVar(&artifactFile, "file", "", "read Markdown body from file (use - for stdin)")
	artifactSaveCmd.Flags().StringVar(&artifactProducer, "producer", "atm-cli", "artifact producer")
	artifactSaveCmd.Flags().StringSliceVar(&artifactSourceRaw, "source", nil, "source as document-id[#start-end]")
	artifactCmd.AddCommand(artifactSaveCmd)

	rootCmd.AddCommand(knowledgeCmd, memoryCmd, artifactCmd)
}

// Args: cobra.NoArgs + RunE: showHelp so unknown subcommands error instead of silently showing help.
var knowledgeCmd = &cobra.Command{Use: "knowledge", Short: "Manage the central ATM knowledge base", Args: noSubcommandArgs, RunE: showHelp}
var memoryCmd = &cobra.Command{Use: "memory", Short: "Recall and manage shared ATM memory", Args: noSubcommandArgs, RunE: showHelp}
var artifactCmd = &cobra.Command{Use: "artifact", Short: "Save versioned ATM artifacts", Args: noSubcommandArgs, RunE: showHelp}

func currentKnowledgeService() knowledge.Service {
	return knowledge.NewService(knowledge.ServiceOptions{DataDir: config.AtmDir})
}

type knowledgeQualitySummaryView struct {
	Documents  int `json:"documents"`
	Returned   int `json:"returned"`
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	Retrievals int `json:"retrievals"`
	Adopted    int `json:"adopted"`
	Corrected  int `json:"corrected"`
	Rejected   int `json:"rejected"`
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List knowledge documents without loading their content",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		documents, err := currentKnowledgeService().List(cmd.Context(), knowledge.ListInput{Collections: knowledgeListCollections})
		if err != nil {
			return err
		}
		documents, err = paginate(documents, knowledgeListOffset, knowledgeListLimit)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(documents)
			return nil
		}
		for _, document := range documents {
			fmt.Printf("%s  [%s] %s\n", document.DocumentID, document.Collection, document.Title)
		}
		return nil
	},
}

var knowledgeAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit knowledge for duplicates, staleness, source drift, and low quality",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := knowledge.Audit(config.AtmDir, knowledge.AuditOptions{StaleDays: knowledgeAuditStaleDays})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(report)
			return nil
		}
		fmt.Printf("Knowledge audit: %d active / %d total, %d issues\n", report.Active, report.Documents, len(report.Issues))
		for _, issue := range report.Issues {
			fmt.Printf("%s  %s  %s\n  %s\n  next: %s\n", issue.Severity, issue.Code, strings.Join(issue.DocumentIDs, ","), issue.Detail, issue.SuggestedAction)
		}
		return nil
	},
}

var knowledgeFeedbackCmd = &cobra.Command{
	Use:   "feedback <document-id>",
	Short: "Record whether recalled knowledge was adopted, corrected, or rejected",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		event, err := currentKnowledgeService().Feedback(cmd.Context(), knowledge.FeedbackInput{
			DocumentID: args[0], SessionID: knowledgeFeedbackSession, Query: knowledgeFeedbackQuery,
			Outcome: knowledgeFeedbackOutcome, Note: knowledgeFeedbackNote,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(event)
		} else {
			fmt.Println(event.ID)
		}
		return nil
	},
}

var knowledgeQualityCmd = &cobra.Command{
	Use:   "quality [document-id]",
	Short: "Show retrieval feedback and quality scores",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		input := knowledge.QualityInput{IssuesOnly: knowledgeQualityIssuesOnly}
		if len(args) == 1 {
			input.DocumentID = args[0]
		}
		result, err := currentKnowledgeService().Quality(cmd.Context(), input)
		if err != nil {
			return err
		}
		values := result.Qualities
		values, err = paginate(values, knowledgeQualityOffset, knowledgeQualityLimit)
		if err != nil {
			return err
		}
		if knowledgeQualitySummary {
			summary := knowledgeQualitySummaryView{
				Documents: result.Totals.Documents, Returned: len(values), Offset: knowledgeQualityOffset, Limit: knowledgeQualityLimit,
				Retrievals: result.Totals.Retrievals, Adopted: result.Totals.Adopted, Corrected: result.Totals.Corrected, Rejected: result.Totals.Rejected,
			}
			if jsonOutput {
				output.JSON(summary)
			} else {
				fmt.Printf("documents=%d returned=%d retrievals=%d adopted=%d corrected=%d rejected=%d\n", summary.Documents, summary.Returned, summary.Retrievals, summary.Adopted, summary.Corrected, summary.Rejected)
			}
			return nil
		}
		if jsonOutput {
			output.JSON(values)
			return nil
		}
		for _, value := range values {
			fmt.Printf("%.3f  [%s] %s  adopted=%d corrected=%d rejected=%d retrieved=%d\n", value.Score, value.Collection, value.Title, value.Adopted, value.Corrected, value.Rejected, value.Retrievals)
		}
		return nil
	},
}

var knowledgeSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the central knowledge base",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := currentKnowledgeService().Search(cmd.Context(), knowledge.SearchInput{
			Query: args[0], SessionID: knowledgeSearchSession,
			Options: knowledge.SearchOptions{
				Limit: knowledgeLimit, Collections: knowledgeSearchCollections, Domains: knowledgeSearchDomains, Tags: knowledgeSearchTags,
				Projects: knowledgeSearchProjects, Statuses: knowledgeSearchStatuses,
			},
		})
		if err != nil {
			return err
		}
		hits := result.Hits
		if jsonOutput {
			output.JSON(hits)
			return nil
		}
		if len(hits) == 0 {
			fmt.Println("No matching knowledge found.")
			return nil
		}
		for _, hit := range hits {
			fmt.Printf("%.3f  [%s] %s:%d-%d  %s\n  %s\n", hit.Score, hit.Collection, hit.DocumentID, hit.LineStart, hit.LineEnd, hit.Title, hit.Snippet)
		}
		return nil
	},
}

var knowledgeCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List knowledge collections for routing before search",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return printCollectionCatalog(cmd.Context(), knowledgeCatalogVerbose)
	},
}

// printCollectionCatalog takes verbosity as an argument rather than reading the
// flag, so callers other than `knowledge catalog` can ask for the long form.
// There used to be one — `knowledge collection list`, which was `catalog
// --verbose` under a second name — and it was removed rather than kept as a
// shortcut that had to be discovered separately.
func printCollectionCatalog(ctx context.Context, verbose bool) error {
	catalog, err := currentKnowledgeService().Catalog(ctx)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(catalog)
		return nil
	}
	for _, collection := range catalog {
		fmt.Printf("%s  %s  (%d documents)\n", collection.ID, collection.Name, collection.DocumentCount)
		if verbose && collection.Description != "" {
			fmt.Printf("  %s\n", collection.Description)
		}
		if len(collection.Topics) > 0 {
			topics := collection.Topics
			if !verbose && len(topics) > 4 {
				fmt.Printf("  topics: %s, +%d more\n", strings.Join(topics[:4], ", "), len(topics)-4)
			} else {
				fmt.Printf("  topics: %s\n", strings.Join(topics, ", "))
			}
		}
	}
	return nil
}

var knowledgeGetCmd = &cobra.Command{
	Use:   "get <document-id>",
	Short: "Read a central knowledge document",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		document, err := currentKnowledgeService().Get(cmd.Context(), knowledge.GetInput{DocumentID: args[0]})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(document)
		} else {
			fmt.Print(document.Content)
		}
		return nil
	},
}

var knowledgeUpdateCmd = &cobra.Command{
	Use:   "update <document-id> [markdown]",
	Short: "Update a knowledge document body",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readCommandBody(args, 1, knowledgeUpdateFile)
		if err != nil {
			return err
		}
		document, err := currentKnowledgeService().SaveDocument(cmd.Context(), knowledge.SaveDocumentInput{
			Content: &knowledge.SetDocumentContentInput{DocumentID: args[0], Content: body},
		})
		if err != nil {
			return err
		}
		return printRecordID(document, document.Metadata.ID)
	},
}

var knowledgeEditCmd = &cobra.Command{
	Use:   "edit <document-id>",
	Short: "Edit knowledge document metadata or archive it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		input := knowledge.SetDocumentMetadataInput{DocumentID: args[0]}
		if cmd.Flags().Changed("title") {
			input.Title = &knowledgeEditTitle
		}
		if cmd.Flags().Changed("collection") {
			input.Collection = &knowledgeEditCollection
		}
		if cmd.Flags().Changed("status") {
			input.Status = &knowledgeEditStatus
		}
		if cmd.Flags().Changed("domain") {
			input.Domains = &knowledgeEditDomains
		}
		if cmd.Flags().Changed("tag") {
			input.Tags = &knowledgeEditTags
		}
		if cmd.Flags().Changed("project") {
			input.Projects = &knowledgeEditProjects
		}
		document, err := currentKnowledgeService().SaveDocument(cmd.Context(), knowledge.SaveDocumentInput{Metadata: &input})
		if err != nil {
			return err
		}
		return printRecordID(document, document.Metadata.ID)
	},
}

var knowledgeDeleteCmd = &cobra.Command{
	Use:   "delete <document-id>",
	Short: "Permanently delete a knowledge document without deleting its external imported source",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := confirmDestructive(
			cmd,
			knowledgeDeleteYes,
			fmt.Sprintf("Permanently delete knowledge document %s?", args[0]),
		)
		if err != nil || !confirmed {
			return err
		}
		document, err := currentKnowledgeService().DeleteDocument(cmd.Context(), knowledge.DeleteDocumentInput{
			DocumentID: args[0], Confirmed: true,
		})
		if err != nil {
			return err
		}
		return printRecordID(document, document.Metadata.ID)
	},
}

var knowledgeAddCmd = &cobra.Command{
	Use:   "add <title> [markdown]",
	Short: "Add a document to the central knowledge base",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readCommandBody(args, 1, knowledgeAddFile)
		if err != nil {
			return err
		}
		document, err := currentKnowledgeService().SaveDocument(cmd.Context(), knowledge.SaveDocumentInput{
			Create: &knowledge.CreateDocumentInput{
				Title: args[0], Content: body, Collection: knowledgeAddCollection, Domains: knowledgeAddDomains,
				Tags: knowledgeAddTags, Projects: knowledgeAddProjects, Producer: knowledgeAddProducer,
			},
		})
		if err != nil {
			return err
		}
		return printRecordID(document, document.Metadata.ID)
	},
}

var knowledgeImportCmd = &cobra.Command{
	Use:   "import <file-or-directory>",
	Short: "Explicitly import Markdown into the central knowledge base",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		documents, err := currentKnowledgeService().ImportDocument(cmd.Context(), knowledge.ImportDocumentInput{
			Path:       args[0],
			Collection: knowledgeImportCollection, Domains: knowledgeImportDomains, Tags: knowledgeImportTags,
			Projects: knowledgeImportProjects, Producer: knowledgeImportProducer,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(documents)
		} else {
			for _, document := range documents {
				fmt.Println(document.Metadata.ID)
			}
		}
		return nil
	},
}

var knowledgeDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Inspect central knowledge, memory, and artifact storage",
	RunE: func(cmd *cobra.Command, args []string) error {
		documents, err := knowledge.Discover(config.AtmDir)
		if err != nil {
			return err
		}
		orphans, err := knowledge.OrphanedFeedbackDocuments(config.AtmDir)
		if err != nil {
			return err
		}
		memories, err := knowledge.MemoryEventCount()
		if err != nil {
			return err
		}
		result := struct {
			KnowledgeSchema int      `json:"knowledge_schema"`
			MemorySchema    int      `json:"memory_schema"`
			ArtifactSchema  int      `json:"artifact_schema"`
			Documents       int      `json:"documents"`
			MemoryEvents    int      `json:"memory_events"`
			KnowledgePath   string   `json:"knowledge_path"`
			DatabasePath    string   `json:"database_path"`
			ArtifactsPath   string   `json:"artifacts_path"`
			OrphanFeedback  []string `json:"orphan_feedback_documents"`
		}{
			knowledge.KnowledgeSchemaVersion, knowledge.MemorySchemaVersion, knowledge.ArtifactSchemaVersion,
			len(documents), memories, filepath.Join(config.AtmDir, "knowledge"),
			config.AtmDB, filepath.Join(config.AtmDir, "artifacts"), orphans,
		}
		if jsonOutput {
			output.JSON(result)
		} else {
			fmt.Printf("Knowledge: v%d (%d documents)\nMemory: v%d (%d events)\nArtifacts: v%d\nMarkdown: %s\nDatabase: %s\n",
				result.KnowledgeSchema, result.Documents, result.MemorySchema, result.MemoryEvents,
				result.ArtifactSchema, result.KnowledgePath, result.DatabasePath)
			// Feedback names markdown files, which no foreign key can guard. A
			// document removed outside ATM leaves rows behind; say so.
			if len(orphans) > 0 {
				fmt.Printf("Feedback for %d missing document(s): %s\n",
					len(orphans), strings.Join(orphans, ", "))
			}
		}
		return nil
	},
}

var memoryRecallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Recall shared ATM memory",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		result, err := currentKnowledgeService().RecallMemory(cmd.Context(), knowledge.RecallMemoryInput{
			Query: query,
			Scope: memoryRecallScope,
			Limit: memoryLimit,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(result.Hits)
			return nil
		}
		if len(result.Hits) == 0 {
			fmt.Println("No matching memories found.")
			return nil
		}
		for _, hit := range result.Hits {
			fmt.Printf("%.3f  %s  %s\n  %s\n", hit.Score, hit.ID, hit.Scope, hit.Content)
		}
		return nil
	},
}

var memoryRememberCmd = &cobra.Command{
	Use: "remember [content]", Short: "Append a shared memory fact", Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := readCommandBody(args, 0, memoryWriteFile)
		if err != nil {
			return err
		}
		event, err := knowledge.RememberWithMetadata(memoryWriteScope, content, memoryTags, memoryWriteMetadata())
		return printMemoryEvent(event, err)
	},
}

var memorySupersedeCmd = &cobra.Command{
	Use: "supersede <memory-id> [content]", Short: "Replace an active memory fact", Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := readCommandBody(args, 1, memoryWriteFile)
		if err != nil {
			return err
		}
		result, err := currentKnowledgeService().SupersedeMemory(cmd.Context(), knowledge.SupersedeMemoryInput{
			TargetID: args[0],
			Scope:    memoryWriteScope,
			Content:  content,
			Tags:     memoryTags,
			Source:   memorySource,
		})
		if err != nil {
			return err
		}
		return printMemoryEvent(&result.Event, nil)
	},
}

var memoryForgetCmd = &cobra.Command{
	Use: "forget <memory-id>", Short: "Forget an active memory fact", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		event, err := knowledge.ForgetWithMetadata(args[0], memoryWriteScope, memoryWriteMetadata())
		return printMemoryEvent(event, err)
	},
}

func memoryWriteMetadata() map[string]string {
	if strings.TrimSpace(memorySource) == "" {
		return nil
	}
	return map[string]string{"source": strings.TrimSpace(memorySource)}
}

var artifactSaveCmd = &cobra.Command{
	Use: "save <title> [markdown]", Short: "Save an artifact in ATM central storage", Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readCommandBody(args, 1, artifactFile)
		if err != nil {
			return err
		}
		sources, err := parseSourceRefs(artifactSourceRaw)
		if err != nil {
			return err
		}
		artifact, err := knowledge.SaveArtifact(config.AtmDir, args[0], body, artifactProducer, sources)
		if err != nil {
			return err
		}
		return printRecordID(artifact, artifact.Metadata.ID)
	},
}

func readCommandBody(args []string, bodyIndex int, fileFlag string) (string, error) {
	if fileFlag != "" {
		var data []byte
		var err error
		if fileFlag == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(fileFlag)
		}
		return string(data), err
	}
	if len(args) > bodyIndex {
		return args[bodyIndex], nil
	}
	return "", fmt.Errorf("provide Markdown body or --file")
}

// printRecordID is printMemoryEvent's shape for the document-shaped commands:
// JSON callers want the whole record, humans and shell pipelines want only the
// ID. Document and Artifact carry different metadata types, so the ID comes in
// already resolved rather than through a constraint that would fit neither.
func printRecordID(record any, id string) error {
	if jsonOutput {
		output.JSON(record)
		return nil
	}
	fmt.Println(id)
	return nil
}

func printMemoryEvent(event *knowledge.MemoryEvent, err error) error {
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(event)
	} else {
		fmt.Println(event.ID)
	}
	return nil
}

func parseSourceRefs(values []string) ([]knowledge.SourceRef, error) {
	sources := make([]knowledge.SourceRef, 0, len(values))
	for _, value := range values {
		ref := knowledge.SourceRef{DocumentID: value}
		if marker := strings.LastIndex(value, "#"); marker >= 0 {
			lineRange := strings.SplitN(value[marker+1:], "-", 2)
			start, err := strconv.Atoi(lineRange[0])
			if err != nil {
				return nil, fmt.Errorf("invalid source line range: %s", value)
			}
			ref.LineStart = start
			ref.LineEnd = start
			if len(lineRange) == 2 {
				ref.LineEnd, err = strconv.Atoi(lineRange[1])
			}
			if err != nil || ref.LineStart <= 0 || ref.LineEnd < ref.LineStart {
				return nil, fmt.Errorf("invalid source line range: %s", value)
			}
			ref.DocumentID = value[:marker]
		}
		if strings.TrimSpace(ref.DocumentID) == "" {
			return nil, fmt.Errorf("source document id must not be empty: %s", value)
		}
		sources = append(sources, ref)
	}
	return sources, nil
}
