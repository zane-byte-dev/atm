package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// realSuffix names the displaced binary. Kept in the same directory on purpose:
// some of these tools are shell dispatchers that locate their own next hop
// relative to $0, and moving one elsewhere would break it.
const realSuffix = "-atm-real"

// shimMarker is how a shim recognises itself, which is what lets `status` tell
// "not installed" apart from "installed and then overwritten by a tool upgrade".
const shimMarker = "atm-guard-shim"

// RealBinPath is where the real binary lives once a shim is installed in front of
// it.
func RealBinPath(binPath string) string { return binPath + realSuffix }

// IsRealBinPath reports whether a path is a displaced binary. `guard approve`
// uses this to refuse to execute anything that did not come through a shim: the
// approvals table is the one place in the database whose contents are later
// handed to exec, so what may be executed is narrowed to the binaries the user
// installed a gate in front of.
func IsRealBinPath(path string) bool {
	return strings.HasSuffix(path, realSuffix)
}

// BinPathFromReal recovers the path a shim occupies from the displaced binary
// beside it.
func BinPathFromReal(realPath string) string {
	return strings.TrimSuffix(realPath, realSuffix)
}

// ShimState is what `guard status` reports about one tool.
type ShimState struct {
	Tool     string `json:"tool"`
	BinPath  string `json:"bin_path"`
	RealPath string `json:"real_path,omitempty"`
	// Installed means a shim of ours currently occupies BinPath.
	Installed bool `json:"installed"`
	// BinExists distinguishes "not gated" from "the path we were told about is gone"
	// — usually a CLI that moved or was uninstalled. Without it a stale recorded
	// path reads as a plain "not enabled", and the only way to find out is to press
	// the button and read an error.
	BinExists bool `json:"bin_exists"`
	// Clobbered means something else occupies BinPath while a displaced binary is
	// still sitting beside it — almost always a tool that upgraded itself over the
	// shim. Reported separately from "not installed" because it is not the same
	// problem and does not have the same fix.
	Clobbered bool `json:"clobbered"`
	// ShadowedBy is an executable of the same name that PATH finds first, which
	// means invocations by bare name never reach this gate at all. A gate that
	// reports itself installed while being bypassed is worse than no gate.
	ShadowedBy string `json:"shadowed_by,omitempty"`
	Rules      int    `json:"rules"`
}

// Healthy reports whether this tool is actually gated.
func (s ShimState) Healthy() bool {
	return s.Installed && !s.Clobbered && s.ShadowedBy == "" && s.Rules > 0
}

// Resolve decides which path to interpose at: the override if given, otherwise
// whatever PATH resolves the tool to, otherwise the configured bin.
//
// Defaulting to PATH matters more than it looks. Several of these tools exist in
// two places on a machine, and installing a gate on the copy PATH does not use
// gates nothing while looking completely successful.
func Resolve(tool, override string) (string, error) {
	if override != "" {
		return filepath.Clean(override), nil
	}
	if configured := strings.TrimSpace(Tools()[tool].Bin); configured != "" {
		return filepath.Clean(configured), nil
	}
	path, err := exec.LookPath(tool)
	if err != nil {
		return "", fmt.Errorf("cannot find %s on PATH; pass --bin with its absolute path", tool)
	}
	return filepath.Clean(path), nil
}

// Status inspects one tool without changing anything.
func Status(tool, binPath string) (ShimState, error) {
	state := ShimState{
		Tool:      tool,
		BinPath:   binPath,
		RealPath:  RealBinPath(binPath),
		Rules:     len(Rules(tool)),
		BinExists: fileExists(binPath),
	}
	realExists := fileExists(state.RealPath)
	shim, err := isOurShim(binPath)
	if err != nil {
		return state, err
	}
	switch {
	case shim:
		state.Installed = true
		// A shim whose displaced binary is gone cannot run anything. Same signal as
		// a clobber: the tool is broken and needs reinstalling.
		state.Clobbered = !realExists
	case realExists:
		state.Clobbered = true
	}
	state.ShadowedBy = shadowingPath(tool, binPath)
	return state, nil
}

