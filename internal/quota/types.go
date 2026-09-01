// Package quota owns rate-limit and credit readings and the policy for turning
// them into something a person can act on. Command and IPC adapters choose how
// to render these values; they do not read agent logs, open the quota history,
// or run provider commands.
package quota

// Input is the complete application request. An empty Agent asks for every agent
// ATM can read. Live opts into the one reading that leaves this machine — Grok's
// billing endpoint — so the default stays a local log read.
type Input struct {
	Agent string `json:"agent,omitempty"`
	Live  bool   `json:"live,omitempty"`
}

// Snapshot is one reading of everything ATM can see. Agents is keyed by agent
// name; a nil value means that agent reported nothing, which serializes as null
// and is how the desktop tells "no data" from "not asked for".
type Snapshot struct {
	Agents map[string]*AgentQuota
	// Order is the agents that reported a rate-limit window, in ATM's fixed
	// reporting order. An agent known only through a provider card is not in it:
	// those are rendered as their own section, sorted by name.
	Order []string
	// Warnings are provider failures. They never fail the snapshot: `atm quota`
	// answers from live agent logs and worked before any provider existed, so one
	// broken integration must not take the other readings down with it.
	Warnings []string
}

// AgentQuota is one agent's reading. The JSON tags are the published shape the
// desktop decodes, so a field added here changes that contract on purpose.
type AgentQuota struct {
	// DisplayName is the heading a text view prints. It belongs here rather than
	// in the renderer because the name carries meaning — Antigravity's reading
	// covers only one of the account's two model groups, and a heading that hid
	// that would overstate what was measured.
	DisplayName string `json:"-"`

	Plan          string         `json:"plan,omitempty"`
	Primary       *Window        `json:"primary,omitempty"`
	Secondary     *Window        `json:"secondary,omitempty"`
	Source        string         `json:"source,omitempty"`
	Products      []Product      `json:"products,omitempty"`
	ProviderCards []ProviderCard `json:"provider_cards,omitempty"`
}

// Windows is the agent's rate-limit windows in reporting order, so a renderer
// does not have to know that "primary" and "secondary" are all there ever are.
func (agent *AgentQuota) Windows() []*Window {
	if agent == nil {
		return nil
	}
	windows := make([]*Window, 0, 2)
	for _, window := range []*Window{agent.Primary, agent.Secondary} {
		if window != nil {
			windows = append(windows, window)
		}
	}
	return windows
}

// Window is one rate-limit window.
type Window struct {
	// UsedPercent is what the agent reported, unmodified. It stays raw in the
	// published shape because consumers built their own reset handling on it.
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
	// ResetsIn is present only while the reset is still ahead. An elapsed
	// countdown is not information, so the key disappears rather than reading 0.
	ResetsIn string `json:"resets_in,omitempty"`
	Trend    *Trend `json:"trend,omitempty"`

	// DisplayPercent applies the one policy the raw reading cannot: a window
	// whose reset time has passed has already refilled, so it is empty now even
	// though the log still carries the old percentage. Excluded from JSON to keep
	// used_percent the raw value consumers already interpret themselves.
	DisplayPercent float64 `json:"-"`
	// ResetPending says whether ResetsAt is still ahead, so a renderer decides
	// what to show without reimplementing the comparison against now.
	ResetPending bool `json:"-"`
}

// Product is one product's share of a shared credit pool (Grok bills GrokBuild /
// GrokChat / GrokImagine against the same weekly window).
type Product struct {
	Name        string  `json:"product"`
	UsedPercent float64 `json:"used_percent"`
}
