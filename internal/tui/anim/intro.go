package anim

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// introCell is one character of the intro grid: a rune, its color index (-1 for
// the default foreground), and whether it is the trailing continuation half of
// a wide (CJK) glyph and so emits nothing of its own.
type introCell struct {
	r     rune
	color int
	cont  bool
}

// introBeat is one stepped readout in the activation log: it appears once frame
// reaches reveal and is drawn as "LABEL ……… VALUE".
type introBeat struct {
	reveal     int
	label      string
	value      string
	labelColor int
	valueColor int
}

// introReadouts is the NERV/MAGI boot log, revealed line by line. The MAGI three
// come online first, then the synchronization and field readouts — the
// institutional theater of an EVA activation. Reveal frames are spread across
// evaIntroFrames (see internal/tui/model.go); the last lands well before it.
var introReadouts = []introBeat{
	{reveal: 7, label: "MELCHIOR-1", value: "ONLINE", labelColor: Steel, valueColor: DataGreen},
	{reveal: 11, label: "BALTHASAR-2", value: "ONLINE", labelColor: Steel, valueColor: DataGreen},
	{reveal: 15, label: "CASPER-3", value: "ONLINE", labelColor: Steel, valueColor: DataGreen},
	{reveal: 30, label: "HARMONICS", value: "NOMINAL", labelColor: Steel, valueColor: DataGreen},
	{reveal: 35, label: "EGO BORDER", value: "CLEAR", labelColor: Steel, valueColor: DataGreen},
	{reveal: 40, label: "LCL PRESSURE", value: "STABLE", labelColor: Steel, valueColor: WireCyan},
	{reveal: 45, label: "A.T. FIELD", value: "DISENGAGED", labelColor: NervOrange, valueColor: DataGreen},
}

// Intro beat thresholds. They are spread across evaIntroFrames so the sequence
// keeps unfolding for the whole activation, ending on the ACTIVATION flash.
const (
	introSubtitleAt   = 3
	introMagiAt       = 20 // "MAGI SYSTEM ONLINE" consensus banner
	introSyncAt       = 24 // synchronization line + sync-ratio bar begin
	introSyncFull     = 58 // frame the sync ratio reaches 100%
	introEvangelAt    = 50
	introActivationAt = 56
)

