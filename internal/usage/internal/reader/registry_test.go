package reader

import "testing"

// Every launched tool self-registers (via its file's init()), and the dedup in
// All() collapses tools that share a reader (cursor + cursor-agent).
func TestRegistry(t *testing.T) {
	for _, tool := range []string{"claude", "codex", "opencode", "copilot", "cursor", "cursor-agent"} {
		if For(tool) == nil {
			t.Errorf("no reader registered for %q", tool)
		}
	}
	if For("nope") != nil {
		t.Error("unknown tool should resolve to nil")
	}
	names := map[string]bool{}
	for _, r := range All() {
		if names[r.Name()] {
			t.Errorf("All() returned %q twice", r.Name())
		}
		names[r.Name()] = true
	}
	// claude, codex, opencode, copilot, cursor — cursor-agent dedups into cursor.
	if len(names) != 5 {
		t.Fatalf("All() distinct readers = %d (%v), want 5", len(names), names)
	}
}

// Register makes a new tool a one-file drop-in.
func TestRegisterExtends(t *testing.T) {
	Register("demo-tool", func() Reader { return claudeReader{} })
	defer delete(registry, "demo-tool")
	if For("demo-tool") == nil {
		t.Fatal("Register did not take effect")
	}
}
