package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/textmodel"

	"github.com/spf13/cobra"
)

func init() {
	configCmd.AddCommand(updatePricingCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configTestTextModelCmd)
	configCredentialCmd.AddCommand(configCredentialStatusCmd)
	configCredentialCmd.AddCommand(configCredentialSetCmd)
	configCredentialCmd.AddCommand(configCredentialDeleteCmd)
	configCmd.AddCommand(configCredentialCmd)
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
  atm config test-text-model  Test the built-in text service without changing a Todo
  atm config credential ...   Manage the local DeepSeek credential
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
  text_model_base_url URL         DeepSeek/OpenAI-compatible endpoint used by
                                  ATM's built-in text service
  text_model_name MODEL           Model used by the built-in text service
                                  (default deepseek-v4-flash)
  text_model_source LABEL         Short provenance shown on refined Todos
                                  (default deepseek, rendered as "from LABEL")
  todo_refine_prompt TEXT         Editable refinement policy appended to ATM's
                                  fixed safety and JSON-shape prompt. The default
                                  keeps one feature's implementation phases in a
                                  single Todo; pass an empty string to restore it
  todo_refine_on_add true|false After a human files a todo in the App, run one
                                model pass to polish the card and split complex
                                work (default false — 优化 is an action on the
                                Todo, not something that happens on add). CLI
                                todo add is never implicit; use
                                todo add --refine or todo refine <id>. The saved API Key lives in
                                ~/.atm/credentials.json (mode 0600); CLI users
                                can temporarily override it with DEEPSEEK_API_KEY`,
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
	"text_model_base_url":               parseHTTPURLValue,
	"text_model_name":                   parseNonEmptyStringValue,
	"text_model_source":                 parseTextModelSourceValue,
	"todo_refine_prompt":                parseTodoRefinePromptValue,
	"todo_refine_on_add":                parseBoolValue,
}

func parseTextModelSourceValue(s string) (any, error) {
	value := strings.Join(strings.Fields(s), " ")
	if value == "" {
		return nil, fmt.Errorf("source label must not be empty")
	}
	if utf8.RuneCountInString(value) > 80 {
		return nil, fmt.Errorf("source label must be at most 80 characters")
	}
	return value, nil
}

func parseTodoRefinePromptValue(s string) (any, error) {
	value := strings.TrimSpace(s)
	if utf8.RuneCountInString(value) > 4000 {
		return nil, fmt.Errorf("todo refine prompt must be at most 4000 characters")
	}
	return value, nil
}

func parseHTTPURLValue(s string) (any, error) {
	value := strings.TrimRight(strings.TrimSpace(s), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("expected an http or https URL, got %q", s)
	}
	return value, nil
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
		case "text_model_base_url":
			value = config.TextModelBaseURL
		case "text_model_name":
			value = config.TextModelName
		case "text_model_source":
			value = config.TextModelSource
		case "todo_refine_prompt":
			value = config.TodoRefinePrompt
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

var configTestTextModelCmd = &cobra.Command{
	Use:   "test-text-model",
	Short: "Test the built-in text service without changing a Todo",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := textmodel.Check(cmd.Context(), 45*time.Second)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(result)
			return nil
		}
		fmt.Printf("DeepSeek text model is ready (%d ms)\n", result.LatencyMS)
		return nil
	},
}

var configCredentialCmd = &cobra.Command{
	Use:   "credential",
	Short: "Manage the local DeepSeek credential",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var configCredentialStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report whether the DeepSeek credential is configured",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		configured, err := config.TextModelAPIKeyConfigured()
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"configured": configured})
			return nil
		}
		if configured {
			fmt.Println("DeepSeek API Key is configured")
		} else {
			fmt.Println("DeepSeek API Key is not configured")
		}
		return nil
	},
}

var configCredentialSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Read the DeepSeek API Key from stdin and save it privately",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		const maxCredentialBytes = 64 << 10
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxCredentialBytes+1))
		if err != nil {
			return fmt.Errorf("read DeepSeek API Key from stdin: %w", err)
		}
		if len(data) > maxCredentialBytes {
			return fmt.Errorf("DeepSeek API Key exceeds %d bytes", maxCredentialBytes)
		}
		if err := config.SaveTextModelAPIKey(string(data)); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"configured": true})
			return nil
		}
		fmt.Printf("Saved DeepSeek API Key to %s\n", config.CredentialsPath())
		return nil
	},
}

var configCredentialDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the locally saved DeepSeek credential",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := config.DeleteTextModelAPIKey(); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"configured": false})
			return nil
		}
		fmt.Println("Deleted DeepSeek API Key")
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
