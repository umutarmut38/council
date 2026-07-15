package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	runstore "github.com/umutarmut38/council/internal/session"
	"github.com/umutarmut38/council/internal/usage"
)

func TestUsageMetadataIgnoresCommand(t *testing.T) {
	a := config.AgentConfig{Command: []string{"claude", "--model", "haiku"}}
	if tool, _, conf := usageTool(a); tool != usage.UnknownValue || conf != usage.Unknown {
		t.Fatalf("usageTool parsed command = %q/%q, want unknown", tool, conf)
	}
	if model, _, conf := usageModel(a); model != usage.UnknownValue || conf != usage.Unknown {
		t.Fatalf("usageModel parsed command = %q/%q, want unknown", model, conf)
	}
	a.Usage.Model = "sonnet"
	a.Usage.Tool = "claude"
	if model, _, _ := usageModel(a); model != "sonnet" {
		t.Fatalf("usageModel = %q, want explicit usage.model", model)
	}
	if tool, _, _ := usageTool(a); tool != "claude" {
		t.Fatalf("usageTool = %q, want explicit usage.tool", tool)
	}
}

func TestRecordUsageInputMetersWirePrompt(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		Agents: map[string]config.AgentConfig{
			"claude": {
				Command: []string{"claude", "--model", "haiku"},
				Usage:   config.AgentUsageConfig{Model: "haiku"},
				Terminal: config.TerminalConfig{
					SendMode:           "paste",
					BeforeSendSequence: "ctrl+u",
					SubmitSequence:     "cr",
				},
				Personality: "architect",
			},
		},
		Personalities: map[string]config.PersonalityConfig{
			"architect": {PromptPrefix: "You are The Architect."},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), []byte("cfg"), []byte("{}"))
	session := agent.NewSession("claude", cfg.Agents["claude"], "")
	// ensureRun opens the session's raw log; close it (via Terminate) before the
	// t.TempDir cleanup runs, or Windows can't delete the still-open raw/*.log.
	defer session.Terminate()
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, time.Millisecond, nil, nil)
	if err := m.ensureRun(); err != nil {
		t.Fatal(err)
	}
	m.recordUsageInput(session, "plan", "Build it.")
	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Model != "haiku" || e.ModelSource != usage.MetaSourceConfig {
		t.Fatalf("model = %q/%q, want config haiku", e.Model, e.ModelSource)
	}
	if e.Tool != usage.UnknownValue {
		t.Fatalf("tool = %q, want unknown without usage.tool", e.Tool)
	}
	if e.UserInputChars >= e.WireInputChars {
		t.Fatalf("wire chars should include personality/paste/submit bytes: user=%d wire=%d", e.UserInputChars, e.WireInputChars)
	}
	if !strings.Contains(e.PromptPreview, "Architect") || e.PromptHash == "" {
		t.Fatalf("prompt correlation fields missing: %+v", e)
	}
}

func usageConfigOn() config.UsageConfig {
	return config.UsageConfig{Enabled: true, Estimator: usage.EstimatorBytes4}
}

func TestUsageTickOnlyRearmsHeartbeat(t *testing.T) {
	m := Model{Config: config.Config{Usage: usageConfigOn()}}
	updated, cmd := m.update(usageTickMsg{})
	if cmd == nil {
		t.Fatal("usage tick must re-arm the TUI heartbeat")
	}
	if updated.(Model).usageReconcileBusy {
		t.Fatal("heartbeat must not start provider reconciliation")
	}
}

func TestRecordUsageEventInvalidatesAffectedReconciledRows(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		Agents: map[string]config.AgentConfig{
			"a": {Command: []string{"tool"}, Usage: config.AgentUsageConfig{Tool: "claude"}},
			"b": {Command: []string{"tool"}, Usage: config.AgentUsageConfig{Tool: "codex"}},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	m := Model{
		Config:     cfg,
		Store:      store,
		usageTally: map[string]usage.TokenPair{},
		usageRate: map[string]usage.Rate{
			"a": {InputPerToken: 0.001, Currency: "USD", Found: true},
		},
		usageReconciled: map[string]reconciledCost{
			"a":      {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
			"claude": {cost: 2, currency: "USD", confidence: usage.Reported, priced: true},
			"b":      {cost: 3, currency: "USD", confidence: usage.Reported, priced: true},
		},
	}
	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 4})

	if _, ok := m.usageReconciled["a"]; ok {
		t.Fatal("new local usage must invalidate the agent's reconciled row")
	}
	if _, ok := m.usageReconciled["claude"]; ok {
		t.Fatal("new local usage must invalidate the shared tool row")
	}
	if _, ok := m.usageReconciled["b"]; !ok {
		t.Fatal("another agent's reconciled row should remain valid")
	}
	if got := m.usageBorderSuffix("a"); got != " | ~$0.0040" {
		t.Fatalf("badge = %q, want live estimate after invalidation", got)
	}
}

