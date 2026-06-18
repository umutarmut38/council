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
	"github.com/umutarmut38/council/internal/tui/anim"
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
	statusText := page + " · " + m.Status
	railText := ""
	if m.progress != nil {
		railText = m.progress.phaseRail()
	}

	c := m.chrome()
	if m.headShown() {
		return m.renderHeaderBand(c, statusText, railText)
	}

	header := c.title.Render(fitText(line, m.Width)) + "\n" + c.status.Render(fitText(statusText, m.Width))
	if railText != "" {
		header += "\n" + c.rail.Render(fitText(railText, m.Width))
	}
	return header
}

// headBandHeadWidth is the width of the docked 3D head within the header band;
// the title/status/rail occupy the remaining width to its left.
const headBandHeadWidth = 22

// renderHeaderBand lays out the themed NERV header as three columns: the COUNCIL
// block banner on the left, the compact NERV logo centered in the gap between
// them (and vertically centered in the band), and the rotating 3D EVA-01 head
// docked on the right. The status + NERV marking run along the bottom of the
// left+middle region. Every row is exactly m.Width columns so the View invariant
// holds.
func (m Model) renderHeaderBand(c chromeStyles, statusText, railText string) string {
	headH := headerBandHeight

	headW := headBandHeadWidth
	if headW > m.Width-24 {
		headW = m.Width - 24
	}
	if headW < 12 {
		headW = 12
	}
	beforeW := m.Width - headW // the left + middle region, before the docked head

	head := anim.Head(headW, headH, m.animFrame, anim.AccentForPhase(m.phase))

	// COUNCIL block banner anchors the left of the band (falling back to NERV
	// when the band is too narrow for COUNCIL).
	banner := anim.Banner("COUNCIL")
	if anim.BannerWidth(banner) > beforeW {
		banner = anim.Banner("NERV")
	}
	bannerW := anim.BannerWidth(banner)
	if bannerW > beforeW {
		bannerW = beforeW
	}

	// The NERV logo sits centered in the gap between the banner and the head, and
	// vertically centered within the band.
	gapW := beforeW - bannerW
	logo := nervLogoBlock(gapW)
	logoTop := (headH - len(logo)) / 2

	before := make([]string, headH)
	for i := 0; i < headH; i++ {
		leftCell := fitText("", bannerW)
		if i < len(banner) && bannerW > 0 {
			leftCell = c.title.Render(fitText(banner[i], bannerW))
		}
		midCell := fitText("", gapW)
		if li := i - logoTop; li >= 0 && li < len(logo) {
			midCell = logo[li]
		}
		before[i] = leftCell + midCell
	}
	// Status + NERV marking along the bottom, spanning the full left+middle
	// region (clear of the banner rows and the centered logo).
	if headH >= 3 {
		before[headH-3] = c.status.Render(fitText(statusText, beforeW))
	}
	if headH >= 2 {
		mark := "NERV // 中央ドグマ"
		if railText != "" {
			mark = railText
		}
		before[headH-2] = c.rail.Render(fitText(mark, beforeW))
	}

	rows := make([]string, headH)
	for i := 0; i < headH; i++ {
		rows[i] = before[i] + head[i]
	}
	return strings.Join(rows, "\n")
}

// NERV logo text: the wordmark with a small half-leaf motif (block elements, all
// display-width 1) evoking the institutional emblem, plus the motto (kept as one
// line when wide enough, split across two when the gap is narrower).
const (
	nervEmblem = "N E R V  ▞▟█▙"
	nervMotto  = "GOD'S IN HIS HEAVEN, ALL'S RIGHT WITH THE WORLD"
	nervMotto1 = "GOD'S IN HIS HEAVEN,"
	nervMotto2 = "ALL'S RIGHT WITH THE WORLD"
)

// nervLogoBlock renders the centered NERV logo to fit a gapW-wide column: the red
// wordmark row, then the dim motto (one line, or two when the gap is narrow).
// Each returned row is exactly gapW visible columns; nil if the gap is too small.
func nervLogoBlock(gapW int) []string {
	if gapW < lipgloss.Width(nervEmblem)+2 {
		return nil
	}
	logo := lipgloss.NewStyle().Bold(true).Foreground(idxColor(anim.AlarmRed))
	sub := lipgloss.NewStyle().Foreground(idxColor(anim.NervOrangeDim))
	rows := []string{logo.Render(centerLine(nervEmblem, gapW))}
	switch {
	case gapW >= lipgloss.Width(nervMotto)+2:
		rows = append(rows, sub.Render(centerLine(nervMotto, gapW)))
	case gapW >= lipgloss.Width(nervMotto2)+2:
		rows = append(rows,
			sub.Render(centerLine(nervMotto1, gapW)),
			sub.Render(centerLine(nervMotto2, gapW)))
	}
	return rows
}

