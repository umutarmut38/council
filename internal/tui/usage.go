package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/usage"
)

// usageReconcileInterval is how often the live header/badge refresh while agents
// are producing output. ponytail: a flat 1s poll — only fires when usageDirty,
// so idle panes cost nothing; no staleness bookkeeping needed.
const usageReconcileInterval = time.Second

// usageTickCmd schedules the next live-usage tick.
func usageTickCmd() tea.Cmd {
	return tea.Tick(usageReconcileInterval, func(time.Time) tea.Msg { return usageTickMsg{} })
}

// maybeReconcileCmd returns a command that reconciles + reprices the active run
// off-thread, but only when an agent has produced output since the last pass and
// none is already running. It captures the run dir and the read-only pricer so
// the closure never touches the model.
func (m *Model) maybeReconcileCmd() tea.Cmd {
	if !m.Config.Usage.Enabled || m.usageReconcileBusy || !m.usageDirty {
		return nil
	}
	runDir := ""
	if m.orch != nil && m.orch.Run() != nil {
		runDir = m.orch.Run().Dir
	} else if m.Store != nil {
		runDir = m.Store.RunDir
	}
	if runDir == "" {
		return nil
	}
	pricer := m.usagePricer
	if pricer == nil {
		pricer = m.buildPricer()
	}
	m.usageDirty = false
	m.usageReconcileBusy = true
	return reconcileRollupCmd(runDir, pricer)
}

// usageFinalizeGrace lets a CLI that flushes its usage only on exit (notably
// Copilot, which writes token totals in session.shutdown) finish writing its
// session file after the panes are terminated, before the final reconcile reads
// it. Small enough to be unnoticeable on quit.
const usageFinalizeGrace = 300 * time.Millisecond

// FinalizeUsage runs one last provider-session reconciliation after the TUI has
// exited. Copilot (and any tool that reports only at shutdown) never surfaces its
// real input during the live loop — it writes token totals only when the process
// exits, which happens when the panes are terminated on quit. Reconciling here
// upgrades the run's persisted estimate (e.g. a bare "hi" showing 101) to the
// reported total, so `council cost` and history are correct without a manual
// re-run. Best-effort and synchronous: the app is already exiting.
func (m Model) FinalizeUsage() {
	if !m.Config.Usage.Enabled {
		return
	}
	runDir := ""
	if m.orch != nil && m.orch.Run() != nil {
		runDir = m.orch.Run().Dir
	} else if m.Store != nil {
		runDir = m.Store.RunDir
	}
	if runDir == "" {
		return
	}
	events, err := usage.LoadEvents(runDir)
	if err != nil || len(events) == 0 {
		return // nothing was ever run; no reason to wait or reconcile
	}
	time.Sleep(usageFinalizeGrace) // let a shutdown-only reporter finish writing
	_, _, _ = usage.ReconcileAndAppend(runDir, events)
}

// reconcileRollupCmd reads the run's events, reconciles provider sessions, prices
// the result, and returns a per-agent rollup for the header/badge. Runs in a
// tea.Cmd goroutine: it only reads the model's captured runDir + read-only pricer.
func reconcileRollupCmd(runDir string, pricer *usage.Pricer) tea.Cmd {
	return func() tea.Msg {
		events, err := usage.LoadEvents(runDir)
		if err != nil || len(events) == 0 {
			return usageReconciledMsg{} // nil rollup → just clears the busy flag
		}
		events, _, _ = usage.ReconcileAndAppend(runDir, events)
		summary := usage.Aggregate(events)
		summary.Price(pricer)
		return usageReconciledMsg{rollup: rollupReconciled(summary)}
	}
}

