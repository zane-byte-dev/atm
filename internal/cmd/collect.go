package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

var (
	collectSourceKind       string
	collectSourceConnector  string
	collectSourceExternalID string
	collectSourceName       string
	collectSourceProject    string
	collectSourceExclude    string
	collectSourceFocus      string
	collectSourceKnowledge  string
	collectSourceStrategy   string
	collectSourceUnit       string
	collectSourceInterval   int
	collectSourcePriority   string
	collectSourceDisabled   bool
	collectSourceID         string
	collectSearchKind       string
	collectSearchConnector  string
	collectSearchLimit      int
	collectHistoryKind      string
	collectHistoryConnector string
	collectHistorySince     string
	collectHistoryLimit     int
	collectHistoryLocal     bool
	collectSearchSource     string
	collectSearchSender     string
	collectSearchSince      string
	collectSearchMessages   int
	collectAnalyzeSince     string
	collectAnalyzeLimit     int
	collectAnalyzeBatches   int
	collectAnalyzeLocal     bool
	collectAnalyzeApply     bool
	collectLimit            int
	collectRunDue           bool
	collectDigestDate       string
	collectDigestDue        bool
	collectDigestDryRun     bool
	collectYes              bool
	collectItemTitle        string
	collectItemProject      string
	collectItemPriority     string
)

func init() {
	collectSourceAddCmd.Flags().StringVar(&collectSourceConnector, "connector", "",
		"registered connector id")
	collectSourceSearchCmd.Flags().StringVar(&collectSearchConnector, "connector", "",
		"registered connector id")
	collectSourceAddCmd.Flags().StringVar(&collectSourceKind, "kind", "", "connector-defined source kind")
	collectSourceAddCmd.Flags().StringVar(&collectSourceExternalID, "id", "", "connector source identifier")
	collectSourceAddCmd.Flags().StringVar(&collectSourceName, "name", "", "human-readable source name")
	collectSourceSearchCmd.Flags().StringVar(&collectSearchKind, "kind", "all", "search kind: group, user, or all")
	collectSourceSearchCmd.Flags().IntVar(&collectSearchLimit, "limit", 10, "maximum candidates to return")
	collectHistoryCmd.Flags().StringVar(&collectHistoryKind, "kind", "all", "name lookup kind: group, user, or all")
	collectHistoryCmd.Flags().StringVar(&collectHistoryConnector, "connector", "",
		"connector used when the argument is not an added source")
	collectHistoryCmd.Flags().StringVar(&collectHistorySince, "since", "",
		"read forward from RFC3339 timestamp or YYYY-MM-DD instead of back from now")
	collectHistoryCmd.Flags().IntVar(&collectHistoryLimit, "limit", 50, "maximum messages to read")
	collectHistoryCmd.Flags().BoolVar(&collectHistoryLocal, "local", false,
		"read only messages already synced, without calling the connector")
	collectSearchCmd.Flags().StringVar(&collectSearchSource, "source", "",
		"limit to one source id or the name of an added source")
	collectSearchCmd.Flags().StringVar(&collectSearchSender, "sender", "", "limit to one sender")
	collectSearchCmd.Flags().StringVar(&collectSearchSince, "since", "",
		"only messages at or after RFC3339 timestamp or YYYY-MM-DD")
	collectSearchCmd.Flags().IntVar(&collectSearchMessages, "limit", 20, "maximum matches to return")
	collectAnalyzeCmd.Flags().StringVar(&collectAnalyzeSince, "since", "",
		"analyse from RFC3339 timestamp or YYYY-MM-DD instead of the most recent messages")
	collectAnalyzeCmd.Flags().IntVar(&collectAnalyzeLimit, "limit", 50, "maximum messages to analyse")
	collectAnalyzeCmd.Flags().IntVar(&collectAnalyzeBatches, "max-batches", 20,
		"maximum conversation batches to classify; each one costs a model call")
	collectAnalyzeCmd.Flags().BoolVar(&collectAnalyzeLocal, "local", false,
		"analyse only messages already synced, without calling the connector")
	collectAnalyzeCmd.Flags().BoolVar(&collectAnalyzeApply, "apply", false,
		"create Todos instead of holding the decisions for confirmation")
	collectSourceAddCmd.Flags().StringVar(&collectSourceProject, "project", "", "default ATM project for extracted work")
	collectSourceAddCmd.Flags().StringVar(&collectSourceExclude, "exclude", "", "comma-separated message keywords to ignore")
	collectSourceAddCmd.Flags().StringVar(&collectSourceFocus, "instruction", "",
		"what to watch this source for, in your own words (e.g. 只关注 MR 和需求)")
	collectSourceAddCmd.Flags().StringVar(&collectSourceKnowledge, "knowledge-collection", "",
		"knowledge collection this source's daily digest is filed into (default: "+
			config.CollectionDigestCollection+")")
	collectSourceAddCmd.Flags().StringVar(&collectSourceStrategy, "strategy", store.CollectionStrategyTasks,
		"processing strategy: tasks (may create Todos) or observe (knowledge only)")
	collectSourceAddCmd.Flags().StringVar(&collectSourceUnit, "decision-unit", store.CollectionDecisionUnitWindow,
		"what one decision covers: window (messages within 15 minutes) or message "+
			"(each message on its own — for notification feeds)")
	collectSourceAddCmd.Flags().IntVar(&collectSourceInterval, "interval", 0,
		"source collection interval in minutes (default: tasks 5, observe 60)")
	collectSourceAddCmd.Flags().StringVar(&collectSourcePriority, "priority", "P2", "default priority: P0, P1, P2, P3")
	collectSourceAddCmd.Flags().BoolVar(&collectSourceDisabled, "disabled", false, "add the source disabled")
	collectRunCmd.Flags().StringVar(&collectSourceID, "source", "", "run one source id instead of every enabled source")
	collectRunCmd.Flags().BoolVar(&collectRunDue, "due", false, "run only sources whose own interval is due")
	collectDigestCmd.Flags().StringVar(&collectSourceID, "source", "", "digest one source id instead of every enabled source")
	collectDigestCmd.Flags().StringVar(&collectDigestDate, "date", "", "local day to digest as YYYY-MM-DD (default: today)")
	collectDigestCmd.Flags().BoolVar(&collectDigestDue, "due", false,
		"skip sources digested within the last "+fmt.Sprint(config.CollectionDigestIntervalMinutes)+" minutes")
	collectDigestCmd.Flags().BoolVar(&collectDigestDryRun, "dry-run", false,
		"print the digest without writing it to the knowledge base")
	collectStatusCmd.Flags().IntVar(&collectLimit, "limit", 100, "maximum recent collection items")
	collectSourceDeleteCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")
	for _, command := range []*cobra.Command{collectItemPromoteCmd, collectItemCorrectCmd} {
		command.Flags().StringVar(&collectItemTitle, "title", "", "corrected Todo title")
		command.Flags().StringVar(&collectItemProject, "project", "", "corrected ATM project (empty clears it)")
		command.Flags().StringVar(&collectItemPriority, "priority", "", "corrected priority: P0, P1, P2, P3")
	}
	collectItemRevertCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")
	collectItemDeleteCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")

	collectSourceCmd.AddCommand(collectSourceListCmd, collectSourceSearchCmd, collectSourceAddCmd,
		collectSourceEnableCmd, collectSourceDisableCmd, collectSourceDeleteCmd)
	collectItemCmd.AddCommand(collectItemReprocessCmd, collectItemPromoteCmd, collectItemCorrectCmd,
		collectItemRevertCmd, collectItemDeleteCmd)
	collectCmd.AddCommand(collectStatusCmd, collectRunCmd, collectDigestCmd, collectEnableCmd,
		collectDisableCmd, collectHistoryCmd, collectSearchCmd, collectAnalyzeCmd,
		collectSourceCmd, collectItemCmd)
	rootCmd.AddCommand(collectCmd)
}

