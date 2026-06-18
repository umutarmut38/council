package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

func evaTestModel(t *testing.T, w, h int) Model {
	t.Helper()
	sessions := []*agent.Session{
		agent.NewSession("claude", config.AgentConfig{}, ""),
		agent.NewSession("codex", config.AgentConfig{}, ""),
	}
	m := NewModel(sessions, nil, 1000, "", 0, nil, nil)
	m.Width = w
	m.Height = h
	m.resizeAgents()
	return m
}

// themed engages EVA mode and skips the intro so the model is in the persistent
// themed state (docked head + NERV chrome + CRT).
func themed(m *Model) {
	m.handleCommand("/eva")
	m.evaIntroDone = true
}

func plainLines(m Model) []string {
	return strings.Split(ansiRE.ReplaceAllString(m.View(), ""), "\n")
}

// TestEvaHeadBandKeepsViewInvariant verifies the themed header band grows the
// header but keeps View() at exactly Height lines x Width columns.
func TestEvaHeadBandKeepsViewInvariant(t *testing.T) {
	m := evaTestModel(t, 80, 30)
	themed(&m)
	if !m.headShown() {
		t.Fatalf("headShown should be true while EVA-themed at 80x30")
	}
	if m.headerLines() != headerBandHeight {
		t.Fatalf("headerLines = %d, want band height %d", m.headerLines(), headerBandHeight)
	}
	lines := plainLines(m)
	if len(lines) != m.Height {
		t.Fatalf("view height = %d, want %d", len(lines), m.Height)
	}
	for i, line := range lines {
		// Display width (CJK NERV labels are 2 cols each), not rune count.
		if got := lipgloss.Width(line); got != m.Width {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, m.Width, line)
		}
	}

	// The themed band carries the block COUNCIL banner and the docked head.
	if !strings.Contains(strings.Join(lines, "\n"), "█") {
		t.Fatalf("themed header band missing block-letter banner")
	}

	off := evaTestModel(t, 80, 30)
	if off.headShown() {
		t.Fatalf("headShown should be false outside EVA mode")
	}
	if off.headerLines() != headerHeight {
		t.Fatalf("compact headerLines = %d, want %d", off.headerLines(), headerHeight)
	}
}

// TestEvaHeadBandSuppressedOnSmallTerminal keeps the head off cramped screens.
func TestEvaHeadBandSuppressedOnSmallTerminal(t *testing.T) {
	m := evaTestModel(t, 60, 18) // height below the band threshold
	themed(&m)
	if m.headShown() {
		t.Fatalf("headShown should be false at 60x18 even while EVA-themed")
	}
	if m.headerLines() != headerHeight {
		t.Fatalf("headerLines = %d, want compact %d", m.headerLines(), headerHeight)
	}
}

// TestEvaToggleIntroSkipRevert walks the full /eva lifecycle: engage -> intro
// plays full-screen -> any key skips into themed mode (without exiting) ->
// second /eva reverts cleanly to the normal header.
func TestEvaToggleIntroSkipRevert(t *testing.T) {
	m := evaTestModel(t, 80, 30)

	handled, cmd := m.handleCommand("/eva")
	if !handled {
		t.Fatalf("/eva should be handled")
	}
	if !m.evaActive || m.evaIntroDone || m.evaIntroFrame != 0 {
		t.Fatalf("after /eva: active=%v introDone=%v frame=%d", m.evaActive, m.evaIntroDone, m.evaIntroFrame)
	}
	if cmd == nil {
		t.Fatalf("/eva should kick the animation loop")
	}

	// Intro plays full-screen (advance enough frames to reveal the MAGI banner).
	m.evaIntroFrame = 24
	flat := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(flat, "MAGI SYSTEM ONLINE") {
		t.Fatalf("activation intro not shown full-screen:\n%s", flat)
	}
	if strings.Contains(flat, "Council |") {
		t.Fatalf("normal header leaked while intro should be playing")
	}

	// Any key skips the intro but does NOT exit EVA mode.
	updated, _ := m.handleKey(keyMsg("x"))
	m = updated.(Model)
	if !m.evaIntroDone {
		t.Fatalf("a key during the intro should mark it done")
	}
	if !m.evaActive {
		t.Fatalf("a key should not exit EVA mode")
	}
	flat = ansiRE.ReplaceAllString(m.View(), "")
	if strings.Contains(flat, "MAGI SYSTEM ONLINE") {
		t.Fatalf("intro should be gone after skip:\n%s", flat)
	}
	if !strings.Contains(flat, "┃") {
		t.Fatalf("themed NERV layout should show after intro skip:\n%s", flat)
	}

	// Second /eva disengages and restores the normal header height.
	m.handleCommand("/eva")
	if m.evaActive {
		t.Fatalf("second /eva should disengage EVA mode")
	}
	if m.evaIntroDone {
		t.Fatalf("evaIntroDone should reset on disengage")
	}
	if m.headerLines() != headerHeight {
		t.Fatalf("header height not restored after disengage: %d", m.headerLines())
	}
}

