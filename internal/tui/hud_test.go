package tui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/command"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

func hudModel(t *testing.T, names ...string) Model {
	t.Helper()
	cfg := config.Config{UI: config.UIConfig{MaxScrollbackLines: 1000}}
	cfg.Normalize()
	sessions := make([]*agent.Session, 0, len(names))
	for _, name := range names {
		sessions = append(sessions, agent.NewSession(name, config.AgentConfig{}, ""))
	}
	m := NewModelWithConfig(sessions, nil, cfg, "", nil, 0, nil, nil)
	m.Width = 100
	m.Height = 30
	return m
}

func TestAdaptiveGridFollowsPaneCount(t *testing.T) {
	cases := []struct {
		agents     []string
		rows, cols int
	}{
		{[]string{"a"}, 1, 1},
		{[]string{"a", "b"}, 1, 2},
		{[]string{"a", "b", "c"}, 2, 2},
		{[]string{"a", "b", "c", "d"}, 2, 2},
		{[]string{"a", "b", "c", "d", "e"}, 2, 2}, // falls back to page_rows x page_cols
	}
	for _, c := range cases {
		m := hudModel(t, c.agents...)
		rows, cols := m.gridDims()
		if rows != c.rows || cols != c.cols {
			t.Fatalf("%d agents: grid %dx%d, want %dx%d", len(c.agents), rows, cols, c.rows, c.cols)
		}
	}

	// A manual adjustment locks the layout.
	m := hudModel(t, "a", "b")
	m.layoutLocked = true
	if rows, cols := m.gridDims(); rows != 2 || cols != 2 {
		t.Fatalf("locked grid = %dx%d, want configured 2x2", rows, cols)
	}

	// And so does disabling it in config.
	off := false
	m = hudModel(t, "a")
	m.Config.UI.AdaptiveGrid = &off
	if rows, cols := m.gridDims(); rows != 2 || cols != 2 {
		t.Fatalf("adaptive off grid = %dx%d, want configured 2x2", rows, cols)
	}
}

func TestPhaseRailRendering(t *testing.T) {
	p := &runProgress{
		Phases: []phaseInfo{
			{Label: "Plan", State: phaseDone, Done: 2, Expected: 2, Counted: true},
			{Label: "Vote", State: phaseActive, Done: 0, Expected: 2, Counted: true},
			{Label: "Build", State: phasePending},
			{Label: "Review", State: phasePending},
			{Label: "Adopt", State: phasePending},
		},
		Next: "/vote",
	}
	rail := p.phaseRail()
	for _, want := range []string{"Plan 2/2 ✓", "Vote 0/2 ●", "Build ○", "Adopt ○", "Next: /vote"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("rail missing %q: %q", want, rail)
		}
	}
}

func TestPaneBadgeStates(t *testing.T) {
	m := hudModel(t, "codex")
	view := m.Agents[0]

	if got := m.paneBadge(view); got != "running" {
		t.Fatalf("idle badge = %q, want running", got)
	}

	m.phase = "vote"
	m.watching = map[string]string{"codex": "/runs/x/votes/codex.md"}
	if got := m.paneBadge(view); got != "vote · waiting for codex.md" {
		t.Fatalf("waiting badge = %q", got)
	}
	view.PhaseDone = true
	if got := m.paneBadge(view); got != "vote · wrote codex.md" {
		t.Fatalf("done badge = %q", got)
	}
	view.PhaseDone = false
	view.Attention = true
	if got := m.paneBadge(view); got != "vote · needs input" {
		t.Fatalf("attention badge = %q", got)
	}

	// Build has no artifact watch; agents are just working.
	m.phase = "build"
	m.watching = nil
	view.Attention = false
	if got := m.paneBadge(view); got != "build · working" {
		t.Fatalf("build badge = %q", got)
	}
}

// feed pushes output through the same path Update uses and reports whether a
// confirmation check was requested.
func feed(m *Model, view *agentView, chunk string) bool {
	m.appendOutput(view, chunk)
	return m.noteAttentionOutput(view)
}

// goIdle backdates the pane's last output so the idle condition holds.
func goIdle(view *agentView) {
	view.lastOutputAt = view.lastOutputAt.Add(-2 * attentionIdleDelay)
}