var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Automatically collect work from external connectors",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var collectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show connector health, sources, runs, and recent decisions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Status is commonly the first collection command run after an upgrade.
		// Open writable so pending schema migrations happen before its queries.
		return withDB(false, func(db *sql.DB) error {
			overview, err := store.LoadCollectionOverview(db, collectLimit)
			if err != nil {
				return err
			}
			archive, err := store.CollectionMessageStatsFor(db)
			if err != nil {
				return err
			}
			value := struct {
				Enabled         bool                         `json:"enabled"`
				IntervalMinutes int                          `json:"interval_minutes"`
				LookbackMinutes int                          `json:"lookback_minutes"`
				RetentionDays   int                          `json:"message_retention_days"`
				ModelCommand    string                       `json:"model_command"`
				ConnectorHealth []collectionConnectorHealth  `json:"connector_health"`
				Messages        store.CollectionMessageStats `json:"messages"`
				store.CollectionOverview
			}{config.CollectionEnabled, config.CollectionIntervalMinutes,
				config.CollectionLookbackMinutes, config.CollectionMessageRetentionDays,
				config.CollectionModelCommand,
				collectionHealth(overview), archive, overview}
			if jsonOutput {
				output.JSON(value)
				return nil
			}
			fmt.Printf("Automatic collection: %v · every %dm · %d/%d sources enabled\n",
				value.Enabled, value.IntervalMinutes, value.Summary.Enabled, value.Summary.Sources)
			fmt.Printf("Today: fetched %d · created %d · appended %d · insight %d · ignored %d · failed %d\n",
				value.Summary.Fetched, value.Summary.Created, value.Summary.Appended,
				value.Summary.Insight, value.Summary.Ignored, value.Summary.Failed)
			for _, digest := range collectionTodaysDigests(value.Digests) {
				fmt.Printf("Digest %s: %s · %s（%d 条）\n", digest.DigestDate,
					emptyAs(digest.Title, digest.DocumentID), digest.Collection, digest.ItemCount)
			}
			fmt.Printf("Synced chat: %d messages · %d conversations%s · %s\n",
				value.Messages.Total, value.Messages.Conversations,
				collectionArchiveSpan(value.Messages), collectionRetentionText(value.RetentionDays))
			if value.Summary.Followups > 0 {
				// A record that filed a Todo is only really finished when that Todo
				// is: the whole point of collecting was to get it done.
				fmt.Printf("Filed Todos: %d · %d still open\n", value.Summary.Followups,
					value.Summary.Followups-value.Summary.FollowupsClosed)
			}
			if pending := collectionPendingProposals(value.Items); pending > 0 {
				// Proposals wait on a person; without a count they are easy to forget.
				fmt.Printf("Awaiting confirmation: %d · atm collect item promote <item-id>\n", pending)
			}
			for _, health := range value.ConnectorHealth {
				detail := ""
				if health.Error != "" {
					detail = " · " + health.Error
				}
				fmt.Printf("%s: %s%s\n", health.Connector, health.Status, detail)
			}
			return nil
		})
	},
}

