package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	runstore "github.com/umutarmut38/council/internal/session"
)

// TestRunDirDeferredUntilFirstPrompt verifies the interactive launch no longer
// litters .council/runs: a deferred store stays unrealized until the user sends
// a prompt, at which point submitInput creates the run directory.
func TestRunDirDeferredUntilFirstPrompt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	store := runstore.NewDeferred(root, nil, nil)
	session := agent.NewSession("a", config.AgentConfig{Command: []string{"true"}}, "")
	model := NewModel([]*agent.Session{session}, store, 1000, "", 0, nil, nil)

	if store.Started() {
		t.Fatal("run should not exist before the first prompt")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("runs root created before any prompt (err=%v)", err)
	}

	// Typing a prompt (broadcast to all) realizes the run. The agent itself
	// isn't started, so the send is a no-op error that submitInput swallows;
	// the run-dir side effect is what we assert.
	model.PromptInput = "hello council"
	model.Target = TargetAll
	model.submitInput()

	if !store.Started() {
		t.Fatal("first prompt should create the run directory")
	}
	if _, err := os.Stat(store.RunDir); err != nil {
		t.Fatalf("run dir not created on first prompt: %v", err)
	}
}
