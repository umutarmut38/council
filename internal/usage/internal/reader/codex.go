package reader

import (
	"bufio"
	"encoding/json"
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

// rollouts lists every rollout-*.jsonl under the sessions tree.
func (r codexReader) rollouts() []string {
	var out []string
	_ = filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// parseSession reads one rollout file. ok is false when its cwd != target.
// parseSession reads cumulative total_token_usage from the last token_count
// event; cached_input stays separate so pricing can apply cache-read rates.
func (r codexReader) parseSession(path, cwd string) (Call, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Call{}, false
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
					return Call{}, false // wrong project — skip without reading the rest
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
			c.InputTokens = u.InputTokens // cumulative — last one wins
			c.OutputTokens = u.OutputTokens
			c.CacheRead = u.CachedInputTokens
		}
	}
	if !matched {
		return Call{}, false
	}
	return c, true
}

func (r codexReader) ReadForCWD(cwd string) ([]Call, error) {
	var calls []Call
	for _, f := range r.rollouts() {
		if c, ok := r.parseSession(f, cwd); ok {
			calls = append(calls, c)
		}
	}
	return calls, nil
}

func init() { Register("codex", func() Reader { return Codex("") }) }
