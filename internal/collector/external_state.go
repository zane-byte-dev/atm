package collector

import (
	"fmt"
	"sort"
	"strings"
)

// externalStateDecision applies connector-verified state before the text model
// gets a chance to create a Todo. It only settles a whole decision unit when
// every fresh message in it is covered and every referenced item is settled.
// Mixed or unannotated chat still reaches the classifier, while an unknown
// lookup fails closed so a transient provider error cannot create stale work.
func externalStateDecision(batch MessageBatch) (Decision, bool, error) {
	if len(batch.Messages) == 0 {
		return Decision{}, false, nil
	}
	settled := []string{}
	allCovered := true
	hasActionable := false
	for _, message := range batch.Messages {
		if !message.ExternalStatesCoverMessage {
			allCovered = false
		}
		if message.ExternalStatesCoverMessage && len(message.ExternalStates) == 0 {
			return Decision{}, false, fmt.Errorf("message %s claims external-state coverage without a state", message.ID)
		}
		for _, state := range message.ExternalStates {
			switch state.Disposition {
			case ExternalDispositionActionable:
				hasActionable = true
			case ExternalDispositionSettled:
				settled = append(settled, externalStateLabel(state))
			case ExternalDispositionUnknown:
				return Decision{}, false, fmt.Errorf("external %s state is unknown for %s",
					state.Kind, state.Reference)
			default:
				return Decision{}, false, fmt.Errorf("unsupported external disposition %q", state.Disposition)
			}
		}
	}
	if hasActionable || !allCovered || len(settled) == 0 {
		return Decision{}, false, nil
	}
	sort.Strings(settled)
	settled = dedupeSortedExternalStateLabels(settled)
	return Decision{Action: "ignore", ItemType: "conversation",
		Reason: "外部事项当前已处理：" + strings.Join(settled, "；"), Confidence: 1}, true, nil
}

func externalStateLabel(state ExternalState) string {
	return strings.TrimSpace(state.Kind) + " " + strings.TrimSpace(state.State) + " " +
		strings.TrimSpace(state.Reference)
}

func dedupeSortedExternalStateLabels(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
