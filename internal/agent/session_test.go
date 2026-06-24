package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

// TestEnableRawLogIsDeferredAndIdempotent covers the lazy raw-log path used when
// the interactive run directory is created on the first prompt: a session starts
// with no log, EnableRawLog wires one up, and repeat calls are no-ops.
func TestEnableRawLogIsDeferredAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := NewSession("agent-a", config.AgentConfig{Command: []string{"true"}}, "")

	// An empty path leaves logging off (the deferred-store default).
	if err := s.EnableRawLog(""); err != nil {
		t.Fatalf("EnableRawLog(\"\"): %v", err)
	}
	if s.rawLog.Load() != nil {
		t.Fatal("empty path must not enable raw logging")
	}

	path := filepath.Join(dir, "raw", "agent-a.log")
	if err := s.EnableRawLog(path); err != nil {
		t.Fatalf("EnableRawLog(path): %v", err)
	}
	first := s.rawLog.Load()
	if first == nil {
		t.Fatal("raw logging should be on after EnableRawLog")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("raw log file not created: %v", err)
	}

	// A second call is a no-op and keeps the original file.
	if err := s.EnableRawLog(filepath.Join(dir, "other.log")); err != nil {
		t.Fatalf("second EnableRawLog: %v", err)
	}
	if s.rawLog.Load() != first {
		t.Fatal("EnableRawLog should be idempotent once logging is on")
	}
	_ = first.Close()
}

func TestTerminalEnvAppendsConfigEnv(t *testing.T) {
	cfg := config.AgentConfig{Env: map[string]string{"OPENAI_BASE_URL": "http://127.0.0.1:8787"}}
	env := terminalEnv(cfg)

	// The config var must be present and appear AFTER the inherited PATH so it
	// overrides any inherited value of the same key.
	var idxVar, idxTerm int = -1, -1
	for i, kv := range env {
		if strings.HasPrefix(kv, "OPENAI_BASE_URL=") {
			idxVar = i
			if kv != "OPENAI_BASE_URL=http://127.0.0.1:8787" {
				t.Fatalf("wrong value: %q", kv)
			}
		}
		if strings.HasPrefix(kv, "TERM=") {
			idxTerm = i
		}
	}
	if idxVar < 0 {
		t.Fatal("config env var not exported to agent")
	}
	if idxTerm < 0 || idxVar < idxTerm {
		t.Fatal("config env must be appended after the base env to win")
	}
}

func TestSortedEnvDeterministic(t *testing.T) {
	got := sortedEnv(map[string]string{"B": "2", "A": "1", "C": "3"})
	want := []string{"A=1", "B=2", "C=3"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("sortedEnv = %v, want %v", got, want)
	}
	if sortedEnv(nil) != nil {
		t.Fatal("nil map should give nil")
	}
}
