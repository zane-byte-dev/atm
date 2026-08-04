package cmd

import "github.com/spf13/cobra"

var sessionCmd = &cobra.Command{
	Use:     "session",
	Short:   "Query and browse AI sessions",
	Aliases: []string{"s"},
	Args:    cobra.NoArgs, // unknown subcommand errors instead of silently showing help
	RunE:    showHelp,
}

func init() {
	rootCmd.AddCommand(sessionCmd)
}
