package anim

import "strings"

// Block-letter "EVA font": a 5-row figlet-style alphabet used for the intro
// title cards (NERV / EVANGELION) and the themed COUNCIL header banner. A
// terminal can't swap fonts, so big institutional lettering is drawn as solid
// block glyphs.

const blockRows = 5

// blockFont maps a rune to its 5-row glyph. Rows within a glyph are padded to a
// common width by Banner, so the source rows need not be perfectly aligned.
var blockFont = map[rune][]string{
	'A': {" ██ ", "█  █", "████", "█  █", "█  █"},
	'B': {"███ ", "█  █", "███ ", "█  █", "███ "},
	'C': {" ███", "█   ", "█   ", "█   ", " ███"},
	'D': {"███ ", "█  █", "█  █", "█  █", "███ "},
	'E': {"████", "█   ", "███ ", "█   ", "████"},
	'F': {"████", "█   ", "███ ", "█   ", "█   "},
	'G': {" ███", "█   ", "█ ██", "█  █", " ███"},
	'H': {"█  █", "█  █", "████", "█  █", "█  █"},
	'I': {"███", " █ ", " █ ", " █ ", "███"},
	'J': {"  ██", "   █", "   █", "█  █", " ██ "},
	'K': {"█  █", "█ █ ", "██  ", "█ █ ", "█  █"},
	'L': {"█   ", "█   ", "█   ", "█   ", "████"},
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'N': {"█   █", "██  █", "█ █ █", "█  ██", "█   █"},
	'O': {" ██ ", "█  █", "█  █", "█  █", " ██ "},
	'P': {"███ ", "█  █", "███ ", "█   ", "█   "},
	'Q': {" ██ ", "█  █", "█  █", "█ ██", " ███"},
	'R': {"███ ", "█  █", "███ ", "█ █ ", "█  █"},
	'S': {" ███", "█   ", " ██ ", "   █", "███ "},
	'T': {"███", " █ ", " █ ", " █ ", " █ "},
	'U': {"█  █", "█  █", "█  █", "█  █", " ██ "},
	'V': {"█   █", "█   █", "█   █", " █ █ ", "  █  "},
	'W': {"█   █", "█   █", "█ █ █", "██ ██", "█   █"},
	'X': {"█   █", " █ █ ", "  █  ", " █ █ ", "█   █"},
	'Y': {"█   █", " █ █ ", "  █  ", "  █  ", "  █  "},
	'Z': {"████", "   █", "  █ ", " █  ", "████"},
	'0': {" ██ ", "█  █", "█  █", "█  █", " ██ "},
	'1': {" █ ", "██ ", " █ ", " █ ", "███"},
	'2': {"███ ", "   █", " ██ ", "█   ", "████"},
	'3': {"███ ", "   █", " ██ ", "   █", "███ "},
	'4': {"█  █", "█  █", "████", "   █", "   █"},
	'5': {"████", "█   ", "███ ", "   █", "███ "},
	'6': {" ██ ", "█   ", "███ ", "█  █", " ██ "},
	'7': {"████", "   █", "  █ ", " █  ", " █  "},
	'8': {" ██ ", "█  █", " ██ ", "█  █", " ██ "},
	'9': {" ██ ", "█  █", " ███", "   █", " ██ "},
	'/': {"   █", "  █ ", " █  ", "█   ", "█   "},
	'-': {"    ", "    ", "████", "    ", "    "},
	'.': {"  ", "  ", "  ", "  ", "██"},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// Banner renders text as 5 rows of block letters joined with a one-column gap.
// Unknown runes render as a space. Rows are returned equal-width.
func Banner(text string) []string {
	rows := make([]string, blockRows)
	parts := make([][]string, 0, len(text))
	for _, r := range strings.ToUpper(text) {
		g, ok := blockFont[r]
		if !ok {
			g = blockFont[' ']
		}
		parts = append(parts, padGlyph(g))
	}
	for r := 0; r < blockRows; r++ {
		seg := make([]string, len(parts))
		for i, g := range parts {
			seg[i] = g[r]
		}
		rows[r] = strings.Join(seg, " ")
	}
	return rows
}

// padGlyph right-pads every row of a glyph to its widest row so columns align.
func padGlyph(g []string) []string {
	w := 0
	for _, row := range g {
		if n := len([]rune(row)); n > w {
			w = n
		}
	}
	out := make([]string, blockRows)
	for i := 0; i < blockRows; i++ {
		row := ""
		if i < len(g) {
			row = g[i]
		}
		if n := len([]rune(row)); n < w {
			row += strings.Repeat(" ", w-n)
		}
		out[i] = row
	}
	return out
}

// BannerWidth is the column width of a rendered banner.
func BannerWidth(rows []string) int {
	if len(rows) == 0 {
		return 0
	}
	return len([]rune(rows[0]))
}

// NervMotto is the institutional motto that arcs under the NERV emblem.
const NervMotto = "GOD'S IN HIS HEAVEN, ALL'S RIGHT WITH THE WORLD"
