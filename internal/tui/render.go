package tui

// Rendering: header, footer, the agent grid, panes, and the overview,
// settings, and runs screens.

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/command"
	"github.com/umutarmut38/council/internal/config"
)

func (m Model) renderHeader() string {
	names := make([]string, 0, len(m.Agents))
	for _, view := range m.Agents {
		name := view.Session.Name
		if view.Session.Done {
			name += " done"
		}
		names = append(names, name)
	}

	agents := "Agents: " + strings.Join(names, ", ")
	line := "Council | " + agents
	if m.Store != nil {
		line += " | " + compressPath(m.Store.RunDir)
	}
	// When the roster doesn't fit, a count keeps the run path visible.
	if lipgloss.Width(line) > m.Width {
		line = fmt.Sprintf("Council | %d agents", len(m.Agents))
		if m.Store != nil {
			line += " | " + compressPath(m.Store.RunDir)
		}
	}
	page := fmt.Sprintf("Page %d/%d · agents %s · grouped by %s", m.PageIndex+1, m.pageCount(), m.pageRangeLabel(), m.groupByLabel())
	if filter := m.displayFilterLabel(); filter != "" {
		page += " · showing " + filter
	}
	if m.ScreenMode != ScreenPanes {
		page = strings.ToUpper(m.screenModeName()) + " · " + page
	}
	header := titleStyle.Render(fitText(line, m.Width)) + "\n" + statusStyle.Render(fitText(page+" · "+m.Status, m.Width))
	if m.progress != nil {
		header += "\n" + railStyle.Render(fitText(m.progress.phaseRail(), m.Width))
	}
	return header
}

func (m Model) renderFooter() string {
	if m.ScreenMode != ScreenPanes {
		hint := "Esc back"
		switch m.ScreenMode {
		case ScreenOverview:
			hint = "Overview: ↑/↓ select · Space show/hide personality · Enter focus · Esc back"
		case ScreenSettings:
			hint = "Settings: ↑/↓ select · ←/→ change · Esc back"
		case ScreenRuns:
			hint = "Runs: ↑/↓ select · Enter resume · Esc back"
		case ScreenArtifacts:
			hint = "Artifacts: ↑/↓ select/scroll · Enter open · e $EDITOR · Esc back"
		case ScreenCompare:
			hint = "Compare: ↑/↓ select · Enter files/diff · d full diff · x mark pair · e $EDITOR · Esc back"
		}
		return strings.Join([]string{
			suggestStyle.Render(fitText(hint, m.Width)),
			inputBoxTop(m.screenModeName(), m.Width),
			inputBoxContent(m.Status, m.Width),
			inputBoxBottom(m.Width),
		}, "\n")
	}

	if m.InputMode == InputDirect {
		hint := suggestStyle.Render(fitText("DIRECT MODE — keystrokes go straight to the pane. Esc/F2 returns to the composer.", m.Width))
		label := "direct: " + m.focusedName()
		content := "keys → " + m.focusedName()
		return strings.Join([]string{hint, inputBoxTop(label, m.Width), inputBoxContent(content, m.Width), inputBoxBottom(m.Width)}, "\n")
	}

	label := m.targetLabel()
	if m.Zoomed {
		label = "[zoom] " + label
	}

	prompt := m.targetPrompt()
	content := prompt + " > " + m.PromptInput + "_"

	lines := m.suggestionBlock()
	lines = append(lines,
		inputBoxTop(label, m.Width),
		inputBoxContent(content, m.Width),
		inputBoxBottom(m.Width),
	)
	return strings.Join(lines, "\n")
}

// suggestionBlock is the area above the composer: the vertical @file picker
// or command palette while one is being typed, a single hint line otherwise.
func (m Model) suggestionBlock() []string {
	if m.filePaletteActive() {
		return m.renderFilePalette()
	}
	if m.paletteActive() {
		return m.renderPalette()
	}
	return []string{m.suggestionLine()}
}

// suggestionLine shows matching /commands while one is being typed, and the key
// hints otherwise.
func (m Model) suggestionLine() string {
	if strings.HasPrefix(m.PromptInput, "/") {
		prefix := strings.ToLower(strings.TrimPrefix(strings.Fields(m.PromptInput + " ")[0], "/"))
		all := command.Composers()
		parts := make([]string, 0, len(all))
		for _, c := range all {
			if strings.HasPrefix(c.Name, prefix) {
				entry := "/" + c.Name
				if c.Args != "" {
					entry += " " + c.Args
				}
				parts = append(parts, entry+" — "+c.Desc)
			}
		}
		text := strings.Join(parts, "   ")
		if text == "" {
			text = "no matching command"
		}
		return suggestStyle.Render(fitText(text, m.Width))
	}

	// During a run, next actions beat the generic shortcut list; blocked
	// panes beat everything.
	if hint, ok := m.contextHint(); ok {
		if len(m.attentionAgents()) > 0 {
			return warnStyle.Render(fitText(hint, m.Width))
		}
		return suggestStyle.Render(fitText(hint, m.Width))
	}

	help := "Enter send | Ctrl+G overview | F2 direct | Ctrl+B target | Ctrl+F zoom | Ctrl+N/P page | Tab focus | @file"
	return faintStyle.Render(fitText(help, m.Width))
}