func TestStaleCostResultDoesNotReplaceLiveEstimate(t *testing.T) {
	cfg := config.Config{Usage: usageConfigOn()}
	cfg.Normalize()
	m := Model{Config: cfg, Store: runstore.NewDeferred(t.TempDir(), nil, nil)}
	requestRevision := m.usageRevision
	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 4})
	if m.usageRevision != requestRevision+1 {
		t.Fatalf("usage revision = %d, want %d after local event", m.usageRevision, requestRevision+1)
	}
	updated, _ := m.update(costViewMsg{
		stamp:    "run",
		body:     "snapshot",
		revision: requestRevision,
		rollup: map[string]reconciledCost{
			"a": {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
		},
	})
	if got := updated.(Model).usageReconciled; len(got) != 0 {
		t.Fatalf("stale /cost result replaced newer live state: %+v", got)
	}
}

func TestCostRequestDoesNotOverlap(t *testing.T) {
	m := Model{usageReconcileBusy: true}
	if cmd := m.cmdCost(); cmd != nil {
		t.Fatal("a second /cost request must not start another provider scan")
	}
	if m.Status != "cost -- already reconciling" {
		t.Fatalf("status = %q, want already reconciling", m.Status)
	}
}

func TestCostCommandBuildsRollupWithoutPersisting(t *testing.T) {
	cfg := config.Config{
		Usage: config.UsageConfig{
			Enabled:   true,
			Estimator: usage.EstimatorBytes4,
			Prices: map[string]config.PriceProfile{
				"local": {InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD", Source: "test"},
			},
		},
		Agents: map[string]config.AgentConfig{
			"a": {Command: []string{"tool"}, Usage: config.AgentUsageConfig{Model: "custom", PriceProfile: "local"}},
		},
	}
	cfg.Normalize()
	m := Model{Config: cfg, Store: runstore.NewDeferred(t.TempDir(), nil, nil)}
	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 1000})

	before, err := usage.LoadEvents(m.Store.RunDir)
	if err != nil || len(before) != 1 {
		t.Fatalf("initial events: %v (%d)", err, len(before))
	}
	cmd := m.cmdCost()
	if cmd == nil || !m.usageReconcileBusy {
		t.Fatal("/cost must start one background command and mark it busy")
	}
	msg, ok := cmd().(costViewMsg)
	if !ok || msg.err != nil || msg.body == "" {
		t.Fatalf("cost result = %#v, want rendered success", msg)
	}
	if rc := msg.rollup["a"]; !rc.priced || rc.cost <= 0 {
		t.Fatalf("rollup = %+v, want priced agent", msg.rollup)
	}
	after, err := usage.LoadEvents(m.Store.RunDir)
	if err != nil || len(after) != len(before) {
		t.Fatalf("/cost persisted ledger rows: before=%d after=%d err=%v", len(before), len(after), err)
	}

	updated, _ := m.update(msg)
	next := updated.(Model)
	if next.usageReconcileBusy || !next.usageReconciled["a"].priced {
		t.Fatalf("completed cost state: busy=%v rollup=%+v", next.usageReconcileBusy, next.usageReconciled)
	}
}

