package parser

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// antigravityQuotaSample is a real RetrieveUserQuotaSummary response, captured
// from a running language server. Reformatted for readability and nothing else:
// the point of testing against it is that the field names and nesting are the
// upstream's, not a guess at them.
const antigravityQuotaSample = `{
  "response": {
    "groups": [
      {
        "displayName": "Gemini Models",
        "description": "Models within this group: Gemini Flash, Gemini Pro",
        "buckets": [
          {
            "bucketId": "gemini-weekly",
            "displayName": "Weekly Limit Remaining",
            "description": "You have used some of your weekly limit, it will fully refresh in 3 days.",
            "window": "weekly",
            "remainingFraction": 0.99838,
            "resetTime": "2026-08-20T06:44:35Z"
          },
          {
            "bucketId": "gemini-5h",
            "displayName": "Five Hour Limit Remaining",
            "window": "5h",
            "remainingFraction": 1,
            "resetTime": "2026-08-17T11:03:49Z"
          }
        ]
      },
      {
        "displayName": "Claude and GPT models",
        "description": "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
        "buckets": [
          {
            "bucketId": "3p-weekly",
            "displayName": "Weekly Limit Remaining",
            "window": "weekly",
            "remainingFraction": 1,
            "resetTime": "2026-08-24T06:03:49Z"
          },
          {
            "bucketId": "3p-5h",
            "displayName": "Five Hour Limit Remaining",
            "window": "5h",
            "remainingFraction": 1,
            "resetTime": "2026-08-17T11:03:49Z"
          }
        ]
      }
    ]
  }
}`

func TestAntigravityQuotaFromJSON(t *testing.T) {
	got := antigravityQuotaFromJSON([]byte(antigravityQuotaSample))
	if got == nil {
		t.Fatal("antigravityQuotaFromJSON returned nil")
	}
	if got.Primary == nil || got.Secondary == nil {
		t.Fatalf("primary=%v secondary=%v", got.Primary, got.Secondary)
	}
	// Primary is the short window, matching Codex.
	if got.Primary.WindowMinutes != 300 {
		t.Fatalf("primary window = %d, want 300", got.Primary.WindowMinutes)
	}
	if got.Secondary.WindowMinutes != 7*24*60 {
		t.Fatalf("secondary window = %d, want %d", got.Secondary.WindowMinutes, 7*24*60)
	}
	// The upstream reports what is left; ATM reports what is used.
	if got.Primary.UsedPercent != 0 {
		t.Fatalf("primary used = %v, want 0 from remainingFraction 1", got.Primary.UsedPercent)
	}
	if math.Abs(got.Secondary.UsedPercent-0.162) > 1e-6 {
		t.Fatalf("secondary used = %v, want ~0.162 from remainingFraction 0.99838", got.Secondary.UsedPercent)
	}
	wantReset := time.Date(2026, 8, 20, 6, 44, 35, 0, time.UTC).Unix()
	if got.Secondary.ResetsAt != wantReset {
		t.Fatalf("secondary reset = %d, want %d", got.Secondary.ResetsAt, wantReset)
	}
	if got.Source != "local" {
		t.Fatalf("source = %q", got.Source)
	}
	// The Claude/GPT group is deliberately dropped rather than squeezed into
	// Products: the App renders products as segments of the primary window's own
	// bar, so a separate pool there would draw a false claim. See
	// antigravityQuotaFromJSON.
	if len(got.Products) != 0 {
		t.Fatalf("products = %#v; the second group must not be reported here", got.Products)
	}
}

func TestAntigravityQuotaFromJSONRejectsUnusable(t *testing.T) {
	cases := map[string]string{
		"not json":          "Client sent an HTTP request to an HTTPS server.",
		"connect error":     `{"code":"unauthenticated","message":"missing CSRF token"}`,
		"empty response":    `{}`,
		"no groups":         `{"response":{"groups":[]}}`,
		"only other groups": `{"response":{"groups":[{"displayName":"Claude and GPT models","buckets":[{"window":"weekly","remainingFraction":1}]}]}}`,
		// A bucket with no fraction is "unknown", not "full": it must not become a
		// 0%-used reading.
		"gemini without fractions": `{"response":{"groups":[{"displayName":"Gemini Models","buckets":[{"window":"weekly"},{"window":"5h"}]}]}}`,
	}
	for name, body := range cases {
		if got := antigravityQuotaFromJSON([]byte(body)); got != nil {
			t.Errorf("%s: expected nil, got %#v", name, got)
		}
	}
}

// A fraction outside 0..1 would otherwise render as a negative or >100% bar.
func TestAntigravityQuotaClampsFraction(t *testing.T) {
	body := `{"response":{"groups":[{"displayName":"Gemini Models","buckets":[
		{"window":"5h","remainingFraction":1.5},
		{"window":"weekly","remainingFraction":-0.2}]}]}}`
	got := antigravityQuotaFromJSON([]byte(body))
	if got == nil {
		t.Fatal("returned nil")
	}
	if got.Primary.UsedPercent != 0 {
		t.Fatalf("primary used = %v, want clamped to 0", got.Primary.UsedPercent)
	}
	if got.Secondary.UsedPercent != 100 {
		t.Fatalf("secondary used = %v, want clamped to 100", got.Secondary.UsedPercent)
	}
}