// collectionTodaysDigests narrows the digest ledger to today, so status stays a
// picture of the current day rather than a growing list of past documents.
func collectionTodaysDigests(digests []store.CollectionDigest) []store.CollectionDigest {
	today := time.Now().In(config.Loc).Format("2006-01-02")
	current := []store.CollectionDigest{}
	for _, digest := range digests {
		if digest.DigestDate == today {
			current = append(current, digest)
		}
	}
	return current
}

// collectionPendingProposals counts decisions an on-demand analysis is holding.
func collectionPendingProposals(items []store.CollectionItem) int {
	pending := 0
	for _, item := range items {
		if item.ProposedAction != "" {
			pending++
		}
	}
	return pending
}

func collectionArchiveSpan(stats store.CollectionMessageStats) string {
	if stats.Oldest == 0 {
		return ""
	}
	return " · " + time.Unix(stats.Oldest, 0).In(config.Loc).Format("2006-01-02") + " onwards"
}

func collectionRetentionText(days int) string {
	if days < 1 {
		return "kept forever"
	}
	return fmt.Sprintf("kept %d days", days)
}

type collectionConnectorHealth struct {
	Connector string `json:"connector"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	CheckedAt int64  `json:"checked_at,omitempty"`
}

func collectionHealth(overview store.CollectionOverview) []collectionConnectorHealth {
	connectorIDs := map[string]bool{}
	if registry, err := collector.DefaultRegistry(); err == nil {
		for _, id := range registry.IDs() {
			connectorIDs[id] = true
		}
	}
	for _, source := range overview.Sources {
		connectorIDs[source.Connector] = true
	}
	for _, run := range overview.Runs {
		connectorIDs[run.Connector] = true
	}
	healthByID := map[string]collectionConnectorHealth{}
	for id := range connectorIDs {
		healthByID[id] = collectionConnectorHealth{Connector: id, Status: "not_checked"}
	}
	for _, run := range overview.Runs {
		health, ok := healthByID[run.Connector]
		if !ok || health.CheckedAt != 0 || run.Status == "running" {
			continue
		}
		health.CheckedAt = run.FinishedAt
		if run.Status == "succeeded" {
			health.Status = "ready"
		} else {
			health.Status, health.Error = collectionFailureStatus(run.Error), run.Error
		}
		healthByID[run.Connector] = health
	}
	ids := make([]string, 0, len(healthByID))
	for id := range healthByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	health := make([]collectionConnectorHealth, 0, len(ids))
	for _, id := range ids {
		health = append(health, healthByID[id])
	}
	return health
}

func collectionFailureStatus(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "not_authenticated") || strings.Contains(lower, "auth login") ||
		strings.Contains(lower, "未登录") || strings.Contains(lower, "登录失效") {
		return "auth_required"
	}
	if strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "权益") || strings.Contains(lower, "权限") {
		return "permission_required"
	}
	return "error"
}

var collectRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run enabled collectors once",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		service := collector.DefaultService()
		var report collector.RunReport
		var err error
		if collectRunDue {
			report, err = service.RunDue(context.Background(), collectSourceID)
		} else {
			report, err = service.Run(context.Background(), collectSourceID)
		}
		if jsonOutput {
			output.JSON(report)
		}
		if err != nil {
			return err
		}
		if !jsonOutput {
			if len(report.Runs) == 0 {
				fmt.Println("No enabled collection sources.")
				return nil
			}
			for _, run := range report.Runs {
				fmt.Printf("%s: fetched=%d analyzed=%d created=%d appended=%d insight=%d ignored=%d failed=%d\n",
					run.SourceID, run.FetchedCount, run.AnalyzedCount, run.CreatedCount,
					run.AppendedCount, run.InsightCount, run.IgnoredCount, run.FailedCount)
			}
		}
		return nil
	},
}

var collectDigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Distil a source's insights for one day into a knowledge document",
	Long: "Turn the 'insight' decisions collection made for a day into one knowledge " +
		"document per source. A day's digest is rewritten in place as more insights " +
		"arrive, so running this repeatedly never files a second document for the same day.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := collector.DefaultService().Digest(context.Background(), collectSourceID,
			collector.DigestOptions{Date: collectDigestDate, DueOnly: collectDigestDue,
				DryRun: collectDigestDryRun})
		if jsonOutput {
			output.JSON(report)
		}
		if err != nil {
			return err
		}
		if jsonOutput {
			return nil
		}
		if len(report.Results) == 0 {
			fmt.Println("No enabled collection sources.")
			return nil
		}
		for _, result := range report.Results {
			name := emptyAs(result.SourceName, result.SourceID)
			switch result.Status {
			case "skipped":
				fmt.Printf("%s %s: skipped · %s\n", name, result.Date, result.Reason)
			default:
				fmt.Printf("%s %s: %s · %s · %s（%d 条沉淀）\n", name, result.Date, result.Status,
					result.Collection, result.Title, result.ItemCount)
			}
			if result.Body != "" {
				fmt.Printf("\n%s\n\n", result.Body)
			}
		}
		return nil
	},
}

var collectEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable automatic collection",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setCollectionEnabled(true)
	},
}

var collectDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable automatic collection",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setCollectionEnabled(false)
	},
}

func setCollectionEnabled(enabled bool) error {
	if err := config.SetConfigValue("collection_enabled", enabled); err != nil {
		return err
	}
	config.CollectionEnabled = enabled
	if jsonOutput {
		output.JSON(map[string]any{"enabled": enabled})
	} else {
		fmt.Printf("Automatic collection enabled = %v\n", enabled)
	}
	return nil
}

var collectSourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage connector sources",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var collectSourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List collection sources",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(true, func(db *sql.DB) error {
			sources, err := store.ListCollectionSources(db, "", false)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(sources)
				return nil
			}
			for _, source := range sources {
				fmt.Printf("%-20s %-8s %-5s %-24s strategy=%-7s every=%4dm project=%-12s knowledge=%-12s enabled=%v %s\n",
					source.ID, source.Connector, source.Kind, source.ExternalID,
					source.Strategy, source.IntervalMinutes, emptyAs(source.Project, "-"),
					emptyAs(source.KnowledgeCollection, config.CollectionDigestCollection),
					source.Enabled, source.Name)
				// Printed only when it deviates: a column reading "window" on
				// every row would just widen an already wide line.
				if source.DecisionUnit == store.CollectionDecisionUnitMessage {
					fmt.Printf("%-20s 每条消息单独判定，同一时段的其他消息只作上下文\n", "")
				}
				if source.Instruction != "" {
					fmt.Printf("%-20s 关注：%s\n", "", source.Instruction)
				}
			}
			return nil
		})
	},
}

var collectSourceSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Find connector sources by name before adding them",
	Long:  "Ask a registered connector to find collection source candidates by name.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		searcher, err := collectionSearchConnectorFor(collectSearchConnector)
		if err != nil {
			return err
		}
		candidates, err := searcher.Search(context.Background(), collectSearchKind, args[0], collectSearchLimit)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"candidates": candidates})
			return nil
		}
		if len(candidates) == 0 {
			fmt.Printf("没有找到匹配「%s」的来源。%s\n", args[0], collectionSearchHint)
			return nil
		}
		fmt.Print(collectionCandidateList(candidates))
		return nil
	},
}

const collectionSearchHint = "换个更短、连续且能区分来源的关键词试试。"

func collectionCandidateList(candidates []collector.Candidate) string {
	var builder strings.Builder
	for index, candidate := range candidates {
		parts := []string{candidate.Name}
		if candidate.Detail != "" {
			parts = append(parts, candidate.Detail)
		}
		parts = append(parts, candidate.ExternalID)
		fmt.Fprintf(&builder, "%d. [%s] %s\n", index+1,
			collectionKindLabel(candidate.Kind), strings.Join(parts, " · "))
	}
	return builder.String()
}

func collectionKindLabel(kind string) string {
	switch kind {
	case "group":
		return "群聊"
	case "user", "contact":
		return "联系人"
	case "", collector.DirectoryKindAll:
		return "来源"
	}
	return kind
}

var collectSourceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add or update a connector source",
	Long:  "Add or update a collection source using its connector-defined kind and identifier.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := collectionSourceTarget(context.Background())
		if err != nil {
			return err
		}
		if strings.TrimSpace(collectSourceName) != "" {
			target.Name = collectSourceName
		}
		return withDB(false, func(db *sql.DB) error {
			source, err := store.UpsertCollectionSource(db, store.CollectionSource{
				Connector: strings.ToLower(strings.TrimSpace(collectSourceConnector)), Kind: target.Kind,
				ExternalID: target.ExternalID, Name: target.Name,
				Project: collectSourceProject, ExcludePattern: collectSourceExclude,
				Instruction: collectSourceFocus, KnowledgeCollection: collectSourceKnowledge,
				Strategy: collectSourceStrategy, DecisionUnit: collectSourceUnit,
				IntervalMinutes: collectSourceInterval, Priority: collectSourcePriority,
				Enabled: !collectSourceDisabled,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(source)
			} else {
				fmt.Printf("Saved %s: %s\n", source.ID, emptyAs(source.Name, source.ExternalID))
			}
			return nil
		})
	},
}

// collectionSourceTarget validates the exact connector identity to persist.
// Discovery is deliberately separate (`source search`), so ATM never guesses
// what connector-specific names or identifiers mean.
func collectionSourceTarget(ctx context.Context) (collector.Candidate, error) {
	connectorID := strings.ToLower(strings.TrimSpace(collectSourceConnector))
	if connectorID == "" {
		return collector.Candidate{}, fmt.Errorf("pass --connector <id>")
	}
	if _, err := collectionConnector(connectorID); err != nil {
		return collector.Candidate{}, err
	}
	kind, externalID := strings.TrimSpace(collectSourceKind), strings.TrimSpace(collectSourceExternalID)
	if kind == "" || externalID == "" {
		return collector.Candidate{}, fmt.Errorf("pass both --kind and --id")
	}
	return collector.Candidate{Kind: kind, ExternalID: externalID, Name: collectSourceName}, nil
}

func resolveCollectionCandidateFor(ctx context.Context, connectorID, searchKind, value string) (collector.Candidate, error) {
	if strings.TrimSpace(connectorID) == "" {
		return collector.Candidate{}, fmt.Errorf("pass --connector <id>")
	}
	searcher, err := collectionSearchConnectorFor(connectorID)
	if err != nil {
		return collector.Candidate{}, err
	}
	candidates, err := searcher.Search(ctx, searchKind, value, 10)
	if err != nil {
		return collector.Candidate{}, err
	}
	if len(candidates) == 0 {
		return collector.Candidate{}, fmt.Errorf("没有找到匹配「%s」的%s。%s",
			value, collectionKindLabel(searchKind), collectionSearchHint)
	}
	// Connectors can match metadata beyond the returned display name. Narrowing
	// to results that carry the keyword in their own name is exact, not a guess —
	// two of those are still genuinely ambiguous.
	named := []collector.Candidate{}
	for _, candidate := range candidates {
		if collector.MatchesName(candidate, value) {
			named = append(named, candidate)
		}
	}
	if len(named) == 1 {
		return named[0], nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(named) > 1 {
		candidates = named
	}
	return collector.Candidate{}, fmt.Errorf("「%s」匹配到 %d 个结果，请用更精确的名字，或直接用 ID：\n%s",
		value, len(candidates), collectionCandidateList(candidates))
}

var collectSourceEnableCmd = collectionSourceToggleCommand("enable", true)
var collectSourceDisableCmd = collectionSourceToggleCommand("disable", false)

func collectionSourceToggleCommand(name string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <source-id>",
		Short: strings.Title(name) + " a collection source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(false, func(db *sql.DB) error {
				if err := store.SetCollectionSourceEnabled(db, args[0], enabled); err != nil {
					return err
				}
				if jsonOutput {
					output.JSON(map[string]any{"id": args[0], "enabled": enabled})
				}
				return nil
			})
		},
	}
}

var collectSourceDeleteCmd = &cobra.Command{
	Use:   "delete <source-id>",
	Short: "Delete a collection source while retaining its audit history",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := confirmDestructive(cmd, collectYes, "Delete collection source "+args[0]+"?")
		if err != nil || !confirmed {
			return err
		}
		return withDB(false, func(db *sql.DB) error {
			return store.DeleteCollectionSource(db, args[0])
		})
	},
}

var collectHistoryCmd = &cobra.Command{
	Use:   "history <source-id | source name>",
	Short: "Read a connector conversation and sync it locally",
	Long: "Print a connector conversation oldest first and store it locally, so the " +
		"same messages can be re-read offline and found by `atm collect search`. No " +
		"Todo is created. Accepts a collection source id or the name of a source already " +
		"added. Search-capable connectors may also resolve a name. Pass --local to read only " +
		"what is already stored.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source, err := collectionHistorySource(context.Background(), args[0])
		if err != nil {
			return err
		}
		options := collector.HistoryOptions{Limit: collectHistoryLimit}
		if since := strings.TrimSpace(collectHistorySince); since != "" {
			options.Since, err = parseSessionSince(since)
			if err != nil {
				return err
			}
		}
		read := collectionHistoryRead{Source: collectionHistorySourceOf(source)}
		if collectHistoryLocal {
			read.Messages, err = collectionHistoryFromStore(source, options)
			if err != nil {
				return err
			}
		} else if read, err = collectionHistorySync(source, options); err != nil {
			return err
		}
		// Any read of a conversation is a moment to enforce retention, --local
		// included: it asks not to touch the network, not to skip housekeeping.
		if err := collectionPruneMessages(); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(read)
			return nil
		}
		if read.Error != "" {
			// The messages below are real but possibly behind, and saying so is the
			// difference between "nothing new" and "could not check".
			fmt.Fprintf(cmd.ErrOrStderr(), "%s 读取失败（%s），以下是本地已同步的记录。\n",
				source.Connector, read.Error)
		}
		fmt.Print(collectionHistoryText(source, read.Messages))
		return nil
	},
}

// collectionHistoryRead is what one history read produced. Stale marks messages
// served from the local archive because the connector could not be reached, so a caller
// can tell "up to date" from "the best we still have".
type collectionHistoryRead struct {
	Source   collectionHistorySourceJSON `json:"source"`
	Messages []collector.Message         `json:"messages"`
	// Always reported, including zero: "nothing new was said" is an answer, and
	// omitting it would leave a caller unable to tell it from "did not sync".
	Synced int    `json:"synced"`
	Stale  bool   `json:"stale,omitempty"`
	Error  string `json:"error,omitempty"`
}

// collectionHistorySync reads from the source connector and stores what came
// back. When it fails but the conversation was synced
// before, the local copy is served instead of an error: an expired login should
// not take away history already on disk.
func collectionHistorySync(source store.CollectionSource,
	options collector.HistoryOptions) (collectionHistoryRead, error) {
	read := collectionHistoryRead{Source: collectionHistorySourceOf(source)}
	connector, err := collectionConnector(source.Connector)
	if err != nil {
		return read, err
	}
	historian, ok := connector.(collector.HistoryConnector)
	if !ok {
		return read, fmt.Errorf("collection connector %s does not support history", source.Connector)
	}
	messages, err := historian.History(context.Background(), source, options)
	if err != nil {
		local, localErr := collectionHistoryFromStore(source, options)
		if localErr != nil || len(local) == 0 {
			return read, err
		}
		read.Messages, read.Stale, read.Error = local, true, compactCollectionError(err)
		return read, nil
	}
	read.Messages = messages
	writeErr := withDB(false, func(db *sql.DB) error {
		synced, err := store.PutCollectionMessages(db, collector.CollectionMessagesFor(source, messages))
		read.Synced = synced
		return err
	})
	return read, writeErr
}

// collectionPruneMessages drops synced chat past its retention window. A zero
// window keeps everything, and then this does nothing at all.
func collectionPruneMessages() error {
	cutoff := store.RetentionCutoff(config.CollectionMessageRetentionDays, time.Now())
	if cutoff <= 0 {
		return nil
	}
	return withDB(false, func(db *sql.DB) error {
		_, err := store.PruneCollectionMessages(db, cutoff)
		return err
	})
}

func collectionHistoryFromStore(source store.CollectionSource,
	options collector.HistoryOptions) ([]collector.Message, error) {
	query := store.CollectionMessageQuery{Connector: source.Connector,
		ConversationID: source.ExternalID, Limit: options.Limit}
	if !options.Since.IsZero() {
		query.SinceTS = options.Since.Unix()
	}
	var messages []collector.Message
	err := withDB(true, func(db *sql.DB) error {
		stored, err := store.ListCollectionMessages(db, query)
		if err != nil {
			return err
		}
		messages = make([]collector.Message, 0, len(stored))
		for _, message := range stored {
			messages = append(messages, collector.Message{ID: message.MessageID,
				ConversationID: message.ConversationID, Sender: message.Sender,
				CreatedAt: message.CreatedAt, Content: message.Content})
		}
		return nil
	})
	return messages, err
}

func collectionHistorySourceOf(source store.CollectionSource) collectionHistorySourceJSON {
	return collectionHistorySourceJSON{ID: source.ID, Connector: source.Connector,
		Kind: source.Kind, ExternalID: source.ExternalID, Name: source.Name}
}

// compactCollectionError keeps a connector failure to one line so it reads as a note
// above the messages rather than a wall of JSON.
func compactCollectionError(err error) string {
	message := strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
	if len(message) > 160 {
		return message[:160] + "…"
	}
	return message
}

var collectSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search synced connector messages stored locally",
	Long: "Search messages `atm collect history` and `atm collect run` have synced, " +
		"newest first. This never calls a connector: it reads what is already on disk.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := store.CollectionMessageQuery{
			Sender: strings.TrimSpace(collectSearchSender), Limit: collectSearchMessages,
		}
		if since := strings.TrimSpace(collectSearchSince); since != "" {
			parsed, err := parseSessionSince(since)
			if err != nil {
				return err
			}
			query.SinceTS = parsed.Unix()
		}
		return withDB(true, func(db *sql.DB) error {
			if source := strings.TrimSpace(collectSearchSource); source != "" {
				resolved, err := collectionStoredSource(db, source)
				if err != nil {
					return err
				}
				query.Connector = resolved.Connector
				query.ConversationID = resolved.ExternalID
			}
			matches, err := store.SearchCollectionMessages(db, args[0], query)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(map[string]any{"keyword": args[0], "returned": len(matches), "matches": matches})
				return nil
			}
			if len(matches) == 0 {
				fmt.Printf("本地已同步的聊天里没有「%s」。%s\n", args[0], collectionSearchScopeHint)
				return nil
			}
			fmt.Printf("%d 条命中「%s」\n", len(matches), args[0])
			for _, message := range matches {
				at := time.Unix(message.CreatedAt, 0).In(config.Loc).Format("2006-01-02 15:04:05")
				fmt.Printf("%s [%s] [%s] %s\n", at,
					emptyAs(message.ConversationName, message.ConversationID),
					emptyAs(message.Sender, "?"), truncLine(cleanMsg(message.Content), 200))
			}
			return nil
		})
	},
}

var collectAnalyzeCmd = &cobra.Command{
	Use:   "analyze <source-id | 来源名>",
	Short: "Classify a window of one source's chat into task decisions",
	Long: "Analyse chat that automatic collection will never look at — it only reads " +
		"forward from its checkpoint. Decisions are held for confirmation by default: " +
		"nothing reaches the Todo list until `atm collect item promote <item-id>` or " +
		"--apply. Each conversation batch costs one model call, so --max-batches is the " +
		"spend limit. The source must already be added (`atm collect source add`).",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		options := collector.AnalyzeOptions{Limit: collectAnalyzeLimit,
			MaxBatches: collectAnalyzeBatches, Local: collectAnalyzeLocal, Apply: collectAnalyzeApply}
		if since := strings.TrimSpace(collectAnalyzeSince); since != "" {
			parsed, err := parseSessionSince(since)
			if err != nil {
				return err
			}
			options.Since = parsed
		}
		var source store.CollectionSource
		if err := withDB(false, func(db *sql.DB) error {
			resolved, err := collectionStoredSource(db, args[0])
			if err != nil {
				// Analysis and everything after it (promote, revert, the app's
				// lists) reads the item's source row, so an unadded group has
				// nowhere to hang its decisions.
				return fmt.Errorf("%w\n先添加成来源再分析：atm collect source add --group %q --disabled"+
					"（--disabled 只用于分析，不会开启后台收集）", err, args[0])
			}
			source = resolved
			return nil
		}); err != nil {
			return err
		}
		report, err := collector.DefaultService().Analyze(context.Background(), source.ID, options)
		if jsonOutput {
			output.JSON(report)
			if err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Print(collectionAnalyzeText(report, options.Apply))
		return nil
	},
}

func collectionAnalyzeText(report collector.AnalyzeReport, applied bool) string {
	var builder strings.Builder
	for _, item := range report.Items {
		label := "已忽略"
		switch {
		case item.ProposedAction == "create":
			label = "建议新建"
		case item.ProposedAction == "append":
			label = "建议补充"
		case item.Action == "create":
			label = "已新建"
		case item.Action == "append":
			label = "已补充"
		case item.Action == "insight":
			label = "已沉淀"
		}
		parts := []string{emptyAs(item.Title, collectionItemFallbackTitle(item))}
		if item.Priority != "" {
			parts = append(parts, item.Priority)
		}
		if item.Project != "" {
			parts = append(parts, item.Project)
		}
		if item.Confidence > 0 {
			parts = append(parts, fmt.Sprintf("置信 %.2f", item.Confidence))
		}
		if item.TodoID != "" {
			parts = append(parts, item.TodoID)
		}
		parts = append(parts, item.ID)
		fmt.Fprintf(&builder, "[%s] %s\n", label, strings.Join(parts, " · "))
	}
	fmt.Fprintf(&builder, "分析 %d 段 · 跳过 %d 段", report.Analyzed, report.Skipped)
	if report.Proposed > 0 {
		fmt.Fprintf(&builder, " · 待确认 %d", report.Proposed)
	}
	if report.Applied > 0 {
		fmt.Fprintf(&builder, " · 已落地 %d", report.Applied)
	}
	if report.Insights > 0 {
		fmt.Fprintf(&builder, " · 沉淀 %d", report.Insights)
	}
	fmt.Fprintf(&builder, " · 忽略 %d", report.Ignored)
	if report.Failed > 0 {
		fmt.Fprintf(&builder, " · 失败 %d", report.Failed)
	}
	if report.Remaining > 0 {
		fmt.Fprintf(&builder, " · 还剩 %d 段未分析（--max-batches 提高上限）", report.Remaining)
	}
	builder.WriteString("\n")
	if !applied && report.Proposed > 0 {
		builder.WriteString("确认后落地：atm collect item promote <item-id>\n")
	}
	if report.Insights > 0 {
		builder.WriteString("沉淀内容写入知识库：atm collect digest --source " + report.SourceID + "\n")
	}
	return builder.String()
}

func collectionItemFallbackTitle(item store.CollectionItem) string {
	if line := strings.SplitN(strings.TrimSpace(item.RawContext), "\n", 2)[0]; line != "" {
		return truncLine(cleanMsg(line), 60)
	}
	return "（无标题）"
}

// collectionSearchScopeHint keeps the difference between "not said" and "not
// synced" in front of whoever is searching.
const collectionSearchScopeHint = "只搜本地已同步的部分——先用 collect history 拉一次要找的群或时间段。"

// collectionStoredSource resolves a source id or the name of an added source
// without touching the network: searching the local archive is a local
// operation, and a name that needs a connector to resolve has nothing stored yet.
func collectionStoredSource(db *sql.DB, value string) (store.CollectionSource, error) {
	if source, err := store.GetCollectionSource(db, value); err == nil {
		return source, nil
	}
	sources, err := store.ListCollectionSources(db, "", false)
	if err != nil {
		return store.CollectionSource{}, err
	}
	for _, source := range sources {
		if strings.EqualFold(strings.TrimSpace(source.Name), value) || source.ExternalID == value {
			return source, nil
		}
	}
	return store.CollectionSource{}, fmt.Errorf(
		"没有这个来源：%s。用 atm collect source list 看已添加的来源，或直接传 openConversationId", value)
}

// collectionHistorySourceJSON identifies the conversation that was read.
// Reading a chat does not touch source configuration, so it reports identity
// only: a group resolved by name but never added has no id, priority, or
// enabled state, and emitting those zero values would read as a disabled source.
type collectionHistorySourceJSON struct {
	ID         string `json:"id,omitempty"`
	Connector  string `json:"connector"`
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name,omitempty"`
}

