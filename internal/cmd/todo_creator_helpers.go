package cmd

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"
)

// todoCreatorFromEnvironment answers "who is filing this todo" for commands
// outside the Work binding adapter.
func todoCreatorFromEnvironment() string {
	if agent := normalizeBindingAgent(""); agent != "" {
		return agent
	}
	return store.TodoCreatorMe
}

func resolveTodoCreator(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return store.NormalizeTodoCreator(flag)
	}
	return todoCreatorFromEnvironment(), nil
}