func (m Model) targetLabel() string {
	switch m.Target {
	case TargetFocused:
		return "→ " + m.focusedName()
	case TargetPersonality:
		return "personality: " + m.personalityLabel(m.TargetName)
	case TargetCategory:
		return "category: " + m.categoryLabel(m.TargetName)
	default:
		return "broadcast to all"
	}
}

func (m Model) targetPrompt() string {
	switch m.Target {
	case TargetFocused:
		return m.focusedName()
	case TargetPersonality:
		return "personality:" + m.personalityLabel(m.TargetName)
	case TargetCategory:
		return "category:" + m.categoryLabel(m.TargetName)
	default:
		return "all"
	}
}

func inputBoxTop(label string, width int) string {
	if width < 2 {
		return borderStyle.Render(strings.Repeat("─", max0(width)))
	}
	inner := width - 2
	lbl := ""
	if label != "" {
		lbl = "─ " + label + " "
	}
	lbl = truncateText(lbl, inner)
	pad := inner - lipgloss.Width(lbl)
	if pad < 0 {
		pad = 0
	}
	return borderStyle.Render("╭" + lbl + strings.Repeat("─", pad) + "╮")
}

func inputBoxBottom(width int) string {
	if width < 2 {
		return borderStyle.Render(strings.Repeat("─", max0(width)))
	}
	return borderStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")
}

func inputBoxContent(text string, width int) string {
	if width < 2 {
		return inputStyle.Render(fitText(text, width))
	}
	inner := width - 2
	return borderStyle.Render("│") + inputStyle.Render(fitText(" "+text, inner)) + borderStyle.Render("│")
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func (m Model) renderBody(bodyHeight int) string {
	switch m.ScreenMode {
	case ScreenOverview:
		return strings.Join(m.renderOverview(bodyHeight), "\n")
	case ScreenSettings:
		return strings.Join(m.renderSettings(bodyHeight), "\n")
	case ScreenRuns:
		return strings.Join(m.renderRuns(bodyHeight), "\n")
	case ScreenArtifacts:
		return strings.Join(m.renderArtifacts(bodyHeight), "\n")
	case ScreenCompare:
		return strings.Join(m.renderCompare(bodyHeight), "\n")
	default:
		return m.renderGrid(bodyHeight)
	}
}

func (m Model) renderGrid(bodyHeight int) string {
	if len(m.Agents) == 0 {
		return strings.Join(blankBlock(m.Width, bodyHeight), "\n")
	}

	if m.Zoomed {
		return strings.Join(m.renderPane(m.FocusedIndex, m.Width, bodyHeight), "\n")
	}

	rows, cols := m.gridDims()
	widths := distribute(m.Width, cols)
	heights := distribute(bodyHeight, rows)
	indexes := m.visibleAgentIndexes()

	rowViews := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		paneLines := make([][]string, 0, cols)
		for col := 0; col < cols; col++ {
			pos := row*cols + col
			if pos >= len(indexes) {
				paneLines = append(paneLines, blankBlock(widths[col], heights[row]))
				continue
			}
			paneLines = append(paneLines, m.renderPane(indexes[pos], widths[col], heights[row]))
		}
		for line := 0; line < heights[row]; line++ {
			var b strings.Builder
			for col := 0; col < cols; col++ {
				b.WriteString(paneLines[col][line])
			}
			rowViews = append(rowViews, b.String())
		}
	}

	return strings.Join(rowViews, "\n")
}

