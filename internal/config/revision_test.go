package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestRevisionPatchSerializesCompetingEditorsAndPreservesUnknownFields(t *testing.T) {
	previous := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), "config.json")
	t.Cleanup(func() { ConfigPath = previous })
	if err := os.WriteFile(ConfigPath, []byte(`{"future":{"keep":true},"owner_name":"Original"}`), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := loadRawConfigAt(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := configRevision(raw)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"One", "Two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- Default.ApplyRevision(expected, SettingsPatch{OwnerName: &name})
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, application.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	current, err := loadRawConfigAt(ConfigPath)
	if err != nil || current["future"] == nil || (current["owner_name"] != "One" && current["owner_name"] != "Two") {
		t.Fatalf("data lost: %+v %v", current, err)
	}
	before, _ := os.ReadFile(ConfigPath)
	bad := -1
	if err := Default.ApplyRevision(expected, SettingsPatch{CollectionIntervalMinutes: &bad}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid patch accepted: %v", err)
	}
	after, _ := os.ReadFile(ConfigPath)
	if string(before) != string(after) {
		t.Fatal("invalid patch modified config")
	}
}
