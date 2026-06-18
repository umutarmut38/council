package tui

// Chrome theming. The header/footer/pane styles are selected per-render from a
// chromeStyles set rather than read from the package-level vars directly, so
// /eva mode can recolor the whole UI in the NERV palette and revert cleanly when
// toggled off — without ever mutating the shared package styles.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/tui/anim"
)

// chromeStyles is the set of styles the render paths use for the header, footer,
// and pane chrome.
type chromeStyles struct {
	title   lipgloss.Style
	status  lipgloss.Style
	rail    lipgloss.Style
	border  lipgloss.Style
	focus   lipgloss.Style
	suggest lipgloss.Style
	input   lipgloss.Style
	warn    lipgloss.Style
	heading lipgloss.Style
	faint   lipgloss.Style
}

// evaThemed reports whether the persistent NERV theme is active — EVA mode is on
// and its activation intro has finished (during the intro the UI is replaced by
// the full-screen sequence, so the chrome theme does not apply yet).
func (m Model) evaThemed() bool {
	return m.evaActive && m.evaIntroDone
}

// chrome selects the active chrome styles: the NERV palette while EVA mode is
// themed, otherwise the normal package styles.
func (m Model) chrome() chromeStyles {
	if m.evaThemed() {
		return evaChrome()
	}
	return defaultChrome()
}

// defaultChrome mirrors the package-level styles (the normal palette).
func defaultChrome() chromeStyles {
	return chromeStyles{
		title:   titleStyle,
		status:  statusStyle,
		rail:    railStyle,
		border:  borderStyle,
		focus:   focusStyle,
		suggest: suggestStyle,
		input:   inputStyle,
		warn:    warnStyle,
		heading: headingStyle,
		faint:   faintStyle,
	}
}

// evaChrome is the persistent NERV theme: orange labels, green data, cyan rail,
// red alerts — all indexed-256 so it honors the color constraint.
func evaChrome() chromeStyles {
	fg := func(i int) lipgloss.Style { return lipgloss.NewStyle().Foreground(idxColor(i)) }
	return chromeStyles{
		title:   fg(anim.NervOrange).Bold(true),
		status:  fg(anim.DataGreen),
		rail:    fg(anim.WireCyan),
		border:  fg(anim.NervOrangeDim),
		focus:   fg(anim.NervOrange).Bold(true),
		suggest: fg(anim.DataGreen),
		input:   fg(anim.Steel),
		warn:    fg(anim.AlarmRed),
		heading: fg(anim.NervOrange).Bold(true),
		faint:   fg(anim.SteelDim),
	}
}

// idxColor builds a lipgloss color from a 256-color index.
func idxColor(i int) lipgloss.Color {
	return lipgloss.Color(strconv.Itoa(i))
}

// CRT overlay. Applied to the whole themed screen as the last render step. It is
// built entirely from zero-width indexed-256 background SGR, so it tints rows
// without changing any line's visible width (the View invariants still hold).
const (
	crtReset = "\x1b[0m"
	// Indexed grayscale backgrounds (232 = near-black .. 237 = dim gray). Kept
	// deliberately low-contrast so the texture is a faint phosphor shimmer, not
	// bold stripes or a flashing field.
	crtGapBG    = 232 // near-black gap between scanlines
	crtLineBG   = 234 // lit scanline (content rows) — only a faint lift over the gap
	crtTrail3BG = 234 // far edge of the sweep's trailing glow
	crtTrail2BG = 235
	crtTrail1BG = 236 // near edge of the sweep's trailing glow
	crtSweepBG  = 237 // gentle refresh sweep bar
	crtMinBG    = 232 // darkest background the flicker will reach
	crtFlickerN = 31  // rare flicker cadence (frames) — an occasional blip, not a pulse
)

// applyCRT lays a faint retro-CRT texture over a fully composed screen, built
// entirely from zero-width indexed-256 backgrounds so every line keeps its exact
// visible width. Four low-contrast layers combine per row: gentle scanlines (a
// lit row barely lifted over a near-black gap), a soft refresh bar that sweeps
// downward with a short trailing glow above it, a vignette that darkens the top
// and bottom edges, and a rare, faint whole-field flicker every crtFlickerN
// frames — a shimmer rather than a flash.
func applyCRT(screen string, frame int) string {
	lines := strings.Split(screen, "\n")
	n := len(lines)
	if n == 0 {
		return screen
	}
	sweep := frame % n
	dim := 0
	if frame%crtFlickerN == 0 {
		dim = 1
	}
	for i, line := range lines {
		// Scanlines: lit phosphor row over a darker gap row.
		bg := crtLineBG
		if i%2 == 1 {
			bg = crtGapBG
		}
		// Sweep bar with a short trailing glow on the rows just above it.
		switch sweep - i {
		case 0:
			bg = crtSweepBG
		case 1:
			if bg < crtTrail1BG {
				bg = crtTrail1BG
			}
		case 2:
			if bg < crtTrail2BG {
				bg = crtTrail2BG
			}
		case 3:
			if bg < crtTrail3BG {
				bg = crtTrail3BG
			}
		}
		// Vignette: cap the brightness toward the top and bottom edges.
		edge := i
		if e := n - 1 - i; e < edge {
			edge = e
		}
		if cap, ok := crtVignetteCap(edge); ok && bg > cap {
			bg = cap
		}
		// Subtle global flicker, clamped so it never dips below the floor.
		if bg-dim >= crtMinBG {
			bg -= dim
		}
		lines[i] = crtRowBG(line, bg)
	}
	return strings.Join(lines, "\n")
}

// crtVignetteCap returns the maximum background brightness allowed at edge rows
// (distance from the nearest top/bottom edge), darkening the tube's edges.
func crtVignetteCap(edge int) (int, bool) {
	switch edge {
	case 0:
		return 232, true
	case 1:
		return 232, true
	case 2:
		return 233, true
	}
	return 0, false
}

// crtRowBG paints an indexed background across a row, re-asserting it after every
// SGR reset inside the row so foreground changes in the content never punch a
// hole in the band. All added codes are zero-width.
func crtRowBG(line string, bg int) string {
	set := "\x1b[48;5;" + strconv.Itoa(bg) + "m"
	line = strings.ReplaceAll(line, crtReset, crtReset+set)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+set)
	return set + line + crtReset
}

// evaPaneColors cycles unfocused pane borders across the NERV accents so the
// roster stays readable while themed.
var evaPaneColors = []int{anim.NervOrange, anim.DataGreen, anim.WireCyan}

// evaPaneStyle is the EVA-mode pane-border style: the focused pane is full
// orange, the rest cycle across the NERV accents and are muted (a computed
// darker index, never SGR faint) while unfocused.
func evaPaneStyle(index int, focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Bold(true).Foreground(idxColor(anim.NervOrange))
	}
	accent := evaPaneColors[index%len(evaPaneColors)]
	if _, muted, ok := paneBorderColors(strconv.Itoa(accent)); ok {
		return lipgloss.NewStyle().Foreground(muted)
	}
	return lipgloss.NewStyle().Foreground(idxColor(accent))
}
