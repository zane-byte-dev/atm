package work

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
)

// checkExpectedTodo runs only after the Work write lock has been acquired.
// A browser must identify the version it edited; CLI callers remain compatible
// when they do not need an optimistic precondition.
func checkExpectedTodo(call application.Call, todo Todo, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" && call.Actor.Origin == application.OriginWeb {
		return metadataInvalidArgument("expected_etag is required for Web todo changes", "expected_etag", expected)
	}
	if expected != "" && TodoETag(todo) != expected {
		return todoETagConflict(todo, expected)
	}
	return nil
}
