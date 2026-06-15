package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/umutarmut38/council/internal/cmdrun"
	"github.com/umutarmut38/council/internal/config"
)

// IssueSpec describes where the task text comes from. Exactly one source is
// used, in priority order: GitHub issue, file, then inline text.
type IssueSpec struct {
	Inline string
	File   string
	Number int
}

var fileRefPattern = regexp.MustCompile(`@([^\s@]+)`)

// FileRefOptionsFromConfig derives expansion limits from the config: absolute
// paths need files.allow_absolute AND a policy other than safe.
func FileRefOptionsFromConfig(cfg config.Config) FileRefOptions {
	return FileRefOptions{
		AllowAbsolute: cfg.Files.AllowAbsolute && !cfg.Policy.IsSafe(),
		MaxBytes:      cfg.Files.MaxRefBytes(),
	}
}

// FileRefOptions constrains @path expansion. The zero value is the safe
// default: only files inside baseDir, capped at 256 KiB, binaries skipped.
type FileRefOptions struct {
	// AllowAbsolute permits absolute paths and paths that escape baseDir.
	AllowAbsolute bool
	// MaxBytes caps a single expanded file (0 = 256 KiB).
	MaxBytes int
	// Warn receives a message for each token that was skipped and why.
	Warn func(msg string)
}

func (o FileRefOptions) maxBytes() int {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return 256 << 10
}

func (o FileRefOptions) warn(msg string) {
	if o.Warn != nil {
		o.Warn(msg)
	}
}

// ResolveIssue produces the task text for a run, expanding @path references in
// inline/file text. baseDir resolves relative paths (usually the cwd).
func ResolveIssue(spec IssueSpec, baseDir string, opts FileRefOptions) (string, error) {
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
		return ExpandFileRefs(string(data), baseDir, opts), nil
	default:
		if strings.TrimSpace(spec.Inline) == "" {
			return "", errors.New("no issue provided: pass text, --file, or --issue")
		}
		return ExpandFileRefs(spec.Inline, baseDir, opts), nil
	}
}

// ExpandFileRefs replaces @path tokens whose file exists with the file's
// contents inlined under a labeled delimiter. Tokens that don't resolve to a
// readable file are left untouched, and trailing punctuation is preserved.
// Unless opts.AllowAbsolute is set, only paths inside baseDir expand — a
// pasted task must not be able to quietly inline ~/.ssh keys or /etc files
// into an agent prompt. Oversized and binary files are skipped.
func ExpandFileRefs(text string, baseDir string, opts FileRefOptions) string {
	return fileRefPattern.ReplaceAllStringFunc(text, func(tok string) string {
		raw := tok[1:]
		path := strings.TrimRight(raw, ".,;:!?)")
		trailing := raw[len(path):]

		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(baseDir, path)
		}
		if !opts.AllowAbsolute && escapesBase(full, baseDir) {
			opts.warn(fmt.Sprintf("@%s: outside the working directory; set files.allow_absolute to expand it", path))
			return tok
		}
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() {
			return tok
		}
		if fi.Size() > int64(opts.maxBytes()) {
			opts.warn(fmt.Sprintf("@%s: %d bytes exceeds the %d byte limit (files.max_bytes)", path, fi.Size(), opts.maxBytes()))
			return tok
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return tok
		}
		if isBinary(data) {
			opts.warn(fmt.Sprintf("@%s: skipped binary file", path))
			return tok
		}
		body := strings.TrimRight(string(data), "\n")
		return fmt.Sprintf("\n--- file: %s ---\n%s\n--- end file: %s ---\n%s", path, body, path, trailing)
	})
}

// escapesBase reports whether path resolves outside base.
func escapesBase(path, base string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isBinary uses the classic NUL-byte heuristic on the first 8000 bytes.
func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}

func fetchGitHubIssue(number int) (string, error) {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "gh", Args: []string{"issue", "view", strconv.Itoa(number), "--json", "title,body"}})
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
