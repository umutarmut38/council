package tui

import (
	"os"
	"path/filepath"
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

func TestRunes4OutputCountsSplitUTF8Once(t *testing.T) {
	cfg := config.Config{
		Usage:  config.UsageConfig{Enabled: true, Estimator: usage.EstimatorRunes4},
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()

	encoded := []byte("€€€€")
	m.appendOutput(m.Agents[0], string(encoded[:2]))
	m.appendOutput(m.Agents[0], string(encoded[2:7]))
	m.appendOutput(m.Agents[0], string(encoded[7:]))
	m.flushUsageOutputs()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].OutputTokens != 1 || events[0].TranscriptCharsTotal != 4 {
		t.Fatalf("split UTF-8 usage = %+v, want 4 runes / 1 token", events)
	}
}

func TestRestartClearsDirectInputForOldSession(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	old := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{old}, runstore.NewDeferred(t.TempDir(), nil, nil), cfg, "", nil, 0, nil, nil)
	m.directTyped[old] = "old partial line"
	m.cmdRestart("a")
	if _, ok := m.directTyped[old]; ok {
		t.Fatal("restart retained direct input for the old PTY session")
	}
	if fresh := m.Agents[0].Session; fresh == old || m.directTyped[fresh] != "" {
		t.Fatalf("fresh session inherited old direct input: old=%p fresh=%p value=%q", old, fresh, m.directTyped[fresh])
	}
	defer m.Agents[0].Session.Terminate()
}

func TestRestartPreservesOutputAfterLedgerFailure(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	old := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{old}, store, cfg, "", nil, 0, nil, nil)
	m.appendOutput(m.Agents[0], "output pending across restart\n")

	eventsPath := filepath.Join(store.RunDir, "usage", "events.jsonl")
	if err := os.MkdirAll(eventsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	m.cmdRestart("a")
	fresh := m.Agents[0].Session
	defer fresh.Terminate()
	if m.Agents[0].usageOutputUnits == 0 {
		t.Fatal("restart discarded output after failed ledger append")
	}
	if err := os.Remove(eventsPath); err != nil {
		t.Fatal(err)
	}
	m.flushUsageOutputs()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Source != usage.SourceTranscript {
		t.Fatalf("restart retry events = %+v, want one transcript event", events)
	}
}

func TestDirectModeFailedRunSetupClearsSubmittedLine(t *testing.T) {
	root := t.TempDir()
	blockedRoot := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Usage:    usageConfigOn(),
		Sessions: config.SessionConfig{RootDir: blockedRoot},
		Agents:   map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, runstore.NewDeferred(blockedRoot, nil, nil), cfg, "", nil, 0, nil, nil)
	m.InputMode = InputDirect
	m.directTyped[session] = "already sent line"

	updated, _ := m.handleDirectKey(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)
	if _, ok := next.directTyped[session]; ok {
		t.Fatal("failed run setup retained a line already submitted to the PTY")
	}
}

func usageConfigOn() config.UsageConfig {
	return config.UsageConfig{Enabled: true, Estimator: usage.EstimatorBytes4}
}

func TestFinalizeUsageFlushesTranscriptOutput(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	session := agent.NewSession("a", cfg.Agents["a"], "")
	defer session.Terminate()
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, time.Millisecond, nil, nil)
	if err := m.ensureRun(); err != nil {
		t.Fatal(err)
	}
	m.appendOutput(m.Agents[0], "final transcript output\n")

	m.FinalizeUsage()
	m.FinalizeUsage()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	var outputs int
	for _, event := range events {
		if event.Source == usage.SourceTranscript {
			outputs++
		}
	}
	if outputs != 1 {
		t.Fatalf("transcript events = %d, want 1 after finalization: %+v", outputs, events)
	}
}

func TestRecoverFinalOutputPersistsUnacknowledgedSuffix(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()

	// Bubble Tea processed and acknowledged the first chunk.
	first := []byte("first output\n")
	firstEnd := session.TrackOutput(first)
	updated, _ := m.update(AgentOutputMsg{Name: "a", Session: session, Data: first, End: firstEnd})
	m = updated.(Model)
	if err := m.flushUsageOutputs(); err != nil {
		t.Fatal(err)
	}
	// The session emitted a second chunk after Program.Run stopped. It remains
	// unacknowledged and must be recovered by the final model.
	session.TrackOutput([]byte("shutdown tail\n"))
	m.RecoverFinalOutput()
	m.RecoverFinalOutput()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("recovered events = %+v, want first output plus one shutdown tail", events)
	}
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

