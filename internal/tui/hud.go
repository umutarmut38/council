package tui

// The orchestrator HUD: run progress derived from artifacts on disk, the
// header phase rail, and the context-aware footer hint. Progress is cached on
// the model (refreshProgress) because View renders on every PTY chunk and
// must not hit the filesystem each time.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

// phaseState is one segment of the phase rail.
type phaseState int

const (
	phasePending phaseState = iota // ○ not started
	phaseActive                    // ● in progress
	phaseDone                      // ✓ complete
)

type phaseInfo struct {
	Label    string
	State    phaseState
	Done     int  // artifacts present
	Expected int  // artifacts expected (0 = no counter shown)
	Counted  bool // whether Done/Expected are meaningful
}

// runProgress is a snapshot of where the active run stands.
type runProgress struct {
	Stamp   string
	Phases  []phaseInfo // Plan, Vote, Build, Review, Adopt
	Next    string      // the next useful command, e.g. "/vote"
	Current string      // name of the active phase ("" when idle)

	Plans   int // artifacts on disk
	Votes   int
	Diffs   int
	Reviews int

	PlanWinner       string
	PlanWinnerLetter string
	BuildWinner      string
	Adopted          string
}

// refreshProgress recomputes the cached run progress from disk. Call it on
// phase transitions, artifact polls, and orchestration commands — not from
// View.
func (m *Model) refreshProgress() {
	m.progress = m.computeProgress()
}

func (m *Model) computeProgress() *runProgress {
	if m.orch == nil || m.orch.Run() == nil {
		return nil
	}
	summary, err := orchestrate.SummarizeRun(m.orch.Run().RootDir, m.orch.Run().Stamp)
	if err != nil {
		return nil
	}
	return m.progressFromSummary(summary)
}

// progressFromSummary builds the run progress from an already-loaded summary so
// callers that have one (e.g. /status) don't re-read result.json and the run
// artifacts a second time.
func (m *Model) progressFromSummary(summary orchestrate.RunSummary) *runProgress {
	run := m.orch.Run()
	p := &runProgress{Stamp: run.Stamp, Current: m.phase}

	p.PlanWinner = summary.Winner
	p.PlanWinnerLetter = summary.WinnerLetter
	p.BuildWinner = m.buildWinnerQuiet()
	p.Plans, p.Votes, p.Diffs, p.Reviews = len(summary.Plans), len(summary.Votes), len(summary.Diffs), len(summary.Reviews)
	if adopted, ok := run.Adoption(); ok {
		p.Adopted = adopted
	}

	// Reflect live worktree activity throughout the build, not just until the
	// first diff: /compare can capture diffs mid-build, and the rail must keep
	// climbing as more agents start working rather than freezing at that count.
	// The count comes from the cache filled off-thread by the build progress
	// tick — View/refreshProgress must never shell out to git themselves.
	buildActive := 0
	if m.phase == "build" {
		buildActive = m.buildActive
	}
	buildDone := buildRailDone(m.phase, len(summary.Diffs), buildActive)

	planExpected := m.phaseExpected("plan", config.PhasePlan, len(summary.Plans))
	voteExpected := m.phaseExpected("vote", config.PhaseVote, len(summary.Votes))
	buildExpected := m.phaseExpected("build", config.PhaseBuild, buildDone)
	reviewExpected := m.phaseExpected("review", config.PhaseReview, len(summary.Reviews))

	plan := phaseInfo{Label: "Plan", Done: len(summary.Plans), Expected: planExpected, Counted: true}
	vote := phaseInfo{Label: "Vote", Done: len(summary.Votes), Expected: voteExpected, Counted: true}
	build := phaseInfo{Label: "Build", Done: buildDone, Expected: buildExpected, Counted: buildDone > 0 || m.phase == "build"}
	review := phaseInfo{Label: "Review", Done: len(summary.Reviews), Expected: reviewExpected, Counted: len(summary.Reviews) > 0 || m.phase == "review"}
	adopt := phaseInfo{Label: "Adopt"}

	// Completion is judged by outcomes, not just counts: a vote is complete
	// when a winner exists, a review when a build winner exists.
	switch {
	case p.Adopted != "":
		adopt.State = phaseDone
		fallthrough
	case p.BuildWinner != "":
		review.State, build.State, vote.State, plan.State = phaseDone, phaseDone, phaseDone, phaseDone
	case len(summary.Diffs) > 0 && m.phase != "build":
		build.State, vote.State, plan.State = phaseDone, phaseDone, phaseDone
	case p.PlanWinner != "":
		vote.State, plan.State = phaseDone, phaseDone
	case len(summary.Plans) > 0 && planExpected > 0 && len(summary.Plans) >= planExpected && m.phase != "plan":
		plan.State = phaseDone
	}
	switch m.phase {
	case "plan", "refine":
		plan.State = phaseActive
	case "vote":
		vote.State = phaseActive
	case "build":
		build.State = phaseActive
	case "review":
		review.State = phaseActive
	}

	p.Next = m.nextCommand(p, summary.Plans, summary.Diffs)
	p.Phases = []phaseInfo{plan, vote, build, review, adopt}
	return p
}

