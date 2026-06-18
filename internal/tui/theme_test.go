package tui

import (
	"strings"
	"testing"
)

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
