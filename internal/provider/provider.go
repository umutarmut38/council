// Package provider reads coding agents' own local session files to recover the
// real token counts they recorded, so council can upgrade its local estimates
// to reported numbers. Each Reader knows one tool's on-disk format; tools
// without a Reader keep council's estimate. This is the native half of the
// hybrid design — the long tail of tools is handled by the codeburn adapter.
//
// Readers are scoped to a working directory because council knows each agent's
// cwd/worktree, which is the key that maps a tool's session back to the council
// pane that produced it (see internal/usage correlation).
package provider

import "time"

// Call is one tool-reported unit of usage. Fields beyond tokens (SessionID,
// ProjectPath, Timestamp, UserMessage) are correlation keys: they let council
// attribute the call to a specific pane even when two instances of the same CLI
// run at once.
type Call struct {
	Provider     string
	Model        string
	SessionID    string
	ProjectPath  string
	UserMessage  string
	Timestamp    time.Time
	InputTokens  int
	OutputTokens int
	CacheCreate  int
	CacheRead    int
}

// Reader reads one tool's sessions for a given working directory.
type Reader interface {
	// Name is the tool key (e.g. "claude").
	Name() string
	// ReadForCWD returns the tool's reported calls whose sessions ran in cwd.
	// A missing session store yields no calls and no error.
	ReadForCWD(cwd string) ([]Call, error)
	// LatestModel returns the model most recently used in cwd, for auto-
	// discovery when the user did not pin usage.model. "" when unknown.
	LatestModel(cwd string) (string, error)
}

// Native returns the built-in readers for tools council launches. Only claude
// is implemented so far; codex/cursor/copilot/opencode are stubbed pending
// their (SQLite-backed) ports, and fall back to estimated until then.
func Native() []Reader {
	return []Reader{Claude("")}
}

// ReaderFor returns the native reader for a tool key, or nil when none exists.
func ReaderFor(tool string) Reader {
	switch tool {
	case "claude":
		return Claude("")
	}
	return nil
}
