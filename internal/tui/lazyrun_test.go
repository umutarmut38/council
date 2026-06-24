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
	// ensureRun opens the raw log; close it before TempDir cleanup so Windows
	// (which can't delete open files) can remove the run directory.
	defer session.Terminate()
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

// TestSlashAllRealizesRun verifies the /all command (which is handled before the
// normal send path) still creates the run on the first prompt.
func TestSlashAllRealizesRun(t *testing.T) {
	store := runstore.NewDeferred(filepath.Join(t.TempDir(), "runs"), nil, nil)
	session := agent.NewSession("a", config.AgentConfig{Command: []string{"true"}}, "")
	defer session.Terminate()
	model := NewModel([]*agent.Session{session}, store, 1000, "", 0, nil, nil)

	model.PromptInput = "/all hello"
	model.submitInput()

	if !store.Started() {
		t.Fatal("/all should realize the run on the first prompt")
	}
}

// TestSubmitInputRestoresComposerWhenRunCreationFails verifies the user's typed
// prompt is not lost when the deferred run directory cannot be created.
func TestSubmitInputRestoresComposerWhenRunCreationFails(t *testing.T) {
	// A regular file where the runs root's parent should be a directory makes
	// MkdirAll (and therefore Store.Ensure) fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := runstore.NewDeferred(filepath.Join(blocker, "runs"), nil, nil)
	session := agent.NewSession("a", config.AgentConfig{Command: []string{"true"}}, "")
	defer session.Terminate()
	model := NewModel([]*agent.Session{session}, store, 1000, "", 0, nil, nil)

	model.PromptInput = "important prompt"
	model.Target = TargetAll
	model.submitInput()

	if store.Started() {
		t.Fatal("run must not be marked started when Ensure fails")
	}
	if model.PromptInput != "important prompt" {
		t.Fatalf("composer should be restored on failure, got %q", model.PromptInput)
	}
}
