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

func TestClaudeReaderMissingDir(t *testing.T) {
	calls, err := Claude(t.TempDir()).ReadForCWD("/nope")
	if err != nil || len(calls) != 0 {
		t.Fatalf("missing dir: %v / %d calls", err, len(calls))
	}
}

// scanFile silently skips a file that vanished after the glob (ErrNotExist) but
// surfaces any other open error so reconciliation can note it.
func TestClaudeScanFileSurfacesRealOpenError(t *testing.T) {
	r := claudeReader{}
	// A missing file is not an error (raced with deletion).
	if err := r.scanFile(filepath.Join(t.TempDir(), "gone.jsonl"), "/x", newClaudeScan()); err != nil {
		t.Fatalf("missing file should be skipped, got %v", err)
	}
	// A NUL byte makes os.Open fail with EINVAL (not ErrNotExist) on every OS — a
	// portable stand-in for a real open failure.
	if err := r.scanFile("bad\x00name.jsonl", "/x", newClaudeScan()); err == nil {
		t.Fatal("a malformed path must surface an error, got nil")
	}
}

// A session that switches models mid-way (e.g. /model) yields one call per
// model, so opus tokens are never priced at the haiku rate.
func TestClaudeReaderSplitsPerModel(t *testing.T) {
	root := t.TempDir()
	cwd := "/test/proj"
	writeSession(t, root, cwd, "s1.jsonl", `{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:00:00Z","message":{"model":"claude-opus-4-6","usage":{"input_tokens":100,"output_tokens":40}}}
{"type":"assistant","cwd":"/test/proj","sessionId":"s1","timestamp":"2026-06-27T10:05:00Z","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":10,"output_tokens":4}}}
`)
	calls, err := Claude(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	byModel := map[string]Call{}
	for _, c := range calls {
		byModel[c.Model] = c
	}
	if len(calls) != 2 || byModel["claude-opus-4-6"].InputTokens != 100 || byModel["claude-haiku-4-5"].InputTokens != 10 {
		t.Fatalf("per-model split wrong: %+v", calls)
	}
	// Both calls share the session's activity interval.
	if byModel["claude-opus-4-6"].LastActivity != byModel["claude-haiku-4-5"].LastActivity {
		t.Fatalf("session interval not shared: %+v", calls)
	}
}

// The slug must match Claude Code's sanitization (all non-alphanumerics → "-"),
// including underscores, which the old slash/dot-only slug missed.
func TestClaudeSlugNonAlphanumeric(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/x/my_repo": "-Users-x-my-repo",
		`C:\repo`:          "C--repo",
		"/a/b.c":           "-a-b-c",
	} {
		if got := claudeSlug(in); got != want {
			t.Errorf("claudeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