// buildRailDone picks the Build rail's Done count. While the build is live it
// tracks live worktree activity, but diffs may already be captured (via
// /compare), so it uses max(diffs, active): a newly-active worktree still
// advances the rail, and the count never drops below what was captured. Outside
// the build it is simply the captured-diff count (the authoritative number).
func buildRailDone(phase string, diffs, active int) int {
	if phase == "build" && active > diffs {
		return active
	}
	return diffs
}

// phaseExpected returns how many artifacts a phase should produce: the watch
// set while the phase is live, otherwise the eligible participants (never
// less than what already exists on disk).
func (m *Model) phaseExpected(label string, phase config.Phase, have int) int {
	expected := 0
	if m.phase == label && len(m.watching) > 0 {
		expected = len(m.watching)
	} else {
		expected = len(m.orch.AgentsForPhase(phase))
	}
	if have > expected {
		expected = have
	}
	return expected
}

func (m *Model) buildWinnerQuiet() string {
	winner, err := m.orch.BuildWinner()
	if err != nil {
		return ""
	}
	return winner
}

// nextCommand recommends the single most useful next step.
func (m *Model) nextCommand(p *runProgress, plans, diffs []string) string {
	switch m.phase {
	case "plan", "refine", "vote", "review":
		return "/finish if a pane is stuck"
	case "build":
		if len(m.pendingBuild) > 0 {
			return "/start-build"
		}
		return "/review when builds finish · /compare to peek"
	}
	switch {
	case p.Adopted != "":
		return "git diff, then commit"
	case p.BuildWinner != "":
		return "/compare or /adopt"
	case len(diffs) > 0:
		return "/review"
	case p.PlanWinner != "":
		return "/build (or /refine first)"
	case len(plans) > 0:
		return "/vote"
	default:
		return "/plan <issue>"
	}
}

// phaseRail renders the compact progress line shown in the header:
//
//	Plan 2/2 ✓ Vote 0/2 ● Build ○ Review ○ Adopt ○ · Next: /vote
func (p *runProgress) phaseRail() string {
	if p == nil {
		return ""
	}
	parts := make([]string, 0, len(p.Phases))
	for _, ph := range p.Phases {
		seg := ph.Label
		if ph.Counted && ph.Expected > 0 {
			seg += fmt.Sprintf(" %d/%d", ph.Done, ph.Expected)
		}
		switch ph.State {
		case phaseDone:
			seg += " ✓"
		case phaseActive:
			seg += " ●"
		default:
			seg += " ○"
		}
		// Pin the outcome to the rail so the winner survives across phases. On a
		// narrow terminal fitText drops this tail first, which is acceptable.
		if ph.State == phaseDone {
			switch ph.Label {
			case "Vote":
				if p.PlanWinner != "" {
					if p.PlanWinnerLetter != "" {
						seg += fmt.Sprintf(" %s(%s)", shortAgent(p.PlanWinner), p.PlanWinnerLetter)
					} else {
						seg += " " + shortAgent(p.PlanWinner)
					}
				}
			case "Review":
				if p.BuildWinner != "" {
					seg += " " + shortAgent(p.BuildWinner)
				}
			}
		}
		parts = append(parts, seg)
	}
	rail := strings.Join(parts, "  ")
	if p.Next != "" {
		rail += " · Next: " + p.Next
	}
	return rail
}

// attentionAgents lists panes flagged as likely needing direct input.
func (m Model) attentionAgents() []string {
	var out []string
	for _, v := range m.Agents {
		if v.Attention && !v.Session.Done {
			out = append(out, v.Session.Name)
		}
	}
	return out
}