// collectionHistorySource accepts whatever identifies a conversation, most
// precise first: a source id, the name of an already added source, a raw
// identifier, and finally a name the selected connector can look up. The
// database lookup is best effort — reading a chat must work before any source
// has been added, so a missing database just falls through to the search.
func collectionHistorySource(ctx context.Context, value string) (store.CollectionSource, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return store.CollectionSource{}, fmt.Errorf("pass a source id, source name, or connector identifier")
	}
	var stored store.CollectionSource
	found := false
	_ = withDB(true, func(db *sql.DB) error {
		if source, err := store.GetCollectionSource(db, value); err == nil {
			stored, found = source, true
			return nil
		}
		sources, err := store.ListCollectionSources(db, "", false)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if strings.EqualFold(strings.TrimSpace(source.Name), value) {
				stored, found = source, true
				return nil
			}
		}
		return nil
	})
	if found {
		return stored, nil
	}
	connectorID := strings.ToLower(strings.TrimSpace(collectHistoryConnector))
	if connectorID == "" {
		return store.CollectionSource{}, fmt.Errorf("source is not configured; pass --connector <id> to search externally")
	}
	candidate, err := resolveCollectionCandidateFor(ctx, connectorID, collectHistoryKind, value)
	if err != nil {
		return store.CollectionSource{}, err
	}
	return store.CollectionSource{Connector: connectorID,
		Kind: candidate.Kind, ExternalID: candidate.ExternalID, Name: candidate.Name}, nil
}

