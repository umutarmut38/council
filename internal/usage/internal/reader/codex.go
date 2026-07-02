package reader

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// codexReader reads Codex CLI sessions from ~/.codex/sessions/YYYY/MM/DD/
// rollout-*.jsonl. The first line (session_meta) carries the cwd; turn_context
// lines carry the model; token_count event_msg lines carry cumulative usage.
type codexReader struct{ root string }

// Codex returns a reader for Codex sessions. Empty root uses ~/.codex/sessions
// (honoring $CODEX_HOME).
func Codex(root string) Reader {
	if root == "" {
		home, _ := os.UserHomeDir()
		base := os.Getenv("CODEX_HOME")
		if base == "" {
			base = filepath.Join(home, ".codex")
		}
		root = filepath.Join(base, "sessions")
	}
	return codexReader{root: root}
}

func (codexReader) Name() string { return "codex" }

type codexLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type      string `json:"type"`
		Cwd       string `json:"cwd"`
		Model     string `json:"model"`
		SessionID string `json:"session_id"`
		Message   string `json:"message"` // event_msg/user_message: the typed prompt
		Info      struct {
			TotalTokenUsage struct {
				InputTokens       int `json:"input_tokens"`
				OutputTokens      int `json:"output_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// codexScanDays bounds the sessions-tree walk. Reconciliation only ever looks
// at the CURRENT run's window, so date directories older than this can never
// match — without the bound every sweep (up to 1/s while agents stream) opens
// every rollout file ever written.
// ponytail: calendar prune with a generous margin; widen if a single council
// run ever spans more than a week.
const codexScanDays = 8

// rollouts lists rollout-*.jsonl files under the sessions tree, skipping
// YYYY/MM/DD date directories older than codexScanDays. Directories that don't
// parse as dates are always walked.
func (r codexReader) rollouts() []string {
	cutoff := time.Now().UTC().AddDate(0, 0, -codexScanDays)
	var out []string
	_ = filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel, rerr := filepath.Rel(r.root, path); rerr == nil {
				parts := strings.Split(rel, string(filepath.Separator))
				if len(parts) == 3 {
					if t, terr := time.Parse("2006/01/02", strings.Join(parts, "/")); terr == nil && t.Before(cutoff) {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// parseSession reads one rollout file. ok is false when its cwd != target. A
// non-nil error means a real read failure (perms, other FS error); a file that
// vanished since the walk (os.ErrNotExist) is skipped without error. It reads
// cumulative total_token_usage from the last token_count event; cached_input
// stays separate so pricing can apply cache-read rates.
func (r codexReader) parseSession(path, cwd string) (Call, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Call{}, false, nil // raced with deletion since the walk → skip
		}
		return Call{}, false, err // perms / other FS error → surface
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)

	c := Call{Provider: "codex"}
	first, matched := true, false
	for sc.Scan() {
		var l codexLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if first {
			first = false
			if l.Type == "session_meta" {
				if l.Payload.Cwd != cwd {
					return Call{}, false, nil // wrong project — skip without reading the rest
				}
				matched = true
				c.ProjectPath = l.Payload.Cwd
				c.SessionID = l.Payload.SessionID
				c.CallID = l.Payload.SessionID
				if t, e := time.Parse(time.RFC3339, l.Timestamp); e == nil {
					c.Timestamp = t
				}
			}
		}
		// Any later line advances LastActivity so the reconcile window sees the
		// session's full activity interval, not just its launch time.
		if t, e := time.Parse(time.RFC3339, l.Timestamp); e == nil && t.After(c.LastActivity) {
			c.LastActivity = t
		}
		if l.Payload.Model != "" {
			c.Model = l.Payload.Model
		}
		// The first user_message event is the council prompt as typed (carrying the
		// personality prefix) — unlike the response_item user turns, which codex
		// pads with synthetic <environment_context> and AGENTS.md instruction blocks.
		if c.UserMessage == "" && l.Type == "event_msg" && l.Payload.Type == "user_message" {
			if t := strings.TrimSpace(l.Payload.Message); t != "" {
				c.UserMessage = clip(t)
			}
		}
		if l.Type == "event_msg" && l.Payload.Type == "token_count" {
			u := l.Payload.Info.TotalTokenUsage
			// codex's total_token_usage.input_tokens is the session's CUMULATIVE
			// input and INCLUDES the cached (context-reuse) portion. Keep the
			// cached part separate (priced at the cache-read rate) and record only
			// the FRESH input in InputTokens — matching claude's convention where
			// input_tokens excludes cache. Otherwise the re-sent context is
			// double-counted: once in the Input column at the full rate and again
			// as CacheRead, badly inflating the reported Input (the 39.9k bug).
			c.OutputTokens = u.OutputTokens // cumulative — last one wins
			// cached is a subset of the inclusive input; clamp into [0, input] so a
			// malformed record can't leave fresh+cache above the reported input and
			// overcharge (fresh = input - cached stays >= 0, fresh+cache == input).
			cached := u.CachedInputTokens
			if cached < 0 {
				cached = 0
			}
			if cached > u.InputTokens {
				cached = u.InputTokens
			}
			c.CacheRead = cached
			c.InputTokens = u.InputTokens - cached
		}
	}
	if !matched {
		return Call{}, false, nil
	}
	return c, true, nil
}

func (r codexReader) ReadForCWD(cwd string) ([]Call, error) {
	var calls []Call
	for _, f := range r.rollouts() {
		c, ok, err := r.parseSession(f, cwd)
		if err != nil {
			return nil, err
		}
		if ok {
			calls = append(calls, c)
		}
	}
	return calls, nil
}

func init() { Register("codex", func() Reader { return Codex("") }) }