// renderOverview is the run dashboard: run/phase/next-action and per-phase
// progress on top, then the agent roster with roles and artifact state.
func (m Model) renderOverview(bodyHeight int) []string {
	lines := make([]string, 0, bodyHeight)

	if p := m.progress; p != nil {
		title := "Run " + p.Stamp
		if m.phase != "" {
			title += " · Phase: " + capitalize(m.phase)
		}
		lines = append(lines, headingStyle.Render(fitText(title, m.Width)))
		if p.Next != "" {
			lines = append(lines, fitText("Next: "+p.Next, m.Width))
		}
		lines = append(lines, "")
		for _, ph := range p.Phases {
			status := "not started"
			switch ph.State {
			case phaseDone:
				status = "complete"
			case phaseActive:
				status = "in progress"
			}
			counter := ""
			if ph.Counted && ph.Expected > 0 {
				counter = fmt.Sprintf("%d/%d ", ph.Done, ph.Expected)
			}
			lines = append(lines, fitText(fmt.Sprintf("  %-8s %s%s", ph.Label, counter, status), m.Width))
		}
		lines = append(lines, "")
		lines = append(lines, headingStyle.Render(fitText("Agents", m.Width)))
	}

	indexes := m.overviewIndexes()
	lastGroup := ""
	for pos, idx := range indexes {
		if idx < 0 || idx >= len(m.Agents) {
			continue
		}
		view := m.Agents[idx]
		group := m.agentGroupLabel(view.Session.Name)
		if group != lastGroup {
			lines = append(lines, headingStyle.Render(fitText(group, m.Width)))
			lastGroup = group
		}
		marker := " "
		if pos == m.OverviewIndex {
			marker = ">"
		}
		visibility := "visible"
		if !m.agentIsDisplayed(view.Session.Name) {
			visibility = "hidden"
		}
		page := m.pageForIndex(idx) + 1
		label := fmt.Sprintf("%s %s · %s · %s · %s · page %d · %s",
			marker, view.Session.Name, m.agentRoleLabel(view.Session.Name),
			m.agentPersonalityLabel(view.Session.Name), visibility, page, m.paneBadge(view))
		line := fitText(label, m.Width)
		if view.Attention && !view.Session.Done {
			line = warnStyle.Render(line)
		}
		lines = append(lines, line)
	}
	if len(indexes) == 0 {
		lines = append(lines, "No agents")
	}
	return fitBlock(lines, m.Width, bodyHeight)
}

// agentRoleLabel summarizes an agent's structural role(s).
func (m Model) agentRoleLabel(name string) string {
	agentCfg, ok := m.Config.Agents[name]
	if !ok || len(agentCfg.Role) == 0 {
		return "worker+reviewer"
	}
	worker := agentCfg.HasRole(config.RoleWorker)
	reviewer := agentCfg.HasRole(config.RoleReviewer)
	switch {
	case worker && reviewer:
		return "worker+reviewer"
	case worker:
		return "worker"
	case reviewer:
		return "reviewer"
	default:
		return "no role"
	}
}

func (m Model) renderSettings(bodyHeight int) []string {
	items := m.settingsItems()
	lines := make([]string, 0, bodyHeight)
	lines = append(lines, headingStyle.Render(fitText("Settings", m.Width)))
	for i, item := range items {
		marker := " "
		if i == m.SettingsIndex {
			marker = ">"
		}
		lines = append(lines, fitText(fmt.Sprintf("%s %s: %s", marker, item.name, item.value), m.Width))
	}
	lines = append(lines, "")
	lines = append(lines, fitText("Current layout: "+m.layoutPreview(), m.Width))
	lines = append(lines, "")
	lines = append(lines, faintStyle.Render(fitText("Changes apply to this session. Edit YAML (ui.adaptive_grid, ui.page_rows, …) to make them permanent.", m.Width)))
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderRuns(bodyHeight int) []string {
	lines := []string{headingStyle.Render(fitText("Runs", m.Width))}
	if len(m.Runs) == 0 {
		lines = append(lines, faintStyle.Render(fitText("No runs found.", m.Width)))
		return fitBlock(lines, m.Width, bodyHeight)
	}
	for i, run := range m.Runs {
		marker := " "
		if i == m.RunIndex {
			marker = ">"
		}
		parts := []string{marker + " " + run.Stamp}
		if run.Winner != "" {
			parts = append(parts, "winner "+run.Winner)
		}
		artifact := fmt.Sprintf("plans:%d votes:%d reviews:%d", len(run.Plans), len(run.Votes), len(run.Reviews))
		parts = append(parts, artifact)
		if run.PromptPreview != "" {
			parts = append(parts, run.PromptPreview)
		}
		lines = append(lines, fitText(strings.Join(parts, " · "), m.Width))
	}
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderPane(index int, width int, height int) []string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}

	view := m.Agents[index]
	state := m.paneBadge(view)
	focused := index == m.FocusedIndex

	marker := " "
	style := borderStyle
	if focused {
		marker = ">"
		style = focusStyle
	}
	// A configured agent color tints the border only — the pane looks normal
	// otherwise. Focused: full-strength color; unfocused: a computed muted
	// shade (never SGR faint, which some terminals render invisibly).
	if colorValue := m.paneColor(view.Session.Name); colorValue != "" {
		if focusedColor, mutedColor, ok := paneBorderColors(colorValue); ok {
			if focused {
				style = lipgloss.NewStyle().Foreground(focusedColor)
			} else {
				style = lipgloss.NewStyle().Foreground(mutedColor)
			}
		} else if focused {
			// Unrecognized format: let lipgloss try it for the focused pane,
			// keep the default muted chrome otherwise.
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorValue))
		}
	}
	// Blocked or failed panes get a visually distinct border so they can't
	// hide among the running ones.
	if view.Session.StartError != nil || (view.Attention && !view.Session.Done) {
		style = warnStyle
	}

	title := fmt.Sprintf(" %s %s [%s] ", marker, view.Session.Name, state)
	titleStyleForPane := style
	if focused {
		titleStyleForPane = style.Bold(true)
	}
	lines := make([]string, 0, height)
	lines = append(lines, titleStyleForPane.Render(topBorder(title, width)))

	bodyHeight := height - 2
	body := view.bodyLines(bodyHeight, width-2)
	for _, line := range body {
		lines = append(lines, style.Render("│")+fitText(line, width-2)+style.Render("│"))
	}
	lines = append(lines, style.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return lines
}

