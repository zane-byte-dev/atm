package cmd

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	exportCmd.Flags().IntVar(&exportDaysFlag, "days", 7, "number of days to look back")
	exportCmd.Flags().StringVar(&exportSinceFlag, "since", "", "export messages since RFC3339 timestamp or YYYY-MM-DD (overrides --days)")
	exportCmd.Flags().StringVar(&exportUntilFlag, "until", "", "export messages before RFC3339 timestamp or YYYY-MM-DD (default: now)")
	exportCmd.Flags().StringVar(&exportProjectFlag, "project", "", "filter by project name (case-insensitive substring)")
	exportCmd.Flags().StringVar(&exportRoleFlag, "role", "", "filter by message role")
	exportCmd.Flags().StringVar(&exportQueryFlag, "query", "", "filter message content (case-insensitive substring)")
	exportCmd.Flags().StringVar(&exportFormatFlag, "format", "json", "output format: json, jsonl, or csv")
	exportCmd.Flags().IntVar(&exportLimitFlag, "limit", 0, "maximum number of exported messages (0 means all)")
	exportCmd.Flags().IntVar(&exportOffsetFlag, "offset", 0, "number of filtered messages to skip")
	exportCmd.MarkFlagsMutuallyExclusive("days", "since")
	sessionCmd.AddCommand(exportCmd)
}

var (
	exportDaysFlag    int
	exportSinceFlag   string
	exportUntilFlag   string
	exportProjectFlag string
	exportRoleFlag    string
	exportQueryFlag   string
	exportFormatFlag  string
	exportLimitFlag   int
	exportOffsetFlag  int
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export raw session data",
	Long: `Export filtered session messages as JSON, newline-delimited JSON, or CSV.

JSON includes schema and pagination metadata. Pagination is applied after
project, role, text, and time filters.`,
	Example: `  atm session export --days 7 --format json
  atm session export --since 2026-08-01 --until 2026-09-01 --project atm --role user --format jsonl
  atm session export --days 30 --query deployment --limit 500 --offset 500 --format json`,
	Args: cobra.NoArgs,
	RunE: runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	format := strings.ToLower(strings.TrimSpace(exportFormatFlag))
	if format != "json" && format != "jsonl" && format != "csv" {
		return fmt.Errorf("unsupported format: %s (use json, jsonl, or csv)", exportFormatFlag)
	}
	if exportLimitFlag < 0 {
		return fmt.Errorf("--limit must not be negative")
	}
	if exportOffsetFlag < 0 {
		return fmt.Errorf("--offset must not be negative")
	}
	days := exportDaysFlag
	if days < 1 {
		days = 1
	}

	return withDB(true, func(db *sql.DB) error {
		now := time.Now().In(config.Loc)
		end := now
		if strings.TrimSpace(exportUntilFlag) != "" {
			parsed, err := parseSessionSince(exportUntilFlag)
			if err != nil {
				return fmt.Errorf("%s", strings.Replace(err.Error(), "--since", "--until", 1))
			}
			end = parsed
		}
		start := startOfDayWindow(end, days)
		if strings.TrimSpace(exportSinceFlag) != "" {
			parsed, err := parseSessionSince(exportSinceFlag)
			if err != nil {
				return err
			}
			start = parsed
		}
		if !start.Before(end) {
			return fmt.Errorf("--since must be earlier than --until")
		}

		rows, err := store.ExportMessages(db, start.Unix(), end.Unix(), agent)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}
		rows = filterExportRows(rows, start.Unix(), end.Unix(), exportProjectFlag, exportRoleFlag, exportQueryFlag)
		total := len(rows)
		rows = pageExportRows(rows, exportOffsetFlag, exportLimitFlag)

		if format == "csv" {
			w := csv.NewWriter(os.Stdout)
			if err := w.Write([]string{"session_id", "agent", "project", "created_at", "role", "content", "ts"}); err != nil {
				return err
			}
			for _, r := range rows {
				if err := w.Write([]string{r.SessionID, r.Agent, r.Project, r.CreatedAt, r.Role, r.Content, strconv.FormatInt(r.TS, 10)}); err != nil {
					return err
				}
			}
			w.Flush()
			return w.Error()
		}

		if format == "jsonl" {
			enc := json.NewEncoder(os.Stdout)
			for _, row := range rows {
				if err := enc.Encode(row); err != nil {
					return err
				}
			}
			return nil
		}
		return writeExportEnvelope(start, end, total, rows)
	})
}

func filterExportRows(rows []store.ExportRow, startTS, endTS int64, project, role, query string) []store.ExportRow {
	project = strings.TrimSpace(project)
	role = strings.ToLower(strings.TrimSpace(role))
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]store.ExportRow, 0, len(rows))
	for _, row := range rows {
		if row.TS > 0 && (row.TS < startTS || row.TS >= endTS) {
			continue
		}
		if project != "" && !config.ProjectMatches(row.Project, project) {
			continue
		}
		if role != "" && strings.ToLower(row.Role) != role {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.Content), query) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func pageExportRows(rows []store.ExportRow, offset, limit int) []store.ExportRow {
	if offset >= len(rows) {
		return []store.ExportRow{}
	}
	end := len(rows)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return rows[offset:end]
}

func writeExportEnvelope(start, end time.Time, total int, rows []store.ExportRow) error {
	returned := len(rows)
	payload := map[string]any{
		"schema_version": sessionCLIOutputSchemaVersion,
		"total":          total,
		"returned":       returned,
		"truncated":      exportOffsetFlag+returned < total,
		"limit":          exportLimitFlag,
		"offset":         exportOffsetFlag,
		"since":          start.Format(time.RFC3339),
		"until":          end.Format(time.RFC3339),
		"messages":       rows,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}
