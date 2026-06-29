// Package reader recovers the real token counts coding-agent CLIs record in
// their own local session files, so council can upgrade its local estimates to
// reported numbers. Each tool's reader knows one on-disk format and is scoped to
// a working directory — the key that maps a session back to the council pane
// that produced it, even when two instances of the same CLI run at once.
//
// Adding a tool is one self-contained file: implement Reader and Register it
// from a file-level init(). Nothing central needs editing.
package reader

import (
	"sort"
	"time"
)

// Call is one tool-reported unit of usage. The non-token fields (SessionID,
// ProjectPath, Timestamp, UserMessage) are correlation keys.
type Call struct {
	Provider          string
	Model             string
	SessionID         string
	CallID            string
	ProjectPath       string
	UserMessage       string
	Timestamp         time.Time
	InputTokens       int
	OutputTokens      int
	ReasoningTokens   int
	CacheCreate       int
	CacheRead         int
	WebSearchRequests int
	FastInputTokens   int
	FastOutputTokens  int
}

// Reader reads one tool's sessions for a given working directory. Tools that
// record no tokens return nil from ReadForCWD; council does not use "latest
// session" model guesses as a pricing source.
type Reader interface {
	// Name is the tool key (e.g. "claude").
	Name() string
	// ReadForCWD returns the tool's reported calls whose sessions ran in cwd.
	// A missing session store yields no calls and no error.
	ReadForCWD(cwd string) ([]Call, error)
}

// registry maps a tool key to a factory. Populated by each reader file's init().
var registry = map[string]func() Reader{}

// Register binds a tool key to a reader factory. Call it from a file-level
// init(); several keys may map to the same reader (e.g. cursor + cursor-agent).
func Register(tool string, factory func() Reader) { registry[tool] = factory }

// For returns the reader for a tool key, or nil when none is registered.
func For(tool string) Reader {
	if f, ok := registry[tool]; ok {
		return f()
	}
	return nil
}

// All returns one instance of each distinct reader (deduped by Name, ordered by
// tool key for determinism) — the set the reconciliation sweep consults.
func All() []Reader {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	var out []Reader
	for _, k := range keys {
		r := registry[k]()
		if !seen[r.Name()] {
			seen[r.Name()] = true
			out = append(out, r)
		}
	}
	return out
}
