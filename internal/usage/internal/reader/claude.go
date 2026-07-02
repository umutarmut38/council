package reader

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// claudeReader reads Claude Code transcripts from ~/.claude/projects/<slug>/
// *.jsonl, where <slug> is the working directory with path separators replaced
// by dashes. Each line is an event; assistant events carry message.usage.
type claudeReader struct{ root string }

// Claude returns a reader for Claude Code sessions. An empty root uses
// ~/.claude/projects.
func Claude(root string) Reader {
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".claude", "projects")
	}
	return claudeReader{root: root}
}

func (claudeReader) Name() string { return "claude" }

// claudeSlug mirrors Claude Code's project-dir sanitization: every character
// outside [A-Za-z0-9] becomes a dash. Replacing only slashes and dots (the old
// behavior) never matched paths with underscores, or Windows paths at all
// ("C:\repo" kept its backslashes and colon) — silently disabling claude
// reconciliation there.
func claudeSlug(cwd string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, cwd)
}

type claudeLine struct {
	Type      string `json:"type"`
	CWD       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeScan accumulates one Call per (session, model) — a session that
// switched models mid-way (e.g. /model) must not price all its tokens at the
// LAST model's rate — plus per-session metadata (activity interval, first user
// message) shared by that session's calls.
type claudeScan struct {
	byKey map[string]*Call // sessionID \x00 model
	order []string
	meta  map[string]*claudeMeta
}

type claudeMeta struct {
	first, last time.Time
	userMsg     string
	curModel    string // last real (non-synthetic) model seen in the session
}

func newClaudeScan() *claudeScan {
	return &claudeScan{byKey: map[string]*Call{}, meta: map[string]*claudeMeta{}}
}

func (s *claudeScan) calls() []Call {
	out := make([]Call, 0, len(s.order))
	for _, k := range s.order {
		c := *s.byKey[k]
		if m := s.meta[c.SessionID]; m != nil {
			c.Timestamp, c.LastActivity, c.UserMessage = m.first, m.last, m.userMsg
		}
		out = append(out, c)
	}
	return out
}

func (r claudeReader) ReadForCWD(cwd string) ([]Call, error) {
	dir := filepath.Join(r.root, claudeSlug(cwd))
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	st := newClaudeScan()
	for _, f := range files {
		if err := r.scanFile(f, cwd, st); err != nil {
			return nil, err
		}
	}
	return st.calls(), nil
}

func (r claudeReader) scanFile(path, cwd string, st *claudeScan) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // file raced with deletion since the glob → skip it
		}
		return err // perms / other FS error → surface so reconciliation can note it
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		var l claudeLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue // tolerate non-JSON / partial lines
		}
		// Trust the cwd recorded inside the file over the dir slug.
		if l.CWD != "" && l.CWD != cwd {
			continue
		}
		m := st.meta[l.SessionID]
		if m == nil {
			m = &claudeMeta{}
			st.meta[l.SessionID] = m
		}
		if t, perr := time.Parse(time.RFC3339, l.Timestamp); perr == nil {
			if m.first.IsZero() || t.Before(m.first) {
				m.first = t
			}
			if t.After(m.last) {
				m.last = t
			}
		}
		switch l.Type {
		case "assistant":
			// "<synthetic>" is an internal placeholder, not a model change: its
			// usage stays attributed to the session's current real model.
			if mo := l.Message.Model; mo != "" && mo != "<synthetic>" {
				m.curModel = mo
			}
			u := l.Message.Usage
			if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheCreationInputTokens == 0 && u.CacheReadInputTokens == 0 {
				continue
			}
			key := l.SessionID + "\x00" + m.curModel
			c := st.byKey[key]
			if c == nil {
				c = &Call{Provider: "claude", SessionID: l.SessionID, CallID: l.SessionID + ":" + m.curModel,
					ProjectPath: l.CWD, Model: m.curModel}
				st.byKey[key] = c
				st.order = append(st.order, key)
			}
			c.InputTokens += u.InputTokens
			c.OutputTokens += u.OutputTokens
			c.CacheCreate += u.CacheCreationInputTokens
			c.CacheRead += u.CacheReadInputTokens
		case "user":
			if m.userMsg == "" {
				m.userMsg = firstText(l.Message.Content)
			}
		}
	}
	return sc.Err()
}

// firstText extracts a short preview from a user message's content, which is
// either a JSON string or an array of content blocks.
func firstText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clip(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Text != "" {
				return clip(b.Text)
			}
		}
	}
	return ""
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func init() { Register("claude", func() Reader { return Claude("") }) }
