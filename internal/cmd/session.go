package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Short:   "Query and browse AI sessions",
	Aliases: []string{"s"},
	Args:    noSubcommandArgs, // unknown subcommand errors instead of silently showing help
	RunE:    showHelp,
}

func init() {
	rootCmd.AddCommand(sessionCmd)
}

func currentSessionService() sessionapp.Service {
	return sessionapp.NewService(sessionapp.ServiceOptions{Location: config.Loc})
}

func renderSessionReadMeta(meta sessionapp.ReadMeta) {
	if meta.SyncedFiles > 0 && !jsonOutput {
		output.Progress("Synced %d files.", meta.SyncedFiles)
	}
}
