// Package theme defines the TUI's color palette as a small set of named
// semantic roles (title, status, border, …) plus a registry of built-in themes
// and a way to build custom ones from config. It is deliberately free of
// lipgloss and of the config and tui packages so both can depend on it without a
// cycle: internal/config validates palettes against it, internal/tui resolves a
// Theme into its chrome styles.
//
// All colors are indexed-256 (see color.go for why); a Theme's fields are
// 256-color indices and the chrome's "muted" look is the Border index rather
// than a terminal attribute.
package theme

import (
	"fmt"
	"sort"
	"strings"
)

// Theme is a resolved palette of indexed-256 colors for the TUI chrome. Each
// field is a 256-color index in [0,255]; the field set mirrors the renderer's
// chrome roles one-to-one so the tui mapping stays trivial.
type Theme struct {
	Title   int // brand/header wordmark
	Heading int // section headings (artifacts, settings)
	Status  int // nominal/success readouts, diff additions
	Rail    int // progress rail, diff hunk headers, idle next-action
	Border  int // unfocused pane borders, dividers (the "muted" tone)
	Focus   int // focused pane border / active selection
	Suggest int // command suggestions
	Input   int // composer input text
	Warn    int // warnings/alerts, diff deletions
	Faint   int // de-emphasized secondary text
}

// Default is the stock palette. Its indices reproduce the historical
// package-level styles in internal/tui exactly, so selecting (or omitting)
// "default" is a no-op visual change.
func Default() Theme {
	return Theme{
		Title:   212, // magenta-pink
		Heading: 81,  // cyan
		Status:  114, // green
		Rail:    117, // light cyan
		Border:  238, // near-black gray
		Focus:   175, // mauve
		Suggest: 147, // periwinkle
		Input:   229, // light yellow
		Warn:    214, // orange
		Faint:   241, // dark gray
	}
}

// nord is a cool Frost/Polar-Night palette: steel-blue brand, frost-cyan focus,
// muted green status, slate borders.
func nord() Theme {
	return Theme{
		Title:   110, // steel blue
		Heading: 111, // brighter blue
		Status:  108, // muted green
		Rail:    109, // frost teal
		Border:  239, // slate
		Focus:   117, // bright frost cyan
		Suggest: 152, // pale cyan
		Input:   188, // snow-storm light gray
		Warn:    173, // nord orange
		Faint:   243, // mid gray
	}
}

// solarized is a warm palette: yellow brand, orange headings, amber focus, teal
// rail, on Solarized's muted base grays.
func solarized() Theme {
	return Theme{
		Title:   136, // solarized yellow
		Heading: 166, // solarized orange
		Status:  100, // solarized green (olive)
		Rail:    36,  // solarized cyan/teal
		Border:  240, // base01 gray
		Focus:   214, // bright amber
		Suggest: 66,  // muted teal
		Input:   230, // base2 cream
		Warn:    160, // solarized red
		Faint:   242, // base gray
	}
}

// mono is a high-contrast grayscale chrome with a single chromatic accent (red)
// reserved for warnings, so alerts still read at a glance.
func mono() Theme {
	return Theme{
		Title:   231, // bright white
		Heading: 255, // near-white
		Status:  250, // light gray
		Rail:    245, // mid gray
		Border:  238, // dim
		Focus:   231, // white (brightest = focus)
		Suggest: 244, // gray
		Input:   253, // light gray
		Warn:    203, // light red (the lone accent)
		Faint:   240, // dark gray
	}
}

// builtins is the registry of selectable themes. Keys are lowercase. The retro
// palette is intentionally absent: it stays a runtime-only in-app mode and is
// never a configurable name (so it never surfaces in generated docs).
var builtins = map[string]Theme{
	"default":   Default(),
	"nord":      nord(),
	"solarized": solarized(),
	"mono":      mono(),
}

// reserved are names that may never be selected as a config theme, even via a
// custom ui.themes.<name> palette of that name — "retro" is the runtime-only
// in-app mode, kept off the configurable surface entirely. Keys are lowercase.
var reserved = map[string]bool{
	"retro": true,
}

// IsReserved reports whether name is reserved and so can never be a selectable
// theme. Lookup is case-insensitive and whitespace-trimmed.
func IsReserved(name string) bool {
	return reserved[strings.ToLower(strings.TrimSpace(name))]
}

// Get returns the named built-in theme. Lookup is case-insensitive and
// whitespace-trimmed. ok is false for unknown names (including "retro").
func Get(name string) (Theme, bool) {
	t, ok := builtins[strings.ToLower(strings.TrimSpace(name))]
	return t, ok
}

// IsBuiltin reports whether name is a registered built-in theme.
func IsBuiltin(name string) bool {
	_, ok := Get(name)
	return ok
}

// BuiltinNames returns the registered theme names, sorted, for help text and
// validation error messages.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve picks the theme named by name: a built-in, else a custom palette from
// customs layered over Default, else Default. It never errors — an unknown name
// or an invalid custom palette falls back to Default (config validation surfaces
// those at load time, so this is only reached for already-valid configs).
func Resolve(name string, customs map[string]Palette) Theme {
	name = strings.TrimSpace(name)
	if name == "" || IsReserved(name) {
		return Default()
	}
	if t, ok := Get(name); ok {
		return t
	}
	if p, ok := customs[name]; ok {
		if t, err := p.Resolve(Default()); err == nil {
			return t
		}
	}
	return Default()
}

// Palette is a custom theme as written in config under ui.themes.<name>: each
// role is an optional color string (a 0-255 index or "#rrggbb"); empty roles
// inherit from the base theme it's resolved over. It lives here, beside Theme,
// so the role set has a single source of truth shared by config and tui.
type Palette struct {
	Title   string `yaml:"title,omitempty"`
	Heading string `yaml:"heading,omitempty"`
	Status  string `yaml:"status,omitempty"`
	Rail    string `yaml:"rail,omitempty"`
	Border  string `yaml:"border,omitempty"`
	Focus   string `yaml:"focus,omitempty"`
	Suggest string `yaml:"suggest,omitempty"`
	Input   string `yaml:"input,omitempty"`
	Warn    string `yaml:"warn,omitempty"`
	Faint   string `yaml:"faint,omitempty"`
}

// Resolve layers the palette's set roles over base, returning the resulting
// Theme. An invalid color string yields an error naming the offending role.
func (p Palette) Resolve(base Theme) (Theme, error) {
	out := base
	for _, f := range []struct {
		role string
		raw  string
		dst  *int
	}{
		{"title", p.Title, &out.Title},
		{"heading", p.Heading, &out.Heading},
		{"status", p.Status, &out.Status},
		{"rail", p.Rail, &out.Rail},
		{"border", p.Border, &out.Border},
		{"focus", p.Focus, &out.Focus},
		{"suggest", p.Suggest, &out.Suggest},
		{"input", p.Input, &out.Input},
		{"warn", p.Warn, &out.Warn},
		{"faint", p.Faint, &out.Faint},
	} {
		if strings.TrimSpace(f.raw) == "" {
			continue
		}
		idx, ok := ColorIndex(f.raw)
		if !ok {
			return Theme{}, fmt.Errorf("color %q: %q is not a 0-255 index or #rrggbb", f.role, f.raw)
		}
		*f.dst = idx
	}
	return out, nil
}

// Validate reports the first invalid color in the palette, if any.
func (p Palette) Validate() error {
	_, err := p.Resolve(Default())
	return err
}
