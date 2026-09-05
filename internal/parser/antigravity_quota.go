package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Antigravity does not write its quota anywhere on disk. The only reading
// available is the one its own UI shows, which comes from a Connect-RPC the
// bundled language_server exposes on loopback:
//
//	POST /exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary
//
// That makes this the one quota source in ATM that speaks HTTP on the local
// sampling path. It is treated as a local read rather than a network one: the
// peer is a process already running on this machine, reached over 127.0.0.1, and
// it answers from a cache of its own. The alternative was no Antigravity quota at
// all, since nothing equivalent is persisted.
//
// Everything about the endpoint is per-launch: the port is chosen at startup and
// the CSRF token is a fresh uuid in the process's own argv. Neither is cached —
// only the reading is, below — because a stale port would be silently wrong
// rather than merely stale.

const (
	// antigravityQuotaCacheFresh mirrors the Codex and Grok quota caches: a
	// reading this young is reused rather than re-fetched, so a menu bar refresh
	// loop does not spawn ps and lsof every few seconds.
	antigravityQuotaCacheFresh = 2 * time.Minute
	// antigravityQuotaTimeout bounds the whole exchange. This is an interactive
	// path and a language server that has wedged must cost a moment, not a hang.
	antigravityQuotaTimeout = 3 * time.Second
	antigravityQuotaMethod  = "/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary"
	// antigravityQuotaBodyLimit caps what is read from a loopback peer that may
	// not be the language server at all.
	antigravityQuotaBodyLimit = 1 << 20
	// Window lengths the upstream names as "5h" and "weekly".
	antigravityWindow5h     = 300
	antigravityWindowWeekly = 7 * 24 * 60
	// antigravityGeminiGroup is the group whose windows become the primary and
	// secondary reading; see antigravityQuotaFromJSON.
	antigravityGeminiGroup = "gemini"
)

var antigravityQuotaHTTPClient = &http.Client{Timeout: antigravityQuotaTimeout}

// antigravityCSRFPattern pulls the token out of the language server's command
// line. It is a uuid, so the character class is deliberately narrow: a looser
// match against arbitrary process arguments is a way to send something unintended
// as a credential.
var antigravityCSRFPattern = regexp.MustCompile(`--csrf_token[= ]+([0-9a-fA-F-]{8,64})`)

// antigravityListenPattern matches the loopback listening sockets in lsof output.
var antigravityListenPattern = regexp.MustCompile(`127\.0\.0\.1:(\d{1,5})\b`)

type antigravityQuotaCacheEntry struct {
	FetchedAt time.Time  `json:"fetched_at"`
	Quota     *QuotaInfo `json:"quota"`
}

func antigravityQuotaCachePath() string {
	return filepath.Join(config.AtmDir, "antigravity_quota_cache.json")
}

func readAntigravityQuotaCache(now time.Time, maxAge time.Duration) *QuotaInfo {
	data, err := os.ReadFile(antigravityQuotaCachePath())
	if err != nil {
		return nil
	}
	var entry antigravityQuotaCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return nil
	}
	if entry.Quota == nil || entry.FetchedAt.IsZero() || now.Sub(entry.FetchedAt) > maxAge {
		return nil
	}
	return entry.Quota
}

func writeAntigravityQuotaCache(entry antigravityQuotaCacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(config.AtmDir, 0700); err != nil {
		return
	}
	// 0600: the file holds an account's quota position, and nothing else on the
	// machine needs to read it.
	_ = os.WriteFile(antigravityQuotaCachePath(), data, 0600)
}

// AntigravityQuota returns the latest known Antigravity quota, or nil when there
// is none to be had. Every failure path is nil rather than an error: Antigravity
// not running, a version that moved the endpoint, and a language server that did
// not answer in time all mean the same thing to a caller — no reading right now —
// and `atm quota` has always treated a missing reading as a blank section rather
// than a broken command.
func AntigravityQuota() *QuotaInfo {
	now := time.Now()
	if cached := readAntigravityQuotaCache(now, antigravityQuotaCacheFresh); cached != nil {
		return cached
	}
	info := fetchAntigravityQuota(context.Background())
	if info != nil {
		writeAntigravityQuotaCache(antigravityQuotaCacheEntry{FetchedAt: now, Quota: info})
	}
	return info
}

func fetchAntigravityQuota(ctx context.Context) *QuotaInfo {
	endpoint, ok := findAntigravityLanguageServer(ctx)
	if !ok {
		return nil
	}
	// The server listens on two ports, one HTTPS and one plain HTTP, and their
	// order is not guaranteed across launches. Rather than guess, both are tried:
	// the HTTPS one rejects a plaintext request with a non-JSON 400, so the first
	// port that returns a decodable response is the right one.
	for _, port := range endpoint.ports {
		body, err := postAntigravityRPC(ctx, port, endpoint.csrfToken, antigravityQuotaMethod)
		if err != nil {
			continue
		}
		if info := antigravityQuotaFromJSON(body); info != nil {
			return info
		}
	}
	return nil
}

// antigravityEndpoint is one running language server: the ports it listens on and
// the CSRF token that makes it answer.
type antigravityEndpoint struct {
	ports     []int
	csrfToken string
}

// findAntigravityLanguageServer locates the running language server through ps
// and lsof. macOS offers no way to read another process's arguments or open
// sockets from Go directly, and Antigravity writes no discovery file, so these
// two are the interface.
func findAntigravityLanguageServer(ctx context.Context) (antigravityEndpoint, bool) {
	out, err := runAntigravityCommand(ctx, "ps", "-axww", "-o", "pid=,command=")
	if err != nil {
		return antigravityEndpoint{}, false
	}
	pid, token, ok := parseAntigravityProcess(out)
	if !ok {
		return antigravityEndpoint{}, false
	}
	out, err = runAntigravityCommand(ctx, "lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN")
	if err != nil {
		return antigravityEndpoint{}, false
	}
	ports := parseAntigravityPorts(out)
	if len(ports) == 0 {
		return antigravityEndpoint{}, false
	}
	return antigravityEndpoint{ports: ports, csrfToken: token}, true
}

