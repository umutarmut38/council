package tui

// @file references: suggestion discovery/completion in the composer and
// expansion of @path tokens before a message is sent.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/umutarmut38/council/internal/orchestrate"
)

func (m Model) activeFileRefToken() (string, bool) {
	start, query, ok := m.activeFileRef()
	if !ok {
		return "", false
	}
	return m.PromptInput[start : start+1+len(query)], true
}

func (m Model) activeFileRef() (start int, query string, ok bool) {
	idx := strings.LastIndex(m.PromptInput, "@")
	if idx < 0 {
		return 0, "", false
	}
	if idx > 0 {
		prev, _ := utf8.DecodeLastRuneInString(m.PromptInput[:idx])
		if prev != 0 && !isRefBoundary(prev) {
			return 0, "", false
		}
	}
	query = m.PromptInput[idx+1:]
	if strings.ContainsAny(query, " \t\r\n") {
		return 0, "", false
	}
	if idx == 0 && query != "" && !strings.ContainsAny(query, "./") && m.agentExists(query) {
		return 0, "", false
	}
	token := m.PromptInput[idx:]
	if token != "" && token == m.fileSuggestHidden {
		return 0, "", false
	}
	return idx, query, true
}

func isRefBoundary(r rune) bool {
	return r == ' ' || r == '\t' || r == '(' || r == '[' || r == '{' || r == ':' || r == ','
}

func (m Model) fileSuggestionMatches() []string {
	_, query, ok := m.activeFileRef()
	if !ok {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	prefix := make([]string, 0)
	contains := make([]string, 0)
	for _, choice := range m.FileChoices {
		lower := strings.ToLower(choice)
		switch {
		case query == "", strings.HasPrefix(lower, query):
			prefix = append(prefix, choice)
		case strings.Contains(lower, query):
			contains = append(contains, choice)
		}
	}
	matches := append(prefix, contains...)
	if len(matches) > 8 {
		return matches[:8]
	}
	return matches
}

// filePaletteActive reports whether an @file reference is being typed (and
// has something to suggest), which opens the vertical file palette.
func (m Model) filePaletteActive() bool {
	if m.ScreenMode != ScreenPanes || m.InputMode != InputComposer {
		return false
	}
	_, _, ok := m.activeFileRef()
	return ok
}

// renderFilePalette is the vertical @file list, a sibling of the command
// palette: ↑/↓ select, Enter inserts the path.
func (m Model) renderFilePalette() []string {
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		return []string{suggestStyle.Render(fitText("@file: no matches — Esc closes the picker", m.Width))}
	}
	index := m.FileSuggestIndex
	if index < 0 {
		index = 0
	}
	if index >= len(matches) {
		index = len(matches) - 1
	}
	lines := make([]string, 0, len(matches)+1)
	for i, match := range matches {
		if i == index {
			lines = append(lines, focusStyle.Render(fitText(" > "+match, m.Width)))
		} else {
			lines = append(lines, faintStyle.Render(fitText("   "+match, m.Width)))
		}
	}
	lines = append(lines, faintStyle.Render(fitText(fmt.Sprintf("@file %d/%d · ↑/↓ select · Enter insert · Esc close", index+1, len(matches)), m.Width)))
	return lines
}

func (m *Model) moveFileSuggestion(delta int) bool {
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		return false
	}
	m.FileSuggestIndex = (m.FileSuggestIndex + delta) % len(matches)
	if m.FileSuggestIndex < 0 {
		m.FileSuggestIndex = len(matches) - 1
	}
	return true
}

func (m *Model) acceptFileSuggestion() bool {
	start, query, ok := m.activeFileRef()
	if !ok {
		return false
	}
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		return false
	}
	if m.FileSuggestIndex < 0 {
		m.FileSuggestIndex = 0
	}
	if m.FileSuggestIndex >= len(matches) {
		m.FileSuggestIndex = len(matches) - 1
	}
	end := start + 1 + len(query)
	replacement := "@" + matches[m.FileSuggestIndex]
	if end == len(m.PromptInput) {
		replacement += " "
	}
	m.PromptInput = m.PromptInput[:start] + replacement + m.PromptInput[end:]
	m.FileSuggestIndex = 0
	m.fileSuggestHidden = ""
	return true
}

func discoverFileChoices() []string {
	out, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err == nil {
		return cleanFileChoices(strings.Split(string(out), "\n"))
	}

	paths := make([]string, 0)
	_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == "." {
			return nil
		}
		clean := filepath.ToSlash(path)
		if d.IsDir() {
			if shouldSkipFileChoice(clean, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldSkipFileChoice(clean, false) {
			paths = append(paths, strings.TrimPrefix(clean, "./"))
		}
		return nil
	})
	return cleanFileChoices(paths)
}

func cleanFileChoices(paths []string) []string {
	seen := map[string]bool{}
	choices := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		path = strings.TrimPrefix(path, "./")
		if path == "" || seen[path] || shouldSkipFileChoice(path, false) {
			continue
		}
		seen[path] = true
		choices = append(choices, path)
	}
	sort.Strings(choices)
	return choices
}

func shouldSkipFileChoice(path string, dir bool) bool {
	if path == "" {
		return true
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		switch part {
		case ".git", ".council", "node_modules", "vendor", "dist", "build", "target":
			return true
		}
		if strings.HasPrefix(part, ".") && part != "." {
			switch part {
			case ".github", ".agents", ".codex":
			default:
				return true
			}
		}
	}
	return dir && strings.HasPrefix(filepath.Base(path), ".")
}

// expandRefs inlines @path file references relative to council's working dir,
// honoring the configured expansion limits. A skipped reference surfaces in
// the status line rather than failing the send.
func (m *Model) expandRefs(text string) string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}
	opts := orchestrate.FileRefOptionsFromConfig(m.Config)
	opts.Warn = func(msg string) { m.Status = msg }
	return orchestrate.ExpandFileRefs(text, cwd, opts)
}
