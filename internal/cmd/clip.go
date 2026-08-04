package cmd

import (
	"database/sql"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	sessionCmd.AddCommand(clipCmd)
}

var clipCmd = &cobra.Command{
	Use:   "clip <keyword>",
	Short: "Copy an AI response to clipboard",
	Args:  cobra.ExactArgs(1),
	RunE:  runClip,
}

func runClip(cmd *cobra.Command, args []string) error {
	keyword := args[0]
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	return withDB(true, func(db *sql.DB) error {
		results, err := store.SearchMessages(db, keyword, agent)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}

		// Last match, not first: SearchMessages returns oldest first, and the
		// index keeps sessions whose transcript has already been rotated away,
		// so scanning forwards would reach for the most ancient answer that
		// mentions the keyword. What a caller wants to paste is the latest one.
		var hit *store.SearchHit
		for i := len(results) - 1; i >= 0; i-- {
			if results[i].Role == "assistant" {
				hit = &results[i]
				break
			}
		}
		if hit == nil {
			fmt.Println("No matching AI response found.")
			return nil
		}

		content := cleanMsg(hit.Content)

		if err := copyToClipboard(content); err != nil {
			return err
		}

		preview := truncLine(content, 80)
		fmt.Printf("Copied to clipboard (%s | %s)\n", hit.ShortID, hit.CreatedAt)
		fmt.Printf("  %s\n", preview)
		return nil
	})
}

func copyToClipboard(content string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "linux":
		candidates = [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		return fmt.Errorf("clipboard is not supported on %s", runtime.GOOS)
	}

	for _, c := range candidates {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(content)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", c[0], err)
		}
		return nil
	}
	return fmt.Errorf("no clipboard command found for %s", runtime.GOOS)
}
