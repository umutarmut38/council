package reader

import (
	"bufio"
	"encoding/json"
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

// claudeSlug mirrors Claude Code's project-dir sanitization (path separators and
// dots become dashes).
func claudeSlug(cwd string) string {
	s := strings.ReplaceAll(cwd, "/", "-")
	return strings.ReplaceAll(s, ".", "-")
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

func (r claudeReader) ReadForCWD(cwd string) ([]Call, error) {
	dir := filepath.Join(r.root, claudeSlug(cwd))
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	bySession := map[string]*Call{}
	order := []string{}
	for _, f := range files {
		if err := r.scanFile(f, cwd, bySession, &order); err != nil {
			return nil, err
		}
	}
	out := make([]Call, 0, len(order))
	for _, id := range order {
		out = append(out, *bySession[id])
	}
	return out, nil
}

func (r claudeReader) scanFile(path, cwd string, bySession map[string]*Call, order *[]string) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
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
		c := bySession[l.SessionID]
		if c == nil {
			c = &Call{Provider: "claude", SessionID: l.SessionID, CallID: l.SessionID, ProjectPath: l.CWD}
			bySession[l.SessionID] = c
			*order = append(*order, l.SessionID)
		}
		if t, perr := time.Parse(time.RFC3339, l.Timestamp); perr == nil {
			if c.Timestamp.IsZero() || t.Before(c.Timestamp) {
				c.Timestamp = t
			}
		}
		switch l.Type {
		case "assistant":
			u := l.Message.Usage
			c.InputTokens += u.InputTokens
			c.OutputTokens += u.OutputTokens
			c.CacheCreate += u.CacheCreationInputTokens
			c.CacheRead += u.CacheReadInputTokens
			if m := l.Message.Model; m != "" && m != "<synthetic>" {
				c.Model = m
			}
		case "user":
			if c.UserMessage == "" {
				c.UserMessage = firstText(l.Message.Content)
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
