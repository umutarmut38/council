package reader

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCopilotSession(t *testing.T, root, id, cwd, eventsJSONL string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte("id: "+id+"\ncwd: "+cwd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(eventsJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotReportedTokens(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/proj"
	writeCopilotSession(t, root, "sess1", cwd, `{"type":"assistant.message","data":{"model":"gpt-5.4-mini","outputTokens":29}}
{"type":"session.shutdown","data":{"currentModel":"gpt-5.4-mini","tokenDetails":{"input":{"tokenCount":17454},"output":{"tokenCount":29}},"modelMetrics":{"gpt-5.4-mini":{"usage":{"inputTokens":17454,"outputTokens":29,"reasoningTokens":21}}}}}
`)
	writeCopilotSession(t, root, "sess2", "/other", `{"type":"session.shutdown","data":{"currentModel":"x","tokenDetails":{"input":{"tokenCount":999},"output":{"tokenCount":999}}}}
`)

	calls, err := Copilot(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 { // one model in sess1; sess2 excluded by cwd
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Model != "gpt-5.4-mini" || c.InputTokens != 17454 || c.OutputTokens != 29+21 {
		t.Fatalf("call = %+v, want gpt-5.4-mini 17454 in / 50 out (output+reasoning)", c)
	}

	m, _ := Copilot(root).LatestModel(cwd)
	if m != "gpt-5.4-mini" {
		t.Fatalf("LatestModel = %q", m)
	}
}

// A session still running (no shutdown event) reports nothing yet.
func TestCopilotIgnoresUnfinishedSession(t *testing.T) {
	root := t.TempDir()
	writeCopilotSession(t, root, "live", "/work", `{"type":"assistant.message","data":{"model":"gpt-5.4","outputTokens":5}}
`)
	if calls, _ := Copilot(root).ReadForCWD("/work"); len(calls) != 0 {
		t.Fatalf("unfinished session should report nothing, got %+v", calls)
	}
}