// contextHint is the footer line shown when the composer is idle: blocked
// panes first, then phase-specific next steps, then the generic shortcuts.
func (m Model) contextHint() (string, bool) {
	if blocked := m.attentionAgents(); len(blocked) > 0 {
		name := blocked[0]
		return fmt.Sprintf("%s may need input · F2 direct · /nudge %s · /restart %s · /attention %s off to dismiss", name, name, name, name), true
	}
	p := m.progress
	if p == nil {
		return "", false
	}
	switch m.phase {
	case "plan", "refine", "vote", "review":
		ph := m.railPhase(p)
		done, expected := 0, 0
		if ph != nil {
			done, expected = ph.Done, ph.Expected
		}
		return fmt.Sprintf("%s in progress: %d/%d · Useful: /finish · /resend <agent> · /restart <agent> · /nudge", capitalize(m.phase), done, expected), true
	case "build":
		if len(m.pendingBuild) > 0 {
			return "Build staged in worktrees · Next: /start-build (adjust the tools first if needed)", true
		}
		return "Build in progress · F2 direct if an agent needs approval · /compare to peek · Next: /review when done", true
	}
	switch {
	case p.Adopted != "":
		return fmt.Sprintf("Adopted %s (uncommitted) · review with git diff, then commit · /report for the summary", p.Adopted), true
	case p.BuildWinner != "":
		return fmt.Sprintf("Best build: %s · Next: /compare or /adopt · /judge build <agent> to override", p.BuildWinner), true
	case p.Next != "":
		return "Next: " + p.Next + " · Useful: /status /artifacts", true
	}
	return "", false
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// shortAgent trims a known role suffix (e.g. "codex-worker" -> "codex") for the
// compact rail winner tag. It deliberately does not split on the first hyphen, so
// legitimate names like "codex-2" are left intact. Display only; /status shows the
// full name.
func shortAgent(name string) string {
	for _, suffix := range []string{"-worker", "-reviewer"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

// railPhase returns the rail segment for the currently active phase.
func (m Model) railPhase(p *runProgress) *phaseInfo {
	if p == nil {
		return nil
	}
	label := map[string]string{"plan": "Plan", "refine": "Plan", "vote": "Vote", "build": "Build", "review": "Review"}[m.phase]
	for i := range p.Phases {
		if p.Phases[i].Label == label {
			return &p.Phases[i]
		}
	}
	return nil
}

// phaseReadout is the single-line operational-status line shown in the retro
// header band: the live phase + its artifact count while a phase runs, the next
// action when idle/between/complete, or empty when there is no run. It is the
// simplified replacement for the full phase rail in themed mode.
func (m Model) phaseReadout() string {
	p := m.progress
	if p != nil && m.phase != "" { // a phase is live
		name := strings.ToUpper(m.phase) // PLAN/REFINE/VOTE/BUILD/REVIEW
		if ph := m.railPhase(p); ph != nil && ph.Counted && ph.Expected > 0 {
			return fmt.Sprintf("▶ %s · %d/%d", name, ph.Done, ph.Expected)
		}
		return "▶ " + name
	}
	if p != nil && p.Next != "" { // idle/between/complete → next step
		return "▶ AWAITING · " + p.Next
	}
	return "" // no run → nothing
}

// paneBadge builds the pane title state: process state plus, during an
// orchestration phase, what the agent owes (waiting for PLAN.md / wrote
// VOTE.md / needs input).
func (m Model) paneBadge(view *agentView) string {
	state := "running"
	switch {
	case view.Session.StartError != nil:
		state = "failed"
	case view.Session.Done:
		state = "exited"
		if view.Session.ExitCode != nil && *view.Session.ExitCode != 0 {
			state = fmt.Sprintf("exit %d", *view.Session.ExitCode)
		}
	case view.Attention:
		state = "needs input"
	}

	if m.phase == "" {
		return state
	}
	artifactPath, watched := m.watching[view.Session.Name]
	if !watched {
		// Build has no artifact file; everyone in a phase without a watch set
		// is simply working in it.
		if m.phase == "build" && !view.Session.Done {
			if view.Attention {
				return "build · needs input"
			}
			return "build · working"
		}
		return state
	}
	artifact := filepath.Base(artifactPath)
	switch {
	case view.PhaseDone:
		return fmt.Sprintf("%s · wrote %s", m.phase, artifact)
	case view.Attention && !view.Session.Done:
		return fmt.Sprintf("%s · needs input", m.phase)
	case view.Session.Done:
		return fmt.Sprintf("%s · %s, no %s", m.phase, state, artifact)
	default:
		return fmt.Sprintf("%s · waiting for %s", m.phase, artifact)
	}
}

// paneColor resolves an agent's display color: the agent's own color first,
// then its personality's color, else "" (default chrome).
func (m Model) paneColor(name string) string {
	if agentCfg, ok := m.Config.Agents[name]; ok && strings.TrimSpace(agentCfg.Color) != "" {
		return strings.TrimSpace(agentCfg.Color)
	}
	if _, personality, ok := m.Config.PersonalityForAgent(name); ok && strings.TrimSpace(personality.Color) != "" {
		return strings.TrimSpace(personality.Color)
	}
	return ""
}

// compressPath shortens an absolute path for header display (~ for $HOME).
func compressPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}
