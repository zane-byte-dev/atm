package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

// collectionCommandTodoDispatcher is the temporary CLI process adapter for
// Collector's task-dispatch port. Process construction and project-directory
// discovery stay here at the delivery edge; the Collector service has no
// dependency on ATM's command namespace.
type collectionCommandTodoDispatcher struct {
	executable string
}

func (dispatcher collectionCommandTodoDispatcher) Dispatch(ctx context.Context, todoID, project string) error {
	if strings.TrimSpace(dispatcher.executable) == "" {
		return fmt.Errorf("ATM executable is unavailable for automatic dispatch")
	}
	workDir, err := collectionProjectWorkDir(project)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, dispatcher.executable,
		"todo", "run", todoID, "--cwd", workDir, "--json")
	command.Env = append(os.Environ(), "ATM_SKIP_LOCAL_NOTIFICATION=1")
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("dispatch todo %s: %w", todoID, err)
		}
		return fmt.Errorf("dispatch todo %s: %w: %s", todoID, err, detail)
	}
	return nil
}

func defaultCollectorService() collector.Service {
	service := collector.DefaultService()
	executable, _ := os.Executable()
	service.Dispatcher = collectionCommandTodoDispatcher{executable: executable}
	return service
}

func collectionProjectWorkDir(project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", fmt.Errorf("automatic dispatch requires a Todo project")
	}
	candidates := []string{}
	if filepath.IsAbs(project) {
		candidates = append(candidates, project)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "mox", project),
			filepath.Join(home, "work", project),
			filepath.Join(home, project),
		)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			return absolute, nil
		}
	}
	return "", fmt.Errorf("cannot resolve project directory for %q", project)
}

var (
	collectSourceID         string
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
	collectItemCollection   string
	collectItemReadAll      bool
)

func init() {
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
	collectRunCmd.Flags().StringVar(&collectSourceID, "source", "", "run one source id instead of every enabled source")
	collectRunCmd.Flags().BoolVar(&collectRunDue, "due", false, "run only sources whose own interval is due")
	collectDigestCmd.Flags().StringVar(&collectSourceID, "source", "", "digest one source id instead of every enabled source")
	collectDigestCmd.Flags().StringVar(&collectDigestDate, "date", "", "local day to digest as YYYY-MM-DD (default: today)")
	collectDigestCmd.Flags().BoolVar(&collectDigestDue, "due", false,
		"skip sources digested within the last "+fmt.Sprint(config.CollectionDigestIntervalMinutes)+" minutes")
	collectDigestCmd.Flags().BoolVar(&collectDigestDryRun, "dry-run", false,
		"print the digest without writing it to the knowledge base")
	collectStatusCmd.Flags().IntVar(&collectLimit, "limit", 100, "maximum recent collection items")
	for _, command := range []*cobra.Command{collectItemPromoteCmd, collectItemCorrectCmd} {
		command.Flags().StringVar(&collectItemTitle, "title", "", "corrected Todo title")
		command.Flags().StringVar(&collectItemProject, "project", "", "corrected ATM project (empty clears it)")
		command.Flags().StringVar(&collectItemPriority, "priority", "", "corrected priority: P0, P1, P2, P3")
	}
	collectItemRevertCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")
	collectItemDeleteCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")
	collectItemSaveCmd.Flags().StringVar(&collectItemCollection, "collection", "",
		"knowledge collection to save into (default: source setting or "+config.CollectionDigestCollection+")")
	collectItemReadCmd.Flags().BoolVar(&collectItemReadAll, "all", false,
		"mark every unread collection result as read")

	collectSourceCmd.AddCommand(collectSourceListCmd, collectSourceSearchCmd, collectSourceAddCmd,
		collectSourceEnableCmd, collectSourceDisableCmd,
		collectSourceMuteCmd, collectSourceUnmuteCmd, collectSourceDeleteCmd)
	collectItemCmd.AddCommand(collectItemReprocessCmd, collectItemPromoteCmd, collectItemCorrectCmd,
		collectItemSaveCmd, collectItemReadCmd, collectItemUnreadCmd,
		collectItemArchiveCmd, collectItemUnarchiveCmd,
		collectItemRevertCmd, collectItemDeleteCmd)
	collectCmd.AddCommand(collectStatusCmd, collectRunCmd, collectDigestCmd, collectEnableCmd,
		collectDisableCmd, collectHistoryCmd, collectSearchCmd, collectAnalyzeCmd,
		collectSourceCmd, collectItemCmd)
	rootCmd.AddCommand(collectCmd)
}

