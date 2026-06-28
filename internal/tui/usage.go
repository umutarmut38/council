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
	r := m.usageResolver()
	for name, a := range m.Config.Agents {
		if costs, _, ok := r.Resolve(a.Usage.Model, a.Usage.PriceProfile); ok {
			m.usageRate[name] = costs
		}
	}
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
	if e.Model == "" {
		e.Model = au.Model
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
		CWD: m.Config.Agents[agent].CWD, InputChars: len(text), InputTokens: usage.EstimateTokens(text),
	})
}

// recordUsageOutput meters an agent's transcript. The transcript is cumulative,
// so it records only the delta over output already logged for this agent —
// keeping the total equal to the estimate of the final transcript rather than
// double-counting on each phase-end save.
func (m Model) recordUsageOutput(agent, content string) {
	if !m.Config.Usage.Enabled || m.Store == nil || m.Store.RunDir == "" {
		return
	}
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
	if !m.Config.Usage.ShowTotalInHeader || m.usageTally == nil {
		return ""
	}
	var total float64
	anyTokens, priced := false, false
	for name, t := range m.usageTally {
		if t.Input == 0 && t.Output == 0 {
			continue
		}
		anyTokens = true
		if r, ok := m.usageRate[name]; ok {
			total += float64(t.Input)*r.Input + float64(t.Output)*r.Output
			priced = true
		}
	}
	if !anyTokens {
		return ""
	}
	if !priced {
		return "cost unknown"
	}
	return fmt.Sprintf("Run $%.2f est", total)
}

// usageBorderSuffix is the per-agent cost shown in a pane title, e.g. " | $0.18e".
func (m Model) usageBorderSuffix(name string) string {
	if !m.Config.Usage.ShowAgentCostInBorder || m.usageTally == nil {
		return ""
	}
	t := m.usageTally[name]
	if t.Input == 0 && t.Output == 0 {
		return ""
	}
	r, ok := m.usageRate[name]
	if !ok {
		return " | $?"
	}
	cost := float64(t.Input)*r.Input + float64(t.Output)*r.Output
	return fmt.Sprintf(" | $%.2fe", cost)
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
