package tui

// In-chat orchestration: /plan, /vote, /build, /review, /adopt, resume, and
// the phase lifecycle (prompt broadcast, artifact polling, finish).

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	runstore "github.com/umutarmut38/council/internal/session"
)

func (m *Model) cmdPlan(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable (run council inside a git repo)"
		return nil
	}
	issue := strings.TrimSpace(m.expandRefs(rest))
	if issue == "" {
		m.Status = "usage: /plan <issue or @file>"
		return nil
	}
	if err := m.orch.StartRun(issue); err != nil {
		m.Status = "plan: " + err.Error()
		return nil
	}
	if !m.scopePhaseOrWarn("plan") {
		return nil
	}
	prompts, err := m.orch.PlanPrompts()
	if err != nil {
		m.Status = "plan: " + err.Error()
		return nil
	}
	m.beginPhase("plan", config.PhasePlan, prompts)
	m.Status = "planning — run " + m.orch.Run().Stamp
	return m.phaseCmds(m.orch.InteractivePrompts(config.PhasePlan, prompts))
}

func (m *Model) cmdVote() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "vote: " + err.Error()
			return nil
		}
	}
	// A single plan has nothing to rank — auto-select it and skip the vote,
	// mirroring how review collapses to one surviving build.
	found, _, err := m.orch.CollectPlans()
	if err != nil {
		m.Status = "vote: " + err.Error()
		return nil
	}
	switch len(found) {
	case 0:
		m.Status = "vote: no plans found; run /plan first"
		return nil
	case 1:
		var name string
		for n := range found {
			name = n
		}
		if err := m.orch.SetSinglePlanWinner(name); err != nil {
			m.Status = "vote: " + err.Error()
			return nil
		}
		m.refreshProgress()
		m.Status = "only one plan (" + name + ") — /build (or /refine)"
		return nil
	}
	if !m.scopePhaseOrWarn("vote") {
		return nil
	}
	prompts, err := m.orch.VotePrompts()
	if err != nil {
		m.Status = "vote: " + err.Error()
		return nil
	}
	m.beginPhase("vote", config.PhaseVote, prompts)
	m.Status = "voting on plans"
	return m.phaseCmds(m.orch.InteractivePrompts(config.PhaseVote, prompts))
}

func (m *Model) cmdBuild() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "build: " + err.Error()
			return nil
		}
	}
	if !m.scopePhaseOrWarn("build") {
		return nil
	}
	// BuildPrompt creates/resets the worktrees and returns the prompt. We launch
	// the agents in their worktrees now but DO NOT send the prompt yet, so the
	// user can adjust things in each tool first and then run /start-build.
	prompt, err := m.orch.BuildPrompt()
	if err != nil {
		m.Status = "build: " + err.Error()
		return nil
	}
	// Key the prompt by the build participants (workers), not the current panes:
	// after /vote the panes are the reviewers, so keying by panes would stage
	// nothing for /start-build.
	prompts := map[string]string{}
	for _, name := range m.orch.AgentsForPhase(config.PhaseBuild) {
		prompts[name] = prompt
	}
	if len(prompts) == 0 {
		m.Status = "build: no worker agents (role: [worker]) to build"
		return nil
	}
	m.beginPhase("build", config.PhaseBuild, prompts)
	m.pendingBuild = m.orch.InteractivePrompts(config.PhaseBuild, prompts)
	m.Status = "build ready in worktrees — adjust the tools, then /start-build"
	return nil
}

// cmdStartBuild sends the staged build prompt to the agents prepared by /build.
func (m *Model) cmdStartBuild() tea.Cmd {
	if len(m.pendingBuild) == 0 {
		m.Status = "nothing staged — run /build first"
		return nil
	}
	prompts := m.pendingBuild
	m.pendingBuild = nil
	m.Status = "build started"
	return func() tea.Msg { return phasePromptsMsg(prompts) }
}

// cmdReview gates the built implementations (run check command per worktree,
// drop failures) off the UI thread, then continues in handleReviewReady.
func (m *Model) cmdReview() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "review: " + err.Error()
			return nil
		}
	}
	if !m.scopePhaseOrWarn("review") {
		return nil
	}
	m.Status = "running build checks…"
	orch := m.orch
	return func() tea.Msg {
		prompts, survivors, err := orch.ReviewPrompts()
		return reviewReadyMsg{prompts: prompts, survivors: survivors, err: err}
	}
}

