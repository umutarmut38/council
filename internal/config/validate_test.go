package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadExample writes src to a temp config file and runs it through the real
// Load (which fills defaults and normalizes), then Validate — the same path a
// user's ~/.council.yaml takes. It returns Load's error first, then Validate's.
func loadExample(t *testing.T, src string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write example: %v", err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		return err
	}
	return cfg.Validate()
}

// TestValidationExamplesValid pins config snippets that must pass normalization
// and validation. They double as documentation of well-formed configs.
func TestValidationExamplesValid(t *testing.T) {
	examples := map[string]string{
		"minimal enabled agent": `
agents:
  alpha:
    enabled: true
    command: ["alpha-cli"]
`,
		"roles and review gate": `
agents:
  builder:
    enabled: true
    command: ["build-cli"]
    role: ["worker"]
  judge:
    enabled: true
    command: ["judge-cli"]
    role: ["reviewer"]
review:
  check_command: ["go", "test", "./..."]
policy:
  mode: safe
`,
		"personality wiring": `
agents:
  alpha:
    enabled: true
    command: ["alpha-cli"]
    personality: skeptic
personalities:
  skeptic:
    label: Skeptic
    color: "203"
`,
		"all agents disabled": `
agents:
  alpha:
    enabled: false
`,
		"inherit a preset, override role": `
agents:
  my-claude:
    inherit: claude
    enabled: true
    role: ["reviewer"]
`,
	}
	for name, src := range examples {
		t.Run(name, func(t *testing.T) {
			if err := loadExample(t, src); err != nil {
				t.Fatalf("expected valid config, got error: %v", err)
			}
		})
	}
}

// TestValidationExamplesInvalid pins config snippets that must be rejected, with
// the substring each error should mention so the message stays actionable.
func TestValidationExamplesInvalid(t *testing.T) {
	examples := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "enabled agent without a command",
			src: `
agents:
  broken:
    enabled: true
`,
			want: "no command",
		},
		{
			name: "agent name with illegal characters",
			src: `
agents:
  "bad name":
    enabled: true
    command: ["x"]
`,
			want: "invalid agent name",
		},
		{
			name: "agent names that collide after normalization",
			src: `
agents:
  Bot:
    enabled: true
    command: ["x"]
  bot:
    enabled: true
    command: ["y"]
`,
			want: "collide",
		},
		{
			name: "unknown policy mode (typo)",
			src: `
policy:
  mode: agressive
`,
			want: "policy.mode",
		},
		{
			name: "unknown terminal renderer",
			src: `
agents:
  alpha:
    enabled: true
    command: ["alpha-cli"]
    terminal:
      renderer: fancy
`,
			want: "terminal.renderer",
		},
		{
			name: "inherit from an unknown base",
			src: `
agents:
  child:
    inherit: nope
    enabled: true
    command: ["x"]
`,
			want: "unknown agent",
		},
		{
			name: "inherit from itself",
			src: `
agents:
  loop:
    inherit: loop
`,
			want: "itself",
		},
		{
			name: "inherit cycle",
			src: `
agents:
  a:
    inherit: b
  b:
    inherit: a
`,
			want: "circular",
		},
	}
	for _, tc := range examples {
		t.Run(tc.name, func(t *testing.T) {
			err := loadExample(t, tc.src)
			if err == nil {
				t.Fatalf("expected validation error mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestValidateAcceptsDefault guards the baseline: the shipped default config
// (all presets disabled) must validate.
func TestValidateAcceptsDefault(t *testing.T) {
	cfg := Default()
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate, got: %v", err)
	}
}

func TestValidateTheme(t *testing.T) {
	valid := map[string]string{
		"built-in name":                       "ui:\n  theme: nord\n",
		"custom theme defined and referenced": "ui:\n  theme: mine\n  themes:\n    mine:\n      title: \"33\"\n      warn: \"#ff0000\"\n",
		"empty theme is default":              "ui:\n  layout: grid\n",
	}
	for name, src := range valid {
		if err := loadExample(t, src); err != nil {
			t.Errorf("%s: expected valid, got: %v", name, err)
		}
	}

	invalid := map[string]struct{ src, wantSubstr string }{
		"unknown theme name":      {"ui:\n  theme: bogus\n", "unknown"},
		"retro is not selectable": {"ui:\n  theme: retro\n", "selectable"},
		// A custom palette named "retro" must not make it selectable either.
		"retro reserved over custom": {"ui:\n  theme: retro\n  themes:\n    retro:\n      title: \"33\"\n", "selectable"},
		"bad color in palette":       {"ui:\n  themes:\n    mine:\n      focus: nope\n", "ui.themes.mine"},
	}
	for name, tc := range invalid {
		err := loadExample(t, tc.src)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Errorf("%s: error %q should contain %q", name, err, tc.wantSubstr)
		}
	}
}
