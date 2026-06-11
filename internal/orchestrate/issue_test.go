package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandFileRefsInlinesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("DETAILS HERE"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ExpandFileRefs("please fix @spec.md, and ignore @missing.md.", dir, FileRefOptions{})
	if !strings.Contains(got, "DETAILS HERE") {
		t.Fatalf("expected inlined contents, got: %q", got)
	}
	if !strings.Contains(got, "file: spec.md") {
		t.Fatalf("expected labeled delimiter, got: %q", got)
	}
	if !strings.Contains(got, "@missing.md") {
		t.Fatalf("missing file ref should be left as-is, got: %q", got)
	}
	// trailing comma after @spec.md must be preserved
	if !strings.Contains(got, ", and ignore") {
		t.Fatalf("trailing punctuation lost: %q", got)
	}
}

func TestExpandFileRefsRefusesOutsideBase(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	var warned []string
	opts := FileRefOptions{Warn: func(msg string) { warned = append(warned, msg) }}

	got := ExpandFileRefs("read @"+outside+" please", base, opts)
	if strings.Contains(got, "TOP SECRET") {
		t.Fatalf("absolute path outside base must not expand: %q", got)
	}
	if len(warned) == 0 {
		t.Fatal("expected a warning for the skipped reference")
	}

	// ../ escape is refused too.
	got = ExpandFileRefs("read @../secret.txt", base, opts)
	if strings.Contains(got, "TOP SECRET") {
		t.Fatalf("relative escape must not expand: %q", got)
	}

	// With AllowAbsolute the same reference expands.
	got = ExpandFileRefs("read @"+outside, base, FileRefOptions{AllowAbsolute: true})
	if !strings.Contains(got, "TOP SECRET") {
		t.Fatalf("AllowAbsolute should expand: %q", got)
	}
}

func TestExpandFileRefsSkipsHugeAndBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	got := ExpandFileRefs("see @big.txt", dir, FileRefOptions{MaxBytes: 10})
	if !strings.Contains(got, "@big.txt") || strings.Contains(got, "xxxxxxxxxx") {
		t.Fatalf("oversized file should stay a token: %q", got)
	}
	got = ExpandFileRefs("see @bin.dat", dir, FileRefOptions{})
	if !strings.Contains(got, "@bin.dat") || strings.Contains(got, "file: bin.dat") {
		t.Fatalf("binary file should stay a token: %q", got)
	}
}

func TestResolveIssueInlineAndFile(t *testing.T) {
	dir := t.TempDir()

	got, err := ResolveIssue(IssueSpec{Inline: "do the thing"}, dir, FileRefOptions{})
	if err != nil || got != "do the thing" {
		t.Fatalf("inline = %q, err = %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveIssue(IssueSpec{File: "task.md"}, dir, FileRefOptions{})
	if err != nil || !strings.Contains(got, "from file") {
		t.Fatalf("file = %q, err = %v", got, err)
	}

	if _, err := ResolveIssue(IssueSpec{}, dir, FileRefOptions{}); err == nil {
		t.Fatal("expected error when no issue provided")
	}
}
