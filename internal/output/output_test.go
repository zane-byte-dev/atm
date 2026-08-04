package output

import (
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

func TestProgressWritesToStderr(t *testing.T) {
	errOut := captureOutput(t, &os.Stderr, func() {
		Progress("synced %d", 3)
	})
	if errOut != "synced 3\n" {
		t.Fatalf("Progress output = %q", errOut)
	}
}
