package agent

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

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
