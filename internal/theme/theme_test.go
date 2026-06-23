package theme

import "testing"

func TestGetBuiltins(t *testing.T) {
	for _, name := range []string{"default", "nord", "solarized", "mono"} {
		if _, ok := Get(name); !ok {
			t.Errorf("Get(%q) not found, want a built-in", name)
		}
	}
	// Case-insensitive, whitespace-trimmed.
	if _, ok := Get("  Nord "); !ok {
		t.Error("Get should trim/lowercase the name")
	}
}

func TestRetroIsNotABuiltin(t *testing.T) {
	// The retro palette is a runtime-only in-app mode; it must never be a
	// configurable theme (so it never surfaces in generated docs).
	for _, name := range []string{"retro", "bogus", "fancy"} {
		if _, ok := Get(name); ok {
			t.Errorf("Get(%q) found, but it must not be a configurable theme", name)
		}
		if IsBuiltin(name) {
			t.Errorf("IsBuiltin(%q) = true, want false", name)
		}
	}
}

func TestRetroIsReserved(t *testing.T) {
	for _, name := range []string{"retro", "RETRO", "  Retro "} {
		if !IsReserved(name) {
			t.Errorf("IsReserved(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"default", "nord", "mine", ""} {
		if IsReserved(name) {
			t.Errorf("IsReserved(%q) = true, want false", name)
		}
	}
	// A reserved name resolves to the default palette even when a custom palette
	// of that name is defined — it can never be selected.
	customs := map[string]Palette{"retro": {Title: "33"}}
	if got := Resolve("retro", customs); got != Default() {
		t.Errorf("Resolve(retro) = %+v, want Default (reserved, not the custom palette)", got)
	}
}

func TestDefaultMatchesHistoricalPalette(t *testing.T) {
	// These indices reproduce the original internal/tui package styles; changing
	// them is a visible UI change, so lock them.
	d := Default()
	want := Theme{
		Title: 212, Heading: 81, Status: 114, Rail: 117, Border: 238,
		Focus: 175, Suggest: 147, Input: 229, Warn: 214, Faint: 241,
	}
	if d != want {
		t.Fatalf("Default() = %+v, want %+v", d, want)
	}
}

func TestPaletteResolveLayersOverBase(t *testing.T) {
	p := Palette{Title: "33", Warn: "#ff0000"}
	got, err := p.Resolve(Default())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Title != 33 {
		t.Errorf("Title = %d, want 33", got.Title)
	}
	// #ff0000 quantizes to the cube red (196).
	if got.Warn != 196 {
		t.Errorf("Warn = %d, want 196 (#ff0000)", got.Warn)
	}
	// Unset roles inherit the base.
	if got.Status != Default().Status {
		t.Errorf("Status = %d, want inherited %d", got.Status, Default().Status)
	}
}

func TestPaletteResolveRejectsBadColor(t *testing.T) {
	if _, err := (Palette{Focus: "not-a-color"}).Resolve(Default()); err == nil {
		t.Fatal("Resolve should reject an invalid color")
	}
	if err := (Palette{Border: "300"}).Validate(); err == nil {
		t.Fatal("Validate should reject an out-of-range index")
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve("", nil); got != Default() {
		t.Errorf("empty name = %+v, want Default", got)
	}
	if got := Resolve("nope", nil); got != Default() {
		t.Errorf("unknown name = %+v, want Default fallback", got)
	}
	if got, _ := Get("nord"); Resolve("nord", nil) != got {
		t.Error("Resolve(nord) should equal the built-in")
	}
	customs := map[string]Palette{"mine": {Title: "200"}}
	if got := Resolve("mine", customs); got.Title != 200 {
		t.Errorf("custom theme Title = %d, want 200", got.Title)
	}
}

func TestColorIndex(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"212", 212, true},
		{"#ff0000", 196, true},
		{"  16 ", 16, true},
		{"256", 0, false},
		{"#zzz", 0, false},
		{"", 0, false},
	} {
		got, ok := ColorIndex(tc.raw)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ColorIndex(%q) = (%d, %v), want (%d, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
