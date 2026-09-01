package config

import (
	"os"
	"strings"
)

// ExecutablePath resolves the absolute path of the running atm binary, for
// baking into another program's configuration — an agent's hooks document, a
// Guard shim. Hooks and shims run with an unpredictable PATH, so a bare "atm"
// is not good enough.
//
// A symlink is followed one level because the install script puts atm on PATH
// that way: recording the link would make the config break the moment the link
// is repointed or removed, while the target keeps working. A relative link
// target is not resolvable against the wrong base, so the link itself is kept.
func ExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := os.Readlink(path)
	if err != nil {
		return path, nil
	}
	if strings.HasPrefix(resolved, "/") {
		return resolved, nil
	}
	return path, nil
}