func runAntigravityCommand(parent context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, antigravityQuotaTimeout)
	defer cancel()
	// Only stdout is captured. lsof warns on stderr about file systems it cannot
	// stat, which is routine and not an error for this lookup.
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseAntigravityProcess finds the standalone language server in ps output and
// returns its pid and CSRF token.
//
// Both markers are required. Antigravity ships the same binary for other roles,
// and the editor may run more than one; --standalone is the hub process that
// serves this RPC, and a line without a token cannot be talked to anyway.
func parseAntigravityProcess(psOutput string) (pid int, csrfToken string, ok bool) {
	for _, line := range strings.Split(psOutput, "\n") {
		if !strings.Contains(line, "language_server") || !strings.Contains(line, "--standalone") {
			continue
		}
		if !strings.Contains(line, "antigravity") && !strings.Contains(line, "Antigravity") {
			continue
		}
		match := antigravityCSRFPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		parsed, err := strconv.Atoi(fields[0])
		if err != nil || parsed <= 0 {
			continue
		}
		return parsed, match[1], true
	}
	return 0, "", false
}

// parseAntigravityPorts collects the loopback ports from lsof output, in the
// order listed and without duplicates.
func parseAntigravityPorts(lsofOutput string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, line := range strings.Split(lsofOutput, "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		for _, match := range antigravityListenPattern.FindAllStringSubmatch(line, -1) {
			port, err := strconv.Atoi(match[1])
			if err != nil || port <= 0 || port > 65535 || seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, port)
		}
	}
	return ports
}

func postAntigravityRPC(ctx context.Context, port int, csrfToken, method string) ([]byte, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, method)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("x-codeium-csrf-token", csrfToken)
	response, err := antigravityQuotaHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity quota: HTTP %d", response.StatusCode)
	}
	body := make([]byte, 0, 4096)
	buffer := make([]byte, 32<<10)
	for len(body) < antigravityQuotaBodyLimit {
		n, err := response.Body.Read(buffer)
		body = append(body, buffer[:n]...)
		if err != nil {
			break
		}
	}
	return body, nil
}

// The subset of RetrieveUserQuotaSummary this reads. Buckets report how much is
// left, not how much was used, and no absolute ceiling is published at all — so a
// percentage is the most ATM can honestly show, and "3.2M of 5M tokens" is not
// available from this upstream at any effort.
type antigravityQuotaPayload struct {
	Response struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string   `json:"bucketId"`
				DisplayName       string   `json:"displayName"`
				Window            string   `json:"window"`
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	} `json:"response"`
}

// antigravityQuotaFromJSON maps the response onto QuotaInfo. Kept separate from
// the fetch so the mapping is testable against a recorded response.
//
// The upstream reports two independent groups — Gemini models, and Claude/GPT —
// each with a five-hour and a weekly window. Only the Gemini group is reported.
// That is a deliberate omission, not an oversight, and the reasons are structural
// rather than about which group matters more:
//
//   - QuotaInfo carries exactly two windowed readings. The remaining field that
//     could hold more, Products, means something specific to its only consumer:
//     the browser draws products as stacked segments *inside* the primary window's
//     bar, scaled so they never exceed it, with the tooltip "占本周额度池 X%".
//     They are a breakdown of one pool. The Claude/GPT group is a separate pool,
//     so putting it there would render a claim that is simply false.
//   - quota_history is keyed on (agent, window_minutes, ts). A second group's
//     weekly window has the same window length as the first and would collide on
//     that key, corrupting the trend for both.
//
// Reporting the group that covers Antigravity's own models — and in practice
// nearly every call — is the honest subset. Showing the second group properly
// needs a quota model with independent pools in it, which is a larger change than
// this. `atm quota` names the scope in its section header so the reading is not
// mistaken for the whole account.
func antigravityQuotaFromJSON(body []byte) *QuotaInfo {
	var payload antigravityQuotaPayload
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	if len(payload.Response.Groups) == 0 {
		return nil
	}
	info := &QuotaInfo{Timestamp: time.Now(), Source: "local"}
	for _, group := range payload.Response.Groups {
		if !strings.Contains(strings.ToLower(group.DisplayName), antigravityGeminiGroup) {
			continue
		}
		for _, bucket := range group.Buckets {
			// Remaining, not used: an absent fraction means "unknown", which is
			// neither a full nor an empty bucket, so it is skipped rather than
			// defaulted to either.
			if bucket.RemainingFraction == nil {
				continue
			}
			used := (1 - *bucket.RemainingFraction) * 100
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}
			weekly := strings.EqualFold(bucket.Window, "weekly")
			windowMinutes := antigravityWindow5h
			if weekly {
				windowMinutes = antigravityWindowWeekly
			}
			limit := &QuotaLimit{
				UsedPercent:   used,
				WindowMinutes: windowMinutes,
				ResetsAt:      antigravityResetEpoch(bucket.ResetTime),
			}
			// Primary is the short window, matching Codex: the tighter limit is
			// the one a person is about to hit.
			if weekly {
				info.Secondary = limit
			} else {
				info.Primary = limit
			}
		}
	}
	if info.Primary == nil && info.Secondary == nil {
		return nil
	}
	return info
}

func antigravityResetEpoch(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}