func TestAttentionConfirmsBlockingApprovalPrompts(t *testing.T) {
	m := hudModel(t, "copilot")
	view := m.Agents[0]

	if feed(&m, view, "compiling project...\n") {
		t.Fatal("plain output must not become a candidate")
	}
	if !feed(&m, view, "Allow command \"git add -A\"? [y/N]\n") {
		t.Fatal("an on-screen approval prompt should become a candidate")
	}
	// Not flagged yet: the pane must first go quiet.
	if view.Attention {
		t.Fatal("attention must not fire before the idle confirmation")
	}
	m.runAttentionCheck()
	if view.Attention {
		t.Fatal("attention must not fire while the pane is still active")
	}
	goIdle(view)
	m.runAttentionCheck()
	if !view.Attention {
		t.Fatal("idle pane with a visible approval prompt should be flagged")
	}

	// Sending to the agent clears the flag (the user engaged).
	view.clearAttention()
	if view.Attention {
		t.Fatal("clearAttention failed")
	}
}

func TestAttentionIgnoresConversationalQuestions(t *testing.T) {
	// The exact false positive from a real run: codex's greeting matched the
	// old bare "do you want to" pattern.
	m := hudModel(t, "codex")
	view := m.Agents[0]

	if feed(&m, view, "Hi. What do you want to work on in /Users/x/dev/learning/counsil?\n") {
		t.Fatal("a conversational question must not become a candidate")
	}
	goIdle(view)
	m.runAttentionCheck()
	if view.Attention {
		t.Fatal("a conversational question must never flag attention")
	}
}

func TestAttentionAutoClearsWhenAgentMovesOn(t *testing.T) {
	m := hudModel(t, "claude")
	view := m.Agents[0]

	feed(&m, view, "Do you want to proceed? [y/N]\n")
	goIdle(view)
	m.runAttentionCheck()
	if !view.Attention {
		t.Fatal("setup: prompt should be flagged")
	}

	// The user answered inside the tool; the agent scrolls on. Push the
	// prompt off the visible tail and the auto-flag dismisses itself.
	var scroll strings.Builder
	for i := 0; i < attentionTailLines+45; i++ {
		scroll.WriteString("working on step...\n")
	}
	feed(&m, view, scroll.String())
	if view.Attention {
		t.Fatal("auto-set attention should clear once the prompt leaves the screen")
	}

	// A manual flag survives agent output.
	view.Attention = true
	view.AttentionManual = true
	feed(&m, view, "more output\n")
	if !view.Attention {
		t.Fatal("manual /attention must not be auto-cleared by output")
	}
}

func TestAttentionDetectionCanBeDisabled(t *testing.T) {
	m := hudModel(t, "claude")
	off := false
	m.Config.UI.DetectApprovalPrompts = &off
	view := m.Agents[0]

	if feed(&m, view, "Do you want to proceed? [y/N]\n") {
		t.Fatal("disabled detection must not schedule checks")
	}
	goIdle(view)
	m.runAttentionCheck()
	if view.Attention {
		t.Fatal("disabled detection must never flag attention")
	}
}

func TestContextHintPrioritizesBlockedPanes(t *testing.T) {
	m := hudModel(t, "copilot", "codex")
	m.progress = &runProgress{Next: "/vote"}

	hint, ok := m.contextHint()
	if !ok || !strings.Contains(hint, "Next: /vote") {
		t.Fatalf("idle hint = %q, %v", hint, ok)
	}

	m.Agents[0].Attention = true
	hint, ok = m.contextHint()
	if !ok || !strings.Contains(hint, "copilot may need input") || !strings.Contains(hint, "/nudge copilot") {
		t.Fatalf("blocked hint = %q", hint)
	}
}

