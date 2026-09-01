package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

const (
	sessionBindingStateUnbound           = "unbound"
	sessionBindingStateBound             = "bound"
	sessionBindingStateTodoMissing       = "todo_missing"
	sessionBindingStateTodoNotInProgress = "todo_not_in_progress"
)

type sessionBindingContext struct {
	State             string                   `json:"state"`
	Binding           store.TodoSessionBinding `json:"binding"`
	Todo              *compactTodoContext      `json:"todo,omitempty"`
	Observed          bool                     `json:"observed"`
	ObservedSessionID string                   `json:"observed_session_id,omitempty"`
}

func loadSessionBindingContexts(sessions []parser.Session) ([]sessionBindingContext, map[int]int, error) {
	bindings, err := store.ListActiveTodoSessionBindings()
	if err != nil {
		return nil, nil, fmt.Errorf("load active session bindings: %w", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return nil, nil, fmt.Errorf("load todos for session bindings: %w", err)
	}
	matches := matchBindingContextsToSessions(bindings, sessions)
	return buildSessionBindingContexts(bindings, todos, sessions, matches), matches, nil
}

// buildSessionBindingContexts takes the match map rather than deriving it: its
// caller needs the same map, and computing it in both places walked every
// binding against every live session twice per command.
func buildSessionBindingContexts(bindings []store.TodoSessionBinding, todos *store.TodoFile, sessions []parser.Session, matches map[int]int) []sessionBindingContext {
	base := workapp.BuildBindingContexts(bindings, todos)
	contexts := make([]sessionBindingContext, 0, len(base))
	for bindingIndex, item := range base {
		context := sessionBindingContext{State: item.State, Binding: item.Binding, Todo: item.Todo}
		if sessionIndex, ok := matches[bindingIndex]; ok {
			context.Observed = true
			context.ObservedSessionID = sessions[sessionIndex].SessionID
		}
		contexts = append(contexts, context)
	}
	return contexts
}

// matchBindingContextsToSessions returns binding-index -> live-session-index.
// Exact IDs win; prefix matching is allowed only for IDs of at least 8
// characters because some clients expose only a stable short session ID.
func matchBindingContextsToSessions(bindings []store.TodoSessionBinding, sessions []parser.Session) map[int]int {
	result := map[int]int{}
	usedSessions := map[int]bool{}
	for bindingIndex, binding := range bindings {
		for sessionIndex, session := range sessions {
			if usedSessions[sessionIndex] || binding.SessionID != session.SessionID {
				continue
			}
			result[bindingIndex] = sessionIndex
			usedSessions[sessionIndex] = true
			break
		}
	}
	for bindingIndex, binding := range bindings {
		if _, matched := result[bindingIndex]; matched {
			continue
		}
		for sessionIndex, session := range sessions {
			if usedSessions[sessionIndex] || !sessionIDsShareStableFragment(binding.SessionID, session.SessionID) {
				continue
			}
			result[bindingIndex] = sessionIndex
			usedSessions[sessionIndex] = true
			break
		}
	}
	return result
}

func sessionIDsShareStableFragment(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if len(left) < 8 || len(right) < 8 {
		return false
	}
	return strings.Contains(left, right) || strings.Contains(right, left)
}

func currentSessionBindingContext(sessionID string) (*sessionBindingContext, error) {
	result, err := workapp.Default.Current(
		context.Background(), sessionBindingCLICall("current-context", sessionID), workapp.CurrentInput{},
	)
	if err != nil {
		return nil, err
	}
	if result.Context == nil {
		return nil, nil
	}
	return &sessionBindingContext{
		State: result.Context.State, Binding: result.Context.Binding, Todo: result.Context.Todo,
	}, nil
}
