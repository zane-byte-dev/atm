package background

import (
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestConnectorAuthenticationMuteUsesAllEnabledSources(t *testing.T) {
	sources := []store.CollectionSource{
		{Connector: "chat", Enabled: true, Muted: true},
		{Connector: "chat", Enabled: true, Muted: false},
		{Connector: "chat", Enabled: false, Muted: false},
		{Connector: "other", Enabled: true, Muted: false},
	}
	if connectorNotificationsMuted("chat", sources) {
		t.Fatal("one unmuted enabled sibling must keep the connector login prompt visible")
	}
	sources[1].Muted = true
	if !connectorNotificationsMuted("chat", sources) {
		t.Fatal("all enabled sources are muted")
	}
	if connectorNotificationsMuted("missing", sources) {
		t.Fatal("an unknown connector must fail open so a login outage is not lost")
	}
}
