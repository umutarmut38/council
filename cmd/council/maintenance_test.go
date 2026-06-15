package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/umutarmut38/council/internal/config"
)

func TestDetectStack(t *testing.T) {
	tests := []struct {
		name      string
		marker    string
		wantStack string
		wantCmd   []string
	}{
		{"go", "go.mod", "go", []string{"go", "test", "./..."}},
		{"node", "package.json", "node", []string{"npm", "test"}},
		{"rust", "Cargo.toml", "rust", []string{"cargo", "test"}},
		{"python pyproject", "pyproject.toml", "python", []string{"pytest"}},
		{"python setup.py", "setup.py", "python", []string{"pytest"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.marker), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			stack, cmd := detectStack(dir)
			if stack != tc.wantStack {
				t.Fatalf("detectStack stack = %q, want %q", stack, tc.wantStack)
			}
			if !reflect.DeepEqual(cmd, tc.wantCmd) {
				t.Fatalf("detectStack cmd = %v, want %v", cmd, tc.wantCmd)
			}
		})
	}

	t.Run("no marker", func(t *testing.T) {
		stack, cmd := detectStack(t.TempDir())
		if stack != "" || cmd != nil {
			t.Fatalf("detectStack on empty dir = (%q, %v), want empty", stack, cmd)
		}
	})

	t.Run("go wins over node when both present", func(t *testing.T) {
		dir := t.TempDir()
		for _, m := range []string{"go.mod", "package.json"} {
			if err := os.WriteFile(filepath.Join(dir, m), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		stack, _ := detectStack(dir)
		if stack != "go" {
			t.Fatalf("detectStack precedence = %q, want go", stack)
		}
	})
}

func TestCouncilStackErrors(t *testing.T) {
	chdir(t, t.TempDir()) // an empty, non-stack directory

	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"no args", nil, "usage: council stack"},
		{"unknown subcommand", []string{"bogus"}, "unknown stack command"},
		{"set without value", []string{"set"}, "usage: council stack set"},
		{"set unknown stack", []string{"set", "haskell"}, "unknown stack"},
		{"detect with no stack", []string{"detect"}, "no known stack detected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := captureErr(t, func() error { return councilStack(tc.args) })
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("councilStack(%v) error = %v, want it to contain %q", tc.args, err, tc.contains)
			}
		})
	}
}

