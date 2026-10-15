package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

// TestOpenReportsSchemaMismatchAsPermanent covers the failure that used to reach
// callers as a bare infrastructure error. Every write path's fallback wrapper
// turned it into a retryable "unavailable: <operation>", so an agent retried a
// binary/database version mismatch that no retry can fix and never saw the
// version numbers that name the actual remedy.
func TestOpenReportsSchemaMismatchAsPermanent(t *testing.T) {
	for _, test := range []struct {
		name    string
		version int
		want    string
	}{
		{name: "newer database", version: SchemaVersion + 1, want: "newer than this atm build"},
		{name: "unsupported older database", version: MinUpgradableVersion - 1, want: "no longer supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTempDB(t)
			if _, err := db.Exec(`UPDATE schema_version SET version = ?`, test.version); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open()
			if reopened != nil {
				reopened.Close()
			}
			if err == nil {
				t.Fatal("Open accepted a mismatched schema")
			}

			var appErr *application.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("Open error is %T, want *application.Error: %v", err, err)
			}
			if appErr.Code != application.CodeFailedPrecondition {
				t.Errorf("code = %q, want %q", appErr.Code, application.CodeFailedPrecondition)
			}
			if appErr.Retryable {
				t.Error("a schema mismatch is marked retryable, so callers will retry forever")
			}
			if !errors.Is(err, application.ErrFailedPrecondition) {
				t.Error("errors.Is does not match ErrFailedPrecondition")
			}
			if !strings.Contains(appErr.Message, test.want) {
				t.Errorf("message %q does not contain %q", appErr.Message, test.want)
			}
			if appErr.Details["database_schema"] != test.version || appErr.Details["build_schema"] != SchemaVersion {
				t.Errorf("details = %#v", appErr.Details)
			}

			// The typed cause stays reachable for callers that want to branch on
			// the versions rather than on the application vocabulary.
			var mismatch *SchemaMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("typed cause is not reachable through the application error: %v", err)
			}
			if mismatch.Database != test.version || mismatch.Build != SchemaVersion {
				t.Errorf("mismatch = %#v", mismatch)
			}
		})
	}
}

// TestSchemaOpenErrorLeavesOtherFailuresAlone keeps the promotion narrow: only a
// schema mismatch becomes a precondition failure. A transient database error must
// still reach callers unchanged so their own retryable classification survives.
func TestSchemaOpenErrorLeavesOtherFailuresAlone(t *testing.T) {
	transient := errors.New("database is locked")
	if got := schemaOpenError(transient); got != transient {
		t.Fatalf("schemaOpenError rewrote an unrelated error: %v", got)
	}

	mismatch := &SchemaMismatchError{Database: 58, Build: 57, Message: "database schema v58 is newer than this atm build (v57): upgrade atm"}
	promoted := schemaOpenError(mismatch)
	if promoted == nil || promoted.Error() == "" {
		t.Fatal("schema mismatch was not promoted")
	}
	if !strings.Contains(promoted.Error(), "v58") || !strings.Contains(promoted.Error(), "v57") {
		t.Errorf("promoted error dropped the version numbers: %q", promoted.Error())
	}
}

func TestSchemaMismatchErrorNilIsSafe(t *testing.T) {
	var mismatch *SchemaMismatchError
	if got := mismatch.Error(); got != "<nil>" {
		t.Fatalf("nil Error() = %q", got)
	}
}
