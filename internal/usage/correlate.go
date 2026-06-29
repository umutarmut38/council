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

type estimateCandidate struct {
	event Event
}

func reconcileWith(events []Event, readerFor func(string) reader.Reader) ReconcileResult {
	type groupKey struct{ cwd, tool string }
	groups := map[groupKey][]estimateCandidate{}
	runID := ""
	var minT, maxT time.Time
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
		groups[groupKey{cwd: e.CWD, tool: e.Tool}] = append(groups[groupKey{cwd: e.CWD, tool: e.Tool}], estimateCandidate{event: e})
	}

	inWindow := func(t time.Time) bool {
		if minT.IsZero() || t.IsZero() {
			return true
		}
		return !t.Before(minT.Add(-time.Minute)) && !t.After(maxT.Add(10*time.Minute))
	}

	type repKey struct {
		agent, phase, cwd, tool, model, providerSessionID, providerCallID string
		replaces                                                          string
	}
	reported := map[repKey]*Event{}
	var notes []string
	addNote := func(s string) { notes = append(notes, s) }

	for k, candidates := range groups {
		rd := readerFor(k.tool)
		if rd == nil {
			continue
		}
		calls, err := rd.ReadForCWD(k.cwd)
		if err != nil {
			addNote(fmt.Sprintf("%s/%s: provider session reader failed: %v", k.tool, k.cwd, err))
			continue
		}
		for _, call := range calls {
			if !inWindow(call.Timestamp) {
				continue
			}
			matched, note := matchCallCandidates(call, candidates)
			if note != "" {
				addNote(note)
			}
			if len(matched) == 0 {
				continue
			}
			replaces := replacementKeys(matched)
			phase := matched[0].event.Phase
			agent := matched[0].event.Agent
			promptHash := matched[0].event.PromptHash
			if len(replaces) > 1 {
				promptHash = PromptHash(strings.Join(replaces, "\n"))
			}
			providerCallID := call.CallID
			if providerCallID == "" {
				providerCallID = call.SessionID
			}
			model := call.Model
			if model == "" {
				model = UnknownValue
			}
			ev := Event{
				RunID: runID, Agent: agent, Phase: phase, Source: SourceProvider, Confidence: Reported,
				Tool: k.tool, ToolSource: MetaSourceProvider, ToolConfidence: Reported,
				Model: model, ModelSource: MetaSourceProvider, ModelConfidence: Reported,
				PriceModel: UnknownValue, PriceSource: Unknown, PriceConfidence: Unknown,
				OutputBasis: OutputBasisProviderReported,
				CWD:         k.cwd, PromptHash: promptHash, PromptPreview: call.UserMessage,
				ProviderSessionID: call.SessionID, ProviderCallID: providerCallID, Replaces: replaces,
				InputTokens: call.InputTokens, OutputTokens: call.OutputTokens,
				ReasoningTokens: call.ReasoningTokens, CacheCreateTokens: call.CacheCreate,
				CacheReadTokens: call.CacheRead, WebSearchRequests: call.WebSearchRequests,
				FastInputTokens: call.FastInputTokens, FastOutputTokens: call.FastOutputTokens,
			}
			ev.ReconcileKey = ev.Key().String()
			rk := repKey{agent: agent, phase: phase, cwd: k.cwd, tool: k.tool, model: model, providerSessionID: call.SessionID, providerCallID: providerCallID, replaces: strings.Join(replaces, ",")}
			cur := reported[rk]
			if cur == nil {
				copy := ev
				reported[rk] = &copy
				continue
			}
			cur.InputTokens += ev.InputTokens
			cur.OutputTokens += ev.OutputTokens
			cur.ReasoningTokens += ev.ReasoningTokens
			cur.CacheCreateTokens += ev.CacheCreateTokens
			cur.CacheReadTokens += ev.CacheReadTokens
			cur.WebSearchRequests += ev.WebSearchRequests
			cur.FastInputTokens += ev.FastInputTokens
			cur.FastOutputTokens += ev.FastOutputTokens
		}
	}

	out := make([]Event, 0, len(reported))
	for _, e := range reported {
		if e.InputTokens == 0 && e.OutputTokens == 0 && e.ReasoningTokens == 0 && e.CacheCreateTokens == 0 && e.CacheReadTokens == 0 {
			continue
		}
		e.normalize()
		out = append(out, *e)
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

func replacementKeys(candidates []estimateCandidate) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		key := c.event.ReconcileKey
		if key == "" {
			key = c.event.Key().String()
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func matchCallCandidates(call reader.Call, candidates []estimateCandidate) ([]estimateCandidate, string) {
	byAgent := map[string][]estimateCandidate{}
	for _, c := range candidates {
		byAgent[c.event.Agent] = append(byAgent[c.event.Agent], c)
	}
	if len(byAgent) == 1 {
		if matched := promptMatches(call.UserMessage, candidates); len(matched) > 0 {
			return expandMatchedKeys(matched, candidates), ""
		}
		keys := replacementKeys(candidates)
		if len(keys) == 1 {
			return candidates, ""
		}
		return nil, "provider session attribution ambiguous: one agent has multiple unmatched prompts in " + call.ProjectPath
	}
	if strings.TrimSpace(call.UserMessage) == "" {
		return nil, "provider session attribution ambiguous: same tool/cwd and no provider user message"
	}
	matched := promptMatches(call.UserMessage, candidates)
	agents := map[string]bool{}
	for _, c := range matched {
		agents[c.event.Agent] = true
	}
	if len(agents) == 1 && len(matched) > 0 {
		return expandMatchedKeys(matched, candidates), ""
	}
	if len(matched) > 1 {
		return nil, "provider session attribution ambiguous: provider user message matched multiple panes"
	}
	return nil, "provider session attribution ambiguous: provider user message matched no pane"
}

func promptMatches(userMessage string, candidates []estimateCandidate) []estimateCandidate {
	var promptOut []estimateCandidate
	for _, c := range candidates {
		if promptMatch(userMessage, c.event.PromptPreview) {
			promptOut = append(promptOut, c)
		}
	}
	if len(promptOut) > 0 {
		return promptOut
	}
	var out []estimateCandidate
	for _, c := range candidates {
		if promptMatch(userMessage, c.event.Fingerprint) {
			out = append(out, c)
		}
	}
	return out
}

func expandMatchedKeys(matched, candidates []estimateCandidate) []estimateCandidate {
	keys := map[string]bool{}
	for _, c := range matched {
		keys[c.event.ReconcileKey] = true
	}
	var out []estimateCandidate
	for _, c := range candidates {
		if keys[c.event.ReconcileKey] {
			out = append(out, c)
		}
	}
	return out
}

// fpMinMatch is how many normalized characters must align for a prompt/fingerprint
// to count.
const fpMinMatch = 24

func promptMatch(userMessage, prompt string) bool {
	a, b := fpNormalize(userMessage), fpNormalize(prompt)
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < fpMinMatch {
		return false
	}
	return a[:n] == b[:n]
}

func fpNormalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

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