func (m Model) handleReviewReady(msg reviewReadyMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Status = "review: " + msg.err.Error()
		return m, nil
	}
	switch len(msg.survivors) {
	case 0:
		m.Status = "no builds passed the checks"
		return m, nil
	case 1:
		_ = m.orch.SetSingleWinner(msg.survivors[0])
		m.Status = "only " + msg.survivors[0] + " passed — /adopt to apply it"
		m.refreshProgress()
		return m, nil
	default:
		m.beginPhase("review", config.PhaseReview, msg.prompts)
		m.Status = fmt.Sprintf("reviewing %d builds", len(msg.survivors))
		return m, m.phaseCmds(m.orch.InteractivePrompts(config.PhaseReview, msg.prompts))
	}
}

// scopePhaseOrWarn restricts the next orchestration phase to the agents picked
// by the current /target (all/focus/personality/category). Returns false (and
// sets a status) when the target matches no agents.
func (m *Model) scopePhaseOrWarn(phase string) bool {
	names := m.scopedAgentNames()
	m.orch.SetScope(names)
	if names != nil && len(names) == 0 {
		label := m.TargetName
		if label == "" {
			label = "the current target"
		}
		m.Status = phase + ": no agents match " + label
		return false
	}
	if names != nil {
		m.Status = phase + ": " + strings.Join(names, ", ")
	}
	return true
}

// scopedAgentNames returns the agents selected by the current /target, or nil
// for "all".
func (m *Model) scopedAgentNames() []string {
	if m.orch == nil {
		return nil
	}
	switch m.Target {
	case TargetFocused:
		return []string{m.focusedName()}
	case TargetPersonality:
		return m.agentsMatching(func(name string) bool {
			pn, _, ok := m.Config.PersonalityForAgent(name)
			return ok && pn == m.TargetName
		})
	case TargetCategory:
		return m.agentsMatching(func(name string) bool {
			pn, _, ok := m.Config.PersonalityForAgent(name)
			if !ok {
				return false
			}
			cn, _, ok2 := m.Config.CategoryForPersonality(pn)
			return ok2 && cn == m.TargetName
		})
	default:
		return nil // TargetAll
	}
}

func (m *Model) agentsMatching(pred func(string) bool) []string {
	out := []string{}
	for _, name := range m.orch.Agents() {
		if pred(name) {
			out = append(out, name)
		}
	}
	return out
}

// cmdAdopt applies a build's diff to the working tree. It opens the same
// full-screen preview as /preview (files, dirty-tree warning, the diff) and
// waits for an explicit y — a status-line-only confirmation proved far too
// easy to miss, leaving users certain they had adopted when nothing was
// applied. With no argument it adopts the reviewed winner; "/adopt <agent>"
// overrides. policy.mode: aggressive applies immediately.
func (m *Model) cmdAdopt(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "adopt: " + err.Error()
			return nil
		}
	}
	arg := strings.TrimSpace(rest)

	if strings.EqualFold(arg, "confirm") {
		if m.pendingAdopt == nil {
			m.Status = "nothing staged — run /adopt first"
			return nil
		}
		return m.applyAdopt(m.pendingAdopt.Agent)
	}

	if !m.Config.Policy.ConfirmDestructive() {
		plan, err := m.orch.PlanAdopt(arg)
		if err != nil {
			m.Status = "adopt: " + err.Error()
			return nil
		}
		if plan.CheckError != "" {
			m.Status = fmt.Sprintf("adopt: %s's diff does not apply cleanly: %s", plan.Agent, firstLine(plan.CheckError))
			return nil
		}
		return m.applyAdopt(plan.Agent)
	}
	m.cmdPreview(arg)
	return nil
}

func (m *Model) applyAdopt(agentName string) tea.Cmd {
	m.pendingAdopt = nil
	adopted, files, err := m.orch.Adopt(agentName)
	if err != nil {
		m.Status = "adopt: " + err.Error()
		return nil
	}
	m.Status = fmt.Sprintf("applied %s's changes (%d files, uncommitted) — review with `git diff`, then commit", adopted, len(files))
	m.refreshProgress()
	return nil
}

