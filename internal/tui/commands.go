package tui

// Composer commands: /send, /all, targeting, paging, show/hide, and message
// dispatch to one or more agents.

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/command"
	"github.com/umutarmut38/council/internal/setup"
)

func (m *Model) handlePageCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = fmt.Sprintf("page %d/%d", m.PageIndex+1, m.pageCount())
		return
	}
	switch strings.ToLower(fields[1]) {
	case "next", "n":
		m.nextPage()
	case "prev", "previous", "p":
		m.previousPage()
	default:
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			m.Status = "usage: /page next|prev|number"
			return
		}
		m.gotoPage(n - 1)
	}
}

func (m *Model) handleTargetCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /target all|focus|personality name|category name"
		return
	}
	switch strings.ToLower(fields[1]) {
	case "all":
		m.Target = TargetAll
		m.TargetName = ""
		m.Status = "input targets all agents"
	case "focus", "focused":
		m.Target = TargetFocused
		m.TargetName = ""
		m.Status = "input targets " + m.focusedName()
	case "personality", "p":
		if len(fields) < 3 {
			m.Status = "usage: /target personality name"
			return
		}
		name, ok := m.resolvePersonality(fields[2])
		if !ok {
			m.Status = "unknown personality: " + fields[2]
			return
		}
		m.Target = TargetPersonality
		m.TargetName = name
		m.Status = "input targets personality " + m.personalityLabel(name)
	case "category", "c":
		if len(fields) < 3 {
			m.Status = "usage: /target category name"
			return
		}
		name, ok := m.resolveCategory(fields[2])
		if !ok {
			m.Status = "unknown category: " + fields[2]
			return
		}
		m.Target = TargetCategory
		m.TargetName = name
		m.Status = "input targets category " + m.categoryLabel(name)
	default:
		m.Status = "usage: /target all|focus|personality name|category name"
	}
}

func (m *Model) handleShowCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /show all|personality names|category name"
		return
	}
	switch strings.ToLower(fields[1]) {
	case "all":
		m.DisplayPersonalities = nil
		m.ensurePageForFocus()
		m.resizeAgents()
		m.Status = "showing all personalities"
	case "personality", "personalities", "p":
		if len(fields) < 3 {
			m.Status = "usage: /show personality name[,name]"
			return
		}
		names, ok := m.resolvePersonalityList(strings.Join(fields[2:], " "))
		if !ok || len(names) == 0 {
			m.Status = "no matching personalities"
			return
		}
		m.setDisplayedPersonalities(names)
	case "category", "c":
		if len(fields) < 3 {
			m.Status = "usage: /show category name"
			return
		}
		category, ok := m.resolveCategory(fields[2])
		if !ok {
			m.Status = "unknown category: " + fields[2]
			return
		}
		names := m.personalitiesForCategory(category)
		if len(names) == 0 {
			m.Status = "category has no personalities: " + fields[2]
			return
		}
		m.setDisplayedPersonalities(names)
	default:
		m.Status = "usage: /show all|personality names|category name"
	}
}

func (m *Model) handleHideCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /hide personality name"
		return
	}
	nameArg := fields[1]
	if strings.EqualFold(nameArg, "personality") && len(fields) >= 3 {
		nameArg = fields[2]
	}
	name, ok := m.resolvePersonality(nameArg)
	if !ok {
		m.Status = "unknown personality: " + nameArg
		return
	}
	if len(m.DisplayPersonalities) == 0 {
		m.DisplayPersonalities = m.allUsedPersonalities()
	}
	if len(m.DisplayPersonalities) == 1 && m.DisplayPersonalities[name] {
		m.Status = "at least one personality must stay visible"
		return
	}
	delete(m.DisplayPersonalities, name)
	m.ensurePageForFocus()
	m.resizeAgents()
	m.Status = "hid " + m.personalityLabel(name)
}

// completeCommand fills in the first matching command name when the user is
// still typing the command word (after a leading "/"). Returns true if it
// changed the input, so Tab only completes instead of switching focus then.
func (m *Model) completeCommand() bool {
	if !strings.HasPrefix(m.PromptInput, "/") || strings.Contains(m.PromptInput, " ") {
		return false
	}
	prefix := strings.ToLower(strings.TrimPrefix(m.PromptInput, "/"))
	for _, c := range command.Composers() {
		if strings.HasPrefix(c.Name, prefix) {
			m.PromptInput = "/" + c.Name + " "
			m.Status = "/" + c.Name + " — " + c.Desc
			return true
		}
	}
	return false
}

