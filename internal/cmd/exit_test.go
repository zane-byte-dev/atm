package cmd

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitErrorCarriesItsStatusAndStaysUnwrappable(t *testing.T) {
	underlying := errors.New("denied by user")
	coded := exitError{code: 77, err: underlying}

	var found exitError
	if !errors.As(fmt.Errorf("wrapped: %w", coded), &found) {
		t.Fatal("errors.As did not find exitError through a wrap; Execute would exit 1")
	}
	if found.ExitCode() != 77 {
		t.Fatalf("exit code = %d, want 77", found.ExitCode())
	}
	if !errors.Is(coded, underlying) {
		t.Fatal("exitError does not unwrap to its cause")
	}
	if coded.Error() != "denied by user" {
		t.Fatalf("message = %q, want the cause's own message", coded.Error())
	}
}

// The reserved codes are sysexits values chosen so they cannot be confused with
// a gated tool's own 1 or 2.
func TestReservedGuardExitCodesAreDistinct(t *testing.T) {
	codes := map[string]int{
		"denied":  guardExitDenied,
		"pending": guardExitPending,
		"blocked": guardExitBlocked,
	}
	seen := map[int]string{}
	for name, code := range codes {
		if code == 0 || code == 1 || code == 2 {
			t.Fatalf("%s uses %d, which a gated tool can return itself", name, code)
		}
		if other, ok := seen[code]; ok {
			t.Fatalf("%s and %s share exit code %d", name, other, code)
		}
		seen[code] = name
	}
}