// previewDiffLimit caps how much diff body the preview pager loads; beyond
// this the viewer points at the file on disk instead of rendering it.
const previewDiffLimit = 512 << 10

// cmdPreview shows what /adopt would change — files, dirty-tree overlap, and
// the full diff content — without touching the tree. A clean preview is also
// staged, so `/adopt confirm` right after applies exactly what was shown.
func (m *Model) cmdPreview(rest string) {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "preview: " + err.Error()
			return
		}
	}
	plan, err := m.orch.PlanAdopt(strings.TrimSpace(rest))
	if err != nil {
		m.Status = "preview: " + err.Error()
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Adopt preview: %s\n\nDiff: %s\n\n## Files (%d)\n\n", plan.Agent, plan.DiffPath, len(plan.Files))
	for _, f := range plan.Files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	if len(plan.DirtyFiles) > 0 {
		fmt.Fprintf(&b, "\n## Uncommitted working-tree files (%d)\n\n", len(plan.DirtyFiles))
		for _, f := range plan.DirtyFiles {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	status := ""
	if plan.CheckError != "" {
		fmt.Fprintf(&b, "\n## git apply --check FAILED\n\n%s\n", plan.CheckError)
		m.pendingAdopt = nil
		status = fmt.Sprintf("preview %s: diff does NOT apply cleanly", plan.Agent)
	} else {
		b.WriteString("\ngit apply --check: OK\n\nApply this diff to your working tree?  y = apply now · n = cancel · Esc = close (then /adopt confirm)\n")
		staged := plan
		m.pendingAdopt = &staged
		status = fmt.Sprintf("previewing %s — y applies, n cancels", plan.Agent)
	}

	// The diff itself, so the change is inspectable without leaving the TUI.
	if fi, statErr := os.Stat(plan.DiffPath); statErr == nil && fi.Size() > previewDiffLimit {
		fmt.Fprintf(&b, "\n## Diff\n\n(%d bytes — too large to render inline; open %s)\n", fi.Size(), plan.DiffPath)
	} else if data, readErr := os.ReadFile(plan.DiffPath); readErr == nil {
		fmt.Fprintf(&b, "\n## Diff\n\n%s\n", strings.TrimRight(string(data), "\n"))
	}

	m.openArtifactText("adopt preview: "+plan.Agent, b.String())
	m.artifactFile = plan.DiffPath // `e` opens the diff in $EDITOR
	m.Status = status
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// cmdStatus opens a persistent full-screen snapshot of the run: the phase rail
// (with the pinned winner), live counts, and — reusing the same section builders as
// /report — the plan vote breakdown (winner, Borda tally, per-voter ballots) and the
// build/review/adopt outcome. It works the same in every phase because empty sections
// are simply omitted.
func (m *Model) cmdStatus() {
	if m.orch == nil || m.orch.Run() == nil {
		m.Status = "no active run"
		return
	}
	run := m.orch.Run()
	phase := m.phase
	if phase == "" {
		phase = "idle"
	}

	// Read the run once from disk and drive both the rail/counts and the report
	// off the same snapshot, so the two halves can't disagree and we don't pay
	// for SummarizeRun twice.
	summary, err := orchestrate.SummarizeRun(run.RootDir, run.Stamp)
	if err != nil {
		m.Status = "status: " + err.Error()
		return
	}
	m.progress = m.progressFromSummary(summary)

	var b strings.Builder
	fmt.Fprintf(&b, "# Status — run %s · phase %s\n\n", run.Stamp, phase)
	if m.progress != nil {
		b.WriteString(m.progress.phaseRail())
		fmt.Fprintf(&b, "\n\nPlans %d · Votes %d · Diffs %d · Reviews %d\n",
			m.progress.Plans, m.progress.Votes, m.progress.Diffs, m.progress.Reviews)
	}
	if working := m.buildingAgents(); len(working) > 0 {
		fmt.Fprintf(&b, "\nBuilding: %s\n", strings.Join(working, ", "))
	}

	b.WriteString(orchestrate.StatusReport(run, summary))

	m.openArtifactText("status: "+run.Stamp, b.String())
	m.Status = "status — run " + run.Stamp + " · phase " + phase
}

// buildingAgents lists build participants whose pane is still running, so /status can
// show live build activity. Returns nil outside the build phase.
func (m Model) buildingAgents() []string {
	if m.phase != "build" || m.orch == nil {
		return nil
	}
	building := map[string]bool{}
	for _, name := range m.orch.AgentsForPhase(config.PhaseBuild) {
		building[name] = true
	}
	var out []string
	for _, v := range m.Agents {
		if building[v.Session.Name] && !v.Session.Done {
			out = append(out, v.Session.Name)
		}
	}
	return out
}

// cmdClean is a two-step removal: the first call previews the worktrees and
// branches that would be deleted; "/clean confirm" removes them. policy.mode:
// aggressive skips the confirmation.
func (m *Model) cmdClean(rest string) {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	arg := strings.TrimSpace(rest)
	if strings.EqualFold(arg, "confirm") || !m.Config.Policy.ConfirmDestructive() {
		if !m.pendingClean && strings.EqualFold(arg, "confirm") {
			m.Status = "nothing staged — run /clean first"
			return
		}
		m.pendingClean = false
		removed, err := m.orch.Clean()
		if err != nil {
			m.Status = "clean: " + err.Error()
			return
		}
		m.Status = fmt.Sprintf("removed %d worktree(s)", len(removed))
		return
	}

	worktrees, err := m.orch.ListWorktrees()
	if err != nil {
		m.Status = "clean: " + err.Error()
		return
	}
	if len(worktrees) == 0 {
		m.Status = "no council worktrees to remove"
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# /clean would remove %d worktree(s)\n\n", len(worktrees))
	for _, wt := range worktrees {
		fmt.Fprintf(&b, "- %s\n  branch %s\n", wt.Path, wt.Branch)
	}
	b.WriteString("\nRun /clean confirm to remove them.\n")
	m.pendingClean = true
	m.openArtifactText("clean preview", b.String())
	m.Status = fmt.Sprintf("%d worktree(s) would be removed — /clean confirm", len(worktrees))
}

func (m *Model) cmdRuns() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	runs, err := orchestrate.ListRuns(m.Config.Sessions.RootDir, 20)
	if err != nil {
		m.Status = "runs: " + err.Error()
		return
	}
	m.Runs = runs
	m.RunIndex = 0
	m.ScreenMode = ScreenRuns
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Status = fmt.Sprintf("%d run(s)", len(runs))
}

// cmdJudge lets the human pick a winner directly: /judge plan <agent|letter>
// or /judge build <agent>.
func (m *Model) cmdJudge(rest string) {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "judge: " + err.Error()
			return
		}
	}
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		m.Status = "usage: /judge plan <agent|letter> | /judge build <agent>"
		return
	}
	switch strings.ToLower(fields[0]) {
	case "plan":
		winner, err := m.orch.JudgePlan(fields[1])
		if err != nil {
			m.Status = "judge: " + err.Error()
			return
		}
		m.Status = "human judgment recorded: plan winner is " + winner + " — type /build"
		m.refreshProgress()
	case "build":
		winner, err := m.orch.JudgeBuild(fields[1])
		if err != nil {
			m.Status = "judge: " + err.Error()
			return
		}
		m.Status = "human judgment recorded: build winner is " + winner + " — /adopt to apply"
		m.refreshProgress()
	default:
		m.Status = "usage: /judge plan <agent|letter> | /judge build <agent>"
	}
}