func collectionConnector(id string) (collector.Connector, error) {
	registry, err := collector.DefaultRegistry()
	if err != nil {
		return nil, err
	}
	return registry.Resolve(id)
}

func collectionSearchConnectorFor(id string) (collector.SearchConnector, error) {
	connector, err := collectionConnector(id)
	if err != nil {
		return nil, err
	}
	searcher, ok := connector.(collector.SearchConnector)
	if !ok {
		return nil, fmt.Errorf("collection connector %s does not support search; pass --kind and --id", id)
	}
	return searcher, nil
}

func collectionHistoryText(source store.CollectionSource, messages []collector.Message) string {
	var builder strings.Builder
	title := emptyAs(source.Name, source.ExternalID)
	if len(messages) == 0 {
		fmt.Fprintf(&builder, "%s：这段时间没有消息。\n", title)
		return builder.String()
	}
	first := time.Unix(messages[0].CreatedAt, 0).In(config.Loc)
	last := time.Unix(messages[len(messages)-1].CreatedAt, 0).In(config.Loc)
	fmt.Fprintf(&builder, "%s · %d 条 · %s ~ %s\n", title, len(messages),
		first.Format("2006-01-02 15:04"), last.Format("2006-01-02 15:04"))
	for _, message := range messages {
		at := time.Unix(message.CreatedAt, 0).In(config.Loc).Format("2006-01-02 15:04:05")
		// Continuation lines are indented so one message stays one block.
		content := strings.ReplaceAll(message.Content, "\n", "\n  ")
		fmt.Fprintf(&builder, "%s [%s] %s\n", at, emptyAs(message.Sender, "?"), content)
	}
	return builder.String()
}

