package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/usage"
)

// initUsage allocates the live tally and resolves each agent's price once, so
// the header/border can render cost as cheap arithmetic in View. No-op when
// usage is disabled (the maps stay nil and every usage path short-circuits).
func (m *Model) initUsage() {
	if !m.Config.Usage.Enabled {
		return
	}
	m.usageTally = map[string]usage.TokenPair{}
	m.usageRate = map[string]usage.Rate{}
	m.usageModel = map[string]string{}
	m.usagePricer = m.buildPricer()
	for name, a := range m.Config.Agents {
		model := a.Usage.Model
		if model == "" {
			model = usage.DiscoverModel(agentTool(a), agentCWD(a)) // auto-discover
		}
		m.usageModel[name] = model
		if r := m.usagePricer.Rate(model, a.Usage.PriceProfile); r.Found {
			m.usageRate[name] = r
		}
	}
}

// agentTool maps an agent to the tool key used to pick a native session reader:
// an explicit usage.tool, else the basename of its launch command.
func agentTool(a config.AgentConfig) string {
	if a.Usage.Tool != "" {
		return a.Usage.Tool
	}
	if len(a.Command) > 0 {
		return filepath.Base(a.Command[0])
	}
	return ""
}

// agentCWD is the agent's absolute working directory — the key tools record in
// their session files, used for both discovery and event correlation.
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
// tally. No-op when usage is disabled. Runs on the Update goroutine; the append
// is self-contained so it needs no shared ledger handle. Best-effort: a write
// error never blocks a run.
func (m Model) recordUsageEvent(e usage.Event) {
	if !m.Config.Usage.Enabled || m.Store == nil {
		return
	}
	if m.usageTally != nil { // map is a reference type, so this persists
		t := m.usageTally[e.Agent]
		t.Input += e.InputTokens
		t.Output += e.OutputTokens
		m.usageTally[e.Agent] = t
	}
	dir := m.Store.RunDir
	if dir == "" {
		if err := m.Store.Ensure(); err != nil {
			return
		}
		dir = m.Store.RunDir
	}
	e.RunID = filepath.Base(dir)
	au := m.Config.Agents[e.Agent].Usage
	if e.Model == "" { // explicit model, else the auto-discovered one
		if m.usageModel != nil && m.usageModel[e.Agent] != "" {
			e.Model = m.usageModel[e.Agent]
		} else {
			e.Model = au.Model
		}
	}
	e.PriceProfile = au.PriceProfile
	_ = usage.Append(dir, e)
}

// sessionCWD is the agent's live absolute working directory. It comes from the
// session (not the static config) so build-phase panes — which run in their own
// worktree — record the worktree path the tool's session file is keyed by.
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

// recordPhaseInputs meters the phase's prompts for every live agent. Called from
// Update (never a tea.Cmd) so it stays on the bubbletea goroutine.
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

// recordUsageInput meters a prompt council sent to an agent (estimated tokens).
// Tags the event with the agent's tool + live cwd so reconciliation can find it.
func (m Model) recordUsageInput(s *agent.Session, phase, text string) {
	if !m.Config.Usage.Enabled {
		return
	}
	a := m.Config.Agents[s.Name]
	m.recordUsageEvent(usage.Event{
		Agent: s.Name, Phase: phase, Source: usage.SourcePrompt, Confidence: usage.Estimated,
		Tool: agentTool(a), CWD: sessionCWD(s), InputChars: len(text), InputTokens: usage.EstimateTokens(text),
	})
}

// rediscoverModel fills in an agent's model + rate once its session file exists
// (the agent had to do work first), so the live header/border start pricing
// mid-run without waiting for /cost. Maps are reference types, so the value
// receiver's writes persist.
func (m Model) rediscoverModel(s *agent.Session) {
	if m.usageModel == nil || m.usageModel[s.Name] != "" {
		return
	}
	a := m.Config.Agents[s.Name]
	model := usage.DiscoverModel(agentTool(a), sessionCWD(s))
	if model == "" {
		return
	}
	m.usageModel[s.Name] = model
	if m.usagePricer != nil {
		if r := m.usagePricer.Rate(model, a.Usage.PriceProfile); r.Found {
			m.usageRate[s.Name] = r
		}
	}
}

// recordUsageOutput meters an agent's transcript. The transcript is cumulative,
// so it records only the delta over output already logged for this agent —
// keeping the total equal to the estimate of the final transcript rather than
// double-counting on each phase-end save.
func (m Model) recordUsageOutput(s *agent.Session, content string) {
	if !m.Config.Usage.Enabled || m.Store == nil || m.Store.RunDir == "" {
		return
	}
	m.rediscoverModel(s) // the agent has produced output → its session likely exists now
	total := usage.EstimateTokens(content)
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
	m.recordUsageEvent(usage.Event{
		Agent: s.Name, Phase: m.phase, Source: usage.SourceTranscript, Confidence: usage.Estimated,
		Tool: agentTool(a), CWD: sessionCWD(s), OutputTokens: delta,
	})
}