func TestRecordUsageEventAdvancesAffectedReconciledRow(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		Agents: map[string]config.AgentConfig{
			"a": {Command: []string{"tool"}, Usage: config.AgentUsageConfig{Tool: "claude"}},
			"b": {Command: []string{"tool"}, Usage: config.AgentUsageConfig{Tool: "codex"}},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	m := Model{
		Config:      cfg,
		Store:       store,
		usageRunDir: store.RunDir,
		usageTally:  map[string]usage.TokenPair{},
		usageRate: map[string]usage.Rate{
			"a": {InputPerToken: 0.001, Currency: "USD", Found: true},
		},
		usageReconciled: map[string]reconciledCost{
			"a": {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
			"b": {cost: 3, currency: "USD", confidence: usage.Reported, priced: true},
		},
	}
	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 4})

	a := m.usageReconciled["a"]
	if a.cost < 1.0039 || a.cost > 1.0041 || a.confidence != usage.Estimated {
		t.Fatalf("advanced row = %+v, want prior $1 plus ~$0.004 delta", a)
	}
	if _, ok := m.usageReconciled["b"]; !ok {
		t.Fatal("another agent's reconciled row should remain valid")
	}
	if got := m.usageBorderSuffix("a"); got != " | ~$1.00" {
		t.Fatalf("badge = %q, want cumulative live estimate", got)
	}
}

func TestUsageRunHydratesHistoryBeforeLiveDelta(t *testing.T) {
	runDir := t.TempDir()
	if err := usage.Append(runDir, usage.Event{
		Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated,
		Model: "custom", PriceProfile: "local", InputTokens: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	store, err := runstore.OpenAt(runDir, "resume")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Usage: config.UsageConfig{
			Enabled: true,
			Prices: map[string]config.PriceProfile{
				"local": {InputPerMillion: 1, Currency: "USD", Source: "test"},
			},
		},
		Agents: map[string]config.AgentConfig{
			"a": {Usage: config.AgentUsageConfig{Model: "custom", PriceProfile: "local"}},
		},
	}
	cfg.Normalize()
	m := NewModelWithConfig(nil, store, cfg, "", nil, 0, nil, nil)
	before := m.usageReconciled["a"]
	if !before.priced || before.cost < 0.0009 || before.cost > 0.0011 {
		t.Fatalf("hydrated row = %+v, want historical $0.001", before)
	}

	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 1000})
	after := m.usageReconciled["a"]
	if after.cost < 0.0019 || after.cost > 0.0021 {
		t.Fatalf("advanced row = %+v, want historical + live $0.002", after)
	}
}

func TestPaneReplacementFlushesEachSessionOutput(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	first := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{first}, store, cfg, "", nil, 0, nil, nil)
	m.appendOutput(m.Agents[0], "first session output\n")
	m.flushUsageOutputs()
	first.TrackOutput([]byte("queued before replacement\n"))
	if err := m.flushUsageOutputs(); err != nil {
		t.Fatal(err)
	}

	second := agent.NewSession("a", cfg.Agents["a"], "")
	m.replaceAgentsWithTranscripts([]*agent.Session{second}, nil)
	m.appendOutput(m.Agents[0], "second session output\n")
	m.flushUsageOutputs()
	defer second.Terminate()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	var outputs int
	for _, event := range events {
		if event.Source == usage.SourceTranscript {
			outputs++
		}
	}
	if outputs != 3 {
		t.Fatalf("transcript events = %d, want live + queued old output and new output: %+v", outputs, events)
	}
}

