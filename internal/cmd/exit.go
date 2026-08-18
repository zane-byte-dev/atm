package cmd

// exitError carries a specific process exit status out of a RunE, for the one
// command that needs more than "worked" and "failed".
//
// Every other command in ATM exits 0 or 1, and that is deliberate — a CLI whose
// exit codes are a vocabulary is a CLI nobody can script against confidently.
// `atm guard exec` is the exception because it stands in front of another
// program: on the approved path it must report that program's own status
// verbatim, and on the refused paths it must be distinguishable from the gated
// tool merely failing.
//
// The message is also already written by the time this is returned. The guard
// writes carefully worded instructions to stderr for a *model* to read, and
// Execute's default handler would staple its own error line after them, so
// Execute deliberately prints nothing for an exitError.
type exitError struct {
	code int
	err  error
}

// The guard's reserved statuses. These are sysexits values, picked so they read
// correctly to a human running the command by hand and cannot be confused with
// the gated tool returning 1 or 2 on its own.
const (
	// guardExitBlocked: ATM could not record the request, so it refused to let the
	// command through. Failing open here would send the message silently, which is
	// the exact harm the gate exists to prevent.
	guardExitBlocked = 70 // EX_SOFTWARE
	// guardExitPending: nobody decided within the wait budget. The request is
	// still live and the user may still approve it.
	guardExitPending = 75 // EX_TEMPFAIL
	// guardExitDenied: the user said no.
	guardExitDenied = 77 // EX_NOPERM
)

func (e exitError) Error() string { return e.err.Error() }

func (e exitError) ExitCode() int { return e.code }

func (e exitError) Unwrap() error { return e.err }
