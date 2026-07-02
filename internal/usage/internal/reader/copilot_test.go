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
	if c.Model != "gpt-5.4-mini" || c.InputTokens != 17454 || c.OutputTokens != 29 || c.ReasoningTokens != 21 {
		t.Fatalf("call = %+v, want gpt-5.4-mini 17454 in / 29 out / 21 reasoning", c)
	}
}

// Copilot's modelMetrics.inputTokens is cache-inclusive; the reader must record
// only the fresh input (input - cacheRead) and keep the cached part separate, or
// the re-sent context is double-charged at the full input rate. Numbers are from
// a real session: 35310 inputTokens = 17902 fresh + 17408 cacheRead.
func TestCopilotSeparatesCacheFromInput(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/proj"
	writeCopilotSession(t, root, "sess1", cwd, `{"type":"session.shutdown","data":{"currentModel":"gpt-5.4-mini","tokenDetails":{"input":{"tokenCount":17902},"cache_read":{"tokenCount":17408},"output":{"tokenCount":51}},"modelMetrics":{"gpt-5.4-mini":{"usage":{"inputTokens":35310,"outputTokens":51,"reasoningTokens":22,"cacheReadTokens":17408}}}}}
`)
	calls, err := Copilot(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.InputTokens != 17902 {
		t.Fatalf("InputTokens = %d, want 17902 (fresh = 35310 - 17408 cached)", c.InputTokens)
	}
	if c.CacheRead != 17408 {
		t.Fatalf("CacheRead = %d, want 17408", c.CacheRead)
	}
	if c.OutputTokens != 51 || c.ReasoningTokens != 22 {
		t.Fatalf("out/reasoning = %d/%d, want 51/22", c.OutputTokens, c.ReasoningTokens)
	}
}

// The tokenDetails fallback (no modelMetrics) already stores fresh input; the
// reader must not subtract cache twice.
func TestCopilotTokenDetailsFallbackKeepsFreshInput(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/proj"
	writeCopilotSession(t, root, "sess1", cwd, `{"type":"session.shutdown","data":{"currentModel":"gpt-5.4-mini","tokenDetails":{"input":{"tokenCount":17902},"cache_read":{"tokenCount":17408},"output":{"tokenCount":51}}}}
`)
	calls, err := Copilot(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].InputTokens != 17902 || calls[0].CacheRead != 17408 {
		t.Fatalf("got %d in / %d cache, want 17902 / 17408", calls[0].InputTokens, calls[0].CacheRead)
	}
}

// A malformed record where cacheRead exceeds the (cache-inclusive) input must be
// clamped so fresh stays >= 0 and fresh+cache never exceeds the reported input.
func TestCopilotClampsMalformedCacheRead(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/proj"
	writeCopilotSession(t, root, "sess1", cwd, `{"type":"session.shutdown","data":{"currentModel":"gpt-5.4-mini","modelMetrics":{"gpt-5.4-mini":{"usage":{"inputTokens":100,"outputTokens":5,"cacheReadTokens":999}}}}}
`)
	calls, err := Copilot(root).ReadForCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.InputTokens != 0 || c.CacheRead != 100 {
		t.Fatalf("got %d in / %d cache, want 0 / 100 (cacheRead clamped to input)", c.InputTokens, c.CacheRead)
	}
	if c.InputTokens+c.CacheRead != 100 {
		t.Fatalf("fresh+cache = %d, want 100 (must not exceed reported input)", c.InputTokens+c.CacheRead)
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
