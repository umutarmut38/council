package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/theme"
)

// TestSchemaCoversConfigStructs walks the config structs with reflection and
// fails if any YAML field is missing from Schema(). This is the anti-drift
// guard: a new config field can't ship undocumented.
func TestSchemaCoversConfigStructs(t *testing.T) {
	sectionKeys := map[string]map[string]bool{}
	for _, sec := range Schema() {
		set := map[string]bool{}
		for _, f := range sec.Fields {
			set[f.Key] = true
		}
		sectionKeys[sec.Title] = set
	}

	check := func(section string, typ reflect.Type) {
		set, ok := sectionKeys[section]
		if !ok {
			t.Fatalf("schema has no section %q", section)
		}
		for i := 0; i < typ.NumField(); i++ {
			key := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
			if key == "" || key == "-" {
				continue
			}
			if !documented(set, key) {
				t.Errorf("%s.%s (yaml %q) is undocumented in schema section %q",
					typ.Name(), typ.Field(i).Name, key, section)
			}
		}
	}

	check("agents.<name>", reflect.TypeOf(AgentConfig{}))
	check("agents.<name>.terminal", reflect.TypeOf(TerminalConfig{}))
	check("agents.<name>.orchestration", reflect.TypeOf(OrchestrationConfig{}))
	check("ui", reflect.TypeOf(UIConfig{}))
	check("env, setup, experimental", reflect.TypeOf(ExperimentalConfig{}))
	check("setup[]", reflect.TypeOf(SetupCommand{}))
	check("sessions", reflect.TypeOf(SessionConfig{}))
	check("review", reflect.TypeOf(ReviewConfig{}))
	check("files", reflect.TypeOf(FilesConfig{}))
	check("policy", reflect.TypeOf(PolicyConfig{}))
	check("personality_categories.<name>", reflect.TypeOf(PersonalityCategoryConfig{}))
	check("personalities.<name>", reflect.TypeOf(PersonalityConfig{}))
	check("ui.themes.<name>", reflect.TypeOf(theme.Palette{}))
	check("usage", reflect.TypeOf(UsageConfig{}))
	check("usage.prices.<name>", reflect.TypeOf(PriceProfile{}))
	check("agents.<name>.usage", reflect.TypeOf(AgentUsageConfig{}))

	for _, key := range []string{"env", "setup"} {
		if !documented(sectionKeys["env, setup, experimental"], key) {
			t.Errorf("top-level %q is undocumented", key)
		}
	}
}

// documented reports whether key is in set, either exactly or as the suffix of
// a dotted path (e.g. "setup_env" matches the documented "experimental.setup_env").
func documented(set map[string]bool, key string) bool {
	if set[key] {
		return true
	}
	for k := range set {
		if strings.HasSuffix(k, "."+key) {
			return true
		}
	}
	return false
}

func TestSchemaMarkdownRendersTables(t *testing.T) {
	md := SchemaMarkdown()
	for _, want := range []string{
		"### `agents.<name>`",
		"| Key | Type | Default | Description |",
		"|---|---|---|---|",
		"| `enabled` | bool | `false` | Launch this agent. |",
		"| `mode` | string | `normal` | `safe` \\| `normal` \\| `aggressive` — the automation risk posture. |",
		"### `policy`",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("schema markdown missing %q in:\n%s", want, md)
		}
	}
	if strings.HasSuffix(md, "\n") {
		t.Fatal("schema markdown should not end with a trailing newline")
	}
}