// cmdRefine runs the consensus round: every planner that produced a plan absorbs
// the council's critiques and rewrites its plan, after which the council revotes.
func (m *Model) cmdRefine(note string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "refine: " + err.Error()
			return nil
		}
	}
	prompts, err := m.orch.RefinePrompts(note)
	if err != nil {
		m.Status = "refine: " + err.Error()
		return nil
	}
	participants := make([]string, 0, len(prompts))
	for name := range prompts {
		participants = append(participants, name)
	}
	sort.Strings(participants)
	m.orch.SetScope(participants)
	m.beginPhase("refine", config.PhasePlan, prompts)
	m.Status = fmt.Sprintf("refining %d plan(s) with %s", len(participants), strings.Join(participants, ", "))
	return m.phaseCmds(m.orch.InteractivePrompts(config.PhasePlan, prompts))
}

// cmdReport writes report.md for the current run and opens it.
func (m *Model) cmdReport() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "report: " + err.Error()
			return
		}
	}
	path, err := orchestrate.WriteReport(m.orch.Run())
	if err != nil {
		m.Status = "report: " + err.Error()
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.Status = "wrote " + path
		return
	}
	m.openArtifactText(path, string(data))
	m.Status = "wrote " + path
}

func (m *Model) cmdResume(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	return m.resumeRun(strings.TrimSpace(rest))
}