func TestUsageOutputSurvivesScrollbackTrimming(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		UI:    config.UIConfig{MaxScrollbackLines: 1},
		Agents: map[string]config.AgentConfig{
			"a": {Command: []string{"tool"}},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()

	m.appendOutput(m.Agents[0], "first output line that will be trimmed\n")
	m.flushUsageOutputs()
	m.appendOutput(m.Agents[0], "second output line replacing the first\n")
	m.flushUsageOutputs()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	var outputs int
	for _, event := range events {
		if event.Source == usage.SourceTranscript {
			outputs++
		}
	}
	if outputs != 2 {
		t.Fatalf("transcript events = %d, want both sides of a scrollback trim: %+v", outputs, events)
	}
}

func TestUsageWriteFailureKeepsOutputRetryable(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()
	m.appendOutput(m.Agents[0], "retryable output after a ledger failure\n")

	eventsPath := filepath.Join(store.RunDir, "usage", "events.jsonl")
	if err := os.MkdirAll(eventsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	m.flushUsageOutputs()
	if got := m.usageOutputSeen[session]; got != 0 {
		t.Fatalf("failed write advanced watermark to %d", got)
	}
	if got := m.usageTally["a"].Output; got != 0 {
		t.Fatalf("failed write advanced live tally to %d", got)
	}
	if err := os.Remove(eventsPath); err != nil {
		t.Fatal(err)
	}
	m.flushUsageOutputs()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Source != usage.SourceTranscript {
		t.Fatalf("retry events = %+v, want one transcript event", events)
	}
}

func TestLoadedTranscriptHistoryIsNotBilledAsLiveOutput(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()

	m.LoadTranscripts(map[string]string{"a": "persisted history from an earlier process"})
	m.flushUsageOutputs()

	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("loaded transcript history was billed as new output: %+v", events)
	}
}

func TestStaleCostResultDoesNotReplaceLiveEstimate(t *testing.T) {
	cfg := config.Config{Usage: usageConfigOn()}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	m := Model{Config: cfg, Store: store, usageRunDir: store.RunDir}
	requestRevision := m.usageRevision
	m.recordUsageEvent(usage.Event{Agent: "a", Source: usage.SourcePrompt, Confidence: usage.Estimated, InputTokens: 4})
	if m.usageRevision != requestRevision+1 {
		t.Fatalf("usage revision = %d, want %d after local event", m.usageRevision, requestRevision+1)
	}
	updated, _ := m.update(costViewMsg{
		stamp:    "run",
		runDir:   m.Store.RunDir,
		request:  m.usageCostRequest,
		body:     "snapshot",
		revision: requestRevision,
		rollup: map[string]reconciledCost{
			"a": {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
		},
	})
	if got := updated.(Model).usageReconciled; len(got) != 0 {
		t.Fatalf("stale /cost result replaced newer live state: %+v", got)
	}
	if got := updated.(Model).artifactView; got != "" {
		t.Fatalf("stale /cost result opened an artifact: %q", got)
	}
}

func TestCostResultFromAnotherRunIsDiscarded(t *testing.T) {
	store, err := runstore.OpenAt(t.TempDir(), "current")
	if err != nil {
		t.Fatal(err)
	}
	m := Model{Store: store, usageRevision: 7}
	updated, _ := m.update(costViewMsg{
		stamp:    "old",
		runDir:   t.TempDir(),
		request:  m.usageCostRequest,
		body:     "old run snapshot",
		revision: 7,
		rollup: map[string]reconciledCost{
			"a": {cost: 1, currency: "USD", confidence: usage.Reported, priced: true},
		},
	})
	next := updated.(Model)
	if len(next.usageReconciled) != 0 || next.artifactView != "" {
		t.Fatalf("cross-run result leaked into current run: rollup=%+v artifact=%q", next.usageReconciled, next.artifactView)
	}
}

func TestObsoleteCostResultDoesNotClearNewRequest(t *testing.T) {
	store, err := runstore.OpenAt(t.TempDir(), "current")
	if err != nil {
		t.Fatal(err)
	}
	m := Model{
		Store:              store,
		usageRevision:      7,
		usageCostRequest:   2,
		usageReconcileBusy: true,
		Status:             "cost -- reconciling...",
	}
	updated, _ := m.update(costViewMsg{
		runDir:   store.RunDir,
		request:  1,
		revision: 7,
		body:     "obsolete snapshot",
	})
	next := updated.(Model)
	if !next.usageReconcileBusy || next.Status != "cost -- reconciling..." {
		t.Fatalf("obsolete result changed active request: busy=%v status=%q", next.usageReconcileBusy, next.Status)
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

func TestCostSnapshotFlushesPendingOutputAndStalesOnNewOutput(t *testing.T) {
	cfg := config.Config{
		Usage:  usageConfigOn(),
		Agents: map[string]config.AgentConfig{"a": {Command: []string{"tool"}}},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), nil, nil)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("a", cfg.Agents["a"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, 0, nil, nil)
	defer session.Terminate()
	queued := []byte("queued before cost\n")
	queuedEnd := session.TrackOutput(queued)

	cmd := m.cmdCost()
	if cmd == nil {
		t.Fatal("/cost did not start")
	}
	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Source != usage.SourceTranscript {
		t.Fatalf("pending output was not flushed at cost boundary: %+v", events)
	}
	// Delivery of the already-acknowledged message must not duplicate or stale
	// the snapshot.
	updated, _ := m.update(AgentOutputMsg{Name: "a", Session: session, Data: queued, End: queuedEnd})
	m = updated.(Model)
	result := cmd().(costViewMsg)
	late := []byte("arrived during cost\n")
	lateEnd := session.TrackOutput(late)
	updated, _ = m.update(AgentOutputMsg{Name: "a", Session: session, Data: late, End: lateEnd})
	afterOutput := updated.(Model)
	updated, _ = afterOutput.update(result)
	final := updated.(Model)
	if final.artifactView != "" || !strings.Contains(final.Status, "result stale") {
		t.Fatalf("new output did not stale cost result: status=%q artifact=%q", final.Status, final.artifactView)
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
