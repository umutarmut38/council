// Package anim renders the council's Evangelion-flavored visuals: a genuinely
// 3D, rotating EVA-01 head for the activity band and the NERV activation intro
// for /eva mode.
//
// Everything is emitted as INDEXED 256-color SGR (`\x1b[38;5;Nm`) by hand. That
// is deliberate: it is deterministic (so the output is testable without a tty),
// it bypasses lipgloss's profile detection (which strips color in non-tty
// tests), and it honors the project-wide constraint of indexed-256 only — no
// truecolor, no SGR faint/reverse (see internal/tui/color.go).
package anim

import (
	"strconv"
	"strings"
)

// NERV palette, mapped to the nearest 256-color indices. These mirror the
// canonical web palette from the nerv-ui design system.
const (
	NervOrange    = 208 // labels / headers / focused chrome
	NervOrangeDim = 166 // muted orange for unfocused chrome
	DataGreen     = 83  // nominal data readouts
	WireCyan      = 51  // wireframes / spatial data
	AlarmRed      = 196 // emergencies / blocked panes
	Steel         = 253 // secondary text
	SteelDim      = 240 // de-emphasized secondary text
	HotWhite      = 231 // peak pulse (vote)
)

// eyeGreen is the fixed bright green used for the EVA-01's eyes, kept distinct
// from the body ramp so the eyes always read as the unit's "live" gaze.
const eyeGreen = 47

// charRamp shades the head dark -> bright. Index 0 (space) is reserved for
// empty cells, so a plotted surface point never collapses to blank.
const charRamp = " .:-=+*#%@"

// phosphorRamp is the idle green-phosphor luminance ramp (dark -> bright) so the
// head reads like a CRT vector monitor.
var phosphorRamp = []int{22, 28, 34, 40, 46, 83, 118, 47}

// Per-phase accent ramps. Each runs dark -> bright in its hue so the same
// luminance shading reads correctly whatever the phase tint is.
var (
	amberRamp = []int{52, 130, 166, 202, 208, 214, 220, 226}
	cyanRamp  = []int{17, 23, 30, 37, 44, 51, 87, 123}
	whiteRamp = []int{240, 243, 246, 249, 252, 254, 255, 231}
	redRamp   = []int{52, 88, 124, 160, 196, 202, 203, 210}
	steelRamp = []int{236, 238, 240, 242, 244, 246, 249, 252}
)

// AccentForPhase maps an orchestration phase to the head's accent color index.
// Idle is green phosphor; plan/refine and build are amber/orange; vote pulses
// white; review is cyan. The empty phase ("") is idle.
func AccentForPhase(phase string) int {
	switch phase {
	case "plan", "refine":
		return NervOrange
	case "vote":
		return HotWhite
	case "build":
		return NervOrange
	case "review":
		return WireCyan
	default:
		return DataGreen
	}
}

// rampForAccent selects the luminance ramp matching an accent color index.
func rampForAccent(accent int) []int {
	switch accent {
	case NervOrange:
		return amberRamp
	case WireCyan:
		return cyanRamp
	case HotWhite, 15:
		return whiteRamp
	default:
		return phosphorRamp
	}
}

// sgr returns the indexed-256 foreground escape for a palette index.
func sgr(idx int) string {
	return "\x1b[38;5;" + strconv.Itoa(idx) + "m"
}

// reset clears all SGR attributes.
const reset = "\x1b[0m"

// styledRune wraps a single rune in an indexed-256 foreground color and a
// trailing reset. Used by the intro for one-off accented glyphs.
func styledRune(idx int, r rune) string {
	var b strings.Builder
	b.WriteString(sgr(idx))
	b.WriteRune(r)
	b.WriteString(reset)
	return b.String()
}
