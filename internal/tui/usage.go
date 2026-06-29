package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/usage"
)

// initUsage allocates live usage state. No-op when usage is disabled.
func (m *Model) initUsage() {
	if !m.Config.Usage.Enabled {
		return
	}
	m.usageTally = map[string]usage.TokenPair{}
	m.usageRate = map[string]usage.Rate{}
	m.usageModel = map[string]string{}
	m.usagePricer = m.buildPricer()
	for name, a := range m.Config.Agents {
		model, _, _ := usageModel(a)
		m.usageModel[name] = model
		if r := m.usagePricer.Rate(model, a.Usage.PriceProfile); r.Found {
			m.usageRate[name] = r
		}
	}
}

func usageTool(a config.AgentConfig) (tool, source, confidence string) {
	if a.Usage.Tool != "" {
		return a.Usage.Tool, usage.MetaSourceConfig, usage.Exact
	}
	return usage.UnknownValue, usage.MetaSourceUnknown, usage.Unknown
}

func usageModel(a config.AgentConfig) (model, source, confidence string) {
	if a.Usage.Model != "" {
		return a.Usage.Model, usage.MetaSourceConfig, usage.Exact
	}
	return usage.UnknownValue, usage.MetaSourceUnknown, usage.Unknown
}

// agentFingerprint is the agent's personality prompt prefix, clipped -- the
// distinctive text council prepends to every prompt for this pane.
func agentFingerprint(cfg config.Config, name string) string {
	fp := cfg.AgentPromptPrefix(name)
	if len(fp) > 160 {
		fp = fp[:160]
	}
	return fp
}

