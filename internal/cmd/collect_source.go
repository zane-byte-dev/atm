package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
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
	collectSearchKind       string
	collectSearchConnector  string
	collectSearchLimit      int
)

func init() {
	collectSourceAddCmd.Flags().StringVar(&collectSourceConnector, "connector", "",
		"registered connector id")
	collectSourceSearchCmd.Flags().StringVar(&collectSearchConnector, "connector", "",
		"registered connector id")
	collectSourceAddCmd.Flags().StringVar(&collectSourceKind, "kind", "", "connector-defined source kind")
	collectSourceAddCmd.Flags().StringVar(&collectSourceExternalID, "id", "", "connector source identifier")
	collectSourceAddCmd.Flags().StringVar(&collectSourceName, "name", "", "human-readable source name")
	collectSourceSearchCmd.Flags().StringVar(&collectSearchKind, "kind", collector.DirectoryKindAll,
		"search kind: group, user, or all")
	collectSourceSearchCmd.Flags().IntVar(&collectSearchLimit, "limit", 10, "maximum candidates to return")
	collectSourceAddCmd.Flags().StringVar(&collectSourceProject, "project", "", "default ATM project for extracted work")
	collectSourceAddCmd.Flags().StringVar(&collectSourceExclude, "exclude", "", "comma-separated message keywords to ignore")
	collectSourceAddCmd.Flags().StringVar(&collectSourceFocus, "instruction", "",
		"what to watch this source for, in your own words")
	collectSourceAddCmd.Flags().StringVar(&collectSourceKnowledge, "knowledge-collection", "",
		"default knowledge collection for saved conclusions and digests")
	collectSourceAddCmd.Flags().StringVar(&collectSourceStrategy, "strategy", collector.SourceStrategyTasks,
		"collection strategy: tasks or observe")
	collectSourceAddCmd.Flags().StringVar(&collectSourceUnit, "decision-unit", collector.SourceDecisionUnitWindow,
		"decision unit: window (chat topic) or message (notification feed)")
	collectSourceAddCmd.Flags().IntVar(&collectSourceInterval, "interval", 0,
		"collection interval in minutes (default 5 for tasks, 60 for observe)")
	collectSourceAddCmd.Flags().StringVar(&collectSourcePriority, "priority", "P2", "default priority: P0, P1, P2, P3")
	collectSourceAddCmd.Flags().BoolVar(&collectSourceDisabled, "disabled", false, "add the source disabled")
	collectSourceDeleteCmd.Flags().BoolVarP(&collectYes, "yes", "y", false, "skip confirmation")
}

var collectSourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage connector sources",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var collectSourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List collection sources",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().ListSources(
			cmd.Context(),
			collectionCLICall("source-list"),
			collector.ListSourcesInput{Sync: syncBeforeRead},
		)
		if err != nil {
			return err
		}
		if result.SyncedFiles > 0 && !jsonOutput {
			output.Progress("Synced %d files.", result.SyncedFiles)
		}
		if jsonOutput {
			output.JSON(result.Sources)
			return nil
		}
		for _, source := range result.Sources {
			fmt.Printf("%-20s %-8s %-5s %-24s strategy=%-7s every=%4dm project=%-12s knowledge=%-12s enabled=%v %s\n",
				source.ID, source.Connector, source.Kind, source.ExternalID,
				source.Strategy, source.IntervalMinutes, emptyAs(source.Project, "-"),
				emptyAs(source.KnowledgeCollection, config.CollectionDigestCollection),
				source.Enabled, source.Name)
			if source.Muted {
				fmt.Printf("%-20s 桌面通知已静默，仍照常收集并计入未读\n", "")
			}
			if source.DecisionUnit == collector.SourceDecisionUnitMessage {
				fmt.Printf("%-20s 每条消息单独判定，同一时段的其他消息只作上下文\n", "")
			}
			if source.Instruction != "" {
				fmt.Printf("%-20s 关注：%s\n", "", source.Instruction)
			}
		}
		return nil
	},
}

var collectSourceSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Find connector sources by name before adding them",
	Long:  "Ask a registered connector to find collection source candidates by name.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := defaultCollectorService().SearchSources(
			cmd.Context(),
			collectionCLICall("source-search"),
			collector.SearchSourcesInput{
				Connector: collectSearchConnector,
				Kind:      collectSearchKind,
				Keyword:   args[0],
				Limit:     collectSearchLimit,
			},
		)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(result)
			return nil
		}
		if len(result.Candidates) == 0 {
			fmt.Printf("没有找到匹配「%s」的来源。%s\n", args[0], collectionSearchHint)
			return nil
		}
		fmt.Print(collectionCandidateList(result.Candidates))
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
	case collector.DirectoryKindGroup:
		return "群聊"
	case collector.DirectoryKindUser, "contact":
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
		result, err := defaultCollectorService().SaveSource(
			cmd.Context(),
			collectionCLICall("source-save"),
			collector.SaveSourceInput{
				Connector:           collectSourceConnector,
				Kind:                collectSourceKind,
				ExternalID:          collectSourceExternalID,
				Name:                collectSourceName,
				Project:             collectSourceProject,
				ExcludePattern:      collectSourceExclude,
				Instruction:         collectSourceFocus,
				KnowledgeCollection: collectSourceKnowledge,
				Strategy:            collectSourceStrategy,
				DecisionUnit:        collectSourceUnit,
				IntervalMinutes:     collectSourceInterval,
				Priority:            collectSourcePriority,
				Enabled:             !collectSourceDisabled,
			},
		)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(result.Source)
		} else {
			fmt.Printf("Saved %s: %s\n", result.Source.ID, emptyAs(result.Source.Name, result.Source.ExternalID))
		}
		return nil
	},
}

var collectSourceEnableCmd = collectionSourceToggleCommand("enable", true)
var collectSourceDisableCmd = collectionSourceToggleCommand("disable", false)

func collectionSourceToggleCommand(name string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <source-id>",
		Short: strings.Title(name) + " a collection source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := defaultCollectorService().SetSourceEnabled(
				cmd.Context(),
				collectionCLICall("source-"+name),
				collector.SetSourceEnabledInput{SourceID: args[0], Enabled: enabled},
			)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(result)
			}
			return nil
		},
	}
}

var collectSourceMuteCmd = collectionSourceMuteCommand("mute", true)
var collectSourceUnmuteCmd = collectionSourceMuteCommand("unmute", false)

func collectionSourceMuteCommand(name string, muted bool) *cobra.Command {
	short := "Stop desktop notifications for one collection source"
	if !muted {
		short = "Resume desktop notifications for one collection source"
	}
	return &cobra.Command{
		Use:   name + " <source-id>",
		Short: short,
		Long: short + ". Collection itself is unaffected: a muted source keeps " +
			"collecting, its results keep counting as unread and the sidebar and " +
			"menubar badges still rise. Use `collect source disable` to stop collecting.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := defaultCollectorService().SetSourceMuted(
				cmd.Context(),
				collectionCLICall("source-"+name),
				collector.SetSourceMutedInput{SourceID: args[0], Muted: muted},
			)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(result)
			}
			return nil
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
		_, err = defaultCollectorService().DeleteSource(
			cmd.Context(),
			collectionCLICall("source-delete"),
			collector.DeleteSourceInput{SourceID: args[0], Confirmed: true},
		)
		return err
	},
}