func (m *Model) resumeRun(stamp string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if err := m.orch.UseRun(stamp); err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	transcripts := orchestrate.LoadTranscripts(m.orch.Run().Dir, m.orch.Agents())
	target, err := m.orch.ResumeTarget()
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	if target.Phase != "" {
		return m.resumePhase(target, transcripts)
	}

	store, err := runstore.OpenAt(m.orch.Run().Dir, "resume")
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	sessions := m.orch.ResumeSessions(store)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil
	m.phase = ""
	m.watching = nil
	m.ScreenMode = ScreenPanes
	m.refreshProgress()
	m.replaceAgentsWithTranscripts(sessions, transcripts)
	if target.Status != "" {
		m.Status = target.Status
	} else {
		m.Status = "resumed run " + m.orch.Run().Stamp
	}
	return nil
}

func (m *Model) resumePhase(target orchestrate.ResumeTarget, transcripts map[string]string) tea.Cmd {
	store, err := m.orch.Store(target.Phase)
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	sessions := m.orch.PhaseSessions(target.Phase, store, target.Prompts)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil
	m.ScreenMode = ScreenPanes
	m.phase = string(target.Phase)
	m.watching = m.orch.ArtifactPaths(target.Phase)
	if target.PendingBuild {
		m.pendingBuild = m.orch.InteractivePrompts(target.Phase, target.Prompts)
	}
	_ = m.orch.SaveActivePhase(target.Phase, target.Participants, target.SendPrompts)
	m.Status = target.Status
	m.refreshProgress()
	m.replaceAgentsWithTranscripts(sessions, transcripts)
	if target.PendingBuild {
		return m.phaseCmds(nil)
	}
	if target.SendPrompts {
		return m.phaseCmds(m.orch.InteractivePrompts(target.Phase, target.Prompts))
	}
	return m.phaseCmds(nil)
}

// beginPhase relaunches every pane in its worktree with the phase command and
// arms the artifact watcher for plan/vote.
func (m *Model) beginPhase(label string, phase config.Phase, prompts map[string]string) {
	store, err := m.orch.Store(phase)
	if err != nil {
		m.Status = err.Error()
		return
	}
	sessions := m.orch.PhaseSessions(phase, store, prompts)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil // any new phase invalidates a staged build
	m.phasePrompts = nil // and the prompts /resend would repeat
	m.phase = label
	m.watching = m.orch.ArtifactPaths(phase)
	_ = m.orch.SaveActivePhase(phase, m.orch.AgentsForPhase(phase), false)
	// Refresh BEFORE relaunching panes: the phase rail adds a header line, and
	// the new PTYs must be sized for the body that will actually be rendered.
	m.refreshProgress()
	m.replaceAgents(sessions)
}

func (m *Model) replaceAgents(sessions []*agent.Session) {
	m.replaceAgentsWithTranscripts(sessions, nil)
}

func (m *Model) replaceAgentsWithTranscripts(sessions []*agent.Session, transcripts map[string]string) {
	for _, v := range m.Agents {
		_ = v.Session.Terminate()
	}
	views := make([]*agentView, 0, len(sessions))
	for _, s := range sessions {
		v := &agentView{Session: s, Width: 120, Height: 40}
		v.setScreenSize(120, 40)
		if text := transcripts[s.Name]; text != "" {
			v.appendTranscript(text+"\n", m.MaxScrollback)
			v.applyTerminal(text + "\n")
		}
		views = append(views, v)
	}
	m.Agents = views
	m.sortAgents()
	m.FocusedIndex = 0
	m.PageIndex = 0
	m.Zoomed = false
	m.resizeAgents()
	m.startAll()
}

