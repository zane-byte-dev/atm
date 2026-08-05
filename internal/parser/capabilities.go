package parser

// Capabilities says what a parser is supposed to be able to extract, so
// "extracted nothing" can be told apart from "there was never anything to
// extract". Without this distinction, an agent whose upstream simply does not
// report tokens looks identical to one whose format changed under us — and a
// check that fires on a permanent, known limitation is noise that trains people
// to ignore it.
//
// This describes intent, not observation: it is the contract each parser in this
// package is written against. When a parser starts extracting something new, its
// entry moves with it.
type Capabilities struct {
	// Messages means the transcript carries conversation turns this parser reads.
	Messages bool
	// Usage means the upstream client records token accounting ATM can attribute
	// to a model request.
	Usage bool
}

// capabilities is keyed by Agent.Name(). An agent absent from this map is
// assumed to provide everything, which is the safer default: a new parser that
// silently extracts nothing should be reported, not excused.
var capabilities = map[string]Capabilities{
	"claude": {Messages: true, Usage: true},
	"codex":  {Messages: true, Usage: true},
	"pi":     {Messages: true, Usage: true},
	// Copilot's workspace storage holds the conversation and tool calls but no
	// token or cost detail — see the known limitations in README. Reporting its
	// missing usage as a problem would be reporting the upstream's design.
	"copilot":   {Messages: true, Usage: false},
	"qoder":     {Messages: true, Usage: true},
	"qodercli":  {Messages: true, Usage: true},
	"qoderwork": {Messages: true, Usage: true},
	"grokbuild": {Messages: true, Usage: true},
}

// CapabilitiesFor reports what the named parser is expected to extract.
func CapabilitiesFor(agent string) Capabilities {
	if declared, ok := capabilities[agent]; ok {
		return declared
	}
	return Capabilities{Messages: true, Usage: true}
}
