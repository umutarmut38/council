package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// loadConfig writes src to a temp file and runs it through the real Load (which
// resolves inheritance and normalizes), returning the resulting config.
func loadConfig(t *testing.T, src string) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

// TestInheritBasic: a child with no fields of its own takes the base's command
// and terminal settings.
func TestInheritBasic(t *testing.T) {
	cfg := loadConfig(t, `
agents:
  base:
    enabled: false
    command: ["mytool", "--flag"]
    terminal:
      send_mode: paste
      submit_delay_ms: 300
  child:
    inherit: base
    enabled: true
    role: [worker]
`)
	child := cfg.Agents["child"]
	if !child.Enabled {
		t.Fatal("child should be enabled")
	}
	if !reflect.DeepEqual(child.Command, []string{"mytool", "--flag"}) {
		t.Fatalf("child command = %v, want inherited", child.Command)
	}
	if child.Terminal.SendMode != "paste" {
		t.Fatalf("child send_mode = %q, want inherited paste", child.Terminal.SendMode)
	}
	if child.Terminal.SubmitDelayMs != 300 {
		t.Fatalf("child submit_delay = %d, want inherited 300", child.Terminal.SubmitDelayMs)
	}
	if !reflect.DeepEqual(child.Role, []string{"worker"}) {
		t.Fatalf("child role = %v", child.Role)
	}
}

// TestInheritEnabledNotInherited: enabled is always the child's own value, so
// inheriting an enabled base never silently activates the child.
func TestInheritEnabledNotInherited(t *testing.T) {
	cfg := loadConfig(t, `
agents:
  base:
    enabled: true
    command: ["tool"]
  off-child:
    inherit: base
  on-child:
    inherit: base
    enabled: true
`)
	if cfg.Agents["off-child"].Enabled {
		t.Fatal("off-child must not inherit enabled:true from base")
	}
	if !cfg.Agents["on-child"].Enabled {
		t.Fatal("on-child sets enabled:true explicitly")
	}
	if len(cfg.Agents["off-child"].Command) == 0 {
		t.Fatal("off-child should still inherit the command")
	}
}

// TestInheritOverrides: scalar and terminal overrides win over the base, and a
// *bool terminal field can be flipped back to false.
func TestInheritOverrides(t *testing.T) {
	cfg := loadConfig(t, `
agents:
  base:
    command: ["tool"]
    color: "10"
    personality: builder
    terminal:
      send_mode: paste
      submit_delay_ms: 300
      color: true
  child:
    inherit: base
    enabled: true
    color: "208"
    terminal:
      send_mode: type
      color: false
personalities:
  builder:
    label: Builder
`)
	child := cfg.Agents["child"]
	if child.Color != "208" {
		t.Fatalf("child color = %q, want override 208", child.Color)
	}
	if child.Personality != "builder" {
		t.Fatalf("child personality = %q, want inherited builder", child.Personality)
	}
	if child.Terminal.SendMode != "type" {
		t.Fatalf("child send_mode = %q, want override type", child.Terminal.SendMode)
	}
	if child.Terminal.SubmitDelayMs != 300 {
		t.Fatalf("child submit_delay = %d, want inherited 300", child.Terminal.SubmitDelayMs)
	}
	if child.Terminal.Color == nil || *child.Terminal.Color {
		t.Fatalf("child terminal.color should be overridden to false")
	}
}

// TestInheritOrchestrationMerge: a child inherits the base's phase exclusions
// and adds its own phase command.
func TestInheritOrchestrationMerge(t *testing.T) {
	cfg := loadConfig(t, `
agents:
  base:
    command: ["tool"]
    orchestration:
      exclude_build: true
  child:
    inherit: base
    enabled: true
    orchestration:
      vote_command: ["tool", "--vote"]
`)
	orch := cfg.Agents["child"].Orchestration
	if !orch.ExcludeBuild {
		t.Fatal("child should inherit exclude_build")
	}
	if !reflect.DeepEqual(orch.VoteCommand, []string{"tool", "--vote"}) {
		t.Fatalf("child vote_command = %v", orch.VoteCommand)
	}
}

