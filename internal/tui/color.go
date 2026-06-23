package tui

// Per-agent border colors. The indexed-256 machinery (cube/grayscale mapping,
// hex/index parsing) lives in internal/theme so config validation can reuse it
// without importing tui; this file is just the lipgloss glue that turns a
// configured agent color into the focused/muted border pair.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/theme"
)

// paneBorderColors resolves a configured agent color into two indexed colors:
// the full-strength focused color and a darkened muted variant for unfocused
// panes. ok is false when the value isn't an index or hex color.
func paneBorderColors(raw string) (focused, muted lipgloss.Color, ok bool) {
	r, g, b, ok := theme.ParseColorRGB(raw)
	if !ok {
		return "", "", false
	}
	raw = strings.TrimSpace(raw)
	if idx, err := strconv.Atoi(raw); err == nil && idx >= 0 && idx <= 255 {
		// Keep the exact configured index for the focused border.
		focused = lipgloss.Color(strconv.Itoa(idx))
	} else {
		focused = lipgloss.Color(strconv.Itoa(theme.NearestANSI256(r, g, b)))
	}
	muted = lipgloss.Color(strconv.Itoa(theme.NearestANSI256(r*45/100, g*45/100, b*45/100)))
	return focused, muted, true
}
