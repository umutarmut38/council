package anim

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

var sgrRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return sgrRE.ReplaceAllString(s, "") }

// assertIndexedSGROnly checks the hard color constraint: indexed-256 foreground
// escapes only, never truecolor (38;2;/48;2;) and never SGR faint (\x1b[2m).
func assertIndexedSGROnly(t *testing.T, out, what string) {
	t.Helper()
	if !strings.Contains(out, "\x1b[38;5;") {
		t.Fatalf("%s: no indexed-256 foreground SGR present", what)
	}
	if strings.Contains(out, "\x1b[38;2;") || strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("%s: used truecolor SGR (38;2;/48;2;)", what)
	}
	if strings.Contains(out, "\x1b[2m") {
		t.Fatalf("%s: used SGR faint (\\x1b[2m)", what)
	}
}

func TestHeadDimensionsAndWidth(t *testing.T) {
	for _, dim := range []struct{ w, h int }{{18, 8}, {24, 10}, {32, 14}} {
		lines := Head(dim.w, dim.h, 0)
		if len(lines) != dim.h {
			t.Fatalf("Head(%d,%d) returned %d lines, want %d", dim.w, dim.h, len(lines), dim.h)
		}
		for i, line := range lines {
			if got := runewidth.StringWidth(plain(line)); got != dim.w {
				t.Fatalf("Head(%d,%d) line %d width = %d, want %d: %q", dim.w, dim.h, i, got, dim.w, plain(line))
			}
		}
	}
}

func TestHeadIndexedSGROnly(t *testing.T) {
	out := strings.Join(Head(24, 10, 5), "\n")
	assertIndexedSGROnly(t, out, "head")
}

func TestHeadEyesBrightWhenFrontFacing(t *testing.T) {
	// Frame 0 faces the viewer, so the eyes must paint with the fixed eye color.
	out := strings.Join(Head(18, 8, 0), "\n")
	if !strings.Contains(out, sgr(eyeColor)) {
		t.Fatalf("front-facing head missing its eye color (index %d)", eyeColor)
	}
}

func TestHeadFramesDifferAcrossRotation(t *testing.T) {
	a := strings.Join(Head(18, 8, 0), "\n")
	b := strings.Join(Head(18, 8, 9), "\n")
	if a == b {
		t.Fatalf("head frames 0 and 9 are identical; head is not rotating")
	}
}

// The head holds the EVA-01's fixed palette: purple armor shell, white eyes,
// independent of any phase.
func TestHeadUsesEvaPalette(t *testing.T) {
	out := strings.Join(Head(44, 20, 0), "\n")
	if !strings.Contains(out, sgr(eyeColor)) {
		t.Fatalf("head missing white eyes (index %d)", eyeColor)
	}
	if !containsAnyShade(out, purpleRamp) {
		t.Fatalf("head missing purple armor shades %v", purpleRamp)
	}
}

func containsAnyShade(s string, ramp []int) bool {
	for _, idx := range ramp {
		if strings.Contains(s, sgr(idx)) {
			return true
		}
	}
	return false
}

func TestBannerShape(t *testing.T) {
	b := Banner("NERV")
	if len(b) != blockRows {
		t.Fatalf("banner has %d rows, want %d", len(b), blockRows)
	}
	w := BannerWidth(b)
	for i, row := range b {
		if got := len([]rune(row)); got != w {
			t.Fatalf("banner row %d width = %d, want %d", i, got, w)
		}
	}
	if !strings.Contains(strings.Join(b, ""), "█") {
		t.Fatalf("banner should be drawn with block glyphs")
	}
	if BannerWidth(Banner("COUNCIL")) <= BannerWidth(Banner("NERV")) {
		t.Fatalf("COUNCIL banner should be wider than NERV")
	}
}

func TestHeadTooSmallIsBlank(t *testing.T) {
	lines := Head(4, 2, 0)
	if len(lines) != 2 {
		t.Fatalf("blank head returned %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		if strings.TrimSpace(plain(line)) != "" {
			t.Fatalf("tiny head should be blank, got %q", plain(line))
		}
	}
}

func TestIntroDimensionsWidthAndContent(t *testing.T) {
	// Width/height invariants must hold at any size and frame.
	for _, dim := range []struct{ w, h int }{{60, 20}, {90, 34}, {120, 40}} {
		for _, frame := range []int{0, 12, 42, 56} {
			out := Intro(dim.w, dim.h, frame)
			lines := strings.Split(out, "\n")
			if len(lines) != dim.h {
				t.Fatalf("intro(%d,%d,%d) has %d lines, want %d", dim.w, dim.h, frame, len(lines), dim.h)
			}
			for i, line := range lines {
				if got := runewidth.StringWidth(plain(line)); got != dim.w {
					t.Fatalf("intro(%d,%d,%d) line %d width = %d, want %d: %q", dim.w, dim.h, frame, i, got, dim.w, plain(line))
				}
			}
			assertIndexedSGROnly(t, out, "intro")
		}
	}

	// The full stepped sequence has shown by a late frame.
	flat := plain(Intro(90, 34, 52))
	for _, want := range []string{
		"MELCHIOR-1", "BALTHASAR-2", "CASPER-3",
		"MAGI SYSTEM ONLINE", "SYNCHRONIZATION START", "SYNC RATIO",
		"A.T. FIELD", "E V A N G E L I O N",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("intro (frame 52) missing %q", want)
		}
	}
	// The final ACTIVATION beat lands by the end.
	if !strings.Contains(plain(Intro(90, 34, 60)), "ACTIVATION") {
		t.Fatalf("intro (frame 60) missing ACTIVATION beat")
	}
}

func TestIntroNervFallbackOnNarrowField(t *testing.T) {
	// Too short for the block title -> spaced-caps "N E R V".
	flat := plain(Intro(40, 9, 0))
	if !strings.Contains(flat, "N E R V") {
		t.Fatalf("narrow intro missing N E R V title; got:\n%s", flat)
	}
}

func TestIntroRevealsAreStepped(t *testing.T) {
	early := plain(Intro(60, 20, 0))
	if strings.Contains(early, "E V A N G E L I O N") {
		t.Fatalf("EVANGELION should not appear at frame 0 (stepped reveal)")
	}
	if strings.Contains(early, "MAGI SYSTEM ONLINE") {
		t.Fatalf("MAGI label should not appear at frame 0 (stepped reveal)")
	}
}