func (m *Model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.PromptInput)
	m.PromptInput = ""
	if text == "" {
		return nil
	}

	if handled, cmd := m.handleCommand(text); handled {
		return cmd
	}
	// "@agent msg" targets a pane; "@path" anywhere else inlines a file.
	if strings.HasPrefix(text, "@") {
		first := strings.TrimPrefix(strings.Fields(text)[0], "@")
		if strings.EqualFold(first, "all") || m.agentExists(first) {
			m.handleAddressedInput(text)
			return nil
		}
	}

	text = m.expandRefs(text)
	switch m.Target {
	case TargetAll:
		m.sendAll(text)
		m.Status = "sent to all agents"
		return nil
	case TargetPersonality:
		count := m.sendPersonality(m.TargetName, text)
		m.Status = fmt.Sprintf("sent to %d agent(s) with personality %s", count, m.personalityLabel(m.TargetName))
		return nil
	case TargetCategory:
		count := m.sendCategory(m.TargetName, text)
		m.Status = fmt.Sprintf("sent to %d agent(s) in category %s", count, m.categoryLabel(m.TargetName))
		return nil
	default:
		if session := m.focusedSession(); session != nil {
			if err := sendLine(session, m.Config.PromptForAgent(session.Name, text)); err != nil {
				m.Status = err.Error()
				return nil
			}
			m.Status = "sent to " + session.Name
			return nil
		}
	}
	if session := m.focusedSession(); session != nil {
		if err := sendLine(session, m.Config.PromptForAgent(session.Name, text)); err != nil {
			m.Status = err.Error()
			return nil
		}
		m.Status = "sent to " + session.Name
	}
	return nil
}

