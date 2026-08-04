package cmd

import "testing"

func TestParseBoolValue(t *testing.T) {
	trues := []string{"true", "TRUE", "1", "on", "yes"}
	falses := []string{"false", "FALSE", "0", "off", "no"}
	for _, s := range trues {
		v, err := parseBoolValue(s)
		if err != nil || v != true {
			t.Fatalf("parseBoolValue(%q) = %v, %v", s, v, err)
		}
	}
	for _, s := range falses {
		v, err := parseBoolValue(s)
		if err != nil || v != false {
			t.Fatalf("parseBoolValue(%q) = %v, %v", s, v, err)
		}
	}
	for _, s := range []string{"", "maybe", "2", "enable"} {
		if _, err := parseBoolValue(s); err == nil {
			t.Fatalf("parseBoolValue(%q) should fail", s)
		}
	}
}

func TestParseCollectionConfigValues(t *testing.T) {
	for _, value := range []string{"1", "5", "60"} {
		parsed, err := parsePositiveIntValue(value)
		if err != nil || parsed == nil {
			t.Fatalf("parsePositiveIntValue(%q) = %v, %v", value, parsed, err)
		}
	}
	for _, value := range []string{"", "0", "-1", "five"} {
		if _, err := parsePositiveIntValue(value); err == nil {
			t.Fatalf("parsePositiveIntValue(%q) should fail", value)
		}
	}
	if _, err := parseNonEmptyStringValue("  "); err == nil {
		t.Fatal("blank collection command/path should fail")
	}
	if value, err := parseNonEmptyStringValue("codex"); err != nil || value != "codex" {
		t.Fatalf("parseNonEmptyStringValue = %v, %v", value, err)
	}
}
