package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

var (
	dayFromFlag string
	dayToFlag   string
)

func init() {
	dayRebuildCmd.Flags().StringVar(&dayFromFlag, "from", "", "first local day to rebuild (YYYY-MM-DD; default today)")
	dayRebuildCmd.Flags().StringVar(&dayToFlag, "to", "", "last local day to rebuild, inclusive (YYYY-MM-DD; default --from)")
	dayCmd.AddCommand(dayTodayCmd, dayShowCmd, dayRebuildCmd)
	rootCmd.AddCommand(dayCmd)
}

var dayCmd = &cobra.Command{
	Use:   "day",
	Short: "Build and inspect AI Day concepts",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var dayTodayCmd = &cobra.Command{
	Use:   "today",
	Short: "Build the recent baseline and show today's AI Day concept",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(false, func(db *sql.DB) error {
			if err := syncDaySources(db); err != nil {
				return err
			}
			today := time.Now().In(config.Loc)
			// Zero-configuration first use: derive the recent calendar window before
			// selecting today, otherwise a database with years of session history
			// would still report a cold-start concept until the user discovered the
			// explicit rebuild command.
			summary, err := aiday.Rebuild(cmd.Context(), db, today.AddDate(0, 0, -30), today, config.Loc)
			if err != nil {
				return err
			}
			printDay(summary.Days[len(summary.Days)-1])
			return nil
		})
	},
}

var dayShowCmd = &cobra.Command{
	Use:   "show <YYYY-MM-DD>",
	Short: "Show a previously built AI Day concept",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		day, err := parseLocalDay(args[0])
		if err != nil {
			return err
		}
		return withDB(true, func(db *sql.DB) error {
			result, err := aiday.Load(cmd.Context(), db, day.Format(time.DateOnly))
			if errors.Is(err, aiday.ErrDayNotBuilt) {
				return fmt.Errorf("AI Day %s has not been built; run `atm day rebuild --from %s`", args[0], args[0])
			}
			if err != nil {
				return err
			}
			printDay(result)
			return nil
		})
	},
}

var dayRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild AI Day projections for an inclusive date range",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		from, to, err := dayRebuildRange(time.Now().In(config.Loc), dayFromFlag, dayToFlag)
		if err != nil {
			return err
		}
		return withDB(false, func(db *sql.DB) error {
			if err := syncDaySources(db); err != nil {
				return err
			}
			summary, err := aiday.Rebuild(cmd.Context(), db, from, to, config.Loc)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(summary)
				return nil
			}
			fmt.Printf("Rebuilt %d AI Day projection(s), %s to %s.\n", summary.Count, summary.From, summary.To)
			for _, result := range summary.Days {
				printDayLine(result)
			}
			return nil
		})
	},
}

func parseLocalDay(value string) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, value, config.Loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day %q (use YYYY-MM-DD)", value)
	}
	return day, nil
}

func dayRebuildRange(now time.Time, fromValue, toValue string) (time.Time, time.Time, error) {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, config.Loc)
	var err error
	if fromValue != "" {
		from, err = parseLocalDay(fromValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	to := from
	if toValue != "" {
		to, err = parseLocalDay(toValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("--to %s is before --from %s", to.Format(time.DateOnly), from.Format(time.DateOnly))
	}
	return from, to, nil
}

func syncDaySources(db *sql.DB) error {
	if !syncBeforeRead {
		return nil
	}
	n, err := store.SyncAll(db)
	if err != nil {
		return fmt.Errorf("sync before AI Day rebuild: %w", err)
	}
	if n > 0 && !jsonOutput {
		output.Progress("Synced %d files.", n)
	}
	return nil
}

func printDay(result aiday.Result) {
	if jsonOutput {
		output.JSON(result)
		return
	}
	printDayLine(result)
	if result.Concept == nil {
		fmt.Println("  No indexed AI activity for this day.")
		return
	}
	fmt.Printf("  %s\n", result.Concept.Explanation)
	fmt.Printf("  %d sessions · %d turns · %d tool calls · %s tokens · baseline %d days\n",
		result.Features.SessionCount, result.Features.TurnCount, result.Features.ToolCalls,
		formatDayTokens(result.Features.TotalTokens()), result.BaselineDays)
}

func printDayLine(result aiday.Result) {
	title := "No indexed activity"
	if result.Concept != nil {
		title = result.Concept.Title
	}
	fmt.Printf("%s  %s\n", result.Day, title)
}

func formatDayTokens(value int64) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}
