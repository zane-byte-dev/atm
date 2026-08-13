package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"

	"github.com/spf13/cobra"
)

func init() {
	configCmd.AddCommand(updatePricingCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config [init]",
	Short: "Show or initialize configuration",
	Long: `Show current configuration or create a config file.

  atm config                  Show current effective configuration
  atm config init             Create ~/.atm/config.json with defaults
  atm config get <key>        Read one setting (effective value)
  atm config set <key> <val>  Write one setting to ~/.atm/config.json
  atm config update-pricing   Fetch latest model pricing from OpenRouter

Settable keys:
  owner_name NAME               How to name you when a todo you filed yourself
                                is displayed (default 我). The stored creator
                                stays "me", so renaming yourself never rewrites
                                a record
  grok_live_quota   true|false  Query the Grok billing API for live quota
                                (default false: quota reads local logs only)
  collection_enabled true|false Enable connector collection in the resident App
  collection_interval_minutes N Poll interval in minutes (default 5)
  collection_lookback_minutes N Initial source lookback (default 60)
  collection_message_retention_days N  Days of synced chat to keep
                                (default 90; 0 keeps it forever)
  collection_model_command CMD  Structured classifier command (default codex).
                                Accepts a comma-separated chain tried in order,
                                e.g. "grok,codex" or "grok,codex,rule"; the next
                                one runs when the previous is rate limited,
                                times out or is not installed. codex and grok
                                have built-in profiles; any other CLI needs a
                                collection_model_runners entry in config.json
                                (see docs/collection-model-runner.md)
  todo_refine_on_add true|false After a human files a todo in the App, run one
                                model pass to polish the card and split complex
                                work (default true). CLI todo add is never
                                implicit; use todo add --refine or
                                todo refine <id>. Same model chain as
                                collection_model_command`,
	// Only `init` is a valid positional arg; anything else errors instead of
	// silently falling through to "show config".
	ValidArgs: []string{"init"},
	Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
	RunE:      runConfig,
}

// settableConfigKeys maps `atm config set` keys to a value parser. Kept
// deliberately small: path-style settings should be edited in config.json
// where the surrounding context is visible.
var settableConfigKeys = map[string]func(string) (any, error){
	"owner_name":                  parseNonEmptyStringValue,
	"grok_live_quota":             parseBoolValue,
	"collection_enabled":          parseBoolValue,
	"collection_interval_minutes": parsePositiveIntValue,
	"collection_lookback_minutes": parsePositiveIntValue,
	// 0 is a real setting here: keep synced chat forever.
	"collection_message_retention_days": parseNonNegativeIntValue,
	"collection_model_command":          parseNonEmptyStringValue,
	"todo_refine_on_add":                parseBoolValue,
}

func parseBoolValue(s string) (any, error) {
	switch strings.ToLower(s) {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	}
	return nil, fmt.Errorf("expected true or false, got %q", s)
}

func parsePositiveIntValue(s string) (any, error) {
	value, err := strconv.Atoi(s)
	if err != nil || value < 1 {
		return nil, fmt.Errorf("expected a positive integer, got %q", s)
	}
	return value, nil
}

func parseNonNegativeIntValue(s string) (any, error) {
	value, err := strconv.Atoi(s)
	if err != nil || value < 0 {
		return nil, fmt.Errorf("expected zero or a positive integer, got %q", s)
	}
	return value, nil
}

func parseNonEmptyStringValue(s string) (any, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("value must not be empty")
	}
	return s, nil
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Write one setting to ~/.atm/config.json",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		parse, ok := settableConfigKeys[args[0]]
		if !ok {
			return fmt.Errorf("unknown or non-settable key: %s (settable: %s)", args[0], strings.Join(settableKeyNames(), ", "))
		}
		value, err := parse(args[1])
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", args[0], err)
		}
		if err := config.SetConfigValue(args[0], value); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"key": args[0], "value": value})
			return nil
		}
		fmt.Printf("%s = %v\n", args[0], value)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read one setting (effective value, including env overrides)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var value any
		switch args[0] {
		case "owner_name":
			value = config.OwnerName
		case "grok_live_quota":
			value = config.GrokLiveQuota
		case "collection_enabled":
			value = config.CollectionEnabled
		case "collection_interval_minutes":
			value = config.CollectionIntervalMinutes
		case "collection_lookback_minutes":
			value = config.CollectionLookbackMinutes
		case "collection_message_retention_days":
			value = config.CollectionMessageRetentionDays
		case "collection_model_command":
			value = config.CollectionModelCommand
		case "todo_refine_on_add":
			value = config.TodoRefineOnAdd
		default:
			return fmt.Errorf("unknown key: %s (readable: %s)", args[0], strings.Join(settableKeyNames(), ", "))
		}
		if jsonOutput {
			output.JSON(map[string]any{"key": args[0], "value": value})
			return nil
		}
		fmt.Printf("%v\n", value)
		return nil
	},
}

func settableKeyNames() []string {
	names := make([]string, 0, len(settableConfigKeys))
	for name := range settableConfigKeys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var updatePricingCmd = &cobra.Command{
	Use:   "update-pricing",
	Short: "Fetch latest model pricing from OpenRouter",
	RunE:  runUpdatePricing,
}

func runConfig(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "init" {
		if err := config.InitConfig(); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", config.ConfigPath)
		return nil
	}

	fmt.Println(config.ShowConfig())
	return nil
}

func runUpdatePricing(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return fmt.Errorf("fetch OpenRouter models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("OpenRouter API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt          string `json:"prompt"`
				Completion      string `json:"completion"`
				InputCacheRead  string `json:"input_cache_read"`
				InputCacheWrite string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	// [input, output, cache_write, cache_read] per million tokens
	pricing := make(map[string][4]float64)
	for _, m := range result.Data {
		var p [4]float64
		p[0] = parsePerToken(m.Pricing.Prompt)
		p[1] = parsePerToken(m.Pricing.Completion)
		p[2] = parsePerToken(m.Pricing.InputCacheWrite)
		p[3] = parsePerToken(m.Pricing.InputCacheRead)
		if p[0] <= 0 && p[1] <= 0 {
			continue
		}
		// Use short model name (strip provider prefix)
		name := m.ID
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		pricing[name] = p
	}

	pricingPath := filepath.Join(config.AtmDir, "pricing.json")
	b, _ := json.MarshalIndent(pricing, "", "  ")
	b = append(b, '\n')
	if err := os.MkdirAll(config.AtmDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(pricingPath, b, 0644); err != nil {
		return err
	}

	models := make([]string, 0, len(pricing))
	for k := range pricing {
		models = append(models, k)
	}
	sort.Strings(models)

	fmt.Printf("Updated %s (%d models)\n", pricingPath, len(pricing))

	// Show a few relevant models
	highlights := []string{"claude-opus-4.6", "claude-sonnet-4.6", "claude-fable-5", "gpt-5.5", "deepseek-v4-pro"}
	for _, h := range highlights {
		if p, ok := pricing[h]; ok {
			fmt.Printf("  %-30s  in=$%.2f  out=$%.2f  cache_w=$%.2f  cache_r=$%.2f\n",
				h, p[0], p[1], p[2], p[3])
		}
	}

	return nil
}

func parsePerToken(s string) float64 {
	if s == "" || s == "-1" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f * 1e6
}
