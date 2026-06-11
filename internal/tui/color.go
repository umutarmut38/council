package tui

// Per-agent border colors. Everything is emitted as INDEXED 256-color SGR
// (38;5;N): the 6x6x6 cube and grayscale ramp (indices 16-255) render
// identically in every emulator (only 0-15 are themeable), whereas truecolor
// sequences proved unreliable in some terminals (VS Code) and SGR faint is
// implemented inconsistently everywhere. The unfocused "muted" variant is a
// computed darker index, not a terminal attribute.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// base16 is the reference palette for indices 0-15 (used only to interpret a
// configured base color; output indices are always >= 16).
var base16 = [16]uint32{
	0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xc0c0c0,
	0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
}

// cubeLevels are the channel values of the 6x6x6 color cube (indices 16-231).
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// xterm256RGB returns the reference RGB for a 256-color palette index.
func xterm256RGB(idx int) (r, g, b int) {
	switch {
	case idx < 0 || idx > 255:
		return 0xc0, 0xc0, 0xc0
	case idx < 16:
		v := base16[idx]
		return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
	case idx < 232:
		n := idx - 16
		return cubeLevels[n/36], cubeLevels[(n%36)/6], cubeLevels[n%6]
	default:
		v := 8 + (idx-232)*10
		return v, v, v
	}
}

// nearestANSI256 maps an RGB value to the closest palette index >= 16. The
// grayscale ramp is only considered for near-gray colors, so dimming a hue
// never collapses it into gray.
func nearestANSI256(r, g, b int) int {
	quant := func(v int) int {
		best, bestIdx := 1<<30, 0
		for i, level := range cubeLevels {
			d := v - level
			if d < 0 {
				d = -d
			}
			if d < best {
				best, bestIdx = d, i
			}
		}
		return bestIdx
	}
	ci, cj, ck := quant(r), quant(g), quant(b)
	cubeIdx := 16 + 36*ci + 6*cj + ck

	maxC, minC := r, r
	for _, v := range []int{g, b} {
		if v > maxC {
			maxC = v
		}
		if v < minC {
			minC = v
		}
	}
	if maxC-minC >= 24 {
		return cubeIdx
	}
	// Near-gray: pick whichever of cube vs gray ramp is closer.
	avg := (r + g + b) / 3
	gi := (avg - 8 + 5) / 10
	if gi < 0 {
		gi = 0
	}
	if gi > 23 {
		gi = 23
	}
	grayIdx := 232 + gi
	dist := func(idx int) int {
		ir, ig, ib := xterm256RGB(idx)
		return (ir-r)*(ir-r) + (ig-g)*(ig-g) + (ib-b)*(ib-b)
	}
	if dist(grayIdx) < dist(cubeIdx) {
		return grayIdx
	}
	return cubeIdx
}

// parseColorRGB understands the two formats config accepts: a 256-color index
// ("212") or a hex value ("#ff5f87").
func parseColorRGB(raw string) (r, g, b int, ok bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "#") {
		hex := strings.TrimPrefix(raw, "#")
		if len(hex) == 6 {
			if v, err := strconv.ParseUint(hex, 16, 32); err == nil {
				return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
			}
		}
		return 0, 0, 0, false
	}
	if idx, err := strconv.Atoi(raw); err == nil && idx >= 0 && idx <= 255 {
		r, g, b = xterm256RGB(idx)
		return r, g, b, true
	}
	return 0, 0, 0, false
}

// paneBorderColors resolves a configured agent color into two indexed colors:
// the full-strength focused color and a darkened muted variant for unfocused
// panes. ok is false when the value isn't an index or hex color.
func paneBorderColors(raw string) (focused, muted lipgloss.Color, ok bool) {
	r, g, b, ok := parseColorRGB(raw)
	if !ok {
		return "", "", false
	}
	raw = strings.TrimSpace(raw)
	if idx, err := strconv.Atoi(raw); err == nil && idx >= 0 && idx <= 255 {
		// Keep the exact configured index for the focused border.
		focused = lipgloss.Color(strconv.Itoa(idx))
	} else {
		focused = lipgloss.Color(strconv.Itoa(nearestANSI256(r, g, b)))
	}
	muted = lipgloss.Color(strconv.Itoa(nearestANSI256(r*45/100, g*45/100, b*45/100)))
	return focused, muted, true
}
