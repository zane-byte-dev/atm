// Package diagnose collects the facts needed to debug an ATM install and writes
// them to a file a person can attach to a bug report. Command adapters render the
// summary and choose where the bundle goes; the redaction rules, the shape of the
// bundle, and the refusal to overwrite belong here.
package diagnose

import (
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	syncapp "github.com/zane-byte-dev/atm/internal/sync"
)

// Input is the complete report request. It has no parameters: a support bundle
// that could be aimed would need the reporter to already know where the problem
// is, which is the thing they are asking for help with.
type Input struct{}

// BundleInput asks for the report as a file. An empty Path takes the default
// timestamped name in the working directory.
type BundleInput struct {
	Path string `json:"path,omitempty"`
}

// BundleResult is where the bundle went and how big it is.
type BundleResult struct {
	Path  string `json:"bundle"`
	Bytes int    `json:"bytes"`
}

type ATM struct {
	Version string `json:"version"`
	// SchemaVersion is what this build expects; DatabaseSchemaVersion is what the
	// file on disk actually holds. A mismatch between them is the whole reason
	// both are reported.
	SchemaVersion         int    `json:"schema_version"`
	DatabaseSchemaVersion int    `json:"database_schema_version"`
	DatabasePath          string `json:"database_path"`
	DatabaseExists        bool   `json:"database_exists"`
	DatabaseBytes         int64  `json:"database_bytes"`
	DatabaseError         string `json:"database_error,omitempty"`
	DataDir               string `json:"data_dir"`
	ConfigExists          bool   `json:"config_exists"`
}

type Platform struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
	CPUs      int    `json:"cpus"`
}

// DataEntry describes one top-level entry under ~/.atm by shape only. Names are
// never recursed into: a knowledge file's name is its title, and a support bundle
// has no business carrying those.
type DataEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Bytes   int64  `json:"bytes,omitempty"`
	Entries int    `json:"entries,omitempty"`
}

// Log is the tail of one log file. Without this the bundle could only describe
// the present, so an intermittent fault — failing once a day, fine otherwise —
// looked identical to no fault at all.
type Log struct {
	Path   string   `json:"path"`
	Exists bool     `json:"exists"`
	Lines  []string `json:"lines"`
	// Truncated says the tail hit its cap, so what is here is the recent end of a
	// longer history rather than all of it.
	Truncated bool `json:"truncated"`
}

// Report is the whole bundle. Redaction is part of the payload on purpose: a
// reader has to be able to tell what was removed from what they are looking at.
type Report struct {
	GeneratedAt string               `json:"generated_at"`
	ATM         ATM                  `json:"atm"`
	Platform    Platform             `json:"platform"`
	Sync        syncapp.StatusReport `json:"sync"`
	DataDir     []DataEntry          `json:"data_dir"`
	Doctor      doctorapp.Report     `json:"doctor"`
	Logs        map[string]Log       `json:"logs"`
	Redaction   []string             `json:"redaction"`
}
