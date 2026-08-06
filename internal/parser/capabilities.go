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
	// token or cost detail — see the known limitations in DESIGN.md. Reporting
	// its missing usage as a problem would be reporting the upstream's design.
	"copilot": {Messages: true, Usage: false},
	// Qoder encrypts the conversation. chat_message.content holds base64 of a
	// high-entropy payload — 7.96 bits per byte, no compression header, not
	// decodable — for every user and assistant row on a live database, and the
	// parser skips it rather than index ciphertext as prose. Token accounting is
	// in a separate plaintext column and still comes through, as does
	// session_title, so sessions and spend are reported and only the message text
	// is missing. Same reasoning as qodercli and qoderwork: the extraction stays,
	// the claim does not.
	"qoder": {Messages: false, Usage: true},
	// Qoder CLI transcripts carry no token accounting at all: across every
	// session on a real install, no key path under message.* exists beyond
	// content and role, and nothing anywhere names tokens, usage or cost. The
	// parser's extraction is left in place — if a later version starts writing
	// it, this flips back to true and the numbers appear without further work.
	"qodercli": {Messages: true, Usage: false},
	// QoderWork writes inputTokens and outputTokens, and both are always zero;
	// totalCostUsd likewise. It is not that the fields are missing — durationMs
	// and contextUsageRatio on the same messages hold real values — the upstream
	// simply does not fill the token ones. Same reasoning as qodercli: the
	// extraction stays, the claim does not.
	"qoderwork": {Messages: true, Usage: false},
	"grokbuild": {Messages: true, Usage: true},
}

// CapabilitiesFor reports what the named parser is expected to extract.
func CapabilitiesFor(agent string) Capabilities {
	if declared, ok := capabilities[agent]; ok {
		return declared
	}
	return Capabilities{Messages: true, Usage: true}
}