// initUsage allocates live usage state. No-op when usage is disabled.
func (m *Model) initUsage() {
	if !m.Config.Usage.Enabled {
		return
	}
	m.usageTally = map[string]usage.TokenPair{}
	m.usageRate = map[string]usage.Rate{}
	m.usageOutputSeen = map[string]int{}
	m.usagePricer = m.buildPricer()
	for name, a := range m.Config.Agents {
		model, _, _ := usageModel(a)
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
	// A new prompt is a turn boundary: flush the previous turn's transcript
	// delta first, so output cost accrues per turn instead of only on /save.
	if view := m.findAgentForMessage(s.Name, s); view != nil {
		m.recordUsageOutput(s, view.transcript())
	}
	prompt := m.Config.PromptForAgent(s.Name, userText)
	wire := linePayload(s.Config.Terminal, prompt)
	est := usage.EstimatorFor(m.Config.Usage.Estimator)
	// Estimate the input from the SEMANTIC prompt (personality prefix + user
	// text) the model actually sees — not the wire payload. For paste-mode agents
	// (codex/copilot) linePayload wraps the text in bracketed-paste
	// (\x1b[200~…\x1b[201~) plus submit/before/after control bytes; counting
	// those transport bytes under bytes4 inflated the input tokens. WireInputChars
	// stays as a transport diagnostic.
	inputTokens, inputChars := est.Estimate(prompt)
	_, userChars := est.Estimate(userText)
	_, wireChars := est.Estimate(wire)
	a := m.Config.Agents[s.Name]
	tool, toolSource, toolConf := usageTool(a)
	model, modelSource, modelConf := usageModel(a)
	m.recordUsageEvent(usage.Event{
		Agent: s.Name, Phase: phase, Source: usage.SourcePrompt, Confidence: usage.Estimated,
		Tool: tool, ToolSource: toolSource, ToolConfidence: toolConf,
		Model: model, ModelSource: modelSource, ModelConfidence: modelConf,
		CWD:       sessionCWD(s),
		Estimator: est.Name, UserInputChars: userChars, WireInputChars: wireChars,
		InputChars: inputChars, InputTokens: inputTokens,
		PromptHash: usage.PromptHash(prompt), PromptPreview: usage.PromptPreview(prompt),
	})
}

// recordUsageOutput meters an agent's transcript. Transcript output is a weak
// estimate: it is cumulative, so this records only the delta over output already
// recorded by THIS process (see usageOutputSeen). Flushed at turn boundaries
// (before each new prompt), on manual /save, and when the panes terminate — so
// reader-less tools get an output floor without anyone pressing save.
func (m Model) recordUsageOutput(s *agent.Session, content string) {
	if !m.Config.Usage.Enabled || m.Store == nil || m.Store.RunDir == "" || m.usageOutputSeen == nil {
		return
	}
	est := usage.EstimatorFor(m.Config.Usage.Estimator)
	total, totalChars := est.Estimate(content)
	delta := total - m.usageOutputSeen[s.Name]
	if delta <= 0 {
		return
	}
	m.usageOutputSeen[s.Name] = total
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
	hasTokens, priced, unknown, mixed, anyEstimated := false, false, false, false, false
	addCurrency := func(c string) {
		switch {
		case c == "":
		case currency == "":
			currency = c
		case currency != c:
			mixed = true
		}
	}
	// Agents reconciled by the last /cost use reported numbers; the rest fall
	// back to the live estimated floor.
	for _, rc := range m.usageReconciled {
		hasTokens = true
		if rc.priced {
			total += rc.cost
			priced = true
			addCurrency(rc.currency)
			if rc.confidence == usage.Estimated {
				anyEstimated = true
			}
		}
		if rc.someUnknown || !rc.priced {
			unknown = true
		}
	}
	for name, t := range m.usageTally {
		if m.usageReconcileCovers(name) || !t.Any() {
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
		anyEstimated = true
		addCurrency(r.Currency)
	}
	if !hasTokens {
		return "Run ~$0.00"
	}
	if mixed {
		return "Run mixed currency"
	}
	if unknown && !priced {
		return "Run $?"
	}
	label := compactCost(total, currency)
	if anyEstimated {
		label = "~" + label
	}
	if unknown {
		label += " + unknown"
	}
	return "Run " + label
}

// usageBorderSuffix is the per-agent cost shown in a pane title. Once /cost has
// reconciled, it prefers the reported total (plain $) over the estimated floor
// (~$); see costLabel for the vocabulary.
func (m Model) usageBorderSuffix(name string) string {
	if m.usageTally == nil || !m.Config.Usage.BorderCostEnabled() {
		return ""
	}
	if rc, ok := m.usageReconciled[name]; ok {
		if !rc.priced {
			return " | $?"
		}
		return " | " + costLabel(rc.cost, rc.currency, rc.confidence)
	}
	t := m.usageTally[name]
	if r, ok := m.usageRate[name]; ok && r.Found {
		c, _ := r.Cost(t)
		return " | " + costLabel(c, r.Currency, usage.Estimated)
	}
	if !t.Any() {
		return " | $0.00"
	}
	return " | $?"
}

// usageReconcileCovers reports whether a pane's usage is already inside the
// reconciled rollup: either under its own name, or under its TOOL label — the
// key a combined row gets when several same-tool panes share one cwd. Without
// the tool check the header would add the combined reported total AND each
// pane's estimated floor.
// ponytail: tool coverage is cwd-blind — a same-tool pane isolated in another
// cwd with no reported row yet is briefly skipped too; carry cwd through the
// rollup if that mixed setup ever matters.
func (m Model) usageReconcileCovers(name string) bool {
	if _, ok := m.usageReconciled[name]; ok {
		return true
	}
	if tool := m.Config.Agents[name].Usage.Tool; tool != "" {
		if _, ok := m.usageReconciled[tool]; ok {
			return true
		}
	}
	return false
}

// reconciledCost is a per-agent priced rollup of a /cost summary, used by the
// live header/badge so they reflect reported numbers, not just the estimate.
type reconciledCost struct {
	cost        float64
	currency    string
	confidence  string // weakest token confidence across the agent's priced sessions
	priced      bool   // at least one session resolved to a price
	someUnknown bool   // at least one session had no resolvable price
}

// rollupReconciled aggregates a priced summary into per-agent totals.
func rollupReconciled(summary usage.Summary) map[string]reconciledCost {
	out := map[string]reconciledCost{}
	for _, s := range summary.Sessions {
		rc := out[s.Agent]
		if s.Cost != nil {
			rc.cost += *s.Cost
			rc.priced = true
			rc.confidence = weakerConfidence(rc.confidence, s.Confidence)
			if rc.currency == "" {
				rc.currency = s.Currency
			}
		} else {
			rc.someUnknown = true
		}
		out[s.Agent] = rc
	}
	return out
}

var confidenceRank = map[string]int{usage.Unknown: 0, usage.Estimated: 1, usage.Reported: 2, usage.Exact: 3}

// weakerConfidence returns the lower-confidence of two tiers ("" means none yet).
func weakerConfidence(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case confidenceRank[b] < confidenceRank[a]:
		return b
	default:
		return a
	}
}

// costLabel formats a cost in the estimate-prefix vocabulary shared by the
// header and pane badges: ~$0.02 estimated, $0.02 reported/exact, $? unknown.
// It replaces the old single-letter e/r/x suffixes so one glyph convention
// covers every cost readout.
func costLabel(cost float64, currency, confidence string) string {
	switch confidence {
	case usage.Reported, usage.Exact:
		return compactCost(cost, currency)
	case usage.Estimated:
		return "~" + compactCost(cost, currency)
	default:
		return "$?"
	}
}

func compactCost(cost float64, currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "USD":
		return "$" + usage.FormatMoney(cost)
	case "EUR":
		return "EUR " + usage.FormatMoney(cost)
	case "GBP":
		return "GBP " + usage.FormatMoney(cost)
	case "JPY":
		return fmt.Sprintf("JPY %.0f", cost)
	default:
		return usage.FormatMoney(cost) + " " + strings.ToUpper(currency)
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
			InputPerMillion:      p.InputPerMillion,
			OutputPerMillion:     p.OutputPerMillion,
			CacheWritePerMillion: p.CacheWritePerMillion,
			CacheReadPerMillion:  p.CacheReadPerMillion,
			Currency:             p.Currency,
			Source:               p.Source,
			ReviewedAt:           p.ReviewedAt,
		}
	}
	return out
}