func TestCouncilStackSetWritesAndTrustsLocalConfig(t *testing.T) {
	setTempHome(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	chdir(t, repo)

	if err := captureErr(t, func() error { return councilStack([]string{"set", "go"}) }); err != nil {
		t.Fatalf("stack set go: %v", err)
	}

	path := filepath.Join(repo, ".council.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("local config not written: %v", err)
	}
	var parsed struct {
		Review struct {
			CheckCommand []string `yaml:"check_command"`
		} `yaml:"review"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("written config does not parse: %v", err)
	}
	if want := []string{"go", "test", "./..."}; !reflect.DeepEqual(parsed.Review.CheckCommand, want) {
		t.Fatalf("review.check_command = %v, want %v", parsed.Review.CheckCommand, want)
	}
	if config.LocalConfigTrust(path, raw) != config.Trusted {
		t.Fatal("council stack set should trust the config it just authored")
	}
}

func TestCouncilStackDetectWritesLocalConfig(t *testing.T) {
	setTempHome(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, repo)

	if err := captureErr(t, func() error { return councilStack([]string{"detect"}) }); err != nil {
		t.Fatalf("stack detect: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".council.yaml"))
	if err != nil {
		t.Fatalf("local config not written by detect: %v", err)
	}
	if !strings.Contains(string(raw), "check_command") {
		t.Fatalf("detect did not write a check_command: %s", raw)
	}
}

func TestQueueItemString(t *testing.T) {
	long := strings.Repeat("a", 70)
	tests := []struct {
		name string
		item queueItem
		want string
	}{
		{"issue", queueItem{Issue: 7}, "issue #7"},
		{"file", queueItem{File: "task.md"}, "file task.md"},
		{"short inline", queueItem{Inline: "fix the bug"}, `"fix the bug"`},
		{"long inline truncated", queueItem{Inline: long}, fmt.Sprintf("%q", long[:59]+"~")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.String(); got != tc.want {
				t.Fatalf("queueItem.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQueueSaveLoadRoundTrip(t *testing.T) {
	chdir(t, t.TempDir())

	want := []queueItem{{Issue: 1}, {File: "f.md"}, {Inline: "do the thing"}}
	if err := saveQueue(want); err != nil {
		t.Fatalf("saveQueue: %v", err)
	}
	if _, err := os.Stat(queuePath()); err != nil {
		t.Fatalf("queue file not created: %v", err)
	}
	got, err := loadQueue()
	if err != nil {
		t.Fatalf("loadQueue: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestLoadQueueMissingIsEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	got, err := loadQueue()
	if err != nil {
		t.Fatalf("loadQueue on missing file should not error: %v", err)
	}
	if got != nil {
		t.Fatalf("loadQueue on missing file = %v, want nil", got)
	}
}

func TestCouncilQueueFlows(t *testing.T) {
	chdir(t, t.TempDir())

	t.Run("usage with no args", func(t *testing.T) {
		err := captureErr(t, func() error { return councilQueue(nil) })
		if err == nil || !strings.Contains(err.Error(), "usage: council queue") {
			t.Fatalf("error = %v, want queue usage", err)
		}
	})

	t.Run("unknown subcommand", func(t *testing.T) {
		err := captureErr(t, func() error { return councilQueue([]string{"frobnicate"}) })
		if err == nil || !strings.Contains(err.Error(), "unknown queue command") {
			t.Fatalf("error = %v, want unknown queue command", err)
		}
	})

	t.Run("add requires a target", func(t *testing.T) {
		err := captureErr(t, func() error { return councilQueue([]string{"add"}) })
		if err == nil || !strings.Contains(err.Error(), "queue add") {
			t.Fatalf("error = %v, want add usage", err)
		}
	})

	t.Run("run on empty queue errors", func(t *testing.T) {
		err := captureErr(t, func() error { return councilQueue([]string{"run"}) })
		if err == nil || !strings.Contains(err.Error(), "queue is empty") {
			t.Fatalf("error = %v, want empty-queue error", err)
		}
	})

	t.Run("add, list, clear", func(t *testing.T) {
		if err := captureErr(t, func() error { return councilQueue([]string{"add", "--issue", "42"}) }); err != nil {
			t.Fatalf("add --issue: %v", err)
		}
		if err := captureErr(t, func() error { return councilQueue([]string{"add", "fix", "the", "thing"}) }); err != nil {
			t.Fatalf("add inline: %v", err)
		}
		items, err := loadQueue()
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].Issue != 42 || items[1].Inline != "fix the thing" {
			t.Fatalf("queue after adds = %+v", items)
		}

		out := captureOutput(t, func() {
			if err := councilQueue([]string{"list"}); err != nil {
				t.Errorf("list: %v", err)
			}
		})
		if !strings.Contains(out, "issue #42") || !strings.Contains(out, "fix the thing") {
			t.Fatalf("list output = %q, want both items", out)
		}

		if err := captureErr(t, func() error { return councilQueue([]string{"clear"}) }); err != nil {
			t.Fatalf("clear: %v", err)
		}
		if items, _ := loadQueue(); len(items) != 0 {
			t.Fatalf("queue after clear = %+v, want empty", items)
		}
	})
}

func TestCouncilCleanRuns(t *testing.T) {
	setTempHome(t)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	work := t.TempDir()
	chdir(t, work)
	runsDir := filepath.Join(work, ".council", "runs")
	stamps := []string{"20260101-000000", "20260102-000000", "20260103-000000"}
	for _, s := range stamps {
		if err := os.MkdirAll(filepath.Join(runsDir, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("default keep prunes nothing", func(t *testing.T) {
		if err := captureErr(t, func() error { return councilCleanRuns(nil) }); err != nil {
			t.Fatalf("clean-runs: %v", err)
		}
		if entries, _ := os.ReadDir(runsDir); len(entries) != 3 {
			t.Fatalf("default keep removed runs: %d remain, want 3", len(entries))
		}
	})

	t.Run("dry-run keeps everything", func(t *testing.T) {
		if err := captureErr(t, func() error { return councilCleanRuns([]string{"--keep", "1", "--dry-run"}) }); err != nil {
			t.Fatalf("dry-run: %v", err)
		}
		if entries, _ := os.ReadDir(runsDir); len(entries) != 3 {
			t.Fatalf("dry-run removed runs: %d remain, want 3", len(entries))
		}
	})

	t.Run("prune keeps the newest", func(t *testing.T) {
		if err := captureErr(t, func() error { return councilCleanRuns([]string{"--keep", "1", "--yes"}) }); err != nil {
			t.Fatalf("prune: %v", err)
		}
		entries, _ := os.ReadDir(runsDir)
		if len(entries) != 1 || entries[0].Name() != "20260103-000000" {
			t.Fatalf("after prune = %v, want only the newest stamp", entries)
		}
	})
}

func TestSetupSummary(t *testing.T) {
	tests := []struct {
		name string
		cmd  config.SetupCommand
		want string
	}{
		{
			name: "one-shot with explicit name",
			cmd:  config.SetupCommand{Name: "migrate", Command: []string{"make", "migrate"}},
			want: "migrate  [run-and-wait]  $ make migrate",
		},
		{
			name: "one-shot defaults label to the command",
			cmd:  config.SetupCommand{Command: []string{"echo", "hi"}},
			want: "echo hi  [run-and-wait]  $ echo hi",
		},
		{
			name: "background service",
			cmd:  config.SetupCommand{Name: "proxy", Command: []string{"proxyd"}, Background: true},
			want: "proxy  [background]  $ proxyd",
		},
		{
			name: "background with readiness port",
			cmd:  config.SetupCommand{Name: "proxy", Command: []string{"proxyd"}, Background: true, WaitForPort: 8080},
			want: "proxy  [background, wait for :8080]  $ proxyd",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupSummary(tc.cmd); got != tc.want {
				t.Fatalf("setupSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}
