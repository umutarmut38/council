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

func retroTestModel(t *testing.T, w, h int) Model {
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

// themed engages retro mode and skips the intro so the model is in the persistent
// themed state (docked head + retro chrome + CRT).
func themed(m *Model) {
	m.handleCommand("/eva")
	m.retroIntroDone = true
}

func plainLines(m Model) []string {
	return strings.Split(ansiRE.ReplaceAllString(m.View(), ""), "\n")
}

// TestRetroHeadBandKeepsViewInvariant verifies the themed header band grows the
// header but keeps View() at exactly Height lines x Width columns.
func TestRetroHeadBandKeepsViewInvariant(t *testing.T) {
	m := retroTestModel(t, 80, 30)
	themed(&m)
	if !m.headShown() {
		t.Fatalf("headShown should be true while retro-themed at 80x30")
	}
	if m.headerLines() != headerBandHeight {
		t.Fatalf("headerLines = %d, want band height %d", m.headerLines(), headerBandHeight)
	}
	lines := plainLines(m)
	if len(lines) != m.Height {
		t.Fatalf("view height = %d, want %d", len(lines), m.Height)
	}
	for i, line := range lines {
		// Display width (CJK retro labels are 2 cols each), not rune count.
		if got := lipgloss.Width(line); got != m.Width {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, m.Width, line)
		}
	}

	// The themed band carries the block COUNCIL banner and the docked head.
	if !strings.Contains(strings.Join(lines, "\n"), "█") {
		t.Fatalf("themed header band missing block-letter banner")
	}

	off := retroTestModel(t, 80, 30)
	if off.headShown() {
		t.Fatalf("headShown should be false outside retro mode")
	}
	if off.headerLines() != headerHeight {
		t.Fatalf("compact headerLines = %d, want %d", off.headerLines(), headerHeight)
	}
}

// TestRetroHeadBandSuppressedOnSmallTerminal keeps the head off cramped screens.
func TestRetroHeadBandSuppressedOnSmallTerminal(t *testing.T) {
	m := retroTestModel(t, 60, 18) // height below the band threshold
	themed(&m)
	if m.headShown() {
		t.Fatalf("headShown should be false at 60x18 even while retro-themed")
	}
	if m.headerLines() != headerHeight {
		t.Fatalf("headerLines = %d, want compact %d", m.headerLines(), headerHeight)
	}
}

// TestRetroToggleIntroSkipRevert walks the full /eva lifecycle: engage -> intro
// plays full-screen -> any key skips into themed mode (without exiting) ->
// second /eva reverts cleanly to the normal header.
func TestRetroToggleIntroSkipRevert(t *testing.T) {
	m := retroTestModel(t, 80, 30)

	handled, cmd := m.handleCommand("/eva")
	if !handled {
		t.Fatalf("/eva should be handled")
	}
	if !m.retroActive || m.retroIntroDone || m.retroIntroFrame != 0 {
		t.Fatalf("after /eva: active=%v introDone=%v frame=%d", m.retroActive, m.retroIntroDone, m.retroIntroFrame)
	}
	if cmd == nil {
		t.Fatalf("/eva should kick the animation loop")
	}

	// Splash plays full-screen (advance enough frames to reveal the consensus banner).
	m.retroIntroFrame = 24
	flat := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(flat, "MAGI SYSTEM ONLINE") {
		t.Fatalf("activation intro not shown full-screen:\n%s", flat)
	}
	if strings.Contains(flat, "Council |") {
		t.Fatalf("normal header leaked while intro should be playing")
	}

	// Any key skips the intro but does NOT exit retro mode.
	updated, _ := m.handleKey(keyMsg("x"))
	m = updated.(Model)
	if !m.retroIntroDone {
		t.Fatalf("a key during the intro should mark it done")
	}
	if !m.retroActive {
		t.Fatalf("a key should not exit retro mode")
	}
	flat = ansiRE.ReplaceAllString(m.View(), "")
	if strings.Contains(flat, "MAGI SYSTEM ONLINE") {
		t.Fatalf("intro should be gone after skip:\n%s", flat)
	}
	if !strings.Contains(flat, "┃") {
		t.Fatalf("themed layout should show after intro skip:\n%s", flat)
	}

	// Second /eva disengages and restores the normal header height.
	m.handleCommand("/eva")
	if m.retroActive {
		t.Fatalf("second /eva should disengage retro mode")
	}
	if m.retroIntroDone {
		t.Fatalf("retroIntroDone should reset on disengage")
	}
	if m.headerLines() != headerHeight {
		t.Fatalf("header height not restored after disengage: %d", m.headerLines())
	}
}