// agentCWD is the agent's absolute working directory.
func agentCWD(a config.AgentConfig) string {
	cwd := a.CWD
	if cwd == "" {
		cwd = "."
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// recordUsageEvent appends one usage event for the active run and bumps the live
// tally. No-op when usage is disabled. Runs on the Update goroutine.
func (m Model) recordUsageEvent(e usage.Event) {
	if !m.Config.Usage.Enabled || m.Store == nil {
		return
	}
	dir := m.Store.RunDir
	if dir == "" {
		if err := m.Store.Ensure(); err != nil {
			return
		}
		dir = m.Store.RunDir
	}
	e.RunID = filepath.Base(dir)
	m.enrichUsageEvent(&e)
	if m.usageTally != nil {
		t := m.usageTally[e.Agent]
		t.AddEvent(e)
		m.usageTally[e.Agent] = t
	}
	_ = usage.Append(dir, e)
}

func (m Model) enrichUsageEvent(e *usage.Event) {
	a := m.Config.Agents[e.Agent]
	tool, toolSource, toolConf := usageTool(a)
	if e.Tool == "" {
		e.Tool = tool
	}
	if e.ToolSource == "" {
		e.ToolSource = toolSource
	}
	if e.ToolConfidence == "" {
		e.ToolConfidence = toolConf
	}
	model, modelSource, modelConf := usageModel(a)
	if e.Model == "" {
		e.Model = model
	}
	if e.ModelSource == "" {
		e.ModelSource = modelSource
	}
	if e.ModelConfidence == "" {
		e.ModelConfidence = modelConf
	}
	e.PriceProfile = a.Usage.PriceProfile
	if e.PriceModel == "" {
		e.PriceModel = usage.UnknownValue
	}
	if e.PriceSource == "" {
		e.PriceSource = usage.Unknown
	}
	if e.PriceConfidence == "" {
		e.PriceConfidence = usage.Unknown
	}
	if m.usagePricer != nil {
		if r := m.usagePricer.Rate(e.Model, e.PriceProfile); r.Found {
			e.PriceModel = r.PriceModel
			e.PriceSource = r.Source
			e.PriceConfidence = r.Confidence
			e.PriceResolutionNote = r.Note
			if r.Stale {
				e.PriceResolutionNote = joinEventNote(e.PriceResolutionNote, "stale price")
			}
		} else if e.PriceResolutionNote == "" {
			e.PriceResolutionNote = "price unknown"
		}
	}
	if e.ReconcileKey == "" {
		e.ReconcileKey = e.Key().String()
	}
}

func joinEventNote(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

// sessionCWD is the agent's live absolute working directory.
func sessionCWD(s *agent.Session) string {
	cwd := s.Config.CWD
	if cwd == "" {
		cwd = "."
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// recordPhaseInputs meters the phase's prompts for every live agent.
func (m Model) recordPhaseInputs(prompts map[string]string) {
	if !m.Config.Usage.Enabled {
		return
	}
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		if p := prompts[view.Session.Name]; p != "" {
			m.recordUsageInput(view.Session, m.phase, p)
		}
	}
}

// recordUsageInput meters the actual prompt council writes to an agent PTY.
func (m Model) recordUsageInput(s *agent.Session, phase, userText string) {
	if !m.Config.Usage.Enabled {
		return
	}
	prompt := m.Config.PromptForAgent(s.Name, userText)
	wire := linePayload(s.Config.Terminal, prompt)
	est := usage.EstimatorFor(m.Config.Usage.Estimator)
	inputTokens, inputChars := est.Estimate(wire)
	_, userChars := est.Estimate(userText)
	_, wireChars := est.Estimate(wire)
	a := m.Config.Agents[s.Name]
	tool, toolSource, toolConf := usageTool(a)
	model, modelSource, modelConf := usageModel(a)
	m.recordUsageEvent(usage.Event{
		Agent: s.Name, Phase: phase, Source: usage.SourcePrompt, Confidence: usage.Estimated,
		Tool: tool, ToolSource: toolSource, ToolConfidence: toolConf,
		Model: model, ModelSource: modelSource, ModelConfidence: modelConf,
		CWD: sessionCWD(s), Fingerprint: agentFingerprint(m.Config, s.Name),
		Estimator: est.Name, UserInputChars: userChars, WireInputChars: wireChars,
		InputChars: inputChars, InputTokens: inputTokens,
		PromptHash: usage.PromptHash(prompt), PromptPreview: usage.PromptPreview(prompt),
	})
}

// recordUsageOutput meters an agent's transcript. Transcript output is a weak
// estimate: it is cumulative, so this records only the delta over output already
// logged for this agent.
func (m Model) recordUsageOutput(s *agent.Session, content string) {
	if !m.Config.Usage.Enabled || m.Store == nil || m.Store.RunDir == "" {
		return
	}
	est := usage.EstimatorFor(m.Config.Usage.Estimator)
	total, totalChars := est.Estimate(content)
	existing := 0
	if evs, err := usage.LoadEvents(m.Store.RunDir); err == nil {
		for _, e := range evs {
			if e.Agent == s.Name && e.Source == usage.SourceTranscript {
				existing += e.OutputTokens
			}
		}
	}
	delta := total - existing
	if delta <= 0 {
		return
	}
	a := m.Config.Agents[s.Name]
	tool, toolSource, toolConf := usageTool(a)
	model, modelSource, modelConf := usageModel(a)
	prompt := m.phasePrompts[s.Name]
	agentPrompt := ""
	if prompt != "" {
		agentPrompt = m.Config.PromptForAgent(s.Name, prompt)
	}
	ev := usage.Event{
		Agent: s.Name, Phase: m.phase, Source: usage.SourceTranscript, Confidence: usage.Estimated,
		Tool: tool, ToolSource: toolSource, ToolConfidence: toolConf,
		Model: model, ModelSource: modelSource, ModelConfidence: modelConf,
		CWD: sessionCWD(s), Estimator: est.Name, OutputTokens: delta,
		OutputBasis: usage.OutputBasisPaneTranscriptDelta, TranscriptCharsTotal: totalChars,
	}
	if agentPrompt != "" {
		ev.PromptHash = usage.PromptHash(agentPrompt)
		ev.PromptPreview = usage.PromptPreview(agentPrompt)
		ev.Fingerprint = agentFingerprint(m.Config, s.Name)
	}
	m.recordUsageEvent(ev)
}

// usageHeaderCost is the compact run total for the status line.
func (m Model) usageHeaderCost() string {
	if m.usageTally == nil || !m.Config.Usage.HeaderTotalEnabled() {
		return ""
	}
	var total float64
	currency := ""
	hasTokens, priced, unknown, mixed := false, false, false, false
	for name, t := range m.usageTally {
		if !t.Any() {
			continue
		}
		hasTokens = true
		r, ok := m.usageRate[name]
		if !ok || !r.Found {
			unknown = true
			continue
		}
		c, _ := r.Cost(t)
		total += c
		priced = true
		if currency == "" {
			currency = r.Currency
		} else if currency != r.Currency {
			mixed = true
		}
	}
	if !hasTokens {
		return "Run $0.00 est"
	}
	if mixed {
		return "Run mixed currency"
	}
	if unknown && !priced {
		return "cost unknown"
	}
	label := compactCost(total, currency) + " est"
	if unknown {
		label += " + unknown"
	}
	return "Run " + label
}

// usageBorderSuffix is the per-agent cost shown in a pane title.
func (m Model) usageBorderSuffix(name string) string {
	if m.usageTally == nil || !m.Config.Usage.BorderCostEnabled() {
		return ""
	}
	t := m.usageTally[name]
	if r, ok := m.usageRate[name]; ok && r.Found {
		c, _ := r.Cost(t)
		return " | " + compactCost(c, r.Currency) + "e"
	}
	if !t.Any() {
		return " | $0.00"
	}
	return " | $?"
}

func compactCost(cost float64, currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "USD":
		return fmt.Sprintf("$%.2f", cost)
	case "EUR":
		return fmt.Sprintf("EUR %.2f", cost)
	case "GBP":
		return fmt.Sprintf("GBP %.2f", cost)
	case "JPY":
		return fmt.Sprintf("JPY %.0f", cost)
	default:
		return fmt.Sprintf("%.2f %s", cost, strings.ToUpper(currency))
	}
}

// buildPricer constructs a Pricer from the active config + on-disk price cache.
func (m Model) buildPricer() *usage.Pricer {
	cacheDir := ""
	if m.Store != nil && m.Store.RootDir != "" {
		cacheDir = filepath.Join(filepath.Dir(m.Store.RootDir), "usage")
	}
	return usage.NewPricer(usage.PricerOptions{
		CacheDir:       cacheDir,
		ModelAliases:   m.Config.Usage.ModelAliases,
		Prices:         toPriceProfiles(m.Config.Usage.Prices),
		StaleAfterDays: m.Config.Usage.StalePriceAfterDays,
	})
}

func toPriceProfiles(prices map[string]config.PriceProfile) map[string]usage.PriceProfile {
	if len(prices) == 0 {
		return nil
	}
	out := make(map[string]usage.PriceProfile, len(prices))
	for name, p := range prices {
		out[name] = usage.PriceProfile{
			InputPerMillion:  p.InputPerMillion,
			OutputPerMillion: p.OutputPerMillion,
			Currency:         p.Currency,
			Source:           p.Source,
			ReviewedAt:       p.ReviewedAt,
		}
	}
	return out
}

// cmdCost opens the per-session usage/cost breakdown for the active run.
func (m *Model) cmdCost() {
	runDir, stamp := "", ""
	if m.orch != nil && m.orch.Run() != nil {
		runDir, stamp = m.orch.Run().Dir, m.orch.Run().Stamp
	} else if m.Store != nil && m.Store.RunDir != "" {
		runDir, stamp = m.Store.RunDir, filepath.Base(m.Store.RunDir)
	}
	if runDir == "" {
		m.Status = "cost -- no run yet (send a prompt first)"
		return
	}
	events, err := usage.LoadEvents(runDir)
	if err != nil {
		m.Status = "cost: " + err.Error()
		return
	}
	if len(events) == 0 {
		m.Status = "cost -- no usage recorded yet"
		return
	}
	pricer := m.usagePricer
	if pricer == nil {
		pricer = m.buildPricer()
	}
	events, rec, err := usage.ReconcileAndAppend(runDir, events)
	if err != nil {
		m.Status = "cost reconcile: " + err.Error()
		return
	}
	summary := usage.Aggregate(events)
	summary.Price(pricer)
	for _, note := range rec.Notes {
		summary.Hints = append(summary.Hints, note)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Cost -- run %s\n\n", stamp)
	b.WriteString(usage.FormatTable(summary))
	if src, at := pricer.Origin(); !at.IsZero() {
		fmt.Fprintf(&b, "\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
	}
	m.openArtifactText("cost: "+stamp, b.String())
	m.Status = "cost -- run " + stamp
}
