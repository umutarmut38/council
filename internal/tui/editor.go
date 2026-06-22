package tui

// Integrated editor (/edit): a VSCode-style screen with a collapsible file-tree
// sidebar on the left and the configured editor (ui.editor) running inside a PTY
// pane on the right. The editor PTY reuses the same agent.Session + terminal
// emulator + output pump (m.launch) that powers the council agent panes; tree
// selection opens files in the live editor via ui.editor_open_cmd (default
// "<Esc>:e {path}<CR>", tuned for vim/nvim).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

// editorSessionName is the synthetic name of the integrated editor's PTY
// session. It is deliberately never added to m.Agents, so the editor is not a
// council pane and is never targeted by broadcasts; its output/exit messages are
// routed explicitly in Update.
const editorSessionName = "__editor__"

// editorTreeMaxWidth caps the sidebar so the editor pane keeps most of the width.
const editorTreeMaxWidth = 40

// editorNode is one visible row of the file-tree sidebar (the expanded subset of
// the repo, flattened depth-first).
type editorNode struct {
	Path     string // absolute path
	Name     string // base name for display
	Depth    int
	IsDir    bool
	Expanded bool
}

// cmdEdit opens the integrated editor screen rooted at the repo (or cwd). With a
// path argument, that file is revealed in the tree and opened immediately.
func (m *Model) cmdEdit(rest string) {
	m.enterEditorScreen(detectEditorRoot(), ScreenPanes)

	target := strings.TrimSpace(rest)
	if target != "" {
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(m.editorRoot, target)
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			m.revealEditorPath(abs)
			m.openInEditorPane(abs)
			return
		}
		m.Status = "edit: no such file: " + target
		return
	}
	m.Status = "editor — " + compressPath(m.editorRoot)
}

// enterEditorScreen switches to the editor screen anchored at root, returning to
// returnTo on Esc. It does not launch the editor (that happens on file open).
func (m *Model) enterEditorScreen(root string, returnTo ScreenMode) {
	m.editorRoot = root
	if m.editorExpanded == nil {
		m.editorExpanded = map[string]bool{}
	}
	m.editorExpanded[root] = true
	m.editorReturnScreen = returnTo
	m.buildEditorTree()
	m.editorTreeIndex = 0
	m.editorTreeTop = 0
	m.editorPaneFocused = false
	m.ScreenMode = ScreenEditor
	m.InputMode = InputComposer
	m.PromptInput = ""
}

// openInIntegratedEditor opens path in the integrated editor pane, anchoring the
// tree at the file's git root (so /artifacts and /compare files show in context),
// and returns to returnTo on Esc. It is the in-app counterpart to openInEditor.
func (m *Model) openInIntegratedEditor(path string, returnTo ScreenMode) {
	abs := path
	if !filepath.IsAbs(abs) {
		if a, err := filepath.Abs(abs); err == nil {
			abs = a
		}
	}
	info, statErr := os.Stat(abs)
	isDir := statErr == nil && info.IsDir()
	root := abs
	if !isDir {
		root = filepath.Dir(abs)
	}
	if r, err := orchestrate.DetectRepoRoot(root); err == nil && r != "" {
		root = r
	}
	m.enterEditorScreen(root, returnTo)
	switch {
	case isDir:
		m.Status = "editor — " + compressPath(root)
	case statErr == nil:
		m.revealEditorPath(abs)
		m.openInEditorPane(abs)
	default:
		m.Status = "edit: cannot open " + compressPath(abs)
	}
}

// detectEditorRoot returns the git repo root, falling back to the cwd.
func detectEditorRoot() string {
	if root, err := orchestrate.DetectRepoRoot("."); err == nil && root != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return cwd
	}
	return "."
}

// ---- file tree ----

// buildEditorTree flattens the expanded directories under editorRoot into the
// visible editorTree rows.
func (m *Model) buildEditorTree() {
	m.editorTree = m.editorTree[:0]
	m.appendEditorChildren(m.editorRoot, 0)
}

func (m *Model) appendEditorChildren(dir string, depth int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		name  string
		isDir bool
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		rel, relErr := filepath.Rel(m.editorRoot, full)
		if relErr != nil {
			continue
		}
		if shouldSkipFileChoice(filepath.ToSlash(rel), e.IsDir()) {
			continue
		}
		items = append(items, item{name: e.Name(), isDir: e.IsDir()})
	}
	// Directories first, then files; case-insensitive by name within each group.
	sort.Slice(items, func(i, j int) bool {
		if items[i].isDir != items[j].isDir {
			return items[i].isDir
		}
		return strings.ToLower(items[i].name) < strings.ToLower(items[j].name)
	})
	for _, it := range items {
		full := filepath.Join(dir, it.name)
		expanded := it.isDir && m.editorExpanded[full]
		m.editorTree = append(m.editorTree, editorNode{
			Path:     full,
			Name:     it.name,
			Depth:    depth,
			IsDir:    it.isDir,
			Expanded: expanded,
		})
		if expanded {
			m.appendEditorChildren(full, depth+1)
		}
	}
}

