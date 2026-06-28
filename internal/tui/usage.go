package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/pricing"
	"github.com/umutarmut38/council/internal/provider"
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
	m.usageRate = map[string]pricing.ModelCosts{}
	m.usageModel = map[string]string{}
	m.usagePricer = m.usageResolver()
	for name, a := range m.Config.Agents {
		model := a.Usage.Model
		if model == "" {
			model = discoverModel(a) // auto-discover from the tool's session files
		}
		m.usageModel[name] = model
		if costs, _, ok := m.usagePricer.Resolve(model, a.Usage.PriceProfile); ok {
			m.usageRate[name] = costs
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

// discoverModel reads the agent's most recent model from its tool's session
// files. "" when the tool has no native reader or no session exists yet.
func discoverModel(a config.AgentConfig) string {
	rd := provider.ReaderFor(agentTool(a))
	if rd == nil {
		return ""
	}
	model, _ := rd.LatestModel(agentCWD(a))
	return model
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

// recordUsageInput meters a prompt council sent to an agent (estimated tokens).
func (m Model) recordUsageInput(agent, phase, text string) {
	if !m.Config.Usage.Enabled {
		return
	}
	m.recordUsageEvent(usage.Event{
		Agent: agent, Phase: phase, Source: usage.SourcePrompt, Confidence: usage.Estimated,
		CWD: agentCWD(m.Config.Agents[agent]), InputChars: len(text), InputTokens: usage.EstimateTokens(text),
	})
}

// rediscoverModel fills in an agent's model + rate once its session file exists
// (the agent had to do work first), so the live header/border start pricing
// mid-run without waiting for /cost. Maps are reference types, so the value
// receiver's writes persist.
func (m Model) rediscoverModel(agent string) {
	if m.usageModel == nil || m.usageModel[agent] != "" {
		return
	}
	a := m.Config.Agents[agent]
	model := discoverModel(a)
	if model == "" {
		return
	}
	m.usageModel[agent] = model
	if m.usagePricer != nil {
		if costs, _, ok := m.usagePricer.Resolve(model, a.Usage.PriceProfile); ok {
			m.usageRate[agent] = costs
		}
	}
}

// recordUsageOutput meters an agent's transcript. The transcript is cumulative,
// so it records only the delta over output already logged for this agent —
// keeping the total equal to the estimate of the final transcript rather than
// double-counting on each phase-end save.
func (m Model) recordUsageOutput(agent, content string) {
	if !m.Config.Usage.Enabled || m.Store == nil || m.Store.RunDir == "" {
		return
	}
	m.rediscoverModel(agent) // the agent has produced output → its session likely exists now
	total := usage.EstimateTokens(content)
	existing := 0
	if evs, err := usage.LoadEvents(m.Store.RunDir); err == nil {
		for _, e := range evs {
			if e.Agent == agent && e.Source == usage.SourceTranscript {
				existing += e.OutputTokens
			}
		}
	}
	delta := total - existing
	if delta <= 0 {
		return
	}
	m.recordUsageEvent(usage.Event{
		Agent: agent, Phase: m.phase, Source: usage.SourceTranscript, Confidence: usage.Estimated,
		OutputTokens: delta,
	})
}

// usageHeaderCost is the compact run total for the status line, e.g.
// "Run $0.42 est". Empty when nothing is metered; "cost unknown" when tokens
// exist but no agent has a known price. Pure read of the live tally.
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
			total += float64(t.Input)*r.Input + float64(t.Output)*r.Output
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
		cost := float64(t.Input)*r.Input + float64(t.Output)*r.Output
		return fmt.Sprintf(" | $%.2fe", cost)
	}
	if t.Input == 0 && t.Output == 0 {
		return " | $0.00" // no usage yet
	}
	return " | $?" // metered but price unknown
}

// usageResolver builds a pricing resolver from the active config + on-disk cache.
func (m Model) usageResolver() *pricing.Resolver {
	cacheDir := ""
	if m.Store != nil && m.Store.RootDir != "" {
		cacheDir = filepath.Join(filepath.Dir(m.Store.RootDir), "usage")
	}
	return pricing.New(pricing.Options{
		CacheDir:   cacheDir,
		UserAlias:  m.Config.Usage.ModelAliases,
		UserPrices: tuiUserPrices(m.Config.Usage.Prices),
	})
}

func tuiUserPrices(prices map[string]config.PriceProfile) map[string]pricing.UserPrice {
	if len(prices) == 0 {
		return nil
	}
	out := make(map[string]pricing.UserPrice, len(prices))
	for name, p := range prices {
		out[name] = pricing.UserPrice{
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
	events = append(events, usage.Reconcile(events, provider.Native())...)
	summary := usage.Aggregate(events)
	summary.Currency = m.Config.Usage.Currency
	summary.Price(m.usageResolver())

	var b strings.Builder
	fmt.Fprintf(&b, "# Cost — run %s\n\n", stamp)
	b.WriteString(usage.FormatTable(summary))
	if src, at := m.usageResolver().Origin(); !at.IsZero() {
		fmt.Fprintf(&b, "\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
	}
	m.openArtifactText("cost: "+stamp, b.String())
	m.Status = "cost — run " + stamp
}
