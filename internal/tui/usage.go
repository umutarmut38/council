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

// recordUsageEvent appends one usage event for the active run. No-op when usage
// is disabled. Runs on the Update goroutine; the append is self-contained so it
// needs no shared ledger handle. Best-effort: a write error never blocks a run.
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
	if m.orch == nil || m.orch.Run() == nil {
		m.Status = "no active run"
		return
	}
	run := m.orch.Run()
	events, err := usage.LoadEvents(run.Dir)
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
	fmt.Fprintf(&b, "# Cost — run %s\n\n", run.Stamp)
	b.WriteString(usage.FormatTable(summary))
	if src, at := m.usageResolver().Origin(); !at.IsZero() {
		fmt.Fprintf(&b, "\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
	}
	m.openArtifactText("cost: "+run.Stamp, b.String())
	m.Status = "cost — run " + run.Stamp
}