// Intro renders one frame of the NERV activation sequence into a full-screen
// w x h field and returns it as a single string of h lines, each exactly w
// visible columns. The sequence reveals in stepped beats keyed off frame — the
// MAGI coming online, a climbing synchronization ratio, instrument readouts, a
// REC timecode and registration marks, and a final ACTIVATION flash — over the
// rotating EVA-01 head, with a vertical scan line sweeping across.
func Intro(w, h, frame int) string {
	if w < 1 || h < 1 {
		return ""
	}
	grid := make([]introCell, w*h)
	for i := range grid {
		grid[i] = introCell{r: ' ', color: -1}
	}

	// Scan line first, so revealed text and the head paint over it.
	scanCol := (frame * 2) % w
	for r := 0; r < h; r++ {
		grid[r*w+scanCol] = introCell{r: '│', color: scanDim}
	}

	// Corner registration marks + a blinking REC timecode frame the field like a
	// monitoring feed.
	placeInstrumentFrame(grid, w, h, frame)

	// Title: the NERV emblem (fig-leaf + wordmark) in NERV red when there's room,
	// the block wordmark on a medium field, spaced caps when tiny.
	emblem := NervEmblem()
	small := Banner("NERV")
	wordBottom := 2 // the row just below the wordmark
	placeRows := func(rows []string) {
		for i, line := range rows {
			placeText(grid, w, h, 1+i, centerCol(w, line), line, EvaRed)
		}
		wordBottom = 1 + len(rows)
	}
	switch {
	case w >= runewidth.StringWidth(emblem[0])+2 && h >= 30:
		placeRows(emblem)
	case w >= runewidth.StringWidth(small[0])+2 && h >= 16:
		placeRows(small)
	default:
		placeCentered(grid, w, h, 1, "N E R V", EvaRed)
	}
	// The motto (red, part of the logo) then the system label (cyan). Their rows
	// are reserved regardless of frame so the head below never shifts when they
	// reveal.
	mottoRow := wordBottom
	subtitleRow := wordBottom + 1
	if frame >= introSubtitleAt {
		placeCentered(grid, w, h, mottoRow, NervMotto, EvaRed)
		placeCentered(grid, w, h, subtitleRow, "NERV CENTRAL DOGMA · MAGI SYSTEM", WireCyan)
	}
	titleBottom := subtitleRow // the head starts below this

	// Layout: a bottom band (sync bar, EVANGELION, ACTIVATION) anchored to the
	// bottom edge, with the readout log stacked above it.
	activationRow := h - 2
	evangelionRow := h - 4
	barRow := h - 6
	readoutBottom := barRow - 2
	n := len(introReadouts)
	syncRow := readoutBottom        // the synchronization line caps the log
	readoutTop := readoutBottom - n // first readout row (MELCHIOR)
	magiRow := readoutTop - 2       // MAGI consensus banner above the stack

	// The rotating EVA-01 head is the centerpiece: blit it FIRST and large,
	// filling the band from just under the title down to the sync bar, so the
	// institutional readouts overlay on top of it like a NERV monitor feed.
	headTop := titleBottom + 1
	headBottom := barRow - 1
	if headH := headBottom - headTop + 1; headH >= 4 {
		headW := w - 4
		if headW > 64 {
			headW = 64
		}
		blitHead(grid, w, h, (w-headW)/2, headTop, headW, headH, frame)
	}

	// Bottom band, over the head.
	if frame >= introActivationAt {
		// Blink between hot white and orange for the final beat.
		col := NervOrange
		if (frame/3)%2 == 0 {
			col = HotWhite
		}
		placeCentered(grid, w, h, activationRow, "▶▶  ACTIVATION  ◀◀", col)
	}
	if frame >= introEvangelAt {
		placeCentered(grid, w, h, evangelionRow, "E V A N G E L I O N", Steel)
	}
	if frame >= introSyncAt {
		frac := float64(frame-introSyncAt) / float64(introSyncFull-introSyncAt)
		placeSyncBar(grid, w, h, barRow, frac)
	}

	// Readout log, over the head.
	if frame >= introMagiAt {
		placeCentered(grid, w, h, magiRow, "──  MAGI SYSTEM ONLINE  ──", DataGreen)
	}
	for i, b := range introReadouts {
		if frame >= b.reveal {
			placeReadout(grid, w, h, readoutTop+i, b.label, b.value, b.labelColor, b.valueColor)
		}
	}
	if frame >= introSyncAt {
		jp := "同期"
		latin := " SYNCHRONIZATION START"
		start := centerCol(w, jp+latin)
		placeText(grid, w, h, syncRow, start, jp, WireCyan)
		placeText(grid, w, h, syncRow, start+runewidth.StringWidth(jp), latin, DataGreen)
	}

	return introCompose(grid, w, h)
}

// placeReadout writes "LABEL ……… VALUE" centered on a row: the label, an ASCII
// dotted leader (dim steel), then the value. Width-aware via placeText.
func placeReadout(grid []introCell, w, h, row int, label, value string, labelColor, valueColor int) {
	target := w * 2 / 5
	if target > 40 {
		target = 40
	}
	if target < 18 {
		target = 18
	}
	lw := runewidth.StringWidth(label)
	vw := runewidth.StringWidth(value)
	dots := target - lw - vw - 2
	if dots < 1 {
		dots = 1
	}
	leader := strings.Repeat(".", dots)
	full := label + " " + leader + " " + value
	start := centerCol(w, full)
	placeText(grid, w, h, row, start, label, labelColor)
	placeText(grid, w, h, row, start+lw+1, leader, SteelDim)
	placeText(grid, w, h, row, start+lw+1+dots+1, value, valueColor)
}

// placeSyncBar draws a centered "SYNC RATIO [████░░░░] NN.N%" gauge whose fill
// and percentage climb with frac (clamped to [0,1]).
func placeSyncBar(grid []introCell, w, h, row int, frac float64) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	barW := w / 3
	if barW > 30 {
		barW = 30
	}
	if barW < 8 {
		barW = 8
	}
	fill := int(float64(barW)*frac + 0.5)
	label := "SYNC RATIO "
	pct := fmt.Sprintf(" %5.1f%%", frac*100)
	full := label + "[" + strings.Repeat("█", barW) + "]" + pct
	start := centerCol(w, full)
	col := start
	placeText(grid, w, h, row, col, label, WireCyan)
	col += runewidth.StringWidth(label)
	placeText(grid, w, h, row, col, "[", SteelDim)
	col++
	placeText(grid, w, h, row, col, strings.Repeat("█", fill), DataGreen)
	placeText(grid, w, h, row, col+fill, strings.Repeat("░", barW-fill), SteelDim)
	col += barW
	placeText(grid, w, h, row, col, "]", SteelDim)
	col++
	placeText(grid, w, h, row, col, pct, DataGreen)
}

