package cmd

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
)

var (
	dayHistoryDays, dayDashboardDays                        int
	dayFeedbackVerdict, dayFeedbackBadge, dayFeedbackLabels string
	daySourceSemantic                                       bool
	dayPrivacySemantic                                      string
	dayPrivacyRetention                                     int
	dayDeleteFrom, dayDeleteTo                              string
	dayDeleteAll, dayDeleteYes, daySourceDeleteYes          bool
	dayFeedbackClear                                        bool
	dayPrivacyRetentionApply                                bool
)

func init() {
	dayHistoryCmd.Flags().IntVar(&dayHistoryDays, "days", 90, "number of calendar days to include")
	dayDashboardCmd.Flags().IntVar(&dayDashboardDays, "days", 180, "number of history days to include")
	dayFeedbackCmd.Flags().StringVar(&dayFeedbackVerdict, "verdict", "", "accurate, inaccurate, or corrected")
	dayFeedbackCmd.Flags().StringVar(&dayFeedbackBadge, "badge", "", "replacement badge id for corrected feedback")
	dayFeedbackCmd.Flags().StringVar(&dayFeedbackLabels, "labels", "", "comma-separated replacement semantic labels")
	dayFeedbackCmd.Flags().BoolVar(&dayFeedbackClear, "clear", false, "remove existing feedback for this day and restore the computed result")
	daySourcesPauseCmd.Flags().BoolVar(&daySourceSemantic, "semantic", true, "also pause semantic classification (kept for explicit contract)")
	daySourcesDeleteCmd.Flags().BoolVar(&daySourceDeleteYes, "yes", false, "confirm deletion of this source's AI Day derived events")
	dayPrivacySetCmd.Flags().StringVar(&dayPrivacySemantic, "semantic", "", "set global semantic classification: on or off")
	dayPrivacySetCmd.Flags().IntVar(&dayPrivacyRetention, "retention", 0, "derived event retention in days (1-3650)")
	dayDeleteCmd.Flags().StringVar(&dayDeleteFrom, "from", "", "first day to delete (YYYY-MM-DD)")
	dayDeleteCmd.Flags().StringVar(&dayDeleteTo, "to", "", "last day to delete (YYYY-MM-DD)")
	dayDeleteCmd.Flags().BoolVar(&dayDeleteAll, "all", false, "delete all AI Day derived data")
	dayDeleteCmd.Flags().BoolVar(&dayDeleteYes, "yes", false, "confirm deletion")
	daySourcesCmd.AddCommand(daySourcesListCmd, daySourcesPauseCmd, daySourcesResumeCmd, daySourcesDeleteCmd)
	dayPrivacyCmd.AddCommand(dayPrivacyShowCmd, dayPrivacySetCmd)
	dayCmd.AddCommand(dayDashboardCmd, dayHistoryCmd, dayAtlasCmd, dayBadgeCmd, dayFeedbackCmd, daySourcesCmd, dayPrivacyCmd, dayExportCmd, dayDeleteCmd)
}

var dayDashboardCmd = &cobra.Command{
	Use: "dashboard", Short: "Build today and return the complete desktop AI Day contract", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dayDashboardDays < 1 || dayDashboardDays > 3650 {
			return fmt.Errorf("--days must be between 1 and 3650")
		}
		return withDB(false, func(db *sql.DB) error {
			if err := syncDaySources(db); err != nil {
				return err
			}
			today := time.Now().In(config.Loc)
			summary, err := aiday.Refresh(cmd.Context(), db, time.Now(), config.Loc, 30)
			if err != nil {
				return err
			}
			atlas, err := aiday.LoadAtlas(cmd.Context(), db)
			if err != nil {
				return err
			}
			history, err := aiday.LoadHistory(cmd.Context(), db, today.AddDate(0, 0, -dayDashboardDays+1).Format(time.DateOnly), today.Format(time.DateOnly))
			if err != nil {
				return err
			}
			privacy, err := aiday.LoadPrivacy(cmd.Context(), db)
			if err != nil {
				return err
			}
			result := aiday.Dashboard{SchemaVersion: aiday.ContractVersion, Today: summary.Days[len(summary.Days)-1], Atlas: atlas, History: history, Privacy: privacy}
			if jsonOutput {
				output.JSON(result)
				return nil
			}
			printDay(result.Today)
			fmt.Printf("Atlas %d/%d · history %d days\n", atlas.Unlocked, atlas.Total, len(history.Days))
			return nil
		})
	},
}

