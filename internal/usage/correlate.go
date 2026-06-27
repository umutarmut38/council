package usage

import (
	"time"

	"github.com/umutarmut38/council/internal/provider"
)

// Reconcile reads tool session files for the agents seen in events and returns
// new `reported` events that upgrade council's estimate with real token counts.
//
// Attribution is by working directory: council knows each agent's cwd, and a
// tool's session file records the cwd it ran in. When exactly one council agent
// used a cwd, every tool call there is unambiguously that agent's. When several
// agents shared a cwd (e.g. two of the same CLI planning in the repo root), the
// match is ambiguous, so those agents are left at their estimate rather than
// risk crediting one pane with another's spend. Build-phase agents each get
// their own worktree, so they always reconcile cleanly.
//
// Returned events are meant to be aggregated alongside the estimates; Aggregate
// then prefers the reported tier per agent. They are not persisted, so repeated
// calls stay idempotent.
func Reconcile(events []Event, readers []provider.Reader) []Event {
	agentCWD := map[string]string{}
	runID := ""
	var minT, maxT time.Time
	for _, e := range events {
		if e.RunID != "" {
			runID = e.RunID
		}
		if e.CWD != "" {
			agentCWD[e.Agent] = e.CWD
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

	cwdAgents := map[string][]string{}
	for agent, cwd := range agentCWD {
		cwdAgents[cwd] = append(cwdAgents[cwd], agent)
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
		agent := agents[0]
		for _, rd := range readers {
			calls, err := rd.ReadForCWD(cwd)
			if err != nil {
				continue
			}
			var in, outTok int
			var model string
			matched := false
			for _, c := range calls {
				if !inWindow(c.Timestamp) {
					continue
				}
				in += c.InputTokens
				outTok += c.OutputTokens
				if c.Model != "" {
					model = c.Model
				}
				matched = true
			}
			if matched {
				out = append(out, Event{
					RunID: runID, Agent: agent, Source: SourceProvider, Confidence: Reported,
					Model: model, CWD: cwd, InputTokens: in, OutputTokens: outTok,
				})
				break
			}
		}
	}
	return out
}
