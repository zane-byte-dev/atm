package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

const (
	quotaProviderProtocolVersion = 1
	quotaProviderOutputLimit     = 1 << 20
	quotaProviderErrorLimit      = 64 << 10
	quotaProviderDefaultTimeout  = 10 * time.Second
)

var quotaProviderToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type quotaProviderRequest struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
}

type quotaProviderResponse struct {
	Version int                 `json:"version"`
	Cards   []quotaProviderCard `json:"cards"`
	Error   string              `json:"error,omitempty"`
}

// quotaProviderCard is deliberately provider-neutral. A private integration
// can expose one or more bounded metrics without teaching ATM about its
// service, endpoints, credentials, or product-specific vocabulary.
type quotaProviderCard struct {
	ID         string                `json:"id"`
	Agent      string                `json:"agent"`
	Provider   string                `json:"provider"`
	Title      string                `json:"title"`
	Period     string                `json:"period,omitempty"`
	ObservedAt string                `json:"observed_at"`
	Source     string                `json:"source,omitempty"`
	Metrics    []quotaProviderMetric `json:"metrics"`
}

type quotaProviderMetric struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	UsedPercent float64 `json:"used_percent"`
	Unit        string  `json:"unit,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	Precision   int     `json:"precision,omitempty"`
}

type quotaProviderResult struct {
	provider string
	cards    []quotaProviderCard
	err      error
}

type quotaProviderBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *quotaProviderBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return written, nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		data = data[:remaining]
	}
	_, _ = buffer.Buffer.Write(data)
	return written, nil
}

func loadQuotaProviderCards(ctx context.Context) (map[string][]quotaProviderCard, []error) {
	if len(config.QuotaProviders) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(config.QuotaProviders))
	for name := range config.QuotaProviders {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make(chan quotaProviderResult, len(names))
	var group sync.WaitGroup
	for _, name := range names {
		providerConfig := config.QuotaProviders[name]
		group.Add(1)
		go func() {
			defer group.Done()
			cards, err := callQuotaProvider(ctx, name, providerConfig)
			results <- quotaProviderResult{provider: name, cards: cards, err: err}
		}()
	}
	group.Wait()
	close(results)

	ordered := make([]quotaProviderResult, 0, len(names))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].provider < ordered[j].provider })

	cardsByAgent := map[string][]quotaProviderCard{}
	var errs []error
	for _, result := range ordered {
		if result.err != nil {
			errs = append(errs, result.err)
			continue
		}
		for _, card := range result.cards {
			cardsByAgent[card.Agent] = append(cardsByAgent[card.Agent], card)
		}
	}
	for agent := range cardsByAgent {
		sort.Slice(cardsByAgent[agent], func(i, j int) bool {
			left, right := cardsByAgent[agent][i], cardsByAgent[agent][j]
			if left.Provider != right.Provider {
				return left.Provider < right.Provider
			}
			return left.ID < right.ID
		})
	}
	return cardsByAgent, errs
}

func callQuotaProvider(parent context.Context, providerID string,
	providerConfig config.QuotaProviderConfig) ([]quotaProviderCard, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if !quotaProviderToken.MatchString(providerID) {
		return nil, fmt.Errorf("quota provider id %q is invalid", providerID)
	}
	commandPath := expandQuotaProviderPath(strings.TrimSpace(providerConfig.Command))
	if commandPath == "" {
		return nil, fmt.Errorf("quota provider %s has no command", providerID)
	}
	timeout := quotaProviderDefaultTimeout
	if providerConfig.TimeoutSeconds > 0 {
		timeout = time.Duration(providerConfig.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	payload, err := json.Marshal(quotaProviderRequest{
		Version: quotaProviderProtocolVersion, Operation: "quota",
	})
	if err != nil {
		return nil, err
	}
	args := append(append([]string{}, providerConfig.Args...), "quota")
	command := exec.CommandContext(ctx, commandPath, args...)
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	stdout := quotaProviderBuffer{limit: quotaProviderOutputLimit}
	stderr := quotaProviderBuffer{limit: quotaProviderErrorLimit}
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("quota provider %s timed out after %s", providerID, timeout)
		}
		message := strings.TrimSpace(stderr.String())
		if stderr.exceeded {
			message += "…"
		}
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("quota provider %s failed: %s", providerID, message)
	}
	if stdout.exceeded {
		return nil, fmt.Errorf("quota provider %s output exceeds %d bytes", providerID, quotaProviderOutputLimit)
	}

	var response quotaProviderResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("decode quota provider %s response: %w", providerID, err)
	}
	if response.Version != quotaProviderProtocolVersion {
		return nil, fmt.Errorf("quota provider %s returned protocol version %d, want %d",
			providerID, response.Version, quotaProviderProtocolVersion)
	}
	if message := strings.TrimSpace(response.Error); message != "" {
		return nil, fmt.Errorf("quota provider %s: %s", providerID, message)
	}
	if err := normalizeQuotaProviderCards(providerID, response.Cards); err != nil {
		return nil, err
	}
	return response.Cards, nil
}

func expandQuotaProviderPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func normalizeQuotaProviderCards(providerID string, cards []quotaProviderCard) error {
	seenCards := map[string]bool{}
	for cardIndex := range cards {
		card := &cards[cardIndex]
		card.ID = strings.ToLower(strings.TrimSpace(card.ID))
		card.Agent = strings.ToLower(strings.TrimSpace(card.Agent))
		card.Provider = strings.ToLower(strings.TrimSpace(card.Provider))
		if card.Provider == "" {
			card.Provider = providerID
		}
		if !quotaProviderToken.MatchString(card.ID) ||
			!quotaProviderToken.MatchString(card.Agent) ||
			!quotaProviderToken.MatchString(card.Provider) {
			return fmt.Errorf("quota provider %s returned invalid card %d identifiers", providerID, cardIndex)
		}
		key := card.Agent + ":" + card.Provider + ":" + card.ID
		if seenCards[key] {
			return fmt.Errorf("quota provider %s returned duplicate card %s", providerID, key)
		}
		seenCards[key] = true
		card.Title = strings.TrimSpace(card.Title)
		card.Period = strings.TrimSpace(card.Period)
		card.Source = strings.TrimSpace(card.Source)
		if card.Title == "" || len(card.Metrics) == 0 {
			return fmt.Errorf("quota provider %s card %s requires title and metrics", providerID, card.ID)
		}
		if card.ObservedAt != "" {
			if _, err := time.Parse(time.RFC3339, card.ObservedAt); err != nil {
				return fmt.Errorf("quota provider %s card %s observed_at: %w", providerID, card.ID, err)
			}
		}
		seenMetrics := map[string]bool{}
		for metricIndex := range card.Metrics {
			metric := &card.Metrics[metricIndex]
			metric.ID = strings.ToLower(strings.TrimSpace(metric.ID))
			metric.Label = strings.TrimSpace(metric.Label)
			metric.Unit = strings.TrimSpace(metric.Unit)
			metric.Currency = strings.ToUpper(strings.TrimSpace(metric.Currency))
			if !quotaProviderToken.MatchString(metric.ID) || metric.Label == "" || seenMetrics[metric.ID] {
				return fmt.Errorf("quota provider %s card %s has invalid or duplicate metric %d",
					providerID, card.ID, metricIndex)
			}
			seenMetrics[metric.ID] = true
			if math.IsNaN(metric.Used) || math.IsInf(metric.Used, 0) || metric.Used < 0 ||
				math.IsNaN(metric.Limit) || math.IsInf(metric.Limit, 0) || metric.Limit <= 0 {
				return fmt.Errorf("quota provider %s card %s metric %s has invalid bounds",
					providerID, card.ID, metric.ID)
			}
			if metric.Precision < 0 || metric.Precision > 6 {
				return fmt.Errorf("quota provider %s card %s metric %s precision must be 0..6",
					providerID, card.ID, metric.ID)
			}
			metric.UsedPercent = metric.Used / metric.Limit * 100
		}
	}
	return nil
}
