package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/version"
)

func TestParseAgentList(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single", "claude", []string{"claude"}},
		{"comma separated", "claude,codex", []string{"claude", "codex"}},
		{"trims surrounding spaces", " claude , codex ", []string{"claude", "codex"}},
		{"drops empty fields", "claude,,codex,", []string{"claude", "codex"}},
		// A non-empty value of only separators trims to an empty (non-nil)
		// slice, unlike a blank value which short-circuits to nil.
		{"only commas", ",,,", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAgentList(tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAgentList(%q) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

// TestMainExitCode covers the dispatch paths that resolve before any config is
// loaded or any agent is launched, so they are fully hermetic.
func TestMainExitCode(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		contains string
	}{
		{"version", []string{"version"}, 0, "council"},
		{"--version", []string{"--version"}, 0, "council"},
		{"-v", []string{"-v"}, 0, "council"},
		{"help", []string{"help"}, 0, "Usage:"},
		// help/-h/--help all print the full usage and exit 0 (not flag's terse
		// dump). flags.Parse intercepts -h/--help in any position and returns
		// ErrHelp, which run() turns into the full usage — including when help
		// follows another global flag, which a first-arg-only check would miss.
		{"-h", []string{"-h"}, 0, "Usage:"},
		{"--help", []string{"--help"}, 0, "Usage:"},
		{"--help after --agents", []string{"--agents", "claude", "--help"}, 0, "Usage:"},
		{"unknown command", []string{"frobnicate"}, 1, "unknown command"},
		{"ask without prompt", []string{"ask"}, 1, "usage: council ask"},
		{"unknown flag", []string{"--nope"}, 1, "not defined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := captureOutput(t, func() { code = mainExitCode(tc.args) })
			if code != tc.wantCode {
				t.Fatalf("mainExitCode(%v) = %d, want %d (output: %q)", tc.args, code, tc.wantCode, out)
			}
			if tc.contains != "" && !strings.Contains(out, tc.contains) {
				t.Fatalf("mainExitCode(%v) output = %q, want it to contain %q", tc.args, out, tc.contains)
			}
		})
	}
}

// TestHelpHasNoTerseFlagDump guards against the FlagSet's default usage leaking
// alongside ours: Parse calls flags.Usage for -h/--help, so unless it is
// suppressed the terse two-flag dump ("Usage of council:") prints to stderr in
// addition to the full usage. captureOutput merges stdout and stderr, so a leak
// is visible here.
func TestHelpHasNoTerseFlagDump(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"-h"}, {"--help"}, {"--agents", "claude", "--help"},
	} {
		out := captureOutput(t, func() { _ = mainExitCode(args) })
		if strings.Contains(out, "Usage of council:") {
			t.Errorf("council %v leaked the terse flag dump:\n%s", args, out)
		}
		if !strings.Contains(out, "Examples:") {
			t.Errorf("council %v missing the full usage:\n%s", args, out)
		}
	}
}

func TestMainExitCodeVersionMatchesVersionString(t *testing.T) {
	var code int
	out := captureOutput(t, func() { code = mainExitCode([]string{"version"}) })
	if code != 0 {
		t.Fatalf("version exit code = %d, want 0", code)
	}
	if !strings.Contains(out, version.String()) {
		t.Fatalf("version output = %q, want it to contain %q", out, version.String())
	}
}

func TestRunUnknownCommandError(t *testing.T) {
	err := run([]string{"definitely-not-a-command"})
	if err == nil {
		t.Fatal("unknown command should return an error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, want it to mention an unknown command", err)
	}
}

func TestRunAskWithoutPromptError(t *testing.T) {
	err := run([]string{"ask"})
	if err == nil {
		t.Fatal("ask without a prompt should return a usage error")
	}
	if !strings.Contains(err.Error(), "usage: council ask") {
		t.Fatalf("error = %v, want the ask usage hint", err)
	}
}

func TestPrintUsageListsCoreCommands(t *testing.T) {
	out := captureOutput(t, printUsage)
	for _, want := range []string{
		"council config init",
		"council doctor",
		"council plan",
		"council vote",
		"council build",
		"council stack detect|set",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("usage output missing %q", want)
		}
	}
}
