//go:build windows

package guard

// Windows has no exec-replace and the installed shim is a POSIX shell script, so
// the gate refuses to operate there rather than half-working. Nothing about shim
// state is worth diagnosing on this platform.
func execInterpositionSupported() bool { return false }