func (v *agentView) bodyLines(height int, width int) []string {
	if v.Session.Config.Terminal.Renderer == "transcript" {
		return v.transcriptLines(height, width)
	}
	return v.screenLines(height, width)
}

func (v *agentView) transcriptLines(height int, width int) []string {
	lines := make([]string, 0, len(v.Lines)+1)
	lines = append(lines, v.Lines...)
	if v.Partial != "" {
		lines = append(lines, v.Partial)
	}

	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, hardWrap(line, width)...)
	}
	if len(wrapped) > height {
		wrapped = wrapped[len(wrapped)-height:]
	}
	for len(wrapped) < height {
		wrapped = append([]string{""}, wrapped...)
	}
	return wrapped
}

func (m Model) pageRangeLabel() string {
	displayed := m.displayAgentIndexes()
	if len(displayed) == 0 {
		return "0 of 0"
	}
	start, end := m.pageBounds()
	return fmt.Sprintf("%d-%d of %d", start+1, end, len(displayed))
}

func (m Model) screenModeName() string {
	switch m.ScreenMode {
	case ScreenOverview:
		return "overview"
	case ScreenSettings:
		return "settings"
	case ScreenRuns:
		return "runs"
	case ScreenArtifacts:
		return "artifacts"
	case ScreenCompare:
		return "compare"
	default:
		return "panes"
	}
}

func (m Model) groupByLabel() string {
	group := strings.TrimSpace(m.Config.UI.GroupBy)
	if group == "" {
		return "none"
	}
	return group
}

func (m Model) displayFilterLabel() string {
	if len(m.DisplayPersonalities) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.DisplayPersonalities))
	for name := range m.DisplayPersonalities {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left := m.Config.Personalities[names[i]]
		right := m.Config.Personalities[names[j]]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return names[i] < names[j]
	})
	return strings.Join(m.personalityLabels(names), ",")
}

// Pane borders use the same rounded box-drawing set as the composer, so the
// whole chrome reads as one surface instead of ASCII art next to unicode.
func topBorder(title string, width int) string {
	if width < 2 {
		return strings.Repeat("─", max0(width))
	}
	inner := width - 2
	title = truncateText(title, inner)
	if lipgloss.Width(title) >= inner {
		return "╭" + title + "╮"
	}
	return "╭" + title + strings.Repeat("─", inner-lipgloss.Width(title)) + "╮"
}

func blankBlock(width int, height int) []string {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	lines := make([]string, height)
	blank := strings.Repeat(" ", width)
	for i := range lines {
		lines[i] = blank
	}
	return lines
}

func fitBlock(lines []string, width int, height int) []string {
	if height < 0 {
		height = 0
	}
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		out = append(out, fitText(line, width))
	}
	for len(out) < height {
		out = append(out, fitText("", width))
	}
	return out
}

func distribute(total int, count int) []int {
	if count <= 0 {
		return nil
	}
	values := make([]int, count)
	base := total / count
	remainder := total % count
	for i := range values {
		values[i] = base
		if i < remainder {
			values[i]++
		}
	}
	return values
}

func fitText(value string, width int) string {
	value = strings.ReplaceAll(value, "\t", "    ")
	value = truncateText(value, width)
	visible := lipgloss.Width(value)
	if visible < width {
		value += strings.Repeat(" ", width-visible)
	}
	return value
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		for _, r := range value {
			return string(r)
		}
		return ""
	}

	var b strings.Builder
	used := 0
	for _, r := range value {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "~"
}

func hardWrap(line string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if line == "" {
		return []string{""}
	}

	lines := make([]string, 0)
	current := strings.Builder{}
	used := 0
	for _, r := range line {
		rw := lipgloss.Width(string(r))
		if rw <= 0 {
			rw = 1
		}
		if used+rw > width && current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += rw
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func dropLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