// Install puts a shim at binPath and moves the real binary aside.
//
// Re-running it after a tool upgraded itself over the shim treats the file now at
// binPath as the new real binary, replacing the stale displaced copy. The obvious
// alternative — keeping the old one — would quietly downgrade the user's CLI every
// time they repaired the gate.
func Install(tool, binPath, atmPath string) (ShimState, error) {
	state, err := Status(tool, binPath)
	if err != nil {
		return state, err
	}
	if state.Installed && !state.Clobbered {
		return state, nil
	}
	if !fileExists(binPath) {
		return state, fmt.Errorf("%s does not exist", binPath)
	}
	if state.Installed && state.Clobbered {
		// Our shim is in place but the real binary is missing: nothing to displace,
		// and overwriting would lose the tool entirely.
		return state, fmt.Errorf(
			"%s is an ATM shim but %s is missing; restore the real binary to %s first",
			binPath, state.RealPath, state.RealPath)
	}

	realPath := RealBinPath(binPath)
	temp := binPath + ".atm-guard-tmp"
	if err := os.WriteFile(temp, []byte(shimScript(tool, atmPath, realPath)), 0o755); err != nil {
		return state, err
	}
	// Write the shim before displacing anything, so a failure here leaves the tool
	// working rather than leaving the machine with no binary at binPath at all.
	if err := os.Rename(binPath, realPath); err != nil {
		os.Remove(temp)
		return state, err
	}
	if err := os.Rename(temp, binPath); err != nil {
		if restoreErr := os.Rename(realPath, binPath); restoreErr != nil {
			return state, fmt.Errorf(
				"installed nothing and could not restore %s from %s: %w", binPath, realPath, restoreErr)
		}
		os.Remove(temp)
		return state, err
	}
	return Status(tool, binPath)
}

// Uninstall puts the real binary back. Idempotent: a tool that was never gated,
// or whose shim a later upgrade already replaced, is left exactly as it is.
func Uninstall(tool, binPath string) (ShimState, error) {
	state, err := Status(tool, binPath)
	if err != nil {
		return state, err
	}
	if !state.Installed {
		if state.Clobbered {
			return state, fmt.Errorf(
				"%s is not an ATM shim, but %s is still here; move it back by hand if it is the binary you want",
				binPath, state.RealPath)
		}
		return state, nil
	}
	realPath := RealBinPath(binPath)
	if !fileExists(realPath) {
		return state, fmt.Errorf("%s is missing, so there is nothing to restore to %s", realPath, binPath)
	}
	if err := os.Rename(realPath, binPath); err != nil {
		return state, err
	}
	return Status(tool, binPath)
}

// shimScript is deliberately terse. It carries no comment describing what was
// moved where: the redirect target is unavoidably visible, but there is no reason
// to also leave instructions for getting around the gate next to it.
func shimScript(tool, atmPath, realPath string) string {
	return "#!/bin/sh\n# " + shimMarker + "\nexec " + shellQuote(atmPath) +
		" guard exec --tool " + shellQuote(tool) + " -- " + shellQuote(realPath) + " \"$@\"\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// isOurShim reads the head of a file rather than trusting its size or mode: the
// question is whether ATM wrote it, and only its contents answer that.
func isOurShim(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	head := make([]byte, 512)
	read, err := file.Read(head)
	if err != nil && read == 0 {
		return false, nil
	}
	return strings.Contains(string(head[:read]), shimMarker), nil
}

// shadowingPath returns an executable of the same name that PATH would reach
// before binPath, or "" when binPath is what PATH resolves to — or when the tool
// is not on PATH at all, which is the normal case for one that is only ever
// invoked by absolute path.
func shadowingPath(tool, binPath string) string {
	resolved, err := exec.LookPath(tool)
	if err != nil {
		return ""
	}
	resolved = filepath.Clean(resolved)
	if resolved == filepath.Clean(binPath) {
		return ""
	}
	if sameFile(resolved, binPath) {
		return ""
	}
	return resolved
}

func sameFile(left, right string) bool {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false
	}
	return os.SameFile(leftInfo, rightInfo)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