var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Automatically collect work from external connectors",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var collectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show connector health, sources, runs, and recent decisions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		value, err := defaultCollectorService().Snapshot(
			cmd.Context(), collectionCLICall("snapshot"), collector.SnapshotInput{ItemLimit: collectLimit},
		)
		if err != nil {
			return err
		}
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
			fmt.Printf("Filed Todos: %d · %d still open\n", value.Summary.Followups,
				value.Summary.Followups-value.Summary.FollowupsClosed)
		}
		if pending := collectionPendingProposals(value.Items); pending > 0 {
			fmt.Printf("Awaiting confirmation: %d · atm collect item promote <item-id>\n", pending)
		}
		if value.Summary.Unread > 0 {
			fmt.Printf("Unread collection results: %d · atm collect item read --all\n", value.Summary.Unread)
		}
		if muted := collectionMutedSources(value.Sources); muted > 0 {
			fmt.Printf("Muted sources: %d · still collected and counted as unread · atm collect source unmute <source-id>\n", muted)
		}
		for _, health := range value.ConnectorHealth {
			fmt.Printf("%s: %s\n", health.Connector, collectionHealthLine(health))
		}
		return nil
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

// collectionMutedSources counts the sources whose results never raise a banner.
func collectionMutedSources(sources []store.CollectionSource) int {
	muted := 0
	for _, source := range sources {
		if source.Muted {
			muted++
		}
	}
	return muted
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

type collectionConnectorHealth = collector.ConnectorHealth

func collectionHealth(overview store.CollectionOverview) []collectionConnectorHealth {
	return defaultCollectorService().ConnectorHealthFor(overview)
}

// collectionHealthLine is the human line for one connector. A flaky connector
// gets its rate rather than its latest error, because the rate is the thing that
// tells you whether to care.
func collectionHealthLine(health collectionConnectorHealth) string {
	switch health.Status {
	case "flaky":
		return fmt.Sprintf("偶发失败 · 最近 %d 次里失败 %d 次，最近一次 %s，之后会自动重试",
			health.RecentRuns, health.RecentFailures, collectionHealthWhen(health.CheckedAt))
	case "ready":
		if health.RecentFailures > 0 {
			return fmt.Sprintf("ready · 最近 %d 次里失败过 %d 次，已恢复",
				health.RecentRuns, health.RecentFailures)
		}
		return "ready"
	case "not_checked":
		return "not_checked"
	default:
		line := health.Status
		if health.ConsecutiveFailures > 1 {
			line += fmt.Sprintf(" · 连续失败 %d 次", health.ConsecutiveFailures)
		}
		if health.Error != "" {
			line += " · " + health.Error
		}
		return line
	}
}

func collectionHealthWhen(ts int64) string {
	if ts <= 0 {
		return "时间未知"
	}
	elapsed := time.Since(time.Unix(ts, 0))
	if elapsed < time.Minute {
		return "刚刚"
	}
	return formatShortDuration(int64(elapsed.Seconds())) + "前"
}

// collectionResolveHealth turns the counts into the one word a human reads.
//
// A classified failure — login expired, permission missing — is reported the
// moment it happens: it will not fix itself, and waiting for a second sample only
// delays telling the user by one interval. An unclassified failure with a recent
// success behind it is reported as `flaky`, which says "nothing to do" without
// pretending nothing happened.
func collectionResolveHealth(health collectionConnectorHealth) collectionConnectorHealth {
	return collector.ResolveConnectorHealth(health)
}

func collectionFailureStatus(message string) string {
	return collector.CollectionFailureStatus(message)
}

var collectRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run enabled collectors once",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := defaultCollectorService().RunCollection(
			cmd.Context(), collectionCLICall("run"),
			collector.RunInput{SourceID: collectSourceID, DueOnly: collectRunDue},
		)
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
		report, err := defaultCollectorService().Digest(context.Background(), collectSourceID,
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
	settings, err := config.Default.Apply(config.SettingsPatch{CollectionEnabled: &enabled})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"enabled": settings.CollectionEnabled})
	} else {
		fmt.Printf("Automatic collection enabled = %v\n", settings.CollectionEnabled)
	}
	return nil
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
		var sinceUnix int64
		if since := strings.TrimSpace(collectHistorySince); since != "" {
			parsed, err := parseSessionSince(since)
			if err != nil {
				return err
			}
			sinceUnix = parsed.Unix()
		}
		read, err := defaultCollectorService().History(
			cmd.Context(), collectionCLICall("history"), collector.HistoryInput{
				Reference: args[0], Connector: collectHistoryConnector, Kind: collectHistoryKind,
				SinceUnix: sinceUnix, Limit: collectHistoryLimit, Local: collectHistoryLocal,
				Sync: syncBeforeRead,
			},
		)
		if err != nil {
			return err
		}
		if read.SyncedFiles > 0 && !jsonOutput {
			output.Progress("Synced %d files.", read.SyncedFiles)
		}
		if jsonOutput {
			output.JSON(read)
			return nil
		}
		source := store.CollectionSource{
			ID: read.Source.ID, Connector: read.Source.Connector, Kind: read.Source.Kind,
			ExternalID: read.Source.ExternalID, Name: read.Source.Name,
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
		report, err := defaultCollectorService().Analyze(context.Background(), source.ID, options)
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
		builder.WriteString("结论确认后保存：atm collect item save <item-id>\n")
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
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var collectItemReprocessCmd = &cobra.Command{
	Use:   "reprocess <item-id>",
	Short: "Run extraction again for a failed, ignored or insight item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().Reprocess(
			cmd.Context(),
			collectionCLICall("reprocess"),
			collector.ReprocessInput{ItemID: args[0]},
		)
		if err != nil {
			return err
		}
		return printCollectionItem(result.Item)
	},
}