// Direct-mode keystrokes bypass the composer, so handleDirectKey accumulates
// the typed line and meters it on Enter — producing the SourcePrompt estimate
// that both feeds the live tally and seeds provider reconciliation (tool+cwd).
func TestDirectModeMetersInputOnEnter(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		Agents: map[string]config.AgentConfig{
			"claude": {
				Command:     []string{"claude"},
				Usage:       config.AgentUsageConfig{Tool: "claude"},
				Personality: "architect",
			},
		},
		Personalities: map[string]config.PersonalityConfig{
			"architect": {PromptPrefix: "You are The Architect."},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	session := agent.NewSession("claude", cfg.Agents["claude"], "")
	// Close the raw log opened by ensureRun before the t.TempDir cleanup, or
	// Windows can't delete the still-open raw/*.log.
	defer session.Terminate()
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, time.Millisecond, nil, nil)
	if err := m.ensureRun(); err != nil {
		t.Fatal(err)
	}
	m.InputMode = InputDirect
	key := func(msg tea.KeyMsg) {
		nm, _ := m.handleDirectKey(msg)
		m = nm.(Model)
	}
	key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi there")}) // paste-shaped multi-rune
	key(tea.KeyMsg{Type: tea.KeyBackspace})
	key(tea.KeyMsg{Type: tea.KeyEnter})
	prompts := func() []usage.Event {
		events, err := usage.LoadEvents(store.RunDir)
		if err != nil {
			t.Fatal(err)
		}
		var out []usage.Event
		for _, e := range events {
			if e.Source == usage.SourcePrompt {
				out = append(out, e)
			}
		}
		return out
	}
	got := prompts()
	if len(got) != 1 {
		t.Fatalf("prompt events = %d, want 1", len(got))
	}
	e := got[0]
	if e.UserInputChars != len("hi ther") {
		t.Fatalf("user chars = %d, want %d (backspace applied)", e.UserInputChars, len("hi ther"))
	}
	if e.Tool != "claude" || e.CWD == "" {
		t.Fatalf("reconcile seed fields missing: tool=%q cwd=%q", e.Tool, e.CWD)
	}
	if !strings.Contains(e.PromptPreview, "hi ther") || e.PromptHash == "" {
		t.Fatalf("prompt correlation fields missing: %+v", e)
	}
	// Direct mode sends raw keystrokes, so the personality prefix must NOT be
	// billed: the model-visible estimate equals the typed text, not the wrapped
	// composer prompt.
	if strings.Contains(e.PromptPreview, "Architect") {
		t.Fatalf("direct mode billed the personality prefix it never sent: %q", e.PromptPreview)
	}
	if e.InputChars != e.UserInputChars {
		t.Fatalf("input chars (%d) should equal user chars (%d) — no personality wrapping in direct mode", e.InputChars, e.UserInputChars)
	}
	// Enter with an empty accumulator records nothing new.
	key(tea.KeyMsg{Type: tea.KeyEnter})
	if got := prompts(); len(got) != 1 {
		t.Fatalf("empty Enter added a prompt event: %d", len(got))
	}
}

// Regression for #3: a paste-mode agent's recorded input estimate must be the
// SEMANTIC prompt, excluding the bracketed-paste (\x1b[200~…\x1b[201~) and
// submit control bytes that only exist on the wire — so it equals a type-mode
// agent's estimate for the same text. WireInputChars keeps the transport size.
func TestInputTokensExcludeTransportBytes(t *testing.T) {
	record := func(sendMode string) usage.Event {
		cfg := config.Config{
			Usage: usageConfigOn(),
			Agents: map[string]config.AgentConfig{
				"a": {
					Command:  []string{"tool"},
					Usage:    config.AgentUsageConfig{Tool: "claude"},
					Terminal: config.TerminalConfig{SendMode: sendMode, SubmitSequence: "cr"},
				},
			},
		}
		cfg.Normalize()
		store := runstore.NewDeferred(t.TempDir(), nil, nil)
		session := agent.NewSession("a", cfg.Agents["a"], "")
		// Close the raw log opened by ensureRun before the t.TempDir cleanup, or
		// Windows can't delete the still-open raw/*.log.
		defer session.Terminate()
		m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, time.Millisecond, nil, nil)
		if err := m.ensureRun(); err != nil {
			t.Fatal(err)
		}
		m.recordUsageInput(session, "plan", "Refactor the parser.")
		events, err := usage.LoadEvents(store.RunDir)
		if err != nil || len(events) != 1 {
			t.Fatalf("events: %v (%d)", err, len(events))
		}
		return events[0]
	}
	paste := record("paste")
	typed := record("type")

	if paste.InputTokens != typed.InputTokens || paste.InputTokens == 0 {
		t.Fatalf("paste input tokens (%d) should equal type-mode (%d) for the same text", paste.InputTokens, typed.InputTokens)
	}
	if paste.InputChars != typed.InputChars {
		t.Fatalf("paste input chars (%d) should equal type-mode (%d)", paste.InputChars, typed.InputChars)
	}
	// The paste wire is strictly larger than the semantic estimate (transport
	// bytes), and larger than the type-mode wire (bracketed-paste wrapper).
	if paste.WireInputChars <= paste.InputChars {
		t.Fatalf("paste WireInputChars (%d) should exceed the semantic InputChars (%d)", paste.WireInputChars, paste.InputChars)
	}
	if paste.WireInputChars <= typed.WireInputChars {
		t.Fatalf("paste wire (%d) should exceed type-mode wire (%d) by the bracketed-paste bytes", paste.WireInputChars, typed.WireInputChars)
	}
}