// centerLine pads s with spaces on both sides to sit centered in exactly width
// visible columns (truncating if it overflows).
func centerLine(s string, width int) string {
	s = truncateText(s, width)
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-w-left)
}

func (m Model) renderFooter() string {
	c := m.chrome()
	eva := m.evaThemed()
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
			c.suggest.Render(fitText(hint, m.Width)),
			inputBoxTop(m.screenModeName(), m.Width, c.border, eva),
			inputBoxContent(m.Status, m.Width, c.border, c.input, eva),
			inputBoxBottom(m.Width, c.border, eva),
		}, "\n")
	}

	if m.InputMode == InputDirect {
		hint := c.suggest.Render(fitText("DIRECT MODE — keystrokes go straight to the pane. Esc/F2 returns to the composer.", m.Width))
		label := "direct: " + m.focusedName()
		content := "keys → " + m.focusedName()
		return strings.Join([]string{hint, inputBoxTop(label, m.Width, c.border, eva), inputBoxContent(content, m.Width, c.border, c.input, eva), inputBoxBottom(m.Width, c.border, eva)}, "\n")
	}

	label := m.targetLabel()
	if m.Zoomed {
		label = "[zoom] " + label
	}

	prompt := m.targetPrompt()
	content := prompt + " > " + m.PromptInput + "_"

	lines := m.suggestionBlock(c)
	lines = append(lines,
		inputBoxTop(label, m.Width, c.border, eva),
		inputBoxContent(content, m.Width, c.border, c.input, eva),
		inputBoxBottom(m.Width, c.border, eva),
	)
	return strings.Join(lines, "\n")
}

// suggestionBlock is the area above the composer: the vertical @file picker
// or command palette while one is being typed, a single hint line otherwise.
func (m Model) suggestionBlock(c chromeStyles) []string {
	if m.filePaletteActive() {
		return m.renderFilePalette()
	}
	if m.paletteActive() {
		return m.renderPalette()
	}
	return []string{m.suggestionLine(c)}
}