var collectItemPromoteCmd = &cobra.Command{
	Use:   "promote <item-id>",
	Short: "Turn an ignored, insight or failed item into a Todo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().Promote(
			cmd.Context(),
			collectionCLICall("promote"),
			collector.PromoteInput{ItemID: args[0], Correction: collectionItemCorrection(cmd)},
		)
		if err != nil {
			return err
		}
		return printCollectionItem(result.Item)
	},
}

var collectItemCorrectCmd = &cobra.Command{
	Use:   "correct <item-id>",
	Short: "Correct the title, project, or priority of an item and its Todo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().Correct(
			cmd.Context(),
			collectionCLICall("correct"),
			collector.CorrectInput{ItemID: args[0], Correction: collectionItemCorrection(cmd)},
		)
		if err != nil {
			return err
		}
		return printCollectionItem(result.Item)
	},
}

var collectItemSaveCmd = &cobra.Command{
	Use:   "save <item-id>",
	Short: "Save an insight conclusion to the knowledge base",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SaveConclusion(
			cmd.Context(),
			collectionCLICall("save"),
			collector.SaveConclusionInput{ItemID: args[0], Collection: collectItemCollection},
		)
		if err != nil {
			return err
		}
		return printCollectionItem(result.Item)
	},
}

var collectItemReadCmd = &cobra.Command{
	Use:   "read <item-id>...",
	Short: "Mark collection results as read",
	Args: func(cmd *cobra.Command, args []string) error {
		if collectItemReadAll {
			if len(args) > 0 {
				return fmt.Errorf("read accepts item ids or --all, not both")
			}
			return nil
		}
		if len(args) == 0 {
			return fmt.Errorf("read requires at least one item id or --all")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SetItemsRead(
			cmd.Context(), collectionCLICall("items-read"), collector.SetItemsReadInput{
				ItemIDs: uniqueStrings(args), All: collectItemReadAll, Read: true,
			},
		)
		if err != nil {
			return err
		}
		return printCollectionReadChange(result)
	},
}

var collectItemUnreadCmd = &cobra.Command{
	Use:   "unread <item-id>...",
	Short: "Mark collection results as unread",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SetItemsRead(
			cmd.Context(), collectionCLICall("items-unread"),
			collector.SetItemsReadInput{ItemIDs: uniqueStrings(args), Read: false},
		)
		if err != nil {
			return err
		}
		return printCollectionReadChange(result)
	},
}

