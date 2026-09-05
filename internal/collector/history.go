package collector

import (
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

// HistoryOptions bounds a read-only look at a conversation. A zero Since means
// "the most recent Limit messages", which is what someone catching up wants.
type HistoryOptions struct {
	Since time.Time
	Limit int
}

// CollectionMessagesFor prepares fetched messages for the archive. The source
// name travels with each row: a conversation read by name was never added as a
// source, so there is no row to join for its label later.
func CollectionMessagesFor(source store.CollectionSource, messages []Message) []store.CollectionMessage {
	stored := make([]store.CollectionMessage, 0, len(messages))
	for _, message := range messages {
		conversation := message.ConversationID
		if conversation == "" {
			conversation = source.ExternalID
		}
		stored = append(stored, store.CollectionMessage{
			Connector: source.Connector, ConversationID: conversation,
			MessageID: message.ID, SourceID: source.ID, ConversationName: source.Name,
			Sender: message.Sender, CreatedAt: message.CreatedAt, Content: message.Content,
		})
	}
	return stored
}