func (m *Model) handleCommand(text string) (bool, tea.Cmd) {
	if !strings.HasPrefix(text, "/") {
		return false, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return true, nil
	}

	word := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	cmd, ok := command.LookupComposer(word)
	if !ok {
		m.Status = "unknown command: " + fields[0]
		return true, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch cmd.Name {
	case "all":
		if rest == "" {
			m.Status = "usage: /all message"
			return true, nil
		}
		m.sendAll(m.expandRefs(rest))
		m.Status = "sent to all agents"
	case "send":
		if len(fields) < 3 {
			m.Status = "usage: /send agent message"
			return true, nil
		}
		name := fields[1]
		message := strings.TrimSpace(strings.TrimPrefix(rest, name))
		m.sendNamed(name, message)
	case "focus":
		if len(fields) < 2 {
			m.Status = "usage: /focus agent"
			return true, nil
		}
		m.focusByName(fields[1])
	case "direct":
		if len(fields) >= 2 {
			m.focusByName(fields[1])
		}
		m.InputMode = InputDirect
		m.PromptInput = ""
		m.Status = "direct input to " + m.focusedName()
	case "zoom":
		if len(fields) >= 2 {
			m.focusByName(fields[1])
			if m.Zoomed {
				m.resizeAgents()
				m.Status = "zoomed " + m.focusedName()
			} else {
				m.toggleZoom()
			}
		} else {
			m.toggleZoom()
		}
	case "page":
		m.handlePageCommand(fields)
	case "overview":
		m.openOverview()
	case "settings":
		m.ScreenMode = ScreenSettings
		m.InputMode = InputComposer
		m.PromptInput = ""
		m.Status = "settings"
	case "runs":
		m.cmdRuns()
	case "resume":
		return true, m.cmdResume(rest)
	case "target":
		m.handleTargetCommand(fields)
	case "show":
		m.handleShowCommand(fields)
	case "hide":
		m.handleHideCommand(fields)
	case "plan":
		return true, m.cmdPlan(rest)
	case "vote":
		return true, m.cmdVote()
	case "build":
		return true, m.cmdBuild()
	case "start-build":
		return true, m.cmdStartBuild()
	case "review":
		return true, m.cmdReview()
	case "adopt":
		return true, m.cmdAdopt(rest)
	case "preview":
		m.cmdPreview(rest)
	case "compare":
		m.cmdCompare()
	case "judge":
		m.cmdJudge(rest)
	case "refine":
		return true, m.cmdRefine()
	case "artifacts":
		m.cmdArtifacts()
	case "report":
		m.cmdReport()
	case "restart":
		m.cmdRestart(rest)
	case "resend":
		m.cmdResend(rest)
	case "nudge":
		m.cmdNudge(rest)
	case "attention":
		m.cmdAttention(rest)
	case "finish":
		m.finishPhase()
	case "status":
		m.cmdStatus()
	case "setup":
		m.cmdSetup()
	case "clean":
		m.cmdClean(rest)
	case "save":
		if err := m.saveTranscripts(); err != nil {
			m.Status = "save failed: " + err.Error()
		} else {
			m.Status = "saved transcripts"
		}
	case "clear":
		m.clearScreens(rest)
	case "quit":
		m.terminateAgents()
		m.Status = "quit with Ctrl+X"
	case "help":
		all := command.Composers()
		names := make([]string, 0, len(all))
		for _, c := range all {
			names = append(names, "/"+c.Name)
		}
		m.Status = "commands: " + strings.Join(names, " ") + "  |  @agent msg, Tab completes"
	default:
		m.Status = "unknown command: " + fields[0]
	}
	return true, nil
}

// cmdRestart terminates and relaunches one pane with its current (phase)
// command, for agents that hang or exit mid-session.
func (m *Model) cmdRestart(rest string) {
	name := strings.TrimSpace(rest)
	if name == "" {
		m.Status = "usage: /restart agent"
		return
	}
	view := m.findAgent(name)
	if view == nil {
		m.Status = "unknown agent: " + name
		return
	}
	old := view.Session
	_ = old.Terminate()
	fresh := agent.NewSession(old.Name, old.Config, old.RawLogPath)
	view.Session = fresh
	view.PhaseDone = false
	view.clearAttention()
	if m.launch != nil {
		m.launch(fresh)
	}
	if m.orch != nil && m.orch.Run() != nil {
		m.orch.Run().RecordRestart(m.phase)
	}
	m.resizeAgents()
	m.Status = "restarted " + old.Name
}

// cmdResend repeats the current phase prompt to one agent (or to every agent
// still missing its artifact when no name is given).
func (m *Model) cmdResend(rest string) {
	if len(m.phasePrompts) == 0 {
		m.Status = "no phase prompt to resend (none sent yet)"
		return
	}
	name := strings.TrimSpace(rest)
	if name != "" {
		view := m.findAgent(name)
		if view == nil {
			m.Status = "unknown agent: " + name
			return
		}
		prompt := m.phasePrompts[view.Session.Name]
		if prompt == "" {
			m.Status = "no phase prompt was sent to " + view.Session.Name
			return
		}
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, prompt))
		m.Status = "resent " + m.phase + " prompt to " + view.Session.Name
		return
	}
	count := 0
	for _, view := range m.Agents {
		if view.PhaseDone || view.Session.Done {
			continue
		}
		if prompt := m.phasePrompts[view.Session.Name]; prompt != "" {
			_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, prompt))
			count++
		}
	}
	m.Status = fmt.Sprintf("resent %s prompt to %d agent(s)", m.phase, count)
}

// cmdAttention manually flags (or clears) a pane as needing user input, for
// approval prompts the automatic detection missed:
// /attention <agent> [off]
func (m *Model) cmdAttention(rest string) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		blocked := m.attentionAgents()
		if len(blocked) == 0 {
			m.Status = "no panes flagged — usage: /attention <agent> [off]"
			return
		}
		m.Status = "needs input: " + strings.Join(blocked, ", ")
		return
	}
	view := m.findAgent(fields[0])
	if view == nil {
		m.Status = "unknown agent: " + fields[0]
		return
	}
	if len(fields) > 1 && strings.EqualFold(fields[1], "off") {
		view.clearAttention()
		m.Status = view.Session.Name + " unflagged"
		return
	}
	view.Attention = true
	view.AttentionManual = true
	m.Status = view.Session.Name + " flagged as needing input — F2 for direct mode"
}

