package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRollout(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "2026", "06", "28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "rollout-2026-06-28T10-00-00-abc.jsonl")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexReaderTotalsAndModel(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/proj"
	// session_meta (cwd) + turn_context (model) + two cumulative token_count events.
	writeRollout(t, root, `{"type":"session_meta","timestamp":"2026-06-28T10:00:00Z","payload":{"cwd":"/work/proj","session_id":"sx"}}
{"type":"turn_context","payload":{"type":"turn_context","model":"gpt-5-codex"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":250,"output_tokens":60}}}}
`)
	calls, err := Codex(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.InputTokens != 250 || c.OutputTokens != 60 { // cumulative last wins
		t.Fatalf("tokens = %d/%d, want 250/60", c.InputTokens, c.OutputTokens)
	}
	if c.Model != "gpt-5-codex" || c.SessionID != "sx" {
		t.Fatalf("meta wrong: %+v", c)
	}
	m, _ := Codex(root).LatestModel(cwd)
	if m != "gpt-5-codex" {
		t.Fatalf("LatestModel = %q, want gpt-5-codex", m)
	}
}

func TestCodexReaderSkipsOtherCWD(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, `{"type":"session_meta","timestamp":"2026-06-28T10:00:00Z","payload":{"cwd":"/other","session_id":"sy"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":999,"output_tokens":999}}}}
`)
	calls, _ := Codex(root).ReadForCWD("/work/proj")
	if len(calls) != 0 {
		t.Fatalf("should skip non-matching cwd, got %+v", calls)
	}
}
