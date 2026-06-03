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

	got := ExpandFileRefs("please fix @spec.md, and ignore @missing.md.", dir)
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

func TestResolveIssueInlineAndFile(t *testing.T) {
	dir := t.TempDir()

	got, err := ResolveIssue(IssueSpec{Inline: "do the thing"}, dir)
	if err != nil || got != "do the thing" {
		t.Fatalf("inline = %q, err = %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveIssue(IssueSpec{File: "task.md"}, dir)
	if err != nil || !strings.Contains(got, "from file") {
		t.Fatalf("file = %q, err = %v", got, err)
	}

	if _, err := ResolveIssue(IssueSpec{}, dir); err == nil {
		t.Fatal("expected error when no issue provided")
	}
}
