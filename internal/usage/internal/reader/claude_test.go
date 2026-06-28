package reader

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSession lays down a Claude Code project dir + jsonl for cwd under root.
func writeSession(t *testing.T, root, cwd, file, content string) {
	t.Helper()
	dir := filepath.Join(root, claudeSlug(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeReaderAggregatesSession(t *testing.T) {
	root := t.TempDir()
	cwd := "/test/proj"
	writeSession(t, root, cwd, "s1.jsonl", `{"type":"user","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:00Z","message":{"content":"do the thing"}}
{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:05Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":40,"cache_read_input_tokens":7}}}
{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:09Z","message":{"model":"<synthetic>","usage":{"input_tokens":50,"output_tokens":10}}}
`)
	calls, err := Claude(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.InputTokens != 150 || c.OutputTokens != 50 || c.CacheRead != 7 {
		t.Fatalf("tokens = %d/%d/%d, want 150/50/7", c.InputTokens, c.OutputTokens, c.CacheRead)
	}
	if c.Model != "claude-sonnet-4-6" { // <synthetic> must not overwrite
		t.Fatalf("model = %q, want claude-sonnet-4-6", c.Model)
	}
	if c.UserMessage != "do the thing" || c.SessionID != "s1" {
		t.Fatalf("correlation fields wrong: %+v", c)
	}
	if c.Timestamp.IsZero() {
		t.Fatal("timestamp not set")
	}
}

func TestClaudeLatestModel(t *testing.T) {
	root := t.TempDir()
	cwd := "/test/proj"
	writeSession(t, root, cwd, "s1.jsonl", `{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:00Z","message":{"model":"claude-opus-4-6","usage":{"input_tokens":1}}}
{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:01Z","message":{"model":"<synthetic>","usage":{"input_tokens":1}}}
`)
	m, err := Claude(root).LatestModel(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if m != "claude-opus-4-6" { // <synthetic> must be ignored
		t.Fatalf("LatestModel = %q, want claude-opus-4-6", m)
	}
}

func TestClaudeLatestModelNoSessions(t *testing.T) {
	m, err := Claude(t.TempDir()).LatestModel("/nope")
	if err != nil || m != "" {
		t.Fatalf("no sessions should give empty/no-error, got %q / %v", m, err)
	}
}

func TestClaudeReaderMissingDir(t *testing.T) {
	calls, err := Claude(t.TempDir()).ReadForCWD("/nope")
	if err != nil || len(calls) != 0 {
		t.Fatalf("missing dir: %v / %d calls", err, len(calls))
	}
}
