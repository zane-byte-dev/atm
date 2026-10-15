package parser

import (
	"context"
	"database/sql"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestSyncSnapshotPinsFingerprintAndParsedContent(t *testing.T) {
	seedQoderDB(t, `INSERT INTO chat_message VALUES ('m','sess-1','user','before','','',110000)`)
	writer, err := sql.Open("sqlite", config.QoderDB+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Ping(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := OpenSyncSnapshot(context.Background(), "qoder", config.QoderDB)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	paths, err := snapshot.Discover()
	if err != nil || len(paths) != 1 {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	a, b, err := snapshot.Version(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("UPDATE chat_message SET content='after!' WHERE id='m'"); err != nil {
		t.Fatal(err)
	}
	parsed, err := snapshot.Parse(paths[0])
	if err != nil || len(parsed.Inputs) != 1 || parsed.Inputs[0].Content != "before" {
		t.Fatalf("mixed snapshots: %+v %v", parsed, err)
	}
	snapshot.Close()
	next, err := OpenSyncSnapshot(context.Background(), "qoder", config.QoderDB)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	c, d, err := next.Version(paths[0])
	if err != nil || a == c && b == d {
		t.Fatalf("concurrent change not detected: %v", err)
	}
	parsed, err = next.Parse(paths[0])
	if err != nil || parsed.Inputs[0].Content != "after!" {
		t.Fatalf("next snapshot lost update: %+v %v", parsed, err)
	}
}
