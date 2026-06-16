package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/command"
)

// TestDocsUpToDate fails when a committed doc's generated region is stale.
// Run `go generate ./...` to refresh.
func TestDocsUpToDate(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestEveryComposerCommandIsDocumented guards the hand-written in-chat command
// prose: a command added to the registry must also be described in
// docs/commands.md.
func TestEveryComposerCommandIsDocumented(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(), "docs/commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, c := range command.Composers() {
		if !strings.Contains(doc, "/"+c.Name) {
			t.Errorf("composer command /%s is not documented in docs/commands.md", c.Name)
		}
	}
}

func TestReplaceRegions(t *testing.T) {
	src := "before\n<!-- BEGIN GENERATED: x -->\nOLD\n<!-- END GENERATED: x -->\nafter\n"
	got, err := replaceRegions(src, map[string]string{"x": "NEW"})
	if err != nil {
		t.Fatal(err)
	}
	want := "before\n<!-- BEGIN GENERATED: x -->\nNEW\n<!-- END GENERATED: x -->\nafter\n"
	if got != want {
		t.Fatalf("replaceRegions = %q, want %q", got, want)
	}

	if _, err := replaceRegions("no markers here", map[string]string{"x": "NEW"}); err == nil {
		t.Fatal("expected an error when markers are missing")
	}
}
