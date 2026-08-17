package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func captureOutput(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	old := *stream
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	*stream = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	*stream = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return string(data)
}

func TestJSONAndErrorOutput(t *testing.T) {
	out := captureOutput(t, &os.Stdout, func() {
		JSON(map[string]string{"status": "ok"})
	})
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("JSON output = %q", out)
	}

	out = captureOutput(t, &os.Stdout, func() {
		Error("boom")
	})
	if !strings.Contains(out, `"success": false`) || !strings.Contains(out, `"error": "boom"`) {
		t.Fatalf("Error output = %q", out)
	}
}

func TestJSONNormalizesNilSlices(t *testing.T) {
	var items []string
	out := captureOutput(t, &os.Stdout, func() {
		JSON(map[string]any{"items": items})
	})
	if !strings.Contains(out, `"items": []`) {
		t.Fatalf("JSON output = %q", out)
	}

	out = captureOutput(t, &os.Stdout, func() {
		JSON(items)
	})
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("JSON output = %q", out)
	}
}

func TestDashesFillsOneCellPerWidth(t *testing.T) {
	cells := Dashes(3, 1, 0)
	if len(cells) != 3 {
		t.Fatalf("Dashes(3, 1, 0) = %v, want 3 cells", cells)
	}
	for i, want := range []string{"---", "-", ""} {
		got, ok := cells[i].(string)
		if !ok || got != want {
			t.Fatalf("cell %d = %v, want %q", i, cells[i], want)
		}
	}
	if cells := Dashes(); len(cells) != 0 {
		t.Fatalf("Dashes() = %v, want no cells", cells)
	}
}

// TestDashesSpreadsIntoRowFormat covers the reason Dashes returns []any: the
// separator has to reach Printf as one argument per column, so a caller can pass
// the header's own format string instead of hand-writing a second one.
func TestDashesSpreadsIntoRowFormat(t *testing.T) {
	out := captureOutput(t, &os.Stdout, func() {
		fmt.Printf("%-4s %2s\n", Dashes(4, 2)...)
	})
	if out != "---- --\n" {
		t.Fatalf("separator row = %q", out)
	}
}

func TestProgressWritesToStderr(t *testing.T) {
	errOut := captureOutput(t, &os.Stderr, func() {
		Progress("synced %d", 3)
	})
	if errOut != "synced 3\n" {
		t.Fatalf("Progress output = %q", errOut)
	}
}