// selectEditorNode keeps the selection on the row for path after a rebuild.
func (m *Model) selectEditorNode(path string) {
	for i, n := range m.editorTree {
		if n.Path == path {
			m.editorTreeIndex = i
			return
		}
	}
}

// revealEditorPath expands every ancestor of abs and selects its row.
func (m *Model) revealEditorPath(abs string) {
	rel, err := filepath.Rel(m.editorRoot, abs)
	if err != nil {
		return
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := m.editorRoot
	for i := 0; i < len(parts)-1; i++ {
		cur = filepath.Join(cur, parts[i])
		m.editorExpanded[cur] = true
	}
	m.buildEditorTree()
	m.selectEditorNode(abs)
}

// activateEditorNode toggles a directory or opens a file on Enter/→.
func (m *Model) activateEditorNode() {
	if m.editorTreeIndex < 0 || m.editorTreeIndex >= len(m.editorTree) {
		return
	}
	node := m.editorTree[m.editorTreeIndex]
	if node.IsDir {
		m.editorExpanded[node.Path] = !m.editorExpanded[node.Path]
		m.buildEditorTree()
		m.selectEditorNode(node.Path)
		return
	}
	m.openInEditorPane(node.Path)
}

// collapseOrParent collapses an expanded directory, otherwise jumps to the
// selection's parent directory (← / h).
func (m *Model) collapseOrParent() {
	if m.editorTreeIndex < 0 || m.editorTreeIndex >= len(m.editorTree) {
		return
	}
	node := m.editorTree[m.editorTreeIndex]
	if node.IsDir && m.editorExpanded[node.Path] {
		m.editorExpanded[node.Path] = false
		m.buildEditorTree()
		m.selectEditorNode(node.Path)
		return
	}
	parent := filepath.Dir(node.Path)
	m.selectEditorNode(parent)
}

// ---- editor PTY pane ----

// launchEditor starts the configured editor in a PTY session opened on path,
// rendered in the right-hand pane. Output is pumped through m.launch exactly like
// council agent panes (AgentOutputMsg / AgentExitMsg).
func (m *Model) launchEditor(path string) {
	argv := append(m.editorArgv(), path)
	cfg := config.AgentConfig{
		Command:  argv,
		CWD:      m.editorRoot,
		Terminal: config.TerminalConfig{}, // zero value: dynamic size, resize on, Enter→\r
	}
	sess := agent.NewSession(editorSessionName, cfg, editorRawLogPath(m))
	m.editorView = &agentView{Session: sess, Width: 80, Height: 24}
	m.editorView.setScreenSize(80, 24)
	m.editorSessionRoot = m.editorRoot // CWD this session runs in (see openInEditorPane)
	m.resizeEditor()                   // size the PTY to the real pane before Start reads startupSize
	if m.launch != nil {
		m.launch(sess)
	}
	m.editorPaneFocused = true
	m.Status = "editing " + filepath.Base(path)
}

// editorRawLogPath returns a raw-log path for the editor session. Session.Start
// requires a writable path, so fall back to a temp file when there is no run
// store.
func editorRawLogPath(m *Model) string {
	if m.Store != nil {
		return m.Store.RawLogPath(editorSessionName)
	}
	return filepath.Join(os.TempDir(), "council-editor-raw.log")
}

// openInEditorPane opens abs (an absolute path) in the running editor via
// ui.editor_open_cmd, or launches/relaunches the editor. It relaunches when
// there is no live editor, in-place open is disabled, or the editor is running
// in a different root: its CWD would be stale, so the open command (and the
// editor's own relative ops) would resolve against the wrong directory.
func (m *Model) openInEditorPane(abs string) {
	live := m.editorView != nil && !m.editorView.Session.Done
	if !live || m.editorSessionRoot != m.editorRoot {
		if live {
			_ = m.editorView.Session.Terminate()
		}
		m.editorView = nil
		m.launchEditor(abs)
		return
	}

	seq, inPlace := m.editorOpenSequence(abs)
	if !inPlace {
		_ = m.editorView.Session.Terminate()
		m.editorView = nil
		m.launchEditor(abs)
		return
	}
	if err := m.editorView.Session.WriteString(seq); err != nil {
		m.Status = "editor: " + err.Error()
		return
	}
	m.editorPaneFocused = true
	m.Status = "opened " + filepath.Base(abs)
}

// editorOpenSequence resolves ui.editor_open_cmd into the keystrokes that open
// path in the live editor. path is absolute (CWD-independent) and vim-escaped so
// names with spaces or Ex metacharacters (e.g. `|`) open correctly. The bool is
// false when in-place opening is disabled (relaunch instead).
func (m *Model) editorOpenSequence(path string) (string, bool) {
	tmpl := "\x1b:e {path}\r" // default: <Esc>:e {path}<CR>
	if m.Config.UI.EditorOpenCmd != nil {
		tmpl = *m.Config.UI.EditorOpenCmd
	}
	if strings.TrimSpace(tmpl) == "" {
		return "", false
	}
	return strings.ReplaceAll(tmpl, "{path}", vimEscapePath(path)), true
}

// vimEscapeSpecial is vim's command-line filename special set (the common subset
// of fnameescape): these break `:e {path}` unless backslash-escaped.
const vimEscapeSpecial = " \t\n*?[{`$\\%#'\"|!<>()&;"

// vimEscapePath backslash-escapes characters special to vim's Ex command line so
// `:e {path}` opens files whose names contain spaces, `|`, `%`, `#`, etc.
func vimEscapePath(path string) string {
	var b strings.Builder
	b.Grow(len(path) + 8)
	for _, r := range path {
		if r < 128 && strings.ContainsRune(vimEscapeSpecial, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// closeEditor leaves the editor screen, returning to its origin. The editor
// session is kept alive so re-entering /edit resumes it (no silent data loss);
// it is torn down by terminateAgents on quit, or by the editor's own quit.
func (m *Model) closeEditor() {
	ret := m.editorReturnScreen
	m.editorReturnScreen = ScreenPanes
	m.editorPaneFocused = false
	m.ScreenMode = ret
	if ret == ScreenPanes {
		m.resizeAgents()
	}
	m.Status = m.screenModeName() // panes / compare / artifacts — wherever we returned
}

// ---- key handling ----

func (m *Model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Pane focus: keystrokes go straight to the editor PTY (Esc passes through so
	// vim/nvim leave insert mode; F2/Ctrl+O returns to the tree).
	if model, cmd, handled := m.routeEditorPaneKey(msg, "file tree — ↑/↓ move · Enter open · Tab editor"); handled {
		return model, cmd
	}

	// Tree focus.
	switch msg.String() {
	case "esc", "q":
		m.closeEditor()
	case "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit
	case "up", "k":
		if m.editorTreeIndex > 0 {
			m.editorTreeIndex--
		}
	case "down", "j":
		if m.editorTreeIndex < len(m.editorTree)-1 {
			m.editorTreeIndex++
		}
	case "g", "home":
		m.editorTreeIndex = 0
	case "G", "end":
		m.editorTreeIndex = max0(len(m.editorTree) - 1)
	case "enter", "right", "l":
		m.activateEditorNode()
	case "left", "h":
		m.collapseOrParent()
	case "tab":
		if m.editorView != nil && !m.editorView.Session.Done {
			m.editorPaneFocused = true
			m.Status = "editor — Esc passes through · F2/Ctrl+O back to tree"
		}
	}
	return m, nil
}

// routeEditorPaneKey routes a key to the editor PTY when the editor pane is
// focused. The bool is true when the key was consumed (the pane had focus), so
// callers fall through to their own left-column handling otherwise. Shared by the
// /edit (tree) and /artifacts (list) split screens; backLabel is the status set
// when F2/Ctrl+O returns focus to the left column.
func (m *Model) routeEditorPaneKey(msg tea.KeyMsg, backLabel string) (tea.Model, tea.Cmd, bool) {
	if !m.editorPaneFocused || m.editorView == nil || m.editorView.Session.Done {
		return m, nil, false
	}
	switch msg.String() {
	case "f2", "ctrl+o":
		m.editorPaneFocused = false
		m.Status = backLabel
		return m, nil, true
	case "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit, true
	}
	m.sendKeyToSession(m.editorView.Session, msg)
	return m, nil, true
}

// sendKeyToSession maps a key to a PTY byte sequence and writes it to session,
// matching direct mode (keyToPTY + submit/after-submit handling). Shared by
// handleDirectKey and the integrated editor pane.
func (m *Model) sendKeyToSession(session *agent.Session, msg tea.KeyMsg) {
	value := keyToPTY(msg, session.Config.Terminal.SubmitSequence)
	if value == "" {
		return
	}
	if msg.String() == "enter" {
		value += optionalSequence(session.Config.Terminal.AfterSubmitSequence)
	}
	if err := session.WriteString(value); err != nil {
		m.Status = err.Error()
	}
}

// ---- layout + rendering ----

// editorTreeWidth is the sidebar width: ~1/3 of the screen, capped, with a floor
// that still leaves room for the editor pane.
func (m Model) editorTreeWidth() int {
	w := m.Width / 3
	if w > editorTreeMaxWidth {
		w = editorTreeMaxWidth
	}
	if w < 16 {
		w = 16
	}
	if w > m.Width-10 {
		w = max0(m.Width - 10)
	}
	return w
}

// resizeEditor sizes the editor PTY + emulator to the right-hand pane.
func (m *Model) resizeEditor() {
	if m.editorView == nil || m.Width == 0 || m.Height == 0 {
		return
	}
	cols := max0(m.Width - m.editorTreeWidth() - 1) // 1 column for the separator
	rows := max0(m.Height - m.chromeLines())
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	m.editorView.setScreenSize(cols, rows)
	_ = m.editorView.Session.Resize(cols, rows)
}

func (m Model) renderEditor(bodyHeight int) []string {
	treeW := m.editorTreeWidth()
	paneW := max0(m.Width - treeW - 1)
	left := m.renderEditorTree(bodyHeight, treeW)
	right := m.renderEditorPane(bodyHeight, paneW, "select a file to edit")
	return m.joinColumns(left, right, treeW, bodyHeight)
}

// renderEditorPane renders the editor PTY (the right column shared by the /edit
// and /artifacts split screens): the live editor with a cursor when focused, its
// frozen last frame when not, or a centered placeholder when no editor is open.
func (m Model) renderEditorPane(height, width int, placeholder string) []string {
	if m.editorView != nil {
		// The cursor renders only while the pane has focus, so it doubles as a
		// focus indicator (the left column shows its own selection).
		if m.editorPaneFocused {
			return m.editorView.screenLinesCursor(height, width)
		}
		return m.editorView.screenLines(height, width)
	}
	pane := blankBlock(width, height)
	if row := height / 2; row >= 0 && row < len(pane) {
		pad := max0((width - len(placeholder)) / 2)
		pane[row] = fitText(strings.Repeat(" ", pad)+placeholder, width)
	}
	return pane
}

// joinColumns zips a fixed-width left column and a right column with a vertical
// separator into height rows.
func (m Model) joinColumns(left, right []string, leftW, height int) []string {
	sep := m.chrome().border.Render("│")
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fitText(l, leftW)+sep+r)
	}
	return out
}

// renderEditorTree renders the sidebar: a header line plus a scrolling window of
// tree rows that always keeps the selection visible.
// editorTreeVisibleTop returns the first tree row index rendered in a tree pane
// of the given height (one row is the FILES heading), scrolled to keep the
// selection visible. Shared by the renderer and mouse hit-testing so a click
// maps to the same entry the user sees.
func (m Model) editorTreeVisibleTop(height int) int {
	visible := max0(height - 1)
	top := m.editorTreeTop
	if m.editorTreeIndex < top {
		top = m.editorTreeIndex
	}
	if visible > 0 && m.editorTreeIndex >= top+visible {
		top = m.editorTreeIndex - visible + 1
	}
	if top < 0 {
		top = 0
	}
	return top
}

func (m Model) renderEditorTree(height, width int) []string {
	c := m.chrome()
	lines := make([]string, 0, height)
	lines = append(lines, c.heading.Render(fitText("FILES  "+compressPath(m.editorRoot), width)))

	top := m.editorTreeVisibleTop(height)

	for i := top; i < len(m.editorTree) && len(lines) < height; i++ {
		n := m.editorTree[i]
		marker := "  "
		if n.IsDir {
			if n.Expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
		}
		row := strings.Repeat("  ", n.Depth) + marker + n.Name
		if n.IsDir {
			row += "/"
		}
		if i == m.editorTreeIndex {
			treeFocused := !m.editorPaneFocused
			if treeFocused {
				lines = append(lines, c.focus.Render(fitText("> "+row, width)))
			} else {
				lines = append(lines, c.suggest.Render(fitText("> "+row, width)))
			}
			continue
		}
		style := c.input
		if n.IsDir {
			style = c.heading
		}
		lines = append(lines, style.Render(fitText("  "+row, width)))
	}
	for len(lines) < height {
		lines = append(lines, fitText("", width))
	}
	return lines
}
