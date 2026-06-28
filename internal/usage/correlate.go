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
// Attribution is by (tool, working directory): council knows each agent's tool
// and cwd, and a tool's session file records the cwd it ran in. A cwd used by
// exactly one council agent is reconciled with that agent's OWN tool reader only
// — never another tool that happened to run in the same directory. A cwd shared
// by several agents is ambiguous and left at its estimate, so no pane is
// credited with another's spend. Per (tool, cwd) the calls are grouped by model,
// so a session that switched models is priced at each model's own rate.
//
// Returned events are aggregated alongside the estimates; Aggregate prefers the
// reported tier per (agent, cwd). They are not persisted, so repeated calls stay
// idempotent.
func Reconcile(events []Event) []Event {
	return reconcileWith(events, reader.For)
}

func reconcileWith(events []Event, readerFor func(string) reader.Reader) []Event {
	agentTool := map[string]string{}
	cwdAgents := map[string]map[string]bool{}
	runID := ""
	var minT, maxT time.Time
	for _, e := range events {
		if e.RunID != "" {
			runID = e.RunID
		}
		if e.Tool != "" {
			agentTool[e.Agent] = e.Tool
		}
		if e.CWD != "" {
			if cwdAgents[e.CWD] == nil {
				cwdAgents[e.CWD] = map[string]bool{}
			}
			cwdAgents[e.CWD][e.Agent] = true
		}
		if t, err := time.Parse(time.RFC3339, e.At); err == nil {
			if minT.IsZero() || t.Before(minT) {
				minT = t
			}
			if maxT.IsZero() || t.After(maxT) {
				maxT = t
			}
		}
	}

	inWindow := func(t time.Time) bool {
		if minT.IsZero() || t.IsZero() {
			return true
		}
		return !t.Before(minT.Add(-time.Minute)) && !t.After(maxT.Add(5*time.Minute))
	}

	var out []Event
	for cwd, agents := range cwdAgents {
		if len(agents) != 1 {
			continue // ambiguous shared cwd → keep estimated
		}
		var agent string
		for a := range agents {
			agent = a
		}
		rd := readerFor(agentTool[agent])
		if rd == nil {
			continue // unknown / unsupported tool → estimate stands
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
		sort.Strings(models) // deterministic output order
		for _, model := range models {
			t := byModel[model]
			if t.Input == 0 && t.Output == 0 {
				continue
			}
			out = append(out, Event{
				RunID: runID, Agent: agent, Source: SourceProvider, Confidence: Reported,
				Model: model, Tool: agentTool[agent], CWD: cwd, InputTokens: t.Input, OutputTokens: t.Output,
			})
		}
	}
	return out
}
