package orchestrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// IssueSpec describes where the task text comes from. Exactly one source is
// used, in priority order: GitHub issue, file, then inline text.
type IssueSpec struct {
	Inline string
	File   string
	Number int
}

var fileRefPattern = regexp.MustCompile(`@([^\s@]+)`)

// ResolveIssue produces the task text for a run, expanding @path references in
// inline/file text. baseDir resolves relative paths (usually the cwd).
func ResolveIssue(spec IssueSpec, baseDir string) (string, error) {
	switch {
	case spec.Number > 0:
		return fetchGitHubIssue(spec.Number)
	case strings.TrimSpace(spec.File) != "":
		path := spec.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read issue file: %w", err)
		}
		return ExpandFileRefs(string(data), baseDir), nil
	default:
		if strings.TrimSpace(spec.Inline) == "" {
			return "", errors.New("no issue provided: pass text, --file, or --issue")
		}
		return ExpandFileRefs(spec.Inline, baseDir), nil
	}
}

// ExpandFileRefs replaces @path tokens whose file exists with the file's
// contents inlined under a labeled delimiter. Tokens that don't resolve to a
// readable file are left untouched, and trailing punctuation is preserved.
func ExpandFileRefs(text string, baseDir string) string {
	return fileRefPattern.ReplaceAllStringFunc(text, func(tok string) string {
		raw := tok[1:]
		path := strings.TrimRight(raw, ".,;:!?)")
		trailing := raw[len(path):]

		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return tok
		}
		body := strings.TrimRight(string(data), "\n")
		return fmt.Sprintf("\n--- file: %s ---\n%s\n--- end file: %s ---\n%s", path, body, path, trailing)
	})
}

func fetchGitHubIssue(number int) (string, error) {
	out, err := exec.Command("gh", "issue", "view", strconv.Itoa(number), "--json", "title,body").Output()
	if err != nil {
		return "", fmt.Errorf("gh issue view %d: %w", number, err)
	}
	var view struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return "", fmt.Errorf("parse gh issue %d: %w", number, err)
	}
	return fmt.Sprintf("# %s\n\n%s", strings.TrimSpace(view.Title), strings.TrimSpace(view.Body)), nil
}
