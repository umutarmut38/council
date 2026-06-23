package theme

// Indexed-256 color helpers. Everything council renders as color is emitted as
// INDEXED 256-color SGR (38;5;N): the 6x6x6 cube and grayscale ramp (indices
// 16-255) render identically in every emulator (only 0-15 are themeable),
// whereas truecolor sequences proved unreliable in some terminals (VS Code) and
// SGR faint is implemented inconsistently everywhere. Unfocused chrome therefore
// takes a theme's explicit Border color rather than a terminal "faint"
// attribute; the darker "muted" shade of a per-agent border color is computed
// from the configured color instead (see paneBorderColors in internal/tui).
//
// These helpers live in this package (free of lipgloss) so both internal/config
// (validation) and internal/tui (rendering) can reuse them without a cycle.

import (
	"strconv"
	"strings"
)

// base16 is the reference palette for indices 0-15, used by Xterm256RGB to turn
// a configured low index into RGB. (Quantization via NearestANSI256 only ever
// returns indices >= 16; a numeric index passed to ColorIndex is kept as-is, so
// a configured 0-15 index survives unchanged.)
var base16 = [16]uint32{
	0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xc0c0c0,
	0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
}

// cubeLevels are the channel values of the 6x6x6 color cube (indices 16-231).
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// Xterm256RGB returns the reference RGB for a 256-color palette index.
func Xterm256RGB(idx int) (r, g, b int) {
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

// NearestANSI256 maps an RGB value to the closest palette index >= 16. The
// grayscale ramp is only considered for near-gray colors, so dimming a hue
// never collapses it into gray.
func NearestANSI256(r, g, b int) int {
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
		ir, ig, ib := Xterm256RGB(idx)
		return (ir-r)*(ir-r) + (ig-g)*(ig-g) + (ib-b)*(ib-b)
	}
	if dist(grayIdx) < dist(cubeIdx) {
		return grayIdx
	}
	return cubeIdx
}

// ParseColorRGB understands the two formats config accepts: a 256-color index
// ("212") or a hex value ("#ff5f87").
func ParseColorRGB(raw string) (r, g, b int, ok bool) {
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
		r, g, b = Xterm256RGB(idx)
		return r, g, b, true
	}
	return 0, 0, 0, false
}

// ColorIndex resolves a configured color string to a 256-color index: a numeric
// index ("212") is returned as-is (0-255), while a hex value ("#ff5f87") is
// quantized to the nearest cube/grayscale index (>= 16). ok is false when the
// value is neither.
func ColorIndex(raw string) (idx int, ok bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n <= 255 {
		return n, true
	}
	r, g, b, ok := ParseColorRGB(raw)
	if !ok {
		return 0, false
	}
	return NearestANSI256(r, g, b), true
}
