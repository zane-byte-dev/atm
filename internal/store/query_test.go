package store

import (
	"strings"
	"testing"
)

func TestAgentDisplayNameIncludesQoderVariants(t *testing.T) {
	cases := map[string]string{"qoder": "Qoder", "qodercli": "Qoder CLI", "qoderwork": "QoderWork", "grokbuild": "Grok Build"}
	for agent, want := range cases {
		if got := AgentDisplayName(agent); got != want {
			t.Fatalf("AgentDisplayName(%q) = %q, want %q", agent, got, want)
		}
	}
}

// The two copies of this switch disagreed about pi and about case, so both now
// have to come out of the single one.
func TestAgentDisplayNameKnowsPiAndFoldsCase(t *testing.T) {
	for _, agent := range []string{"pi", "Pi", "PI"} {
		if got := AgentDisplayName(agent); got != "Pi" {
			t.Fatalf("AgentDisplayName(%q) = %q, want %q", agent, got, "Pi")
		}
	}
	if got := AgentDisplayName("Claude"); got != "Claude Code" {
		t.Fatalf("AgentDisplayName(\"Claude\") = %q", got)
	}
}

func TestSearchMessageScorePrefersFocusedIntentOverBoilerplate(t *testing.T) {
	focused := SearchHit{Role: "user", Content: "搜索需要优化一下，现在相关性太差了"}
	boilerplate := SearchHit{Role: "assistant", Content: strings.Repeat("平台说明与内部运行细节。", 80) + "搜索"}
	if searchMessageScore(focused, "搜索") <= searchMessageScore(boilerplate, "搜索") {
		t.Fatalf("focused score %f should beat boilerplate %f",
			searchMessageScore(focused, "搜索"), searchMessageScore(boilerplate, "搜索"))
	}
}

func TestSearchMessagesAppliesLimitAfterRelevanceRanking(t *testing.T) {
	db := openTempDB(t)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
			 VALUES (?, ?, 'codex', 'atm', '', ?, ?, '', ?)`,
			[]any{"old-focused", "focused", "2026-01-01T00:00:00Z", int64(100), int64(100)},
		},
		{
			`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
			 VALUES (?, ?, 'codex', 'atm', '', ?, ?, '', ?)`,
			[]any{"new-noisy", "noisy", "2026-02-01T00:00:00Z", int64(200), int64(200)},
		},
		{
			"INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, 0, ?, ?, ?)",
			[]any{"old-focused", "user", "搜索排序", int64(100)},
		},
		{
			"INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, 0, ?, ?, ?)",
			[]any{"new-noisy", "assistant", strings.Repeat("无关背景。", 100) + "搜索", int64(200)},
		},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := SearchMessagesWithOptions(db, "搜索", SearchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if matches.Total != 2 || !matches.Truncated || len(matches.Hits) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
	if matches.Hits[0].ShortID != "focused" {
		t.Fatalf("top hit = %#v", matches.Hits[0])
	}
}