// /cost reconciliation rolls up per agent so the live header/badge can show the
// reported cost: a priced reported session, a mixed agent (weakest tier wins),
// and an agent whose model never priced.
func TestRollupReconciled(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	s := usage.Summary{Sessions: []usage.SessionTotal{
		{Agent: "claude-a", Confidence: usage.Reported, Cost: f(0.04), Currency: "USD"},
		{Agent: "claude-a", Confidence: usage.Estimated, Cost: f(0.01), Currency: "USD"},
		{Agent: "codex-a", Confidence: usage.Reported, Cost: nil}, // price unknown
	}}
	rc := rollupReconciled(s)
	a := rc["claude-a"]
	if !a.priced || a.cost < 0.0499 || a.cost > 0.0501 || a.confidence != usage.Estimated {
		t.Fatalf("claude-a = %+v, want priced ~0.05 with weakest (estimated) confidence", a)
	}
	if c := rc["codex-a"]; c.priced || !c.someUnknown {
		t.Fatalf("codex-a = %+v, want unpriced + someUnknown", c)
	}
}

func TestCostLabel(t *testing.T) {
	for _, tc := range []struct {
		conf, want string
	}{
		// Sub-dime so the badge/header path exercises FormatMoney's 4-decimal rule.
		{usage.Exact, "$0.0060"},
		{usage.Reported, "$0.0060"},
		{usage.Estimated, "~$0.0060"},
		{usage.Unknown, "$?"},
		{"", "$?"},
	} {
		if got := costLabel(0.006, "USD", tc.conf); got != tc.want {
			t.Errorf("costLabel(0.006, USD, %q) = %q, want %q", tc.conf, got, tc.want)
		}
	}
}

func TestComposerTokenLabel(t *testing.T) {
	m := Model{Config: config.Config{Usage: usageConfigOn()}}
	m.PromptInput = "hello world foo bar" // 19 bytes, bytes4 → ~4 tokens
	if got := m.composerTokenLabel(); got != " · ~4 tok" {
		t.Fatalf("token label = %q, want \" · ~4 tok\"", got)
	}
	m.PromptInput = "/cost" // slash-commands show nothing
	if got := m.composerTokenLabel(); got != "" {
		t.Fatalf("slash command token label = %q, want empty", got)
	}
	m.PromptInput = ""
	if got := m.composerTokenLabel(); got != "" {
		t.Fatalf("empty input token label = %q, want empty", got)
	}
	off := Model{Config: config.Config{}, PromptInput: "hello"} // usage disabled
	if got := off.composerTokenLabel(); got != "" {
		t.Fatalf("usage-off token label = %q, want empty", got)
	}
}

