package cmd

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	exportCmd.Flags().IntVar(&exportDaysFlag, "days", 7, "number of days to look back")
	exportCmd.Flags().StringVar(&exportFormatFlag, "format", "json", "output format: json or csv")
	sessionCmd.AddCommand(exportCmd)
}

var (
	exportDaysFlag   int
	exportFormatFlag string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export raw session data",
	RunE:  runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	if exportFormatFlag != "json" && exportFormatFlag != "csv" {
		return fmt.Errorf("unsupported format: %s (use json or csv)", exportFormatFlag)
	}

	days := exportDaysFlag
	if days < 1 {
		days = 1
	}

	return withDB(true, func(db *sql.DB) error {
		now := time.Now().In(config.Loc)
		start := startOfDayWindow(now, days)

		rows, err := store.ExportMessages(db, start.Unix(), now.Unix(), agent)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}

		if exportFormatFlag == "csv" {
			w := csv.NewWriter(os.Stdout)
			w.Write([]string{"session_id", "agent", "project", "created_at", "role", "content", "ts"})
			for _, r := range rows {
				w.Write([]string{r.SessionID, r.Agent, r.Project, r.CreatedAt, r.Role, r.Content, strconv.FormatInt(r.TS, 10)})
			}
			w.Flush()
			return w.Error()
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	})
}
