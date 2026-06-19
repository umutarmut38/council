package anim

import (
	_ "embed"
	"strings"
)

// logoRaw is the retro emblem ASCII art (emblem + wordmark + the diagonal),
// embedded from splash.dat.
//
//go:embed splash.dat
var logoRaw string

// logoLines is the emblem with trailing spaces and surrounding blank lines
// trimmed. Callers render it red and center it; Logo scales it to fit.
var logoLines = parseLogo(logoRaw)

func parseLogo(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// LogoSize is the emblem's native width and height in cells.
func LogoSize() (w, h int) {
	for _, l := range logoLines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	return w, len(logoLines)
}

// Logo returns the emblem scaled to fit within maxW x maxH (never upscaled),
// as plain lines for the caller to color red and center.
func Logo(maxW, maxH int) []string {
	return scaleArt(logoLines, maxW, maxH)
}

// scaleArt downsamples ASCII art to fit maxW x maxH, mapping each output cell's
// ink coverage to a density ramp so the silhouette and its edges survive. Art
// that already fits is returned unchanged. The scale is uniform so the shape
// keeps its proportions.
func scaleArt(lines []string, maxW, maxH int) []string {
	if maxW < 1 || maxH < 1 || len(lines) == 0 {
		return nil
	}
	rows := make([][]rune, len(lines))
	srcW := 0
	for i, l := range lines {
		rows[i] = []rune(l)
		if len(rows[i]) > srcW {
			srcW = len(rows[i])
		}
	}
	srcH := len(rows)
	if srcW == 0 {
		return nil
	}
	if srcW <= maxW && srcH <= maxH {
		return lines
	}
	s := float64(srcW) / float64(maxW)
	if sy := float64(srcH) / float64(maxH); sy > s {
		s = sy
	}
	outW := int(float64(srcW)/s + 0.5)
	outH := int(float64(srcH)/s + 0.5)
	const ramp = " .:-=+*#"
	out := make([]string, outH)
	for oy := 0; oy < outH; oy++ {
		var b strings.Builder
		for ox := 0; ox < outW; ox++ {
			x0, x1 := int(float64(ox)*s), int(float64(ox+1)*s)
			y0, y1 := int(float64(oy)*s), int(float64(oy+1)*s)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			ink, total := 0, 0
			for y := y0; y < y1 && y < srcH; y++ {
				for x := x0; x < x1; x++ {
					total++
					if x < len(rows[y]) && rows[y][x] != ' ' {
						ink++
					}
				}
			}
			ch := byte(' ')
			if total > 0 {
				ch = ramp[int(float64(ink)/float64(total)*float64(len(ramp)-1)+0.5)]
			}
			b.WriteByte(ch)
		}
		out[oy] = b.String()
	}
	return out
}