// TestEvaThemeRecolorsBordersIndexedOnly checks the themed chrome uses the NERV
// indexed palette and never truecolor.
func TestEvaThemeRecolorsBordersIndexedOnly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	m := evaTestModel(t, 80, 24)
	themed(&m)

	view := m.View()
	if !strings.Contains(view, "38;5;208") {
		t.Fatalf("themed UI missing NERV orange (38;5;208)")
	}
	if strings.Contains(view, "\x1b[38;2;") || strings.Contains(view, "\x1b[48;2;") {
		t.Fatalf("themed UI used truecolor SGR")
	}
	// CRT banding paints indexed-256 backgrounds.
	if !strings.Contains(view, "\x1b[48;5;") {
		t.Fatalf("themed UI missing CRT background banding (48;5;)")
	}
	// Angular NERV window frames + bilingual classification.
	flat := ansiRE.ReplaceAllString(view, "")
	for _, want := range []string{"┏", "┃", "┗", "NERV//01", "同期"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("themed panes missing %q", want)
		}
	}

	// Toggling off restores the configured (non-NERV) path and drops the CRT.
	m.handleCommand("/eva")
	if m.evaThemed() {
		t.Fatalf("evaThemed should be false after disengage")
	}
	plainAfter := m.View()
	if strings.Contains(plainAfter, "\x1b[48;5;233m") || strings.Contains(plainAfter, "\x1b[48;5;238m") {
		t.Fatalf("CRT banding should be gone after /eva off")
	}
}

// TestAnimTickGatingAndNoStacking covers the frame-loop guards: a single chain,
// rescheduling only while live, and intro auto-finish.
func TestAnimTickGatingAndNoStacking(t *testing.T) {
	m := evaTestModel(t, 80, 30)
	m.evaActive = true // EVA mode is what makes the loop live now
	if c := m.kickAnimLoop(); c == nil || !m.animLoopRunning {
		t.Fatalf("first kick should start the loop (cmd=%v running=%v)", c != nil, m.animLoopRunning)
	}
	if c := m.kickAnimLoop(); c != nil {
		t.Fatalf("second kick should not stack another tick chain")
	}

	before := m.animFrame
	updated, cmd := m.Update(animTickMsg(time.Now()))
	m = updated.(Model)
	if m.animFrame != before+1 {
		t.Fatalf("tick should bump animFrame: got %d, want %d", m.animFrame, before+1)
	}
	if cmd == nil {
		t.Fatalf("tick should reschedule while EVA mode is live")
	}

	// Outside EVA mode -> the loop stops itself.
	stop := evaTestModel(t, 80, 30)
	stop.animLoopRunning = true
	updated, cmd = stop.Update(animTickMsg(time.Now()))
	stop = updated.(Model)
	if cmd != nil {
		t.Fatalf("tick should not reschedule when EVA mode is off")
	}
	if stop.animLoopRunning {
		t.Fatalf("loop should mark itself stopped when not live")
	}

	// Intro auto-finishes after evaIntroFrames.
	intro := evaTestModel(t, 80, 30)
	intro.handleCommand("/eva")
	intro.evaIntroFrame = evaIntroFrames - 1
	updated, _ = intro.Update(animTickMsg(time.Now()))
	intro = updated.(Model)
	if !intro.evaIntroDone {
		t.Fatalf("intro should auto-finish once evaIntroFrame reaches %d", evaIntroFrames)
	}
}