var dayHistoryCmd = &cobra.Command{Use: "history", Short: "Show AI Day history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	if dayHistoryDays < 1 || dayHistoryDays > 3650 {
		return fmt.Errorf("--days must be between 1 and 3650")
	}
	to := time.Now().In(config.Loc)
	from := to.AddDate(0, 0, -dayHistoryDays+1)
	return withDB(true, func(db *sql.DB) error {
		h, err := aiday.LoadHistory(cmd.Context(), db, from.Format(time.DateOnly), to.Format(time.DateOnly))
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(h)
			return nil
		}
		for _, r := range h.Days {
			printDayLine(r)
		}
		return nil
	})
}}
var dayAtlasCmd = &cobra.Command{Use: "atlas", Short: "Show the 12-badge AI Day Atlas", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		a, err := aiday.LoadAtlas(cmd.Context(), db)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(a)
			return nil
		}
		fmt.Printf("AI Day Atlas: %d/%d unlocked\n", a.Unlocked, a.Total)
		for _, b := range a.Badges {
			state := "locked"
			if b.Unlocked {
				state = fmt.Sprintf("L%d", b.Level)
			}
			fmt.Printf("%-22s %-8s %s\n", b.ID, state, b.Name)
		}
		return nil
	})
}}
var dayBadgeCmd = &cobra.Command{Use: "badge <id>", Short: "Show badge progress and evidence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		b, err := aiday.LoadBadge(cmd.Context(), db, args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(b)
			return nil
		}
		fmt.Printf("%s · L%d · 累计 %d 天\n%s\n", b.Name, b.Level, b.QualifiedDays, b.Description)
		if b.NextLevelDays > b.QualifiedDays {
			fmt.Printf("距 L%d 还差 %d 天（%d/%d）\n", b.Level+1, b.NextLevelDays-b.QualifiedDays, b.QualifiedDays, b.NextLevelDays)
		}
		if b.CooldownUntil != "" {
			fmt.Printf("即时徽章冷却至 %s\n", b.CooldownUntil)
		}
		for _, evidence := range b.Evidence {
			fmt.Printf("  · %s %s\n", formatEvidenceValue(evidence), dayEvidenceLabel(evidence.Metric))
		}
		if len(b.QualifiedDates) > 0 {
			shown := b.QualifiedDates
			if len(shown) > 8 {
				shown = shown[:8]
			}
			fmt.Printf("最近达成：%s\n", strings.Join(shown, "  "))
		}
		return nil
	})
}}
var dayFeedbackCmd = &cobra.Command{Use: "feedback <YYYY-MM-DD>", Short: "Confirm, correct, or clear feedback on a daily result", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	day, err := parseLocalDay(args[0])
	if err != nil {
		return err
	}
	if dayFeedbackClear && dayFeedbackVerdict != "" {
		return fmt.Errorf("--clear cannot be combined with --verdict")
	}
	if !dayFeedbackClear && dayFeedbackVerdict == "" {
		return fmt.Errorf("--verdict is required (accurate, inaccurate, corrected) unless --clear is passed")
	}
	labels := splitCSV(dayFeedbackLabels)
	return withDB(false, func(db *sql.DB) error {
		// Clearing is what makes the correction reversible: without it a mis-tap in
		// the app permanently overrode the engine for that day.
		if dayFeedbackClear {
			if err := aiday.ClearFeedback(cmd.Context(), db, args[0]); err != nil {
				return err
			}
		} else {
			f := aiday.Feedback{Day: args[0], Verdict: dayFeedbackVerdict, CorrectedBadge: dayFeedbackBadge, SemanticLabels: labels}
			if err := aiday.SaveFeedback(cmd.Context(), db, f); err != nil {
				return err
			}
		}
		r, err := aiday.RebuildDay(cmd.Context(), db, day, config.Loc)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(r)
		} else {
			printDay(r)
		}
		return nil
	})
}}