// TestRetroThemeRecolorsBordersIndexedOnly checks the themed chrome uses the retro
// indexed palette and never truecolor.
func TestRetroThemeRecolorsBordersIndexedOnly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	m := retroTestModel(t, 80, 24)
	themed(&m)

	view := m.View()
	if !strings.Contains(view, "38;5;208") {
		t.Fatalf("themed UI missing orange (38;5;208)")
	}
	if strings.Contains(view, "\x1b[38;2;") || strings.Contains(view, "\x1b[48;2;") {
		t.Fatalf("themed UI used truecolor SGR")
	}
	// CRT banding paints indexed-256 backgrounds.
	if !strings.Contains(view, "\x1b[48;5;") {
		t.Fatalf("themed UI missing CRT background banding (48;5;)")
	}
	// Angular retro window frames + bilingual classification.
	flat := ansiRE.ReplaceAllString(view, "")
	for _, want := range []string{"┏", "┃", "┗", "NERV//01", "同期"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("themed panes missing %q", want)
		}
	}

	// Toggling off restores the configured (non-retro) path and drops the CRT.
	m.handleCommand("/eva")
	if m.retroThemed() {
		t.Fatalf("retroThemed should be false after disengage")
	}
	plainAfter := m.View()
	if strings.Contains(plainAfter, "\x1b[48;5;233m") || strings.Contains(plainAfter, "\x1b[48;5;238m") {
		t.Fatalf("CRT banding should be gone after /eva off")
	}
}

// TestAnimTickGatingAndNoStacking covers the frame-loop guards: a single chain,
// rescheduling only while live, and intro auto-finish.
func TestAnimTickGatingAndNoStacking(t *testing.T) {
	m := retroTestModel(t, 80, 30)
	m.retroActive = true // retro mode is what makes the loop live now
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
		t.Fatalf("tick should reschedule while retro mode is live")
	}

	// Outside retro mode -> the loop stops itself.
	stop := retroTestModel(t, 80, 30)
	stop.animLoopRunning = true
	updated, cmd = stop.Update(animTickMsg(time.Now()))
	stop = updated.(Model)
	if cmd != nil {
		t.Fatalf("tick should not reschedule when retro mode is off")
	}
	if stop.animLoopRunning {
		t.Fatalf("loop should mark itself stopped when not live")
	}

	// Splash auto-finishes after retroIntroFrames.
	intro := retroTestModel(t, 80, 30)
	intro.handleCommand("/eva")
	intro.retroIntroFrame = retroIntroFrames - 1
	updated, _ = intro.Update(animTickMsg(time.Now()))
	intro = updated.(Model)
	if !intro.retroIntroDone {
		t.Fatalf("intro should auto-finish once retroIntroFrame reaches %d", retroIntroFrames)
	}
}

func TestRetroLoopRunsIntroIndefinitely(t *testing.T) {
	m := retroTestModel(t, 80, 30)
	m.handleCommand("/eva loop")
	if !m.retroActive || !m.retroIntroLoop {
		t.Fatalf("/eva loop should engage retro mode with a looping intro (active=%v loop=%v)", m.retroActive, m.retroIntroLoop)
	}
	cur := m
	for i := 0; i < retroIntroFrames*3; i++ {
		updated, _ := cur.Update(animTickMsg(time.Now()))
		cur = updated.(Model)
	}
	if cur.retroIntroDone {
		t.Fatal("a looping intro must never auto-complete into themed mode")
	}
}

func TestCrtCommandToggles(t *testing.T) {
	m := retroTestModel(t, 80, 30)
	if m.crtOff {
		t.Fatal("the CRT effect should be on by default")
	}
	m.handleCommand("/crt")
	if !m.crtOff {
		t.Fatal("/crt should turn the CRT effect off")
	}
	m.handleCommand("/crt")
	if m.crtOff {
		t.Fatal("/crt again should turn the CRT effect back on")
	}
}
