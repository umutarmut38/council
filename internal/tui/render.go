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
		return m.renderHeaderBand(c)
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

// headerBandMinWidth is the narrowest terminal the docked band fits without
// overflowing m.Width. The 34-column COUNCIL block banner sits beside the
// 22-column head, so the left region (Width-head) must be at least the banner
// width — a 56-column hard floor. 60 clears it with a few columns of slack;
// below it the compact header is used instead. (The centered logo only appears
// once that gap grows wider still.)
const headerBandMinWidth = 60

// renderHeaderBand lays out the themed retro header as three columns: the COUNCIL
// block banner on the left, the compact retro logo centered in the gap between
// them (and vertically centered in the band), and the rotating 3D head
// docked on the right. A single retro operational-status line (the current phase,
// pulsing while live) runs along the bottom of the left+middle region. Every row
// is exactly m.Width columns so the View invariant holds.
func (m Model) renderHeaderBand(c chromeStyles) string {
	headH := headerBandHeight

	headW := headBandHeadWidth
	if headW > m.Width-24 {
		headW = m.Width - 24
	}
	if headW < 12 {
		headW = 12
	}
	beforeW := m.Width - headW // the left + middle region, before the docked head

	head := anim.Mesh(headW, headH, m.animFrame)

	// Left: COUNCIL in block letters. When a phase is live and the band is wide
	// enough, the current phase — with its count — sits right next to it in the
	// same bold block letters, in green; the count joins the block when it fits,
	// else it is dropped. The retro logo always stays centered in the gap between
	// the wordmark(s) and the head; narrow bands drop the phase to the thin
	// status line below.
	council := anim.Banner("COUNCIL")
	cW := anim.BannerWidth(council)

	name := ""
	if m.progress != nil && m.phase != "" {
		name = strings.ToUpper(m.phase)
	}
	count := ""
	if name != "" {
		if ph := m.railPhase(m.progress); ph != nil && ph.Counted && ph.Expected > 0 {
			count = fmt.Sprintf("%d/%d", ph.Done, ph.Expected)
		}
	}
	var phaseBanner []string
	phaseW := 0
	if name != "" {
		label := name
		if count != "" {
			label = name + " " + count
		}
		b := anim.Banner(label)
		if count != "" && cW+2+anim.BannerWidth(b)+18 > beforeW {
			b = anim.Banner(name) // name + count won't fit beside the logo — drop the count
		}
		phaseBanner = b
		phaseW = anim.BannerWidth(b)
	}
	// Only place the phase beside COUNCIL if room remains (>=18) for the centered
	// retro logo.
	phaseBeside := phaseW > 0 && cW+2+phaseW+18 <= beforeW

	leftW := cW
	if phaseBeside {
		leftW = cW + 2 + phaseW
	}
	gapW := beforeW - leftW
	logo := logoBlock(gapW)
	logoTop := (headH - len(logo)) / 2
	phaseStyle := lipgloss.NewStyle().Bold(true).Foreground(idxColor(anim.Green))

	before := make([]string, headH)
	for i := range before {
		before[i] = fitText("", beforeW)
	}
	for i := 0; i < headH; i++ {
		left := fitText("", leftW)
		if i < len(council) {
			seg := c.title.Render(council[i])
			if phaseBeside && i < len(phaseBanner) {
				seg += "  " + phaseStyle.Render(phaseBanner[i])
			}
			left = seg
		}
		mid := fitText("", gapW)
		if li := i - logoTop; li >= 0 && li < len(logo) {
			mid = logo[li]
		}
		before[i] = left + mid
	}
	// Bottom status line only when the phase isn't already shown beside COUNCIL:
	// the thin phase readout (green) on narrow bands, the next action (cyan) when
	// idle, nothing when there is no run.
	if headH >= 2 && !phaseBeside {
		if detail := m.phaseReadout(); detail != "" {
			style := c.rail
			if m.phase != "" {
				style = c.status // green
			}
			before[headH-2] = style.Render(fitText(detail, beforeW))
		}
	}

	rows := make([]string, headH)
	for i := 0; i < headH; i++ {
		rows[i] = before[i] + head[i]
	}
	return strings.Join(rows, "\n")
}

// retro logo text: the wordmark with a small emblem motif (block elements, all
// display-width 1) evoking the institutional emblem, plus the motto (kept as one
// line when wide enough, split across two when the gap is narrower).
const (
	emblemMark = "N E R V  ▞▟█▙"
	motto      = "GOD'S IN HIS HEAVEN, ALL'S RIGHT WITH THE WORLD"
	motto1     = "GOD'S IN HIS HEAVEN,"
	motto2     = "ALL'S RIGHT WITH THE WORLD"
)

