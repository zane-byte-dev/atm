//go:build !windows

package guard

// execInterpositionSupported reports whether the gate's exec-replace shim works
// on this platform. Diagnostics ask before reporting on shim state: on a
// platform with no interposition there is no gate to be broken.
func execInterpositionSupported() bool { return true }
