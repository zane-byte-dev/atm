package parser

import (
	"strconv"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func firstTimestamp(keys []string, maps ...map[string]any) (time.Time, bool) {
	for _, m := range maps {
		if m == nil {
			continue
		}
		for _, key := range keys {
			if v, ok := m[key]; ok {
				if ts, ok := parseTimestampValue(v); ok {
					return ts, true
				}
			}
		}
	}
	return time.Time{}, false
}

func parseTimestampValue(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		return parseTimestampString(t)
	case float64:
		return config.TsToCST(t), true
	case int64:
		return time.Unix(t, 0).In(config.Loc), true
	case int:
		return time.Unix(int64(t), 0).In(config.Loc), true
	default:
		return time.Time{}, false
	}
}

func parseTimestampString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return config.TsToCST(n), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.In(config.Loc), true
		}
	}
	return time.Time{}, false
}
