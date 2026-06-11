package tui

// Experimental approval-prompt detection.
//
// Inferring "this agent is blocked waiting for the user" from a PTY stream is
// inherently heuristic, so the signal is built from what "blocked" actually
// looks like, not from words appearing somewhere in the output:
//
//  1. an approval-looking prompt must be sitting in the last few visible
//     lines of the pane (the live screen tail — not the scrollback, not a
//     chunk that merely mentioned approvals in prose), AND
//  2. the pane must have gone quiet (a truly blocked agent stops emitting
//     output), confirmed on a delayed re-check rather than at match time.
//
// The flag also clears itself when output resumes and the prompt leaves the
// screen — only a manual /attention flag survives that. False negatives are
// acceptable (the user still has /attention); false positives are what
// erode trust in the HUD, so every pattern here is a concrete approval
// phrasing, never conversational ("What do you want to work on?" must not
// match). Disable entirely with ui.detect_approval_prompts: false.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// attentionIdleDelay is how long a pane must stay quiet, with the prompt
	// still on screen, before it is flagged.
	attentionIdleDelay = 2 * time.Second
	// attentionTailLines is how many visible non-empty lines count as "the
	// bottom of the pane" where a blocking prompt would sit.
	attentionTailLines = 6
)

// attentionCheckMsg re-evaluates candidate panes after the idle delay.
type attentionCheckMsg struct{}

// approvalPatterns are concrete approval-prompt phrasings (matched lowercase
// against the screen tail). Keep them specific: bare "do you want to" once
// matched codex's greeting "What do you want to work on in …?".
var approvalPatterns = []string{
	// Option markers.
	"[y/n]", "(y/n)", "y/n?",
	// Claude Code permission dialogs.
	"do you want to proceed", "do you want to allow", "do you want to run",
	"do you want to make this edit", "yes, allow",
	// Codex / Copilot / generic tool approval.
	"allow command", "allow execution", "allow this command", "always allow",
	"approve this", "needs your approval", "waiting for approval",
	// Trust prompts.
	"do you trust", "grant access",
	// Explicit confirms.
	"press enter to confirm", "press y to",
}

// screenTail returns the last n non-empty visible lines of the pane,
// joined for matching.
func (v *agentView) screenTail(n int) string {
	collect := func(lines []string) string {
		tail := make([]string, 0, n)
		for i := len(lines) - 1; i >= 0 && len(tail) < n; i-- {
			if strings.TrimSpace(lines[i]) == "" {
				continue
			}
			tail = append(tail, lines[i])
		}
		// Order is irrelevant for substring matching; skip the reverse.
		return strings.Join(tail, "\n")
	}

	if v.Session.Config.Terminal.Renderer == "transcript" {
		return collect(append(append([]string{}, v.Lines...), v.Partial))
	}
	lines := make([]string, 0, len(v.Screen))
	for _, row := range v.Screen {
		lines = append(lines, cellsText(row))
	}
	return collect(lines)
}

// approvalPromptVisible reports whether the pane's visible tail currently
// looks like a blocking approval prompt.
func approvalPromptVisible(v *agentView) bool {
	tail := strings.ToLower(v.screenTail(attentionTailLines))
	if tail == "" {
		return false
	}
	for _, pattern := range approvalPatterns {
		if strings.Contains(tail, pattern) {
			return true
		}
	}
	return false
}

// noteAttentionOutput is called on every output chunk: it stamps activity,
// auto-clears a stale auto-flag once the prompt has left the screen, and
// reports whether a delayed confirmation check should be scheduled.
func (m *Model) noteAttentionOutput(view *agentView) (schedule bool) {
	view.lastOutputAt = time.Now()
	if view.Session.Done {
		return false
	}
	if !m.Config.UI.ApprovalDetectionEnabled() {
		return false
	}

	if approvalPromptVisible(view) {
		// Candidate: confirm only after the pane goes quiet.
		return !view.Attention
	}
	// The prompt is gone and the agent is producing output again — an
	// auto-set flag has served its purpose. Manual flags stay.
	if view.Attention && !view.AttentionManual {
		view.Attention = false
	}
	return false
}

// scheduleAttentionCheck arms one pending re-check tick (debounced).
func (m *Model) scheduleAttentionCheck() tea.Cmd {
	if m.attentionCheckPending {
		return nil
	}
	m.attentionCheckPending = true
	return tea.Tick(attentionIdleDelay+250*time.Millisecond, func(time.Time) tea.Msg {
		return attentionCheckMsg{}
	})
}

// runAttentionCheck confirms candidates that stayed quiet with the prompt
// still on screen; panes that are still streaming get re-checked later.
func (m *Model) runAttentionCheck() tea.Cmd {
	m.attentionCheckPending = false
	if !m.Config.UI.ApprovalDetectionEnabled() {
		return nil
	}
	again := false
	for _, view := range m.Agents {
		if view.Session.Done || view.Attention {
			continue
		}
		if !approvalPromptVisible(view) {
			continue
		}
		if time.Since(view.lastOutputAt) >= attentionIdleDelay {
			view.Attention = true
			continue
		}
		again = true
	}
	if again {
		return m.scheduleAttentionCheck()
	}
	return nil
}

// clearAttention resets the needs-input flags after the user engages the pane.
func (v *agentView) clearAttention() {
	v.Attention = false
	v.AttentionManual = false
}
