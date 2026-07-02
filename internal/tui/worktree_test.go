package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

func initWTRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	// Resolve symlinks so paths compare equal to what git emits (macOS /var).
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir
}

func wtModel(t *testing.T, repo string, freestyle bool, names ...string) (*Model, []*agent.Session) {
	t.Helper()
	agents := map[string]config.AgentConfig{}
	sessions := make([]*agent.Session, 0, len(names))
	for _, n := range names {
		ac := config.AgentConfig{Enabled: true, Command: []string{"true"}, CWD: repo, Usage: config.AgentUsageConfig{Tool: "claude"}}
		agents[n] = ac
		sessions = append(sessions, agent.NewSession(n, ac, ""))
	}
	cfg := config.Config{Agents: agents, Worktrees: config.WorktreesConfig{Freestyle: freestyle}}
	cfg.Normalize()
	m := NewModelWithConfig(sessions, nil, cfg, "", nil, 0, func(*agent.Session) {}, nil)
	if freestyle {
		m.SetFreeWorktrees(orchestrate.NewFreeWorktrees(repo, cfg.Worktrees))
	}
	return &m, sessions
}

// With the feature off, freestyle panes keep the launch cwd.
func TestStartAllFreestyleOff(t *testing.T) {
	repo := initWTRepo(t)
	m, sessions := wtModel(t, repo, false, "claude-a", "claude-b")
	m.startAll()
	for _, s := range sessions {
		if s.Config.CWD != repo {
			t.Errorf("%s cwd = %q, want the launch dir %q (feature off)", s.Name, s.Config.CWD, repo)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".council", "workspaces")); !os.IsNotExist(err) {
		t.Fatalf("no workspaces should be created when the feature is off (err=%v)", err)
	}
}

// With the feature on, each freestyle pane boots in its own worktree cwd.
func TestStartAllFreestyleOn(t *testing.T) {
	repo := initWTRepo(t)
	m, sessions := wtModel(t, repo, true, "claude-a", "claude-b")
	m.startAll()
	cwdA := sessions[0].Config.CWD
	cwdB := sessions[1].Config.CWD
	wantA := filepath.Join(repo, ".council", "workspaces", "claude-a")
	wantB := filepath.Join(repo, ".council", "workspaces", "claude-b")
	if cwdA != wantA || cwdB != wantB {
		t.Fatalf("freestyle cwds = %q,%q want %q,%q", cwdA, cwdB, wantA, wantB)
	}
	if cwdA == cwdB {
		t.Fatal("same-tool panes must get distinct worktree cwds")
	}
}

// A per-agent `worktree: false` opt-out keeps that pane in the launch dir even
// when the feature is on — the mitigation for tools (e.g. copilot) that need the
// live working tree a clean detached checkout lacks.
func TestStartAllPerAgentOptOut(t *testing.T) {
	repo := initWTRepo(t)
	no := false
	agents := map[string]config.AgentConfig{
		"claude-a": {Enabled: true, Command: []string{"true"}, CWD: repo, Usage: config.AgentUsageConfig{Tool: "claude"}},
		"copilot":  {Enabled: true, Command: []string{"true"}, CWD: repo, Usage: config.AgentUsageConfig{Tool: "copilot"}, Worktree: &no},
	}
	cfg := config.Config{Agents: agents, Worktrees: config.WorktreesConfig{Freestyle: true}}
	cfg.Normalize()
	sessions := []*agent.Session{
		agent.NewSession("claude-a", agents["claude-a"], ""),
		agent.NewSession("copilot", agents["copilot"], ""),
	}
	m := NewModelWithConfig(sessions, nil, cfg, "", nil, 0, func(*agent.Session) {}, nil)
	m.SetFreeWorktrees(orchestrate.NewFreeWorktrees(repo, cfg.Worktrees))
	m.startAll()

	if got := sessions[0].Config.CWD; got != filepath.Join(repo, ".council", "workspaces", "claude-a") {
		t.Errorf("claude-a should get a worktree, cwd = %q", got)
	}
	if got := sessions[1].Config.CWD; got != repo {
		t.Errorf("opted-out copilot should stay in the launch dir, cwd = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".council", "workspaces", "copilot")); !os.IsNotExist(err) {
		t.Fatalf("no worktree should be created for the opted-out agent (err=%v)", err)
	}
}

// A phase relaunch (m.phase != "") must NOT be relocated into a freestyle
// worktree — orchestration keeps its own cwd routing.
func TestStartAllSkipsFreestyleDuringPhase(t *testing.T) {
	repo := initWTRepo(t)
	m, sessions := wtModel(t, repo, true, "claude-a")
	m.phase = "plan"
	m.startAll()
	if sessions[0].Config.CWD != repo {
		t.Errorf("during a phase, cwd = %q, want the launch dir %q (no freestyle relocation)", sessions[0].Config.CWD, repo)
	}
	if _, err := os.Stat(filepath.Join(repo, ".council", "workspaces")); !os.IsNotExist(err) {
		t.Fatal("no freestyle workspace should be created during an orchestration phase")
	}
}

func TestWorktreeMarker(t *testing.T) {
	m := Model{
		freeWorktrees: &orchestrate.FreeWorktrees{},
		worktreeStatus: map[string]worktreeState{
			"behind": {behind: true},
			"dirty":  {dirty: true},
			"both":   {behind: true, dirty: true},
			"fresh":  {},
		},
	}
	for name, want := range map[string]string{
		"behind":  " ⟳",
		"dirty":   " *",
		"both":    " ⟳*",
		"fresh":   "",
		"unknown": "",
	} {
		if got := m.worktreeMarker(name); got != want {
			t.Errorf("worktreeMarker(%q) = %q, want %q", name, got, want)
		}
	}
	// Off: no manager → never a marker.
	off := Model{worktreeStatus: map[string]worktreeState{"x": {behind: true}}}
	if got := off.worktreeMarker("x"); got != "" {
		t.Errorf("marker with feature off = %q, want empty", got)
	}
}

// /refresh refuses a dirty worktree without force and resets it with force.
func TestCmdRefreshGuard(t *testing.T) {
	repo := initWTRepo(t)
	m, sessions := wtModel(t, repo, true, "a")
	m.FocusedIndex = 0
	m.startAll() // creates the worktree for "a"
	wt := sessions[0].Config.CWD
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dirty + no force → refused, work preserved.
	m.cmdRefresh("a")
	if data, _ := os.ReadFile(filepath.Join(wt, "README.md")); string(data) != "changed" {
		t.Fatal("/refresh without force must not discard uncommitted changes")
	}
	// force → reset.
	m.cmdRefresh("a force")
	if data, _ := os.ReadFile(filepath.Join(wt, "README.md")); string(data) != "hi" {
		t.Fatalf("/refresh force should have reset the worktree, got %q", data)
	}
}
