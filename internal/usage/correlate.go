package usage

import (
	"sort"
	"time"

	"github.com/umutarmut38/council/internal/usage/internal/reader"
)

// DiscoverModel reads the model a tool most recently used in cwd, for pricing an
// agent whose usage.model isn't pinned. "" when the tool has no reader or no
// session yet.
func DiscoverModel(tool, cwd string) string {
	rd := reader.For(tool)
	if rd == nil {
		return ""
	}
	m, _ := rd.LatestModel(cwd)
	return m
}

// Reconcile reads tool session files for the agents seen in events and returns
// new `reported` events that upgrade council's estimate with real token counts.
//
// Attribution is by (tool, working directory). A tool's session file records the
// cwd it ran in, read only by that tool's OWN reader — so a claude session and a
// codex session in the same directory don't collide. A (tool, cwd) used by
// exactly one council agent is unambiguous: every session that tool ran there is
// that agent's, grouped by model so a session that switched models prices at
// each rate.
//
// When SEVERAL agents of the same tool share one cwd (e.g. a claude planner and
// a claude voter both in the repo root), their session files overlap — council
// launches all panes together, so the sessions start at the same time and can't
// be told apart by cwd or timing. Those agents are left at their estimate rather
// than risk charging one pane for another's spend. Giving such agents distinct
// working directories (the build phase already uses per-agent worktrees) makes
// them reconcile.
//
// Returned events are aggregated alongside the estimates; Aggregate prefers the
// reported tier per (agent, cwd). They are not persisted, so repeated calls stay
// idempotent.
func Reconcile(events []Event) []Event {
	return reconcileWith(events, reader.For)
}

func reconcileWith(events []Event, readerFor func(string) reader.Reader) []Event {
	agentTool := map[string]string{}
	// cwd -> tool -> set of agents that used it
	cwdToolAgents := map[string]map[string]map[string]bool{}
	runID := ""
	var minT, maxT time.Time
	for _, e := range events {
		if e.RunID != "" {
			runID = e.RunID
		}
		if e.Tool != "" {
			agentTool[e.Agent] = e.Tool
		}
		if t, err := time.Parse(time.RFC3339, e.At); err == nil {
			if minT.IsZero() || t.Before(minT) {
				minT = t
			}
			if maxT.IsZero() || t.After(maxT) {
				maxT = t
			}
		}
		if e.CWD == "" || e.Tool == "" {
			continue
		}
		if cwdToolAgents[e.CWD] == nil {
			cwdToolAgents[e.CWD] = map[string]map[string]bool{}
		}
		if cwdToolAgents[e.CWD][e.Tool] == nil {
			cwdToolAgents[e.CWD][e.Tool] = map[string]bool{}
		}
		cwdToolAgents[e.CWD][e.Tool][e.Agent] = true
	}

	inWindow := func(t time.Time) bool {
		if minT.IsZero() || t.IsZero() {
			return true
		}
		return !t.Before(minT.Add(-time.Minute)) && !t.After(maxT.Add(5*time.Minute))
	}

	var out []Event
	for cwd, tools := range cwdToolAgents {
		for tool, agents := range tools {
			if len(agents) != 1 {
				continue // several same-tool panes share this cwd → can't attribute
			}
			var agent string
			for a := range agents {
				agent = a
			}
			rd := readerFor(tool)
			if rd == nil {
				continue // unsupported tool → estimate stands
			}
			calls, err := rd.ReadForCWD(cwd)
			if err != nil {
				continue
			}
			byModel := map[string]*TokenPair{}
			for _, c := range calls {
				if !inWindow(c.Timestamp) {
					continue
				}
				t := byModel[c.Model]
				if t == nil {
					t = &TokenPair{}
					byModel[c.Model] = t
				}
				t.Input += c.InputTokens
				t.Output += c.OutputTokens
			}
			models := make([]string, 0, len(byModel))
			for m := range byModel {
				models = append(models, m)
			}
			sort.Strings(models)
			for _, model := range models {
				t := byModel[model]
				if t.Input == 0 && t.Output == 0 {
					continue
				}
				out = append(out, Event{
					RunID: runID, Agent: agent, Source: SourceProvider, Confidence: Reported,
					Model: model, Tool: tool, CWD: cwd, InputTokens: t.Input, OutputTokens: t.Output,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		return out[i].Model < out[j].Model
	})
	return out
}