// TestInheritEnvMerge: env is merged per key (child wins) rather than replaced.
func TestInheritEnvMerge(t *testing.T) {
	cfg := loadConfig(t, `
experimental:
  setup_env: true
agents:
  base:
    command: ["tool"]
    env:
      A: "1"
      B: "1"
  child:
    inherit: base
    enabled: true
    env:
      B: "2"
      C: "3"
`)
	env := cfg.Agents["child"].Env
	want := map[string]string{"A": "1", "B": "2", "C": "3"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("child env = %v, want %v", env, want)
	}
}

// TestInheritMultiLevelChain: c inherits b inherits a; c picks up a's fields not
// overridden along the chain.
func TestInheritMultiLevelChain(t *testing.T) {
	cfg := loadConfig(t, `
agents:
  a:
    command: ["a-tool"]
    color: "1"
    personality: pa
  b:
    inherit: a
    color: "2"
  c:
    inherit: b
    enabled: true
    role: [reviewer]
`)
	c := cfg.Agents["c"]
	if len(c.Command) == 0 || c.Command[0] != "a-tool" {
		t.Fatalf("c command = %v, want inherited a-tool", c.Command)
	}
	if c.Color != "2" {
		t.Fatalf("c color = %q, want b's 2", c.Color)
	}
	if c.Personality != "pa" {
		t.Fatalf("c personality = %q, want a's pa", c.Personality)
	}
}

// TestInheritCrossLayer is the idempotency proof: a global base is overridden by
// a local config that also defines a child inheriting it. After merge+resolution
// the child must reflect the LOCAL base, not the global one — confirming
// resolution runs once on the fully-merged map.
func TestInheritCrossLayer(t *testing.T) {
	global := loadConfig(t, `
agents:
  mybase:
    command: ["old"]
    terminal:
      submit_delay_ms: 100
`)
	local := []byte(`
agents:
  mybase:
    command: ["new", "--x"]
  mychild:
    inherit: mybase
    enabled: true
    role: [worker]
`)
	merged, err := ApplyLocalOverride(global, local)
	if err != nil {
		t.Fatal(err)
	}
	merged.ResolveInheritance()
	merged.Normalize()

	child := merged.Agents["mychild"]
	if !reflect.DeepEqual(child.Command, []string{"new", "--x"}) {
		t.Fatalf("child command = %v, want local base [new --x]", child.Command)
	}
	if child.Terminal.SubmitDelayMs != 100 {
		t.Fatalf("child submit_delay = %d, want inherited 100", child.Terminal.SubmitDelayMs)
	}
}

// TestInheritIdempotent: re-resolving an already-resolved config changes nothing.
func TestInheritIdempotent(t *testing.T) {
	cfg := loadConfig(t, `
experimental:
  setup_env: true
agents:
  base:
    command: ["tool", "--x"]
    env:
      A: "1"
    terminal:
      send_mode: paste
  child:
    inherit: base
    enabled: true
    env:
      B: "2"
`)
	before := cfg.Agents["child"]
	cfg.ResolveInheritance()
	cfg.ResolveInheritance()
	after := cfg.Agents["child"]
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("inheritance not idempotent:\n before %+v\n after  %+v", before, after)
	}
}

// TestInheritDepthLimitDoesNotHang: a chain far deeper than the cap terminates
// instead of recursing without bound, and shallow agents still resolve.
func TestInheritDepthLimitDoesNotHang(t *testing.T) {
	cfg := Config{Agents: map[string]AgentConfig{}}
	const n = maxInheritDepth + 10
	for i := 0; i < n; i++ {
		ac := AgentConfig{}
		if i == 0 {
			ac.Command = []string{"root"}
		} else {
			ac.Inherit = fmt.Sprintf("a%d", i-1)
		}
		cfg.Agents[fmt.Sprintf("a%d", i)] = ac
	}
	cfg.ResolveInheritance() // must return, not blow the stack
	if got := cfg.Agents["a1"].Command; len(got) == 0 || got[0] != "root" {
		t.Fatalf("a1 command = %v, want resolved root", got)
	}
}