func (m *Model) LoadTranscripts(transcripts map[string]string) {
	if len(transcripts) == 0 {
		return
	}
	for _, view := range m.Agents {
		if text := transcripts[view.Session.Name]; text != "" {
			view.appendTranscript(text+"\n", m.MaxScrollback)
			view.applyTerminal(text + "\n")
		}
	}
}

func (m *Model) phaseCmds(prompts map[string]string) tea.Cmd {
	prompts = copyPrompts(prompts)
	cmds := []tea.Cmd{}
	if len(prompts) > 0 {
		cmds = append(cmds, tea.Tick(m.initialPromptDelay, func(time.Time) tea.Msg {
			return phasePromptsMsg(prompts)
		}))
	}
	if len(m.watching) > 0 {
		cmds = append(cmds, pollAfter())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func pollAfter() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return pollArtifactsMsg{} })
}

// pollArtifacts marks panes done as their artifact files appear and finishes the
// phase once all are present. Returns the next poll tick, or nil when done.
func (m *Model) pollArtifacts() tea.Cmd {
	if m.phase == "" || len(m.watching) == 0 {
		return nil
	}
	allDone := true
	for _, v := range m.Agents {
		path, ok := m.watching[v.Session.Name]
		if !ok {
			continue
		}
		if v.PhaseDone {
			continue
		}
		if fileExists(path) {
			v.PhaseDone = true
			v.clearAttention()
		} else {
			allDone = false
		}
	}
	m.refreshProgress()
	if allDone {
		m.finishPhase()
		return nil
	}
	return pollAfter()
}

func (m *Model) finishPhase() {
	if m.orch == nil {
		return
	}
	switch m.phase {
	// A refine round runs as a plan phase and is reopened under the "plan" label
	// on resume, so both labels share this collect path. Whether to run the
	// revote reset is decided by RefineRoundActive() (the leftover
	// <agent>.orig.md backups), not the phase label — otherwise a resumed refine
	// would finish as a plain plan phase and leave the stale vote in place, so
	// the next /vote would tally stale ballots against the pre-refine plans.
	case "plan", "refine":
		found, missing, err := m.orch.CollectPlans()
		if err != nil {
			m.Status = "collect plans: " + err.Error()
			return
		}
		noPlan := ""
		if len(missing) > 0 {
			noPlan = " · no plan: " + strings.Join(missing, ",")
		}
		if m.orch.RefineRoundActive() {
			m.orch.ClearRefineBackups()
			// Clear the prior vote's artifacts so the revote re-anonymizes and
			// re-tallies from the refined plans instead of the originals.
			if err := m.orch.ResetVote(); err != nil {
				m.Status = "refine reset: " + err.Error()
				return
			}
			m.orch.SetScope(nil)
			m.Status = fmt.Sprintf("refined %d plan(s) collected — type /vote%s", len(found), noPlan)
		} else {
			m.Status = fmt.Sprintf("collected %d plan(s) — type /vote%s", len(found), noPlan)
		}
	case "vote":
		res, err := m.orch.CollectVotesAndTally()
		if err != nil {
			m.Status = "tally: " + err.Error()
			return
		}
		m.Status = "winner: " + res.WinnerAgent + " (Plan " + res.WinnerLetter + ") — type /build"
	case "build":
		m.Status = "build done — see worktree branches"
	case "review":
		res, err := m.orch.CollectReviewsAndTally()
		if err != nil {
			m.Status = "review tally: " + err.Error()
			return
		}
		status := "best build: " + res.WinnerAgent + " — /adopt (or /adopt <agent>)"
		if all := m.orch.AdoptableBuilds(); len(all) > 1 {
			status += " · builds: " + strings.Join(all, ", ")
		}
		m.Status = status
	}
	m.phase = ""
	m.watching = nil
	_ = m.orch.ClearActivePhase()
	m.refreshProgress()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (m *Model) sendPrompts(prompts map[string]string) {
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		prompt := prompts[view.Session.Name]
		if prompt == "" {
			continue
		}
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, prompt))
	}
}

func copyPrompts(prompts map[string]string) map[string]string {
	if len(prompts) == 0 {
		return nil
	}
	out := make(map[string]string, len(prompts))
	for name, prompt := range prompts {
		out[name] = prompt
	}
	return out
}