// usageHeaderCost is the compact run total for the status line, e.g.
// "Run $0.42 est". Always shown when enabled (default $0.00); "cost unknown"
// when metered but no agent has a known price. Pure read of the live tally.
func (m Model) usageHeaderCost() string {
	if m.usageTally == nil || !m.Config.Usage.HeaderTotalEnabled() {
		return "" // usage off, or header total opted out
	}
	var total float64
	hasTokens, priced := false, false
	for name, t := range m.usageTally {
		if t.Input == 0 && t.Output == 0 {
			continue
		}
		hasTokens = true
		if r, ok := m.usageRate[name]; ok {
			total += float64(t.Input)*r.InputPerToken + float64(t.Output)*r.OutputPerToken
			priced = true
		}
	}
	if hasTokens && !priced {
		return "cost unknown" // metered but no known price → never a silent $0
	}
	return fmt.Sprintf("Run $%.2f est", total) // always shown; defaults to $0.00
}

// usageBorderSuffix is the per-agent cost shown in a pane title, e.g. " | $0.18e".
// Always present when enabled, defaulting to " | $0.00".
func (m Model) usageBorderSuffix(name string) string {
	if m.usageTally == nil || !m.Config.Usage.BorderCostEnabled() {
		return ""
	}
	t := m.usageTally[name]
	if r, ok := m.usageRate[name]; ok {
		cost := float64(t.Input)*r.InputPerToken + float64(t.Output)*r.OutputPerToken
		return fmt.Sprintf(" | $%.2fe", cost)
	}
	if t.Input == 0 && t.Output == 0 {
		return " | $0.00" // no usage yet
	}
	return " | $?" // metered but price unknown
}

// buildPricer constructs a Pricer from the active config + on-disk price cache.
func (m Model) buildPricer() *usage.Pricer {
	cacheDir := ""
	if m.Store != nil && m.Store.RootDir != "" {
		cacheDir = filepath.Join(filepath.Dir(m.Store.RootDir), "usage")
	}
	return usage.NewPricer(usage.PricerOptions{
		CacheDir:     cacheDir,
		ModelAliases: m.Config.Usage.ModelAliases,
		Prices:       toPriceProfiles(m.Config.Usage.Prices),
	})
}

func toPriceProfiles(prices map[string]config.PriceProfile) map[string]usage.PriceProfile {
	if len(prices) == 0 {
		return nil
	}
	out := make(map[string]usage.PriceProfile, len(prices))
	for name, p := range prices {
		out[name] = usage.PriceProfile{
			InputPerMillion: p.InputPerMillion, OutputPerMillion: p.OutputPerMillion,
			Currency: p.Currency, ReviewedAt: p.ReviewedAt,
		}
	}
	return out
}

// cmdCost opens the per-session usage/cost breakdown for the active run.
func (m *Model) cmdCost() {
	// Prefer the orchestration run; fall back to the free-chat session's run dir
	// so /cost works outside an orchestrated flow too.
	runDir, stamp := "", ""
	if m.orch != nil && m.orch.Run() != nil {
		runDir, stamp = m.orch.Run().Dir, m.orch.Run().Stamp
	} else if m.Store != nil && m.Store.RunDir != "" {
		runDir, stamp = m.Store.RunDir, filepath.Base(m.Store.RunDir)
	}
	if runDir == "" {
		m.Status = "cost — no run yet (send a prompt first)"
		return
	}
	events, err := usage.LoadEvents(runDir)
	if err != nil {
		m.Status = "cost: " + err.Error()
		return
	}
	if len(events) == 0 {
		m.Status = "cost — no usage recorded yet"
		return
	}
	pricer := m.usagePricer
	if pricer == nil {
		pricer = m.buildPricer()
	}
	events = append(events, usage.Reconcile(events)...)
	summary := usage.Aggregate(events)
	summary.Currency = m.Config.Usage.Currency
	summary.Price(pricer)

	var b strings.Builder
	fmt.Fprintf(&b, "# Cost — run %s\n\n", stamp)
	b.WriteString(usage.FormatTable(summary))
	if src, at := pricer.Origin(); !at.IsZero() {
		fmt.Fprintf(&b, "\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
	}
	m.openArtifactText("cost: "+stamp, b.String())
	m.Status = "cost — run " + stamp
}