var daySourcesCmd = &cobra.Command{Use: "sources", Short: "Manage AI Day source permissions", Args: cobra.NoArgs, RunE: showHelp}
var daySourcesListCmd = &cobra.Command{Use: "list", Short: "List source permissions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		p, err := aiday.LoadPrivacy(cmd.Context(), db)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(p.Sources)
			return nil
		}
		for _, s := range p.Sources {
			state := "enabled"
			if !s.Enabled {
				state = "paused"
			}
			fmt.Printf("%-16s %-8s semantic=%t events=%d\n", s.Source, state, s.SemanticEnabled, s.EventCount)
		}
		return nil
	})
}}
var daySourcesPauseCmd = &cobra.Command{Use: "pause <source>", Short: "Pause a source without deleting its history", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(false, func(db *sql.DB) error {
		if err := aiday.SetSource(cmd.Context(), db, args[0], false, !daySourceSemantic); err != nil {
			return err
		}
		return printPrivacy(cmd, db)
	})
}}
var daySourcesResumeCmd = &cobra.Command{Use: "resume <source>", Short: "Resume collection and semantic classification", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(false, func(db *sql.DB) error {
		if err := aiday.SetSource(cmd.Context(), db, args[0], true, true); err != nil {
			return err
		}
		return printPrivacy(cmd, db)
	})
}}
var daySourcesDeleteCmd = &cobra.Command{Use: "delete <source>", Short: "Delete a source's derived AI Day events and pause it", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if !daySourceDeleteYes {
		return fmt.Errorf("source deletion requires --yes")
	}
	return withDB(false, func(db *sql.DB) error {
		n, err := aiday.DeleteSource(cmd.Context(), db, args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"source": args[0], "events_deleted": n, "paused": true})
		} else {
			fmt.Printf("Deleted %d derived events for %s and paused the source.\n", n, args[0])
		}
		return nil
	})
}}

var dayPrivacyCmd = &cobra.Command{Use: "privacy", Short: "Inspect and change AI Day privacy", Args: cobra.NoArgs, RunE: showHelp}
var dayPrivacyShowCmd = &cobra.Command{Use: "show", Short: "Show privacy settings and source permissions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error { return printPrivacy(cmd, db) })
}}
var dayPrivacySetCmd = &cobra.Command{Use: "set", Short: "Set semantic classification or retention", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	var semantic *bool
	if dayPrivacySemantic != "" {
		value := strings.ToLower(dayPrivacySemantic)
		if value != "on" && value != "off" {
			return fmt.Errorf("--semantic must be on or off")
		}
		v := value == "on"
		semantic = &v
	}
	var retention *int
	if cmd.Flags().Changed("retention") {
		retention = &dayPrivacyRetention
	}
	if semantic == nil && retention == nil {
		return fmt.Errorf("set --semantic and/or --retention")
	}
	return withDB(false, func(db *sql.DB) error {
		if err := aiday.SetPrivacy(cmd.Context(), db, semantic, retention); err != nil {
			return err
		}
		return printPrivacy(cmd, db)
	})
}}

func printPrivacy(cmd *cobra.Command, db *sql.DB) error {
	p, err := aiday.LoadPrivacy(cmd.Context(), db)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(p)
	} else {
		fmt.Printf("Semantic classification: %t\nRetention: %d days\nRaw content retained: %t\n", p.SemanticEnabled, p.RetentionDays, p.RawRetained)
	}
	return nil
}

var dayExportCmd = &cobra.Command{Use: "export", Short: "Export all AI Day derived data as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		e, err := aiday.ExportAll(cmd.Context(), db)
		if err != nil {
			return err
		}
		output.JSON(e)
		return nil
	})
}}
var dayDeleteCmd = &cobra.Command{Use: "delete", Short: "Delete AI Day derived data for a range", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	if !dayDeleteYes {
		return fmt.Errorf("AI Day deletion requires --yes")
	}
	from, to := dayDeleteFrom, dayDeleteTo
	if dayDeleteAll {
		from = "0001-01-01"
		to = "9999-12-31"
	} else {
		if from == "" {
			return fmt.Errorf("set --from or --all")
		}
		if to == "" {
			to = from
		}
		if _, err := parseLocalDay(from); err != nil {
			return err
		}
		if _, err := parseLocalDay(to); err != nil {
			return err
		}
	}
	return withDB(false, func(db *sql.DB) error {
		s, err := aiday.DeleteRange(cmd.Context(), db, from, to)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(s)
		} else {
			fmt.Printf("Deleted %d events, %d projections, and %d feedback rows.\n", s.Events, s.Projections, s.Feedback)
		}
		return nil
	})
}}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			result = append(result, v)
		}
	}
	return result
}