var collectItemCmd = &cobra.Command{
	Use:   "item",
	Short: "Correct or retry collection decisions",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var collectItemReprocessCmd = &cobra.Command{
	Use:   "reprocess <item-id>",
	Short: "Run extraction again for a failed, ignored or insight item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := collector.DefaultService().Reprocess(context.Background(), args[0])
		if err != nil {
			return err
		}
		return printCollectionItem(item)
	},
}

var collectItemPromoteCmd = &cobra.Command{
	Use:   "promote <item-id>",
	Short: "Turn an ignored, insight or failed item into a Todo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := collector.DefaultService().Promote(args[0], collectionItemCorrection(cmd))
		if err != nil {
			return err
		}
		return printCollectionItem(item)
	},
}

var collectItemCorrectCmd = &cobra.Command{
	Use:   "correct <item-id>",
	Short: "Correct the title, project, or priority of an item and its Todo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := collector.DefaultService().Correct(args[0], collectionItemCorrection(cmd))
		if err != nil {
			return err
		}
		return printCollectionItem(item)
	},
}

var collectItemRevertCmd = &cobra.Command{
	Use:   "revert <item-id>",
	Short: "Revert a mistaken Todo write while preserving its audit trail",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := confirmDestructive(cmd, collectYes, "Revert collection item "+args[0]+"?")
		if err != nil || !confirmed {
			return err
		}
		item, err := collector.DefaultService().Revert(args[0])
		if err != nil {
			return err
		}
		return printCollectionItem(item)
	},
}

