package reader

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
}

// codex's total_token_usage.input_tokens is CUMULATIVE and INCLUDES the cached
// (context-reuse) portion. The reader must record only the FRESH input (total
// minus cached) so the re-sent context isn't double-counted in the Input column
// and in pricing — the root cause of the "input shows as way more than it
// should" report (a real run summed 3 cache-inclusive sessions to 39.9k).
func TestCodexReaderExcludesCachedFromInput(t *testing.T) {
	root := t.TempDir()
	// Two cumulative snapshots; the last wins. input=13287 includes cached=4480.
	writeRollout(t, root, `{"type":"session_meta","timestamp":"2026-06-28T10:00:00Z","payload":{"cwd":"/work/proj","session_id":"sc"}}
{"type":"turn_context","payload":{"type":"turn_context","model":"gpt-5.4-mini"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":8000,"output_tokens":10,"cached_input_tokens":2000}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":13287,"output_tokens":25,"cached_input_tokens":4480}}}}
`)
	calls, err := Codex(root).ReadForCWD("/work/proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	// Fresh input = 13287 - 4480 = 8807 (NOT 13287); cache kept separate.
	if c.InputTokens != 8807 {
		t.Fatalf("InputTokens = %d, want 8807 (13287 total - 4480 cached)", c.InputTokens)
	}
	if c.CacheRead != 4480 {
		t.Fatalf("CacheRead = %d, want 4480", c.CacheRead)
	}
	if c.OutputTokens != 25 {
		t.Fatalf("OutputTokens = %d, want 25 (cumulative last wins)", c.OutputTokens)
	}
}

// The captured UserMessage is the first user_message event (the council prompt
// as typed), not codex's synthetic <environment_context>/AGENTS.md user turns —
// so reconciliation can match it to a pane's personality fingerprint.
func TestCodexReaderCapturesCouncilPrompt(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, `{"type":"session_meta","timestamp":"2026-06-28T10:00:00Z","payload":{"cwd":"/work/proj","session_id":"sx"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/work/proj</cwd>\n</environment_context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n<INSTRUCTIONS>..."}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"You are The Minimalist. Less is more.\n\nWrite the plan."}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2}}}}
`)
	calls, _ := Codex(root).ReadForCWD("/work/proj")
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	want := "You are The Minimalist. Less is more."
	if got := calls[0].UserMessage; got[:len(want)] != want {
		t.Fatalf("UserMessage = %q, want it to start with the council prompt", got)
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
