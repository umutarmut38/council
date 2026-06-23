package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/theme"
	"github.com/umutarmut38/council/internal/tui/anim"
)

// themeToChrome must reproduce the historical defaultChrome for the default
// theme exactly — same foreground and bold for every role — so selecting (or
// omitting) "default" is a no-op visual change. Expectations are read from
// defaultChrome itself rather than hardcoded, so any accidental style drift in
// either path is caught.
func TestThemeToChromeMatchesDefault(t *testing.T) {
	got := themeToChrome(theme.Default())
	want := defaultChrome()
	roles := []struct {
		name      string
		got, want lipgloss.Style
	}{
		{"title", got.title, want.title},
		{"heading", got.heading, want.heading},
		{"status", got.status, want.status},
		{"rail", got.rail, want.rail},
		{"border", got.border, want.border},
		{"focus", got.focus, want.focus},
		{"suggest", got.suggest, want.suggest},
		{"input", got.input, want.input},
		{"warn", got.warn, want.warn},
		{"faint", got.faint, want.faint},
	}
	for _, r := range roles {
		if r.got.GetForeground() != r.want.GetForeground() {
			t.Errorf("%s foreground = %v, want %v", r.name, r.got.GetForeground(), r.want.GetForeground())
		}
		if r.got.GetBold() != r.want.GetBold() {
			t.Errorf("%s bold = %v, want %v", r.name, r.got.GetBold(), r.want.GetBold())
		}
	}
}

// chrome() resolves to the configured base theme normally, the retro palette
// while themed (retro mode wins at runtime), and the historical default when no
// base theme is set (a Model built without NewModelWithConfig).
func TestChromeResolution(t *testing.T) {
	nord, _ := theme.Get("nord")
	base := themeToChrome(nord)

	withBase := Model{baseChrome: &base}
	if got := withBase.chrome().title.GetForeground(); got != idxColor(nord.Title) {
		t.Errorf("base theme: title fg = %v, want %v", got, idxColor(nord.Title))
	}

	retro := Model{baseChrome: &base, retroActive: true, retroIntroDone: true}
	if got := retro.chrome().title.GetForeground(); got != idxColor(anim.Amber) {
		t.Errorf("retro should win over base: title fg = %v, want %v", got, idxColor(anim.Amber))
	}

	var zero Model
	if got := zero.chrome().title.GetForeground(); got != idxColor(theme.Default().Title) {
		t.Errorf("nil base: title fg = %v, want default %v", got, idxColor(theme.Default().Title))
	}
}

// applyCRT paints every row — panes included — so the scanline texture crosses
// the whole console. crtRowBG (tested below) is what keeps an agent's own
// background intact within those rows.
func TestApplyCRTPaintsEveryRow(t *testing.T) {
	lines := []string{"head", "BODY-A", "BODY-B", "foot"}
	screen := strings.Join(lines, "\n")
	out := applyCRT(screen, 0)
	got := strings.Split(out, "\n")
	if len(got) != len(lines) {
		t.Fatalf("line count changed: got %d, want %d", len(got), len(lines))
	}
	for i, line := range got {
		if !strings.Contains(line, "\x1b[48;5;") {
			t.Errorf("row %d (%q) was not painted", i, line)
		}
	}
}

// crtRowBG must re-assert the CRT background after any SGR that returns the
// background to the terminal default, but must NOT clobber an explicit
// background the agent set (solid content legitimately covers the scanline).
func TestCRTRowBGReassertsAfterDefaultBackgroundResets(t *testing.T) {
	const bgIdx = 234
	band := "\x1b[48;5;234m"

	reassert := []struct {
		name, line, after string
	}{
		{"full reset", "a\x1b[0mb", "\x1b[0m" + band},
		{"bare reset", "a\x1b[mb", "\x1b[m" + band},
		{"bg-only reset", "a\x1b[49mb", "\x1b[49m" + band},
		{"fg+bg reset", "a\x1b[39;49mb", "\x1b[39;49m" + band},
	}
	for _, tc := range reassert {
		got := crtRowBG(tc.line, bgIdx)
		if !strings.Contains(got, tc.after) {
			t.Errorf("%s: band not re-asserted after reset; got %q", tc.name, got)
		}
	}

	// An explicit agent background (and a plain foreground change) must be left
	// intact — the band is not re-asserted immediately after them.
	keep := []struct{ name, line, notAfter string }{
		{"explicit indexed bg", "a\x1b[48;5;52mb", "\x1b[48;5;52m" + band},
		{"explicit ansi bg", "a\x1b[42mb", "\x1b[42m" + band},
		{"foreground change", "a\x1b[38;5;82mb", "\x1b[38;5;82m" + band},
	}
	for _, tc := range keep {
		got := crtRowBG(tc.line, bgIdx)
		if strings.Contains(got, tc.notAfter) {
			t.Errorf("%s: band wrongly clobbered the agent's own SGR; got %q", tc.name, got)
		}
	}
}