// logoBlock renders the centered retro logo to fit a gapW-wide column: a big
// red block wordmark when the gap is wide enough, the compact emblem otherwise,
// then the dim motto below (one line, or two when narrower). Each returned row is
// exactly gapW visible columns; nil if the gap is too small for even the emblem.
func logoBlock(gapW int) []string {
	logo := lipgloss.NewStyle().Bold(true).Foreground(idxColor(anim.Crimson))
	sub := lipgloss.NewStyle().Foreground(idxColor(anim.AmberDim))

	var rows []string
	if banner := anim.Banner("NERV"); gapW >= anim.BannerWidth(banner)+2 {
		// Big block wordmark.
		for _, line := range banner {
			rows = append(rows, logo.Render(centerLine(line, gapW)))
		}
	} else if gapW >= lipgloss.Width(emblemMark)+2 {
		rows = append(rows, logo.Render(centerLine(emblemMark, gapW)))
	} else {
		return nil
	}

	switch {
	case gapW >= lipgloss.Width(motto)+2:
		rows = append(rows, sub.Render(centerLine(motto, gapW)))
	case gapW >= lipgloss.Width(motto2)+2:
		rows = append(rows,
			sub.Render(centerLine(motto1, gapW)),
			sub.Render(centerLine(motto2, gapW)))
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
	retro := m.retroThemed()
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
			switch {
			case m.artifactView != "":
				// A synthetic viewer (preview/diff) is the read-only pager even if
				// the editor pane was focused before it opened — its hint wins. e/i
				// only when a real file backs the view (e.g. a compare diff).
				if m.artifactFile != "" {
					hint = "Artifacts: ↑/↓ scroll · e $EDITOR · i editor · Esc back"
				} else {
					hint = "Artifacts: ↑/↓ scroll · Esc back"
				}
			case m.editorPaneFocused:
				hint = "Artifacts: keys → editor · Esc passes through · F2/Ctrl+O back to list"
			default:
				hint = "Artifacts: ↑/↓ select · Enter edit · Tab editor · e $EDITOR · Esc back"
			}
		case ScreenCompare:
			hint = "Compare: ↑/↓ select · Enter files/diff · d full diff · x mark pair · e $EDITOR · i editor · Esc back"
		case ScreenEditor:
			if m.editorPaneFocused {
				hint = "Editor: keys → editor · Esc passes through · F2/Ctrl+O back to tree"
			} else {
				hint = "Editor: ↑/↓ move · Enter open · → expand · ← collapse · Tab editor · Esc back"
			}
		}
		return strings.Join([]string{
			c.suggest.Render(fitText(hint, m.Width)),
			inputBoxTop(m.screenModeName(), m.Width, c.border, retro),
			inputBoxContent(m.Status, m.Width, c.border, c.input, retro),
			inputBoxBottom(m.Width, c.border, retro),
		}, "\n")
	}

	if m.InputMode == InputDirect {
		hint := c.suggest.Render(fitText("DIRECT MODE — keystrokes go straight to the pane. Esc/F2 returns to the composer.", m.Width))
		label := "direct: " + m.focusedName()
		content := "keys → " + m.focusedName()
		return strings.Join([]string{hint, inputBoxTop(label, m.Width, c.border, retro), inputBoxContent(content, m.Width, c.border, c.input, retro), inputBoxBottom(m.Width, c.border, retro)}, "\n")
	}

	label := m.targetLabel()
	if m.Zoomed {
		label = "[zoom] " + label
	}

	prompt := m.targetPrompt()
	content := prompt + " > " + m.PromptInput + "_"

	lines := m.suggestionBlock(c)
	lines = append(lines,
		inputBoxTop(label, m.Width, c.border, retro),
		inputBoxContent(content, m.Width, c.border, c.input, retro),
		inputBoxBottom(m.Width, c.border, retro),
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

	help := "Enter send | Ctrl+G overview | F2 direct | Ctrl+B target | Ctrl+F zoom | Ctrl+N/P page | Tab focus | Ctrl+W mouse | @file"
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

func inputBoxTop(label string, width int, border lipgloss.Style, retro bool) string {
	tl, tr, hbar := "╭", "╮", "─"
	if retro {
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

func inputBoxBottom(width int, border lipgloss.Style, retro bool) string {
	bl, br, hbar := "╰", "╯", "─"
	if retro {
		bl, br, hbar = "┗", "┛", "━"
	}
	if width < 2 {
		return border.Render(strings.Repeat(hbar, max0(width)))
	}
	return border.Render(bl + strings.Repeat(hbar, width-2) + br)
}

func inputBoxContent(text string, width int, border, input lipgloss.Style, retro bool) string {
	vbar := "│"
	if retro {
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
	case ScreenEditor:
		return strings.Join(m.renderEditor(bodyHeight), "\n")
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
	c := m.chrome()
	lines := make([]string, 0, bodyHeight)

	if p := m.progress; p != nil {
		title := "Run " + p.Stamp
		if m.phase != "" {
			title += " · Phase: " + capitalize(m.phase)
		}
		lines = append(lines, c.heading.Render(fitText(title, m.Width)))
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
		lines = append(lines, c.heading.Render(fitText("Agents", m.Width)))
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
			lines = append(lines, c.heading.Render(fitText(group, m.Width)))
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
			line = c.warn.Render(line)
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
	c := m.chrome()
	items := m.settingsItems()
	lines := make([]string, 0, bodyHeight)
	lines = append(lines, c.heading.Render(fitText("Settings", m.Width)))
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
	lines = append(lines, c.faint.Render(fitText("Changes apply to this session. Edit YAML (ui.adaptive_grid, ui.page_rows, …) to make them permanent.", m.Width)))
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderRuns(bodyHeight int) []string {
	c := m.chrome()
	lines := []string{c.heading.Render(fitText("Runs", m.Width))}
	if len(m.Runs) == 0 {
		lines = append(lines, c.faint.Render(fitText("No runs found.", m.Width)))
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
	// While scrolled up the view is not live: show a marker so it's obvious new
	// output is landing below the fold.
	if view.ScrollOffset > 0 {
		state = fmt.Sprintf("%s ↑%d", state, view.ScrollOffset)
	}
	focused := index == m.FocusedIndex

	marker := " "
	style := c.border
	if focused {
		marker = ">"
		style = c.focus
	}
	if m.retroThemed() {
		// retro mode overrides the configured agent colors entirely: the focused
		// pane is full orange, the rest cycle across the retro accents (muted
		// while unfocused). Toggling /eva off restores the configured path.
		style = retroPaneStyle(index, focused)
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
	if m.retroThemed() {
		// Angular retro frame: heavy box, a classification code on the top rail,
		// and a bilingual status tag on the bottom rail.
		side = style.Render("┃")
		code := fmt.Sprintf("NERV//%02d", index+1)
		jp := "同期" // SYNC
		if view.Session.StartError != nil || (view.Attention && !view.Session.Done) {
			jp = "警告" // WARNING
		}
		title := fmt.Sprintf("%s %s [%s]", strings.TrimSpace(marker), view.Session.Name, state)
		topLine = titleStyleForPane.Render(retroTopBorder(title, code, width))
		botLine = style.Render(retroBottomBorder(jp, width))
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

// retroTopBorder draws the heavy retro top rail: ┏━ TITLE ━…━ CODE ━┓, exactly
// width visible columns.
func retroTopBorder(title, code string, width int) string {
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

// retroBottomBorder draws the heavy retro bottom rail: ┗━ LABEL ━…━┛, exactly
// width visible columns. label may contain CJK (width-aware).
func retroBottomBorder(label string, width int) string {
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
	// Scrolled up: render from the plain-text transcript regardless of the
	// configured renderer, since the VT100 screen grid keeps no history beyond
	// the visible rows. At offset 0 the live renderer (screen or transcript)
	// takes over again with full styling.
	if v.ScrollOffset > 0 {
		return v.transcriptLines(height, width)
	}
	if v.Session.Config.Terminal.Renderer == "transcript" {
		return v.transcriptLines(height, width)
	}
	return v.screenLines(height, width)
}

// transcriptWrapped returns every wrapped transcript line (history + partial).
func (v *agentView) transcriptWrapped(width int) []string {
	lines := make([]string, 0, len(v.Lines)+1)
	lines = append(lines, v.Lines...)
	if v.Partial != "" {
		lines = append(lines, v.Partial)
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, hardWrap(line, width)...)
	}
	return wrapped
}

// maxScrollOffset is the largest ScrollOffset that still shows new content: the
// number of wrapped lines above the live window of the given height.
func (v *agentView) maxScrollOffset(height int, width int) int {
	if max := len(v.transcriptWrapped(width)) - height; max > 0 {
		return max
	}
	return 0
}

func (v *agentView) transcriptLines(height int, width int) []string {
	wrapped := v.transcriptWrapped(width)

	// Clamp the scroll offset to the available history, then slice the window
	// `offset` wrapped lines up from the live bottom. Derive the max directly
	// from len(wrapped) so we don't wrap the transcript a second time.
	offset := v.ScrollOffset
	if max := len(wrapped) - height; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	if len(wrapped) > height {
		end := len(wrapped) - offset
		wrapped = wrapped[end-height : end]
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
	case ScreenEditor:
		return "editor"
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
