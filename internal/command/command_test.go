package command

import (
	"strings"
	"testing"
)

func TestLookupCLIResolvesNamesAndAliases(t *testing.T) {
	cases := []struct {
		token string
		want  string
	}{
		{"plan", "plan"},
		{"clean-runs", "clean-runs"},
		{"version", "version"},
		{"--version", "version"},
		{"-v", "version"},
		{"config init", "config init"},
	}
	for _, tc := range cases {
		got, ok := LookupCLI(tc.token)
		if !ok {
			t.Fatalf("LookupCLI(%q) not found", tc.token)
		}
		if got.Name != tc.want {
			t.Fatalf("LookupCLI(%q).Name = %q, want %q", tc.token, got.Name, tc.want)
		}
	}
	if _, ok := LookupCLI("frobnicate"); ok {
		t.Fatal("unknown token should not resolve")
	}
}

func TestIsOrchestration(t *testing.T) {
	for _, name := range []string{"plan", "vote", "build", "review", "adopt", "run", "resume", "status", "report", "pr", "scorecard", "queue", "stack", "clean", "clean-runs"} {
		if !IsOrchestration(name) {
			t.Errorf("%q should be an orchestration command", name)
		}
	}
	for _, name := range []string{"version", "doctor", "trust", "config init", "ask", "launch", "frobnicate"} {
		if IsOrchestration(name) {
			t.Errorf("%q should not be an orchestration command", name)
		}
	}
}

// TestCLIGroupsAreKnown pins that every command lands in a known group (the
// help/docs renderers switch on it). Repo enforcement lives in
// orchestrate.NewController, not in this registry.
func TestCLIGroupsAreKnown(t *testing.T) {
	for _, c := range CLIs() {
		switch c.Group {
		case GroupOrchestration, GroupGeneral:
		default:
			t.Errorf("command %q has an unknown group %d", c.Name, c.Group)
		}
	}
}

func TestCLIRegistryHasNoDuplicateTokens(t *testing.T) {
	seen := map[string]string{}
	for _, c := range CLIs() {
		for _, tok := range append([]string{c.Name}, c.Aliases...) {
			if prev, ok := seen[tok]; ok {
				t.Fatalf("token %q used by both %q and %q", tok, prev, c.Name)
			}
			seen[tok] = c.Name
		}
	}
}

func TestLookupComposerResolvesAliasesCaseInsensitive(t *testing.T) {
	cases := []struct {
		word string
		want string
	}{
		{"all", "all"},
		{"broadcast", "all"},
		{"BROADCAST", "all"},
		{"window", "direct"},
		{"full", "zoom"},
		{"agents", "overview"},
		{"prefs", "settings"},
		{"startbuild", "start-build"},
		{"exit", "quit"},
	}
	for _, tc := range cases {
		got, ok := LookupComposer(tc.word)
		if !ok {
			t.Fatalf("LookupComposer(%q) not found", tc.word)
		}
		if got.Name != tc.want {
			t.Fatalf("LookupComposer(%q).Name = %q, want %q", tc.word, got.Name, tc.want)
		}
	}
	if _, ok := LookupComposer("definitely-not-a-command"); ok {
		t.Fatal("unknown word should not resolve")
	}
}

func TestComposerRegistryHasNoDuplicateWords(t *testing.T) {
	seen := map[string]string{}
	for _, c := range Composers() {
		for _, word := range append([]string{c.Name}, c.Aliases...) {
			if prev, ok := seen[word]; ok {
				t.Fatalf("word %q used by both %q and %q", word, prev, c.Name)
			}
			seen[word] = c.Name
		}
	}
}

// generalUsageBlock is the exact head of the rendered help, pinned so the
// registry-driven renderer cannot silently drift from the historical layout.
const generalUsageBlock = `Usage:
  council [--agents claude,codex] [--no-local-config]
  council [--agents claude,codex] ask "<prompt>"
  council config init [--force] [--interactive]  write the default (safe) config
  council config wizard               interactive setup
  council config add-agent <preset> [--name x] [--role planner,builder,voter,review]
  council config schema [--json]      print the configuration reference (Markdown, or JSON Schema)
  council doctor [--fix]              check config, commands, repo, run dirs (--fix applies safe fixes)
  council trust [--revoke|--show]     trust this repo's .council.yaml
  council version
`

func TestHelpStringRendersSynopsisSummaryAndFlags(t *testing.T) {
	cost, _ := LookupCLI("cost")
	got := HelpString(cost)
	for _, want := range []string{
		"council cost — per-session usage and estimated cost",
		"Usage:\n  council cost",
		"--since 30d",
		"cost prices refresh", // from Long
		"--no-local-config",   // shared flag appended for orchestration commands
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HelpString(cost) missing %q:\n%s", want, got)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("HelpString should not end with a trailing newline")
	}
}

func TestHelpFlagsAppendsNoLocalConfigOnlyWhereAccepted(t *testing.T) {
	// Orchestration commands and doctor accept --no-local-config; config schema
	// does not.
	plan, _ := LookupCLI("plan")
	if !plan.AcceptsNoLocalConfig() {
		t.Error("plan should accept --no-local-config")
	}
	schema, _ := LookupCLI("config schema")
	if schema.AcceptsNoLocalConfig() {
		t.Error("config schema should not accept --no-local-config")
	}
	if strings.Contains(HelpString(schema), "--no-local-config") {
		t.Error("config schema help should not list --no-local-config")
	}
	// queue and stack are orchestration commands but manual parsers that never
	// read --no-local-config, so help must not advertise it.
	for _, name := range []string{"queue", "stack"} {
		c, _ := LookupCLI(name)
		if c.AcceptsNoLocalConfig() {
			t.Errorf("%s should not accept --no-local-config (manual parser ignores it)", name)
		}
		if strings.Contains(HelpString(c), "--no-local-config") {
			t.Errorf("%s help should not list --no-local-config", name)
		}
	}
}

func TestUsageStringRendersBothSections(t *testing.T) {
	got := UsageString()
	if !strings.HasPrefix(got, tagline+"\n\n") {
		t.Fatalf("usage should open with the tagline:\n%q", got)
	}
	if !strings.Contains(got, "\n"+generalUsageBlock) {
		t.Fatalf("usage general section drifted:\n%q", got)
	}
	if !strings.Contains(got, "\n"+orchestrationHeader+"\n") {
		t.Fatal("usage missing the orchestration header")
	}
	// Orientation blocks: global flags, examples, and the config/docs footer.
	for _, want := range []string{flagsBlock, examplesBlock, footer} {
		if !strings.Contains(got, want) {
			t.Fatalf("usage missing an orientation block:\nwant:\n%s\ngot:\n%s", want, got)
		}
	}
	// A described command renders its summary at a padded column.
	if !strings.Contains(got, "council vote [run]") || !strings.Contains(got, "tally ranked votes into a winner") {
		t.Fatalf("usage missing the vote entry:\n%s", got)
	}
	// Synopsis-only commands render bare, without an inline description.
	if !strings.Contains(got, "\n  council version\n") {
		t.Fatalf("version should render as a bare synopsis line:\n%s", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("usage should not end with a trailing newline")
	}
}