// placeInstrumentFrame stamps the corner registration ticks and a blinking REC
// timecode, framing the field like a NERV monitoring feed. All placements clip
// to the grid, so it is safe at any size.
func placeInstrumentFrame(grid []introCell, w, h, frame int) {
	placeText(grid, w, h, 0, 0, "┌─", SteelDim)
	placeText(grid, w, h, 0, w-2, "─┐", SteelDim)
	placeText(grid, w, h, h-1, 0, "└─", SteelDim)
	placeText(grid, w, h, h-1, w-2, "─┘", SteelDim)

	// Blinking REC dot + a fake running timecode in the top-left.
	tc := fmt.Sprintf("%02d:%02d:%02d", (frame/3600)%24, (frame/60)%60, frame%60)
	if (frame/4)%2 == 0 {
		placeText(grid, w, h, 0, 3, "●", AlarmRed)
	}
	placeText(grid, w, h, 0, 5, "REC "+tc, SteelDim)
}

// blitHead renders the head into an hw x hh sub-grid and copies its lit cells
// into the intro grid at (hx, hy), leaving the scan line/labels showing through
// the gaps.
func blitHead(grid []introCell, w, h, hx, hy, hw, hh, frame int) {
	chars, colors, ok := headGrid(hw, hh, frame)
	if !ok {
		return
	}
	for r := 0; r < hh; r++ {
		gy := hy + r
		if gy < 0 || gy >= h {
			continue
		}
		for c := 0; c < hw; c++ {
			ch := chars[r*hw+c]
			if ch == ' ' {
				continue
			}
			gx := hx + c
			if gx < 0 || gx >= w {
				continue
			}
			grid[gy*w+gx] = introCell{r: ch, color: colors[r*hw+c]}
		}
	}
}

const scanDim = 23 // dim cyan, a CRT phosphor scan line under the readability floor

// placeCentered writes s horizontally centered on a row.
func placeCentered(grid []introCell, w, h, row int, s string, color int) {
	placeText(grid, w, h, row, centerCol(w, s), s, color)
}

// centerCol returns the starting column that centers s in width w.
func centerCol(w int, s string) int {
	col := (w - runewidth.StringWidth(s)) / 2
	if col < 0 {
		col = 0
	}
	return col
}

// placeText writes s into the grid starting at (row, col), honoring rune widths
// so wide CJK glyphs occupy two cells (the second marked as a continuation).
// Out-of-range rows and glyphs that would overflow the right edge are skipped.
func placeText(grid []introCell, w, h, row, col int, s string, color int) {
	if row < 0 || row >= h {
		return
	}
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw <= 0 {
			rw = 1
		}
		if col < 0 {
			col += rw
			continue
		}
		if col+rw > w {
			break
		}
		grid[row*w+col] = introCell{r: r, color: color}
		if rw == 2 {
			grid[row*w+col+1] = introCell{r: ' ', color: color, cont: true}
		}
		col += rw
	}
}

// introCompose turns the cell grid into styled lines, coalescing color runs and
// resetting at blanks and line ends. Continuation cells emit nothing (their wide
// glyph already covered the column). Every line is exactly w visible columns.
func introCompose(grid []introCell, w, h int) string {
	lines := make([]string, h)
	for r := 0; r < h; r++ {
		var b strings.Builder
		cur := -1
		for c := 0; c < w; c++ {
			cell := grid[r*w+c]
			if cell.cont {
				continue
			}
			if cell.r == ' ' && cell.color < 0 {
				if cur != -1 {
					b.WriteString(reset)
					cur = -1
				}
				b.WriteByte(' ')
				continue
			}
			if cell.color != cur {
				b.WriteString(sgr(cell.color))
				cur = cell.color
			}
			b.WriteRune(cell.r)
		}
		if cur != -1 {
			b.WriteString(reset)
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}