var collectItemDeleteCmd = &cobra.Command{
	Use:   "delete <item-id>",
	Short: "Delete a processing record while keeping the Todo it wrote",
	Long: "Delete one collection processing record. The Todo it created or appended to " +
		"is kept: the record is collection's own note about a decision, not the work " +
		"itself. Use `atm collect item revert` when the Todo write is what was wrong.\n\n" +
		"A record whose messages still fall inside the next run's re-read window can be " +
		"rebuilt by that run; older records are gone for good.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := confirmDestructive(cmd, collectYes, "Delete collection item "+args[0]+"?")
		if err != nil || !confirmed {
			return err
		}
		return withDB(false, func(db *sql.DB) error {
			item, err := store.DeleteCollectionItem(db, args[0])
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(map[string]any{"id": item.ID, "deleted": true, "todo_id": item.TodoID})
				return nil
			}
			fmt.Printf("Deleted collection item %s\n", item.ID)
			// The Todo outliving its record is the one surprise here, so it is
			// said out loud rather than left to be discovered.
			if item.TodoID != "" {
				fmt.Printf("  todo %s kept\n", item.TodoID)
			}
			return nil
		})
	},
}

func collectionItemCorrection(cmd *cobra.Command) collector.ItemCorrection {
	correction := collector.ItemCorrection{}
	if cmd.Flags().Changed("title") {
		correction.Title = &collectItemTitle
	}
	if cmd.Flags().Changed("project") {
		correction.Project = &collectItemProject
	}
	if cmd.Flags().Changed("priority") {
		correction.Priority = &collectItemPriority
	}
	return correction
}

func printCollectionItem(item store.CollectionItem) error {
	if jsonOutput {
		output.JSON(item)
	} else {
		fmt.Printf("%s: %s todo=%s status=%s\n", item.ID, item.Action, emptyAs(item.TodoID, "-"), item.Status)
		// Whether the next run will pick this up again is the one thing a person
		// decides from here, and it is not readable from the status alone.
		if item.Status == "failed" {
			if store.CollectionRetriesExhausted(item) {
				fmt.Printf("  retries: %d/%d spent; automatic retry stopped, run `atm collect item reprocess %s` after fixing the cause\n",
					item.Attempts, store.MaxCollectionAttempts, item.ID)
			} else {
				fmt.Printf("  retries: %d/%d spent; the next run retries this automatically\n",
					item.Attempts, store.MaxCollectionAttempts)
			}
		}
	}
	return nil
}
