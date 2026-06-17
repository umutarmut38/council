// Package docs regenerates the generated regions of the Markdown docs from the
// code that is their single source of truth: the command registry
// (internal/command) and the config schema (internal/config). The hand-written
// prose around each region is preserved; only the content between the
//
//	<!-- BEGIN GENERATED: <id> --> … <!-- END GENERATED: <id> -->
//
// markers is replaced. Run `go generate ./...` to refresh the files; a test in
// this package fails if the committed docs are stale.
package docs

//go:generate go run ./gen

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/umutarmut38/council/internal/command"
	"github.com/umutarmut38/council/internal/config"
)

// target is one managed doc file and the generated content for each region id
// it contains.
type target struct {
	path    string
	regions map[string]string
}

func targets() []target {
	return []target{
		{path: "docs/commands.md", regions: command.CLIReferenceRegions()},
		{path: "docs/configuration.md", regions: map[string]string{
			"config-schema": config.SchemaMarkdown(),
		}},
	}
}

// repoRoot resolves the repository root from this file's location, so the
// generator and the drift test work regardless of the caller's cwd.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func beginMarker(id string) string { return "<!-- BEGIN GENERATED: " + id + " -->" }
func endMarker(id string) string   { return "<!-- END GENERATED: " + id + " -->" }

// replaceRegions swaps the body between each region's markers with content,
// keeping the markers themselves. It errors if a marker is missing or the end
// precedes the begin.
func replaceRegions(src string, regions map[string]string) (string, error) {
	out := src
	for id, content := range regions {
		begin, end := beginMarker(id), endMarker(id)
		bi := strings.Index(out, begin)
		if bi < 0 {
			return "", fmt.Errorf("missing marker %q", begin)
		}
		ei := strings.Index(out, end)
		if ei < bi {
			return "", fmt.Errorf("missing or misordered marker %q", end)
		}
		out = out[:bi] + begin + "\n" + content + "\n" + end + out[ei+len(end):]
	}
	return out, nil
}

// rendered returns the desired content of one target.
func rendered(t target) (string, error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(), t.path))
	if err != nil {
		return "", err
	}
	return replaceRegions(string(raw), t.regions)
}

// Write regenerates every managed doc file in place.
func Write() error {
	for _, t := range targets() {
		want, err := rendered(t)
		if err != nil {
			return fmt.Errorf("%s: %w", t.path, err)
		}
		if err := os.WriteFile(filepath.Join(repoRoot(), t.path), []byte(want), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Check reports the first managed doc whose committed content differs from what
// the generators would produce.
func Check() error {
	for _, t := range targets() {
		want, err := rendered(t)
		if err != nil {
			return fmt.Errorf("%s: %w", t.path, err)
		}
		got, err := os.ReadFile(filepath.Join(repoRoot(), t.path))
		if err != nil {
			return err
		}
		if string(got) != want {
			return fmt.Errorf("%s is stale; run `go generate ./...`", t.path)
		}
	}
	return nil
}
