package usage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/usage/internal/reader"
)

// ReconcileResult is the provider-session reconciliation sweep.
type ReconcileResult struct {
	Events []Event
	Notes  []string
}

// Reconcile reads tool session files for the agents seen in events and returns
// reported events that can upgrade council's estimates. It is pure; use
// ReconcileAndAppend to persist new reported events idempotently.
func Reconcile(events []Event) []Event {
	return ReconcileDetailed(events).Events
}

// ReconcileDetailed is Reconcile plus ambiguity notes for UI hints/tests.
func ReconcileDetailed(events []Event) ReconcileResult {
	return reconcileWith(events, reader.For)
}

// ReconcileAndAppend persists new reported reconciliation events under runDir.
// Existing reported events are recognized by provider call + replacement keys,
// so repeated /cost or `council cost` calls are idempotent.
func ReconcileAndAppend(runDir string, events []Event) ([]Event, ReconcileResult, error) {
	return reconcileAndAppendWith(runDir, events, reader.For)
}

func reconcileAndAppendWith(runDir string, events []Event, readerFor func(string) reader.Reader) ([]Event, ReconcileResult, error) {
	result := reconcileWith(events, readerFor)
	if len(result.Events) == 0 {
		return events, result, nil
	}
	seen := map[string]bool{}
	for _, e := range events {
		if e.Source == SourceProvider {
			seen[reportedID(e)] = true
		}
	}
	var fresh []Event
	for _, e := range result.Events {
		if id := reportedID(e); !seen[id] {
			fresh = append(fresh, e)
			seen[id] = true
		}
	}
	if len(fresh) == 0 {
		return events, ReconcileResult{Notes: result.Notes}, nil
	}
	if err := Append(runDir, fresh...); err != nil {
		return events, result, err
	}
	combined := append(append([]Event(nil), events...), fresh...)
	return combined, ReconcileResult{Events: fresh, Notes: result.Notes}, nil
}

func reportedID(e Event) string {
	replaces := append([]string(nil), e.Replaces...)
	sort.Strings(replaces)
	return strings.Join([]string{e.Agent, e.Phase, e.CWD, e.Tool, e.Model, e.ProviderSessionID, e.ProviderCallID, strings.Join(replaces, ",")}, "\x00")
}

// reconGroup collects the estimate events that one provider store+cwd should be
// reconciled against, keyed by (tool, cwd). When isolated panes run in their own
// worktrees their cwds differ, so each group holds a single agent and reports
// per pane; when several same-tool panes share one cwd the group holds them all
// and reports one combined row (no per-pane guessing).
type reconGroup struct {
	tool, cwd string
	agents    map[string]bool
	keys      []string // estimate ReconcileKeys this group replaces
	phase     string
}

func reconcileWith(events []Event, readerFor func(string) reader.Reader) ReconcileResult {
	runID := ""
	var minT, maxT time.Time
	groups := map[string]*reconGroup{}
	var order []string
	for _, e := range events {
		e.normalize()
		if e.RunID != "" {
			runID = e.RunID
		}
		if e.Source == SourceProvider || e.Tool == "" || e.Tool == UnknownValue || e.CWD == "" {
			continue
		}
		var qty TokenTotals
		qty.AddEvent(e)
		if !qty.Any() {
			continue
		}
		if t, err := time.Parse(time.RFC3339, e.At); err == nil {
			if minT.IsZero() || t.Before(minT) {
				minT = t
			}
			if maxT.IsZero() || t.After(maxT) {
				maxT = t
			}
		}
		// Group by (tool, cwd): isolated panes have distinct worktree cwds and so
		// land in their own group (per-pane); same-tool panes sharing a cwd land
		// together and report one combined row.
		gk := e.Tool + "\x00" + e.CWD
		g := groups[gk]
		if g == nil {
			g = &reconGroup{tool: e.Tool, cwd: e.CWD, agents: map[string]bool{}, phase: e.Phase}
			groups[gk] = g
			order = append(order, gk)
		}
		g.agents[e.Agent] = true
		key := e.ReconcileKey
		if key == "" {
			key = e.Key().String()
		}
		g.keys = append(g.keys, key)
	}

	inWindow := func(t time.Time) bool {
		if minT.IsZero() || t.IsZero() {
			return true
		}
		return !t.Before(minT.Add(-time.Minute)) && !t.After(maxT.Add(10*time.Minute))
	}

	var out []Event
	var notes []string
	for _, gk := range order {
		g := groups[gk]
		rd := readerFor(g.tool)
		if rd == nil {
			continue
		}
		calls, err := rd.ReadForCWD(g.cwd)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s/%s: provider session reader failed: %v", g.tool, g.cwd, err))
			continue
		}
		// Sum the in-window calls per model. A cumulative group with several
		// same-tool agents is reported as one combined row (no per-pane split);
		// a single-agent group (isolated, or one agent in a cwd) keeps its name.
		perModel := map[string]*TokenTotals{}
		for _, call := range calls {
			if !inWindow(call.Timestamp) {
				continue
			}
			model := call.Model
			if model == "" {
				model = UnknownValue
			}
			t := perModel[model]
			if t == nil {
				t = &TokenTotals{}
				perModel[model] = t
			}
			t.Input += call.InputTokens
			t.Output += call.OutputTokens
			t.Reasoning += call.ReasoningTokens
			t.CacheCreate += call.CacheCreate
			t.CacheRead += call.CacheRead
			t.WebSearch += call.WebSearchRequests
			t.FastInput += call.FastInputTokens
			t.FastOutput += call.FastOutputTokens
		}
		agent := g.tool // combined label when several same-tool panes share a cwd
		if len(g.agents) == 1 {
			for a := range g.agents {
				agent = a
			}
		}
		replaces := dedupeSorted(g.keys)
		promptHash := PromptHash(strings.Join(replaces, "\n"))
		models := make([]string, 0, len(perModel))
		for m := range perModel {
			models = append(models, m)
		}
		sort.Strings(models)
		for _, model := range models {
			t := perModel[model]
			if !t.Any() {
				continue
			}
			ev := Event{
				RunID: runID, Agent: agent, Phase: g.phase, Source: SourceProvider, Confidence: Reported,
				Tool: g.tool, ToolSource: MetaSourceProvider, ToolConfidence: Reported,
				Model: model, ModelSource: MetaSourceProvider, ModelConfidence: Reported,
				PriceModel: UnknownValue, PriceSource: Unknown, PriceConfidence: Unknown,
				OutputBasis: OutputBasisProviderReported,
				CWD:         g.cwd, PromptHash: promptHash, Replaces: replaces,
				InputTokens: t.Input, OutputTokens: t.Output, ReasoningTokens: t.Reasoning,
				CacheCreateTokens: t.CacheCreate, CacheReadTokens: t.CacheRead,
				WebSearchRequests: t.WebSearch, FastInputTokens: t.FastInput, FastOutputTokens: t.FastOutput,
			}
			ev.ReconcileKey = ev.Key().String()
			ev.normalize()
			out = append(out, ev)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		if out[i].Phase != out[j].Phase {
			return out[i].Phase < out[j].Phase
		}
		return out[i].Model < out[j].Model
	})
	sort.Strings(notes)
	return ReconcileResult{Events: out, Notes: compactStrings(notes)}
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func compactStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
