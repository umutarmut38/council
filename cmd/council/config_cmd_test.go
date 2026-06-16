package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestInitConfigWritesDefaultAndRefusesOverwrite(t *testing.T) {
	setTempHome(t)
	cfgPath, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}

	if err := captureErr(t, func() error { return initConfig(nil) }); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("written config does not load: %v", err)
	}
	for name, agent := range cfg.Agents {
		if agent.Enabled {
			t.Fatalf("default config should ship %q disabled", name)
		}
	}

	// A second write without --force must refuse rather than clobber.
	err = captureErr(t, func() error { return initConfig(nil) })
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second initConfig error = %v, want an 'already exists' refusal", err)
	}

	// --force overwrites in place.
	if err := captureErr(t, func() error { return initConfig([]string{"--force"}) }); err != nil {
		t.Fatalf("initConfig --force: %v", err)
	}
}

func TestRunConfigCommandDispatch(t *testing.T) {
	t.Run("unknown subcommand", func(t *testing.T) {
		err := runConfigCommand("nope", nil)
		if err == nil || !strings.Contains(err.Error(), "unknown config command") {
			t.Fatalf("error = %v, want unknown config command", err)
		}
	})

	t.Run("init routes to initConfig", func(t *testing.T) {
		setTempHome(t)
		if err := captureErr(t, func() error { return runConfigCommand("init", nil) }); err != nil {
			t.Fatalf("runConfigCommand init: %v", err)
		}
		cfgPath, _ := config.DefaultPath()
		if _, err := os.Stat(cfgPath); err != nil {
			t.Fatalf("config not written: %v", err)
		}
	})

	t.Run("wizard needs a terminal", func(t *testing.T) {
		setTempHome(t)
		withNonTerminalStdin(t)
		err := captureErr(t, func() error { return runConfigCommand("wizard", nil) })
		if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
			t.Fatalf("error = %v, want an interactive-terminal error", err)
		}
	})

	t.Run("init --interactive needs a terminal", func(t *testing.T) {
		setTempHome(t)
		withNonTerminalStdin(t)
		err := captureErr(t, func() error { return runConfigCommand("init", []string{"--interactive"}) })
		if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
			t.Fatalf("error = %v, want an interactive-terminal error", err)
		}
	})
}

func TestConfigAddAgent(t *testing.T) {
	t.Run("adds an enabled preset", func(t *testing.T) {
		setTempHome(t)
		if err := captureErr(t, func() error { return configAddAgent([]string{"codex"}) }); err != nil {
			t.Fatalf("add-agent codex: %v", err)
		}
		cfg := loadGlobalConfig(t)
		got, ok := cfg.Agents["codex"]
		if !ok || !got.Enabled {
			t.Fatalf("codex not added/enabled: %+v", cfg.Agents["codex"])
		}
	})

	t.Run("role flag before positional preset", func(t *testing.T) {
		setTempHome(t)
		if err := captureErr(t, func() error { return configAddAgent([]string{"codex", "--role", "worker"}) }); err != nil {
			t.Fatalf("add-agent codex --role worker: %v", err)
		}
		cfg := loadGlobalConfig(t)
		if got := cfg.Agents["codex"].Role; !reflect.DeepEqual(got, []string{config.RoleWorker}) {
			t.Fatalf("codex role = %v, want [worker]", got)
		}
	})

	t.Run("custom name keeps preset behaviour", func(t *testing.T) {
		setTempHome(t)
		if err := captureErr(t, func() error {
			return configAddAgent([]string{"codex", "--name", "codex-reviewer", "--role", "reviewer"})
		}); err != nil {
			t.Fatalf("add-agent with --name: %v", err)
		}
		cfg := loadGlobalConfig(t)
		got, ok := cfg.Agents["codex-reviewer"]
		if !ok || !got.Enabled {
			t.Fatalf("named agent not added/enabled: %+v", got)
		}
		if !reflect.DeepEqual(got.Role, []string{config.RoleReviewer}) {
			t.Fatalf("named agent role = %v, want [reviewer]", got.Role)
		}
	})

	t.Run("unknown preset is rejected", func(t *testing.T) {
		setTempHome(t)
		err := captureErr(t, func() error { return configAddAgent([]string{"madeup"}) })
		if err == nil || !strings.Contains(err.Error(), "unknown preset") {
			t.Fatalf("error = %v, want unknown preset", err)
		}
	})

	t.Run("missing preset name is rejected", func(t *testing.T) {
		setTempHome(t)
		err := captureErr(t, func() error { return configAddAgent(nil) })
		if err == nil || !strings.Contains(err.Error(), "usage: council config add-agent") {
			t.Fatalf("error = %v, want usage hint", err)
		}
	})

	t.Run("unknown role is rejected", func(t *testing.T) {
		setTempHome(t)
		err := captureErr(t, func() error { return configAddAgent([]string{"codex", "--role", "captain"}) })
		if err == nil || !strings.Contains(err.Error(), "unknown role") {
			t.Fatalf("error = %v, want unknown role", err)
		}
	})

	t.Run("re-adding an enabled agent is rejected", func(t *testing.T) {
		setTempHome(t)
		if err := captureErr(t, func() error { return configAddAgent([]string{"codex"}) }); err != nil {
			t.Fatalf("first add: %v", err)
		}
		err := captureErr(t, func() error { return configAddAgent([]string{"codex"}) })
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("error = %v, want already-exists refusal", err)
		}
	})
}

// captureErr runs fn, discarding its stdout/stderr noise, and returns its error.
func captureErr(t *testing.T, fn func() error) error {
	t.Helper()
	var err error
	captureOutput(t, func() { err = fn() })
	return err
}

func loadGlobalConfig(t *testing.T) config.Config {
	t.Helper()
	cfgPath, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	return cfg
}

func TestRunConfigSchemaPrintsMarkdown(t *testing.T) {
	out := captureOutput(t, func() {
		if err := runConfigCommand("schema", nil); err != nil {
			t.Fatalf("config schema: %v", err)
		}
	})
	for _, want := range []string{
		"# Configuration schema",
		"### `agents.<name>`",
		"| `enabled` | bool | `false` | Launch this agent. |",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config schema output missing %q in:\n%s", want, out)
		}
	}
}