// cmdCost opens the per-session usage/cost breakdown for the active run. The
// reconcile reads every provider session store on disk, so the work runs in a
// tea.Cmd goroutine (synchronous reconciliation here froze the whole TUI on
// large stores); costViewMsg carries the rendered view back to Update.
func (m *Model) cmdCost() tea.Cmd {
	runDir, stamp := "", ""
	if m.orch != nil && m.orch.Run() != nil {
		runDir, stamp = m.orch.Run().Dir, m.orch.Run().Stamp
	} else if m.Store != nil && m.Store.RunDir != "" {
		runDir, stamp = m.Store.RunDir, filepath.Base(m.Store.RunDir)
	}
	if runDir == "" {
		m.Status = "cost -- no run yet (send a prompt first)"
		return nil
	}
	pricer := m.usagePricer
	if pricer == nil {
		pricer = m.buildPricer()
	}
	m.usageReconcileBusy = true
	m.Status = "cost -- reconciling..."
	return func() tea.Msg {
		out := costViewMsg{stamp: stamp}
		events, err := usage.LoadEvents(runDir)
		if err != nil {
			out.err = err
			return out
		}
		if len(events) == 0 {
			return out // empty body → "no usage recorded yet"
		}
		events, rec, err := usage.ReconcileAndAppend(runDir, events)
		if err != nil {
			out.err = fmt.Errorf("reconcile: %w", err)
			return out
		}
		summary := usage.Aggregate(events)
		summary.Price(pricer)
		summary.Hints = append(summary.Hints, rec.Notes...)
		out.rollup = rollupReconciled(summary)

		var b strings.Builder
		fmt.Fprintf(&b, "# Cost -- run %s\n\n", stamp)
		b.WriteString(usage.FormatTable(summary))
		if src, at := pricer.Origin(); !at.IsZero() {
			fmt.Fprintf(&b, "\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
		}
		out.body = b.String()
		return out
	}
}