func TestCompressPathShortensHome(t *testing.T) {
	var home, subPath, otherPath string
	if runtime.GOOS == "windows" {
		home = `C:\Users\example`
		subPath = `C:\Users\example\dev\x`
		otherPath = `C:\tmp\other`
	} else {
		home = "/Users/example"
		subPath = "/Users/example/dev/x"
		otherPath = "/tmp/other"
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := "~" + string(filepath.Separator) + filepath.Join("dev", "x")
	if got := compressPath(subPath); got != want {
		t.Fatalf("compressPath = %q, want %q", got, want)
	}
	if got := compressPath(otherPath); got != otherPath {
		t.Fatalf("non-home path changed: %q", got)
	}
}

func TestCommandPaletteFiltersAndNavigates(t *testing.T) {
	m := hudModel(t, "a")
	m.PromptInput = "/"

	if !m.paletteActive() {
		t.Fatal("palette should open on /")
	}
	matches := m.paletteMatches()
	if len(matches) < 10 {
		t.Fatalf("bare / should list all commands, got %d", len(matches))
	}
	// With no run, /plan is the top stage suggestion.
	if matches[0].Name != "plan" {
		t.Fatalf("idle palette top = %q, want plan", matches[0].Name)
	}

	// Arrow navigation wraps.
	if !m.movePaletteSelection(1) || m.CmdSuggestIndex != 1 {
		t.Fatalf("down: index = %d, want 1", m.CmdSuggestIndex)
	}
	if !m.movePaletteSelection(-1) || m.CmdSuggestIndex != 0 {
		t.Fatalf("up: index = %d, want 0", m.CmdSuggestIndex)
	}
	m.movePaletteSelection(-1)
	if m.CmdSuggestIndex != len(matches)-1 {
		t.Fatalf("up from top should wrap to %d, got %d", len(matches)-1, m.CmdSuggestIndex)
	}

	// Filtering narrows; completion fills the selected command.
	m.PromptInput = "/re"
	m.CmdSuggestIndex = 0
	filtered := m.paletteMatches()
	for _, c := range filtered {
		if !strings.HasPrefix(c.Name, "re") {
			t.Fatalf("filter leaked %q", c.Name)
		}
	}
	if !m.acceptPaletteSelection() {
		t.Fatal("accept should complete the selection")
	}
	if !strings.HasPrefix(m.PromptInput, "/re") || !strings.HasSuffix(m.PromptInput, " ") {
		t.Fatalf("completed input = %q", m.PromptInput)
	}

	// A space (args mode) closes the palette.
	if m.paletteActive() {
		t.Fatal("palette should close once the command word is complete")
	}
}

func TestStageCommandsFollowThePipeline(t *testing.T) {
	m := hudModel(t, "a")

	top := func() string { return m.stageCommandNames()[0] }

	if top() != "plan" {
		t.Fatalf("no run: top = %q, want plan", top())
	}
	m.progress = &runProgress{Plans: 2}
	if top() != "vote" {
		t.Fatalf("plans done: top = %q, want vote", top())
	}
	m.progress = &runProgress{Plans: 2, PlanWinner: "a"}
	if top() != "build" {
		t.Fatalf("vote done: top = %q, want build", top())
	}
	m.progress = &runProgress{Plans: 2, PlanWinner: "a", Diffs: 2}
	if top() != "review" {
		t.Fatalf("builds done: top = %q, want review", top())
	}
	m.progress = &runProgress{Plans: 2, PlanWinner: "a", Diffs: 2, BuildWinner: "a"}
	if top() != "compare" {
		t.Fatalf("review done: top = %q, want compare", top())
	}
	m.progress = &runProgress{Adopted: "a"}
	if top() != "report" {
		t.Fatalf("adopted: top = %q, want report", top())
	}
	m.phase = "vote"
	if top() != "finish" {
		t.Fatalf("in vote: top = %q, want finish", top())
	}
	m.Agents[0].Attention = true
	if top() != "attention" {
		t.Fatalf("blocked pane: top = %q, want attention", top())
	}
}

func indexOfMatch(matches []command.Composer, name string) int {
	for i, c := range matches {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func TestRecordRecentCommandDedupesAndCaps(t *testing.T) {
	m := hudModel(t, "a")
	m.recordRecentCommand("all")
	m.recordRecentCommand("send")
	m.recordRecentCommand("all") // re-used: moves to front, no duplicate
	if got := strings.Join(m.recentCommands, ","); got != "all,send" {
		t.Fatalf("recent = %q, want %q", got, "all,send")
	}

	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		m.recordRecentCommand(name)
	}
	if len(m.recentCommands) != 6 {
		t.Fatalf("recent capped at %d, want 6", len(m.recentCommands))
	}
	if m.recentCommands[0] != "g" {
		t.Fatalf("newest recent = %q, want g", m.recentCommands[0])
	}
}

func TestHandleCommandRecordsRecentCanonical(t *testing.T) {
	m := hudModel(t, "a")
	m.handleCommand("/help")
	if len(m.recentCommands) == 0 || m.recentCommands[0] != "help" {
		t.Fatalf("recent after /help = %v, want help first", m.recentCommands)
	}
	// An alias is recorded under its canonical name.
	m.handleCommand("/exit")
	if m.recentCommands[0] != "quit" {
		t.Fatalf("recent after /exit = %v, want quit first", m.recentCommands)
	}
	// Unknown commands are not recorded.
	m.handleCommand("/nope")
	if m.recentCommands[0] != "quit" {
		t.Fatalf("unknown command polluted recents: %v", m.recentCommands)
	}
}

func TestPaletteOrdersRecentAfterRecommended(t *testing.T) {
	m := hudModel(t, "a")
	m.recordRecentCommand("quit") // declared last, never stage-recommended
	m.PromptInput = "/"

	matches := m.paletteMatches()
	if matches[0].Name != "plan" {
		t.Fatalf("recommended should stay first, got %q", matches[0].Name)
	}
	quitAt := indexOfMatch(matches, "quit")
	sendAt := indexOfMatch(matches, "send")
	if quitAt == -1 || sendAt == -1 {
		t.Fatalf("expected both quit and send in matches (quit=%d send=%d)", quitAt, sendAt)
	}
	if quitAt >= sendAt {
		t.Fatalf("recent /quit (%d) should sort ahead of declaration-order /send (%d)", quitAt, sendAt)
	}
}

func TestCommandDisabledReasonMirrorsGuards(t *testing.T) {
	m := hudModel(t, "a") // orch == nil

	if got := m.commandDisabledReason("build"); got != "needs a git repo" {
		t.Fatalf("build disabled = %q, want needs a git repo", got)
	}
	if got := m.commandDisabledReason("start-build"); got != "run /build first" {
		t.Fatalf("start-build disabled = %q", got)
	}
	if got := m.commandDisabledReason("finish"); got != "no phase in progress" {
		t.Fatalf("finish disabled = %q", got)
	}
	for _, always := range []string{"help", "runs", "settings"} {
		if got := m.commandDisabledReason(always); got != "" {
			t.Fatalf("%s should never be disabled, got %q", always, got)
		}
	}

	// Preconditions met: the reasons clear.
	m.phase = "vote"
	if got := m.commandDisabledReason("finish"); got != "" {
		t.Fatalf("finish in a phase should be enabled, got %q", got)
	}
	m.pendingBuild = map[string]string{"a": "do it"}
	if got := m.commandDisabledReason("start-build"); got != "" {
		t.Fatalf("start-build with staged work should be enabled, got %q", got)
	}
}

func TestPaletteNextHintFollowsProgress(t *testing.T) {
	m := hudModel(t, "a")
	if hint := m.paletteNextHint(); !strings.Contains(hint, "/plan") {
		t.Fatalf("idle next hint = %q, want /plan", hint)
	}
	m.progress = &runProgress{Next: "/vote"}
	if hint := m.paletteNextHint(); !strings.Contains(hint, "/vote") {
		t.Fatalf("running next hint = %q, want /vote", hint)
	}
}

func TestPaletteRendersKeysReasonsAndNext(t *testing.T) {
	m := hudModel(t, "a")

	m.PromptInput = "/"
	if joined := strings.Join(m.renderPalette(), "\n"); !strings.Contains(joined, "recommended next:") {
		t.Fatalf("palette header missing recommended next:\n%s", joined)
	}

	// A command with a keybinding shows it.
	m.PromptInput = "/overview"
	if joined := strings.Join(m.renderPalette(), "\n"); !strings.Contains(joined, "Ctrl+G") {
		t.Fatalf("palette should show the /overview keybinding:\n%s", joined)
	}

	// A command that can't run yet is shown with its reason.
	m.PromptInput = "/build"
	if joined := strings.Join(m.renderPalette(), "\n"); !strings.Contains(joined, "disabled — needs a git repo") {
		t.Fatalf("palette should explain why /build is disabled:\n%s", joined)
	}
}

func TestFilePaletteIsVerticalAndNavigable(t *testing.T) {
	m := hudModel(t, "a")
	m.FileChoices = []string{"app.js", "index.html", "styles.css"}
	m.PromptInput = "look at @s"

	if !m.filePaletteActive() {
		t.Fatal("file palette should open on @query")
	}
	lines := m.renderFilePalette()
	if len(lines) < 2 {
		t.Fatalf("file palette should be vertical, got %d line(s)", len(lines))
	}
	if !m.moveFileSuggestion(1) {
		t.Fatal("down should move the file selection")
	}

	// The command palette never hijacks an @file query, and vice versa.
	if m.paletteActive() {
		t.Fatal("command palette must stay closed during @file input")
	}
}

func TestPaneColorFallsBackToPersonality(t *testing.T) {
	m := hudModel(t, "codex")
	if got := m.paneColor("codex"); got != "" {
		t.Fatalf("unconfigured agent color = %q, want empty", got)
	}
	agentCfg := m.Config.Agents["codex"]
	agentCfg.Personality = "critic"
	m.Config.Agents["codex"] = agentCfg
	m.Config.Personalities = map[string]config.PersonalityConfig{"critic": {Color: "203"}}
	if got := m.paneColor("codex"); got != "203" {
		t.Fatalf("personality color = %q, want 203", got)
	}
	agentCfg.Color = "39"
	m.Config.Agents["codex"] = agentCfg
	if got := m.paneColor("codex"); got != "39" {
		t.Fatalf("agent color should win, got %q", got)
	}
}

func TestSyntheticViewerEscReturnsToPanes(t *testing.T) {
	m := hudModel(t, "a")
	// A synthetic view (e.g. /compare) — Esc must land on the panes even when
	// an artifacts list exists from earlier browsing.
	m.Artifacts = []artifactEntry{{Label: "plan: a.md", Path: "/tmp/a.md"}}
	m.openArtifactText("compare builds", "table")
	m.closeArtifactView()
	if m.ScreenMode != ScreenPanes {
		t.Fatalf("Esc from synthetic view: screen = %v, want panes", m.ScreenMode)
	}

	// A file opened from the list returns to the list.
	m.ScreenMode = ScreenArtifacts
	m.artifactView = "body"
	m.artifactPath = "/tmp/a.md"
	m.artifactFile = "/tmp/a.md"
	m.viewerFromList = true
	m.closeArtifactView()
	if m.ScreenMode != ScreenArtifacts {
		t.Fatalf("Esc from list-opened file: screen = %v, want artifacts list", m.ScreenMode)
	}
}

func TestPaletteOverlaysBodyWithoutReflow(t *testing.T) {
	m := hudModel(t, "a", "b")
	m.resizeAgents()
	m.appendOutput(m.Agents[0], "TOPLINE\n")

	lineOf := func(view string, needle string) int {
		for i, line := range strings.Split(view, "\n") {
			if strings.Contains(line, needle) {
				return i
			}
		}
		return -1
	}

	closed := m.View()
	closedLines := strings.Count(closed, "\n") + 1

	m.PromptInput = "/"
	open := m.View()
	openLines := strings.Count(open, "\n") + 1

	if closedLines != openLines {
		t.Fatalf("view height changed when palette opened: %d -> %d", closedLines, openLines)
	}
	before, after := lineOf(closed, "TOPLINE"), lineOf(open, "TOPLINE")
	if before == -1 || before != after {
		t.Fatalf("pane content moved when palette opened: row %d -> %d", before, after)
	}
	if !strings.Contains(open, "suggested for this stage") {
		t.Fatal("palette not rendered")
	}
}

func TestPaneBorderColorsStayIndexed(t *testing.T) {
	// Output must be 256-color INDICES (>= 16): the cube/gray indices render
	// identically in every emulator, while truecolor and SGR-faint proved
	// unreliable (VS Code). The focused color keeps the configured index; the
	// muted variant is a computed darker index, never a terminal attribute.
	focused, muted, ok := paneBorderColors("81")
	if !ok || focused != "81" {
		t.Fatalf("index 81 focused = %q (%v), want 81", focused, ok)
	}
	if muted != "23" { // 45%% of #5fd7ff -> dark teal #005f5f
		t.Fatalf("muted 81 = %q, want 23", muted)
	}
	_, muted, ok = paneBorderColors("203")
	if !ok || muted != "52" { // dimmed salmon stays red (#5f0000), not gray
		t.Fatalf("muted 203 = %q (%v), want 52", muted, ok)
	}
	focused, _, ok = paneBorderColors("#ff5f5f")
	if !ok || focused != "203" {
		t.Fatalf("hex #ff5f5f = %q (%v), want index 203", focused, ok)
	}
	if _, _, ok := paneBorderColors("magenta"); ok {
		t.Fatal("named colors are not resolvable here")
	}
}

func TestCompareScreenNavigationAndDiffPager(t *testing.T) {
	m := hudModel(t, "a")
	m.CompareRows = []orchestrate.BuildComparison{
		{Agent: "worker-a", Letter: "A", Files: 2, CheckStatus: "PASS"},
		{Agent: "worker-b", Letter: "B", Files: 3, CheckStatus: "PASS", Winner: true},
	}
	m.ScreenMode = ScreenCompare

	// Row navigation and pair-marking.
	updated, _ := m.handleCompareKey(keyMsg("down"))
	m = *updated.(*Model)
	if m.CompareIndex != 1 {
		t.Fatalf("down: index = %d", m.CompareIndex)
	}
	updated, _ = m.handleCompareKey(keyMsg("x"))
	m = *updated.(*Model)
	if m.compareMarked != "worker-b" {
		t.Fatalf("mark = %q", m.compareMarked)
	}
	updated, _ = m.handleCompareKey(keyMsg("x"))
	m = *updated.(*Model)
	if m.compareMarked != "" {
		t.Fatal("second x should unmark")
	}

	// File level: navigation and Esc unwinding.
	m.compareFiles = &compareFileSet{
		Title:  "worker-a vs base",
		AgentA: "worker-a",
		Files: []orchestrate.DiffFile{
			{Path: "app.js", Status: "M", Patch: "diff --git a/app.js b/app.js\n+new\n-old\n"},
			{Path: "index.html", Status: "A", Patch: "diff --git a/index.html b/index.html\n+hi\n"},
		},
	}
	updated, _ = m.handleCompareKey(keyMsg("down"))
	m = *updated.(*Model)
	if m.CompareFileIndex != 1 {
		t.Fatalf("file down: index = %d", m.CompareFileIndex)
	}
	updated, _ = m.handleCompareKey(keyMsg("enter"))
	m = *updated.(*Model)
	if m.ScreenMode != ScreenArtifacts || !m.artifactIsDiff {
		t.Fatalf("enter should open the diff pager (screen=%v isDiff=%v)", m.ScreenMode, m.artifactIsDiff)
	}
	// Esc from the diff pager returns to compare, not panes.
	m.closeArtifactView()
	if m.ScreenMode != ScreenCompare {
		t.Fatalf("pager Esc: screen = %v, want compare", m.ScreenMode)
	}
	// Esc from files returns to rows; Esc from rows returns to panes.
	updated, _ = m.handleCompareKey(keyMsg("esc"))
	m = *updated.(*Model)
	if m.compareFiles != nil {
		t.Fatal("esc should leave the file level")
	}
	updated, _ = m.handleCompareKey(keyMsg("esc"))
	m = *updated.(*Model)
	if m.ScreenMode != ScreenPanes {
		t.Fatalf("esc from rows: screen = %v, want panes", m.ScreenMode)
	}
}

func TestColorDiffLineStyles(t *testing.T) {
	if colorDiffLine("+added line", 80) == "+added line" {
		t.Fatal("added lines should be styled")
	}
	if colorDiffLine("-removed", 80) == "-removed" {
		t.Fatal("removed lines should be styled")
	}
	if colorDiffLine("plain context", 80) != fitText("plain context", 80) {
		t.Fatal("context lines stay unstyled")
	}
}
