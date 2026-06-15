package main

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestParseWithTrailingFlags(t *testing.T) {
	newFS := func() (*flag.FlagSet, *bool, *bool) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		yes := fs.Bool("yes", false, "")
		dry := fs.Bool("dry-run", false, "")
		return fs, yes, dry
	}

	t.Run("flags interleaved with positionals", func(t *testing.T) {
		fs, yes, dry := newFS()
		pos, err := parseWithTrailingFlags(fs, []string{"run1", "--yes", "agent1", "--dry-run"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := []string{"run1", "agent1"}; !reflect.DeepEqual(pos, want) {
			t.Fatalf("positionals = %v, want %v", pos, want)
		}
		if !*yes || !*dry {
			t.Fatalf("flags not parsed: yes=%v dry=%v", *yes, *dry)
		}
	})

	t.Run("flags only", func(t *testing.T) {
		fs, yes, _ := newFS()
		pos, err := parseWithTrailingFlags(fs, []string{"--yes"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pos != nil {
			t.Fatalf("positionals = %v, want nil", pos)
		}
		if !*yes {
			t.Fatal("--yes not parsed")
		}
	})

	t.Run("no args", func(t *testing.T) {
		fs, _, _ := newFS()
		pos, err := parseWithTrailingFlags(fs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pos != nil {
			t.Fatalf("positionals = %v, want nil", pos)
		}
	})

	t.Run("unknown flag errors", func(t *testing.T) {
		fs, _, _ := newFS()
		if _, err := parseWithTrailingFlags(fs, []string{"run1", "--nope"}); err == nil {
			t.Fatal("expected an error for an unknown flag")
		}
	})
}

func TestRunOrchestrationUnknownCommand(t *testing.T) {
	err := runOrchestration("nonsense", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown orchestration command") {
		t.Fatalf("error = %v, want unknown orchestration command", err)
	}
}

func TestPromptsForAgents(t *testing.T) {
	t.Run("maps every agent to the prompt", func(t *testing.T) {
		got := promptsForAgents([]string{"claude", "codex"}, "implement plan")
		want := map[string]string{"claude": "implement plan", "codex": "implement plan"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("promptsForAgents = %v, want %v", got, want)
		}
	})

	t.Run("no agents yields an empty map", func(t *testing.T) {
		got := promptsForAgents(nil, "x")
		if len(got) != 0 {
			t.Fatalf("promptsForAgents(nil) = %v, want empty", got)
		}
	})
}

func TestCouncilStatusMissingRun(t *testing.T) {
	setTempHome(t)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	chdir(t, t.TempDir())

	err := captureErr(t, func() error { return councilStatus([]string{"20990101-000000"}) })
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want a run-not-found error", err)
	}
}
