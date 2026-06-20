// Package anim renders the council's retro-themed visuals: a genuinely
// 3D, rotating head for the activity band and the retro activation intro
// for retro mode.
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

// retro palette, mapped to the nearest 256-color indices. These mirror the
// canonical web palette tuned for the retro theme.
const (
	Amber    = 208 // labels / headers / focused chrome
	AmberDim = 166 // muted orange for unfocused chrome
	Green    = 83  // nominal data readouts
	Cyan     = 51  // wireframes / spatial data
	Red      = 196 // emergencies / blocked panes
	Steel    = 253 // secondary text
	SteelDim = 240 // de-emphasized secondary text
	White    = 231 // peak pulse (vote)
	Violet   = 141 // purple accent
	Crimson  = 160 // red trim (#d3290f) — focused pane border
)

// eyeColor is the fixed near-white used for the head's eyes (the white sclera
// in the reference), kept distinct from the body so the gaze always reads.
const eyeColor = 231

// charRamp shades the head dark -> bright. Index 0 (space) is reserved for
// empty cells, so a plotted surface point never collapses to blank.
const charRamp = " .:-=+*#%@"

// the head's material ramps (dark -> bright), mapped to the nearest indexed-256
// colors from the head's canonical palette: a lavender-purple shell, neon
// yellow-green accents, grey joints, and red trim. Each shades by luminance so
// the 3D form reads under directional light, but the hue stays fixed (the head
// is the head, not a phase indicator).
var (
	purpleRamp = []int{53, 54, 97, 98, 99, 140, 141, 183}      // body shell
	greenRamp  = []int{22, 28, 70, 76, 113, 149, 155, 191}     // neon accents
	greyRamp   = []int{236, 238, 240, 243, 245, 248, 250, 252} // joints / neck
	redRamp    = []int{52, 88, 124, 160, 196, 202, 203, 210}   // red trim
	darkRamp   = []int{16, 233, 234, 235, 236, 238, 240, 242}  // eye sockets / black
)

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
