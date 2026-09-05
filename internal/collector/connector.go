package collector

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Message is the connector-neutral representation ATM archives and classifies.
type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Sender         string `json:"sender"`
	CreatedAt      int64  `json:"created_at"`
	Content        string `json:"content"`
	// ExternalStates are connector-verified snapshots for work referenced by
	// this message. Message content remains untrusted text; these structured
	// states come from the configured connector and let collection avoid filing
	// work that the upstream system already considers settled.
	ExternalStates []ExternalState `json:"external_states,omitempty"`
	// ExternalStatesCoverMessage asserts that those references account for all
	// actionable meaning in this message, so settled states may ignore it whole.
	ExternalStatesCoverMessage bool `json:"external_states_cover_message,omitempty"`
}

const (
	ExternalDispositionActionable = "actionable"
	ExternalDispositionSettled    = "settled"
	ExternalDispositionUnknown    = "unknown"
)

// ExternalState maps a provider-native state onto the small disposition set
// collection needs. Kind and State stay provider-defined so the core remains
// connector-neutral; Disposition alone controls whether a Todo may be filed.
type ExternalState struct {
	Kind        string `json:"kind"`
	Reference   string `json:"reference"`
	State       string `json:"state"`
	Disposition string `json:"disposition"`
	CheckedAt   int64  `json:"checked_at"`
}

// Connector is the minimum capability required by automatic collection. The
// identifier is persisted with every source, run, item, checkpoint and message,
// so it is a stable protocol name rather than a display label.
type Connector interface {
	ID() string
	Fetch(context.Context, store.CollectionSource, int64) ([]Message, int64, error)
}

// SearchConnector can discover source identifiers from names. Connectors that
// only accept explicit identifiers do not need to implement it.
type SearchConnector interface {
	Connector
	Search(context.Context, string, string, int) ([]Candidate, error)
}

// HistoryConnector can read a bounded conversation window without advancing a
// collection checkpoint. Automatic fetch remains available on every Connector.
type HistoryConnector interface {
	Connector
	History(context.Context, store.CollectionSource, HistoryOptions) ([]Message, error)
}

// Registry routes persisted sources to connectors. It deliberately owns no
// global mutable state: tests and embedders can build an isolated registry,
// while DefaultRegistry reflects the current configuration.
type Registry struct {
	connectors map[string]Connector
}

var connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func NewRegistry(connectors ...Connector) (*Registry, error) {
	registry := &Registry{connectors: map[string]Connector{}}
	for _, connector := range connectors {
		if err := registry.Register(connector); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds or replaces one connector.
func (registry *Registry) Register(connector Connector) error {
	if registry == nil {
		return fmt.Errorf("collector registry is nil")
	}
	if connector == nil {
		return fmt.Errorf("collector connector is nil")
	}
	id := strings.ToLower(strings.TrimSpace(connector.ID()))
	if !connectorIDPattern.MatchString(id) {
		return fmt.Errorf("invalid collection connector id: %q", connector.ID())
	}
	if registry.connectors == nil {
		registry.connectors = map[string]Connector{}
	}
	registry.connectors[id] = connector
	return nil
}

func (registry *Registry) Resolve(id string) (Connector, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if registry == nil || registry.connectors[id] == nil {
		return nil, fmt.Errorf("collection connector is not registered: %s", id)
	}
	return registry.connectors[id], nil
}

func (registry *Registry) IDs() []string {
	ids := make([]string, 0)
	if registry != nil {
		ids = make([]string, 0, len(registry.connectors))
		for id := range registry.connectors {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// ConnectorLoginCommand is the connector's declared way back in after its login
// expired, with a leading `~/` expanded. Empty when the connector declares none,
// which is also the answer for every connector ATM ships with.
func ConnectorLoginCommand(id string) string {
	command := strings.TrimSpace(config.CollectionConnectors[id].LoginCommand)
	// Plain concatenation rather than filepath.Join: this is a command line, not a
	// path, and Join would clean the arguments that follow the executable.
	if strings.HasPrefix(command, "~/") {
		return config.Home + command[1:]
	}
	return command
}

// DefaultRegistry contains executable connectors declared in
// ~/.atm/config.json. Integrations can therefore live and ship independently
// from ATM's public core.
func DefaultRegistry() (*Registry, error) {
	registry, err := NewRegistry()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(config.CollectionConnectors))
	for id := range config.CollectionConnectors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		configured := config.CollectionConnectors[id]
		command := strings.TrimSpace(configured.Command)
		if strings.HasPrefix(command, "~/") {
			command = filepath.Join(config.Home, command[2:])
		}
		timeout := time.Duration(configured.TimeoutSeconds) * time.Second
		if err := registry.Register(CommandConnector{
			ConnectorID: id, Command: command, Args: configured.Args, Timeout: timeout,
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