// suggestionLine shows matching /commands while one is being typed, and the key
// hints otherwise.
func (m Model) suggestionLine(c chromeStyles) string {
	if strings.HasPrefix(m.PromptInput, "/") {
		prefix := strings.ToLower(strings.TrimPrefix(strings.Fields(m.PromptInput + " ")[0], "/"))
		all := command.Composers()
		parts := make([]string, 0, len(all))
		for _, cmd := range all {
			if strings.HasPrefix(cmd.Name, prefix) {
				entry := "/" + cmd.Name
				if cmd.Args != "" {
					entry += " " + cmd.Args
				}
				parts = append(parts, entry+" — "+cmd.Desc)
			}
		}
		text := strings.Join(parts, "   ")
		if text == "" {
			text = "no matching command"
		}
		return c.suggest.Render(fitText(text, m.Width))
	}

	// During a run, next actions beat the generic shortcut list; blocked
	// panes beat everything.
	if hint, ok := m.contextHint(); ok {
		if len(m.attentionAgents()) > 0 {
			return c.warn.Render(fitText(hint, m.Width))
		}
		return c.suggest.Render(fitText(hint, m.Width))
	}

	help := "Enter send | Ctrl+G overview | F2 direct | Ctrl+B target | Ctrl+F zoom | Ctrl+N/P page | Tab focus | @file"
	return c.faint.Render(fitText(help, m.Width))
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

func inputBoxTop(label string, width int, border lipgloss.Style, eva bool) string {
	tl, tr, hbar := "╭", "╮", "─"
	if eva {
		tl, tr, hbar = "┏", "┓", "━"
	}
	if width < 2 {
		return border.Render(strings.Repeat(hbar, max0(width)))
	}
	inner := width - 2
	lbl := ""
	if label != "" {
		lbl = hbar + " " + label + " "
	}
	lbl = truncateText(lbl, inner)
	pad := inner - lipgloss.Width(lbl)
	if pad < 0 {
		pad = 0
	}
	return border.Render(tl + lbl + strings.Repeat(hbar, pad) + tr)
}

func inputBoxBottom(width int, border lipgloss.Style, eva bool) string {
	bl, br, hbar := "╰", "╯", "─"
	if eva {
		bl, br, hbar = "┗", "┛", "━"
	}
	if width < 2 {
		return border.Render(strings.Repeat(hbar, max0(width)))
	}
	return border.Render(bl + strings.Repeat(hbar, width-2) + br)
}

func inputBoxContent(text string, width int, border, input lipgloss.Style, eva bool) string {
	vbar := "│"
	if eva {
		vbar = "┃"
	}
	if width < 2 {
		return input.Render(fitText(text, width))
	}
	inner := width - 2
	return border.Render(vbar) + input.Render(fitText(" "+text, inner)) + border.Render(vbar)
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

	c := m.chrome()
	view := m.Agents[index]
	state := m.paneBadge(view)
	focused := index == m.FocusedIndex

	marker := " "
	style := c.border
	if focused {
		marker = ">"
		style = c.focus
	}
	if m.evaThemed() {
		// EVA mode overrides the configured agent colors entirely: the focused
		// pane is full orange, the rest cycle across the NERV accents (muted
		// while unfocused). Toggling /eva off restores the configured path.
		style = evaPaneStyle(index, focused)
	} else if colorValue := m.paneColor(view.Session.Name); colorValue != "" {
		// A configured agent color tints the border only — the pane looks normal
		// otherwise. Focused: full-strength color; unfocused: a computed muted
		// shade (never SGR faint, which some terminals render invisibly).
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
		style = c.warn
	}

	titleStyleForPane := style
	if focused {
		titleStyleForPane = style.Bold(true)
	}

	side := style.Render("│")
	topLine := titleStyleForPane.Render(topBorder(fmt.Sprintf(" %s %s [%s] ", marker, view.Session.Name, state), width))
	botLine := style.Render("╰" + strings.Repeat("─", width-2) + "╯")
	if m.evaThemed() {
		// Angular NERV frame: heavy box, a classification code on the top rail,
		// and a bilingual status tag on the bottom rail.
		side = style.Render("┃")
		code := fmt.Sprintf("NERV//%02d", index+1)
		jp := "同期" // SYNC
		if view.Session.StartError != nil || (view.Attention && !view.Session.Done) {
			jp = "警告" // WARNING
		}
		title := fmt.Sprintf("%s %s [%s]", strings.TrimSpace(marker), view.Session.Name, state)
		topLine = titleStyleForPane.Render(evaTopBorder(title, code, width))
		botLine = style.Render(evaBottomBorder(jp, width))
	}

	lines := make([]string, 0, height)
	lines = append(lines, topLine)
	bodyHeight := height - 2
	body := view.bodyLines(bodyHeight, width-2)
	for _, line := range body {
		lines = append(lines, side+fitText(line, width-2)+side)
	}
	lines = append(lines, botLine)
	return lines
}

// evaTopBorder draws the heavy NERV top rail: ┏━ TITLE ━…━ CODE ━┓, exactly
// width visible columns.
func evaTopBorder(title, code string, width int) string {
	if width < 8 {
		return strings.Repeat("━", max0(width))
	}
	inner := width - 4 // for "┏━" ... "━┓"
	label := " " + title + " "
	tag := " " + code + " "
	if lipgloss.Width(label)+lipgloss.Width(tag) > inner {
		// Drop the code, then truncate the title, to stay within width.
		tag = ""
		label = truncateText(" "+title+" ", inner)
	}
	fill := inner - lipgloss.Width(label) - lipgloss.Width(tag)
	if fill < 0 {
		fill = 0
	}
	return "┏━" + label + strings.Repeat("━", fill) + tag + "━┓"
}

// evaBottomBorder draws the heavy NERV bottom rail: ┗━ LABEL ━…━┛, exactly
// width visible columns. label may contain CJK (width-aware).
func evaBottomBorder(label string, width int) string {
	if width < 8 {
		return strings.Repeat("━", max0(width))
	}
	inner := width - 4
	seg := " " + label + " "
	if lipgloss.Width(seg) > inner {
		seg = truncateText(seg, inner)
	}
	fill := inner - lipgloss.Width(seg)
	if fill < 0 {
		fill = 0
	}
	return "┗━" + seg + strings.Repeat("━", fill) + "━┛"
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