func TestCostShareSection(t *testing.T) {
	rollup := map[string]reconciledCost{
		"alpha": {cost: 0.75, currency: "USD", confidence: usage.Reported, priced: true},
		"beta":  {cost: 0.25, currency: "USD", confidence: usage.Estimated, priced: true},
		"gamma": {someUnknown: true}, // unpriced → excluded from shares
	}
	out := costShareSection(rollup)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "Share:" {
		t.Fatalf("want 'Share:' header + 2 rows, got %d lines: %q", len(lines), out)
	}
	// Sorted by cost desc: alpha (75%, reported $) before beta (25%, estimated ~$).
	if !strings.Contains(lines[1], "75%") || !strings.Contains(lines[1], "$0.75") || strings.Contains(lines[1], "~$0.75") {
		t.Fatalf("alpha row wrong: %q", lines[1])
	}
	if !strings.Contains(lines[2], "25%") || !strings.Contains(lines[2], "~$0.25") {
		t.Fatalf("beta row (estimated) wrong: %q", lines[2])
	}
	if cells := strings.Count(lines[1], "█") + strings.Count(lines[1], "░"); cells != 20 {
		t.Fatalf("bar width = %d, want 20: %q", cells, lines[1])
	}
	if costShareSection(nil) != "" || costShareSection(map[string]reconciledCost{"x": {someUnknown: true}}) != "" {
		t.Fatal("no priced agents should produce no section")
	}
	// Mixed currencies make summed shares meaningless → suppress the section.
	mixed := map[string]reconciledCost{
		"usd": {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
		"eur": {cost: 1, currency: "EUR", confidence: usage.Reported, priced: true},
	}
	if got := costShareSection(mixed); got != "" {
		t.Fatalf("mixed-currency shares should be suppressed, got %q", got)
	}
}

// After /cost reconciles, the pane badge shows the reported total (plain $) and
// the header drops the ~ estimate prefix — not the stale estimated floor.
func TestBadgeAndHeaderPreferReconciled(t *testing.T) {
	m := Model{
		Config:     config.Config{Usage: usageConfigOn()},
		usageTally: map[string]usage.TokenPair{"claude-a": {Input: 20}},
		usageRate:  map[string]usage.Rate{"claude-a": {Currency: "USD", Found: true}},
		usageReconciled: map[string]reconciledCost{
			// Sub-dime so the badge/header show FormatMoney's 4-decimal precision.
			"claude-a": {cost: 0.006, currency: "USD", confidence: usage.Reported, priced: true},
		},
	}
	if got := m.usageBorderSuffix("claude-a"); got != " | $0.0060" {
		t.Fatalf("badge = %q, want \" | $0.0060\"", got)
	}
	if got := m.usageHeaderCost(); got != "Run $0.0060" {
		t.Fatalf("header = %q, want \"Run $0.0060\" (reported, no ~ prefix)", got)
	}
}

// Cumulative mode: two same-tool panes sharing a cwd reconcile to ONE combined
// row keyed by the TOOL label ("claude"), not any pane name. The header must
// treat both panes as covered by that row — not add their estimated floors on
// top of the combined reported total.
func TestHeaderSkipsEstimatesCoveredByCombinedToolRow(t *testing.T) {
	cfg := config.Config{Usage: usageConfigOn(), Agents: map[string]config.AgentConfig{
		"claude-a": {Usage: config.AgentUsageConfig{Tool: "claude"}},
		"claude-b": {Usage: config.AgentUsageConfig{Tool: "claude"}},
	}}
	m := Model{
		Config: cfg,
		usageTally: map[string]usage.TokenPair{
			"claude-a": {Input: 1000}, "claude-b": {Input: 1000},
		},
		usageRate: map[string]usage.Rate{
			"claude-a": {InputPerToken: 0.001, Currency: "USD", Found: true},
			"claude-b": {InputPerToken: 0.001, Currency: "USD", Found: true},
		},
		usageReconciled: map[string]reconciledCost{
			"claude": {cost: 3.00, currency: "USD", confidence: usage.Reported, priced: true},
		},
	}
	if got := m.usageHeaderCost(); got != "Run $3.00" {
		t.Fatalf("header = %q, want \"Run $3.00\" (combined row covers both panes)", got)
	}
}

func TestWeakerConfidence(t *testing.T) {
	if weakerConfidence("", usage.Reported) != usage.Reported {
		t.Error(`weakerConfidence("", reported) should seed with reported`)
	}
	if weakerConfidence(usage.Reported, usage.Estimated) != usage.Estimated {
		t.Error("estimated is weaker than reported")
	}
	if weakerConfidence(usage.Exact, usage.Reported) != usage.Reported {
		t.Error("reported is weaker than exact")
	}
}
