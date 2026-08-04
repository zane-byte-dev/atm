package parser

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateTextPreservesUTF8(t *testing.T) {
	input := strings.Repeat("中文状态", 80)
	got := truncateText(input, 200)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text is invalid UTF-8: %q", got[len(got)-8:])
	}
	if len([]rune(got)) != 200 {
		t.Fatalf("rune count = %d, want 200", len([]rune(got)))
	}
	if got != string([]rune(input)[:200]) {
		t.Fatal("truncateText returned unexpected content")
	}
}

func TestTruncateTextReturnsShortInputUnchanged(t *testing.T) {
	input := "任务已完成"
	if got := truncateText(input, 200); got != input {
		t.Fatalf("truncateText = %q, want %q", got, input)
	}
}