var collectItemArchiveCmd = &cobra.Command{
	Use:   "archive <item-id>...",
	Short: "Settle collection results without deleting them",
	Long: "Archive collection results as settled. The records stay in the audit ledger, " +
		"their source messages stay handled, and linked Todos are unchanged. Archived " +
		"results can be restored with `atm collect item unarchive`.",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SetItemsArchived(
			cmd.Context(), collectionCLICall("items-archive"),
			collector.SetItemsArchivedInput{ItemIDs: uniqueStrings(args), Archived: true},
		)
		if err != nil {
			return err
		}
		return printCollectionArchiveChange(result)
	},
}

var collectItemUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <item-id>...",
	Short: "Reopen archived collection results",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SetItemsArchived(
			cmd.Context(), collectionCLICall("items-unarchive"),
			collector.SetItemsArchivedInput{ItemIDs: uniqueStrings(args), Archived: false},
		)
		if err != nil {
			return err
		}
		return printCollectionArchiveChange(result)
	},
}

func printCollectionArchiveChange(result collector.SetItemsArchivedResult) error {
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	verb := "Reopened"
	if result.Archived {
		verb = "Archived"
	}
	fmt.Printf("%s %d collection results\n", verb, result.Count)
	return nil
}

func printCollectionReadChange(result collector.SetItemsReadResult) error {
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	state := "unread"
	if result.Read {
		state = "read"
	}
	fmt.Printf("Marked %d collection results %s\n", result.Count, state)
	return nil
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
		result, err := defaultCollectorService().Revert(
			cmd.Context(),
			collectionCLICall("revert"),
			collector.RevertInput{ItemID: args[0], Confirmed: true},
		)
		if err != nil {
			return err
		}
		return printCollectionItem(result.Item)
	},
}

func collectionCLICall(operation string) application.Call {
	return cliApplicationCall("collect-"+operation, "")
}

func collectionAgentFromEnvironment() string {
	return cliAgentFromEnvironment()
}

func collectionSessionFromEnvironment() string {
	return cliSessionFromEnvironment()
}

var collectItemDeleteCmd = &cobra.Command{
	Use:   "delete <item-id>...",
	Short: "Delete processing records while keeping the Todos they wrote",
	Long: "Delete collection processing records. The Todos they created or appended to " +
		"are kept: a record is collection's own note about a decision, not the work " +
		"itself. Use `atm collect item revert` when the Todo write is what was wrong.\n\n" +
		"Several ids delete as one transaction, which is what clearing a whole group in " +
		"the App does: either every named record goes or none does, and an id that is " +
		"already gone stops the batch instead of half-clearing it.\n\n" +
		"A record whose messages still fall inside the next run's re-read window can be " +
		"rebuilt by that run; older records are gone for good.",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids := uniqueStrings(args)
		prompt := "Delete collection item " + ids[0] + "?"
		if len(ids) > 1 {
			prompt = fmt.Sprintf("Delete %d collection items?", len(ids))
		}
		confirmed, err := confirmDestructive(cmd, collectYes, prompt)
		if err != nil || !confirmed {
			return err
		}
		result, err := defaultCollectorService().DeleteItems(
			cmd.Context(), collectionCLICall("items-delete"),
			collector.DeleteItemsInput{ItemIDs: ids, Confirmed: true},
		)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(result)
			return nil
		}
		if result.Count == 1 {
			fmt.Printf("Deleted collection item %s\n", result.Deleted[0].ID)
		} else {
			fmt.Printf("Deleted %d collection items\n", result.Count)
		}
		kept := []string{}
		for _, item := range result.Deleted {
			if item.TodoID != "" {
				kept = append(kept, item.TodoID)
			}
		}
		if len(kept) > 0 {
			fmt.Printf("  todos kept: %s\n", strings.Join(kept, ", "))
		}
		return nil
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