func TestParseAntigravityProcess(t *testing.T) {
	// Real `ps -axww -o pid=,command=` shape: the helper processes and the editor
	// itself appear alongside the language server and must not be picked.
	psOutput := `  3372 /Applications/Antigravity.app/Contents/MacOS/Antigravity
  3431 /Applications/Antigravity.app/Contents/Resources/bin/language_server --standalone --override_ide_name antigravity --subclient_type hub --override_ide_version 2.8.1 --https_server_port 0 --csrf_token 36572fde-c537-4a1f-a17c-8da9bcc1d875 --app_data_dir antigravity
  3433 /Applications/Antigravity.app/Contents/Frameworks/Antigravity Helper (Renderer).app/Contents/MacOS/Antigravity Helper --type=renderer
`
	pid, token, ok := parseAntigravityProcess(psOutput)
	if !ok {
		t.Fatal("parseAntigravityProcess found nothing")
	}
	if pid != 3431 {
		t.Fatalf("pid = %d, want 3431", pid)
	}
	if token != "36572fde-c537-4a1f-a17c-8da9bcc1d875" {
		t.Fatalf("token = %q", token)
	}
}

func TestParseAntigravityProcessRejects(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		"no language server": `  3372 /Applications/Antigravity.app/Contents/MacOS/Antigravity
`,
		// A different product's language server must not be mistaken for this one.
		"other vendor": `  4001 /Applications/Windsurf.app/Contents/Resources/bin/language_server --standalone --csrf_token aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
`,
		// Without a token there is nothing to authenticate with.
		"no token": `  3431 /Applications/Antigravity.app/Contents/Resources/bin/language_server --standalone --override_ide_name antigravity
`,
		"unparsable pid": `  xxxx /Applications/Antigravity.app/Contents/Resources/bin/language_server --standalone --override_ide_name antigravity --csrf_token 36572fde-c537-4a1f-a17c-8da9bcc1d875
`,
	}
	for name, psOutput := range cases {
		if _, _, ok := parseAntigravityProcess(psOutput); ok {
			t.Errorf("%s: expected no match", name)
		}
	}
}

func TestParseAntigravityPorts(t *testing.T) {
	lsofOutput := `COMMAND     PID USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
language_  3431   mj    7u  IPv4 0xc605254d49895394      0t0  TCP 127.0.0.1:50259 (LISTEN)
language_  3431   mj    8u  IPv4 0xaab1dc329b598494      0t0  TCP 127.0.0.1:50260 (LISTEN)
language_  3431   mj    9u  IPv4 0xaab1dc329b598495      0t0  TCP 127.0.0.1:50260 (LISTEN)
language_  3431   mj   10u  IPv4 0xaab1dc329b598496      0t0  TCP 127.0.0.1:50261->127.0.0.1:1234 (ESTABLISHED)
`
	got := parseAntigravityPorts(lsofOutput)
	if len(got) != 2 || got[0] != 50259 || got[1] != 50260 {
		t.Fatalf("ports = %v, want [50259 50260] in order and deduplicated", got)
	}
	if len(parseAntigravityPorts("")) != 0 {
		t.Fatal("empty lsof output produced ports")
	}
}

func TestAntigravityQuotaCacheRoundTrip(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	now := time.Now()
	if got := readAntigravityQuotaCache(now, antigravityQuotaCacheFresh); got != nil {
		t.Fatalf("empty cache returned %#v", got)
	}

	want := &QuotaInfo{
		Primary:   &QuotaLimit{UsedPercent: 12.5, WindowMinutes: 300, ResetsAt: 1786967991},
		Secondary: &QuotaLimit{UsedPercent: 0.162, WindowMinutes: 10080, ResetsAt: 1787208275},
		Source:    "local",
	}
	writeAntigravityQuotaCache(antigravityQuotaCacheEntry{FetchedAt: now, Quota: want})

	got := readAntigravityQuotaCache(now.Add(time.Minute), antigravityQuotaCacheFresh)
	if got == nil {
		t.Fatal("fresh cache returned nil")
	}
	if got.Primary.UsedPercent != want.Primary.UsedPercent ||
		got.Secondary.WindowMinutes != want.Secondary.WindowMinutes ||
		got.Secondary.ResetsAt != want.Secondary.ResetsAt {
		t.Fatalf("round trip = %#v", got)
	}
	// Past the freshness window the reading is re-fetched rather than reused: a
	// quota position goes stale on its own.
	if got := readAntigravityQuotaCache(now.Add(antigravityQuotaCacheFresh+time.Second), antigravityQuotaCacheFresh); got != nil {
		t.Fatalf("stale cache returned %#v", got)
	}

	// The file holds an account's quota position and must not be world-readable.
	info, err := os.Stat(filepath.Join(config.AtmDir, "antigravity_quota_cache.json"))
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("cache mode = %v, want 0600", perm)
	}

	// A hand-corrupted cache degrades to "no reading", not to a panic.
	if err := os.WriteFile(filepath.Join(config.AtmDir, "antigravity_quota_cache.json"), []byte("{not json"), 0600); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}
	if got := readAntigravityQuotaCache(now, antigravityQuotaCacheFresh); got != nil {
		t.Fatalf("corrupt cache returned %#v", got)
	}

	// An entry with no quota in it is not a reading either.
	blank, _ := json.Marshal(antigravityQuotaCacheEntry{FetchedAt: now})
	if err := os.WriteFile(filepath.Join(config.AtmDir, "antigravity_quota_cache.json"), blank, 0600); err != nil {
		t.Fatalf("write blank cache: %v", err)
	}
	if got := readAntigravityQuotaCache(now, antigravityQuotaCacheFresh); got != nil {
		t.Fatalf("blank cache returned %#v", got)
	}
}