// cmdNudge sends a short reminder to produce the expected artifact, to one
// agent or to everyone still missing one.
func (m *Model) cmdNudge(rest string) {
	if m.phase == "" || len(m.watching) == 0 {
		m.Status = "no phase in progress to nudge about"
		return
	}
	nudge := func(view *agentView) bool {
		path, ok := m.watching[view.Session.Name]
		if !ok || view.PhaseDone || view.Session.Done {
			return false
		}
		msg := fmt.Sprintf("Reminder: the council is waiting on your %s artifact. Please finish and write it to: %s", m.phase, path)
		return sendLine(view.Session, msg) == nil
	}
	name := strings.TrimSpace(rest)
	if name != "" {
		view := m.findAgent(name)
		if view == nil {
			m.Status = "unknown agent: " + name
			return
		}
		if nudge(view) {
			m.Status = "nudged " + view.Session.Name
		} else {
			m.Status = view.Session.Name + " is done or not in this phase"
		}
		return
	}
	count := 0
	for _, view := range m.Agents {
		if nudge(view) {
			count++
		}
	}
	m.Status = fmt.Sprintf("nudged %d agent(s)", count)
}

func (m *Model) handleAddressedInput(text string) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		m.Status = "usage: @agent message"
		return
	}

	target := strings.TrimPrefix(fields[0], "@")
	message := m.expandRefs(strings.TrimSpace(strings.TrimPrefix(text, fields[0])))
	if strings.EqualFold(target, "all") {
		m.sendAll(message)
		m.Status = "sent to all agents"
		return
	}
	m.sendNamed(target, message)
}

func (m *Model) sendNamed(name string, message string) {
	if message == "" {
		m.Status = "empty message for " + name
		return
	}
	for _, view := range m.Agents {
		if strings.EqualFold(view.Session.Name, name) {
			if err := sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message)); err != nil {
				m.Status = err.Error()
				return
			}
			view.clearAttention()
			m.Status = "sent to " + view.Session.Name
			return
		}
	}
	m.Status = "unknown agent: " + name
}

func (m *Model) sendAll(message string) {
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
	}
}

func (m *Model) sendPersonality(personality string, message string) int {
	count := 0
	for _, view := range m.recipientViewsForPersonality(personality) {
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
		count++
	}
	return count
}

func (m *Model) sendCategory(category string, message string) int {
	count := 0
	for _, view := range m.recipientViewsForCategory(category) {
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
		count++
	}
	return count
}

func (m Model) recipientViewsForPersonality(personality string) []*agentView {
	views := make([]*agentView, 0)
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		name, _, ok := m.Config.PersonalityForAgent(view.Session.Name)
		if ok && name == personality {
			views = append(views, view)
		}
	}
	return views
}

func (m Model) recipientViewsForCategory(category string) []*agentView {
	views := make([]*agentView, 0)
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		_, personality, ok := m.Config.PersonalityForAgent(view.Session.Name)
		if !ok {
			continue
		}
		if personality.Category == category {
			views = append(views, view)
		}
	}
	return views
}

// SetSetupStatus attaches the pre-launch setup/env observability snapshot so
// /setup can render it. A nil status disables the command's output.
func (m *Model) SetSetupStatus(s *setup.Status) {
	m.setupStatus = s
}

// cmdSetup opens the pre-launch setup/env status (command labels, PIDs,
// lifecycle, readiness, captured output, and exported env keys) in the pager.
func (m *Model) cmdSetup() {
	if m.setupStatus == nil {
		m.Status = "no pre-launch setup or env configured"
		return
	}
	report := m.setupStatus.Snapshot()
	m.openArtifactText("setup status", report.Render())
	count := len(report.Commands)
	m.Status = fmt.Sprintf("setup status — %d command(s), %d exported env key(s)", count, len(report.EnvKeys))
}

func (m *Model) clearScreens(target string) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "all") {
		for _, view := range m.Agents {
			view.clearScreen()
		}
		m.Status = "cleared screens"
		return
	}
	for _, view := range m.Agents {
		if strings.EqualFold(view.Session.Name, target) {
			view.clearScreen()
			m.Status = "cleared " + view.Session.Name
			return
		}
	}
	m.Status = "unknown agent: " + target
}
