package orchestrate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// PlanRef ties an anonymized ballot letter to the agent that authored the plan.
type PlanRef struct {
	Letter string
	Agent  string
	Path   string
}

// AnonymizePlans assigns A, B, C... to the agents in order, so voters rank
// "Plan A/B/C" without knowing (or favoring) the author.
func AnonymizePlans(agents []string, pathFor func(agent string) string) []PlanRef {
	refs := make([]PlanRef, 0, len(agents))
	for i, agent := range agents {
		refs = append(refs, PlanRef{Letter: letter(i), Agent: agent, Path: pathFor(agent)})
	}
	return refs
}

func letter(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return fmt.Sprintf("A%d", i) // overflow fallback; we never expect >26 agents
}

// Ballot is one agent's vote, parsed from its votes/<agent>.md file.
type Ballot struct {
	Voter   string
	Ranking []string // letters best -> worst
	Winner  string   // explicit winner letter, if any
}

// Result is the tallied outcome.
type Result struct {
	Points       map[string]int
	Firsts       map[string]int
	WinnerLetter string
	WinnerAgent  string
	Ballots      []Ballot
}

var (
	winnerPattern  = regexp.MustCompile(`(?i)winner\s*[:=]\s*([A-Za-z])`)
	rankingPattern = regexp.MustCompile(`(?i)ranking\s*[:=]\s*([A-Za-z][A-Za-z0-9 ,>\-]*)`)
	letterToken    = regexp.MustCompile(`[A-Za-z]`)
)

// ParseBallot extracts a ranking and/or winner from free-form vote text. Only
// letters present in valid are kept. If no ranking is given but a winner is,
// the ranking is just that winner.
func ParseBallot(voter, text string, valid []string) Ballot {
	allowed := map[string]bool{}
	for _, l := range valid {
		allowed[strings.ToUpper(l)] = true
	}

	b := Ballot{Voter: voter}
	if m := winnerPattern.FindStringSubmatch(text); m != nil {
		if up := strings.ToUpper(m[1]); allowed[up] {
			b.Winner = up
		}
	}
	if m := rankingPattern.FindStringSubmatch(text); m != nil {
		seen := map[string]bool{}
		for _, tok := range letterToken.FindAllString(m[1], -1) {
			up := strings.ToUpper(tok)
			if allowed[up] && !seen[up] {
				seen[up] = true
				b.Ranking = append(b.Ranking, up)
			}
		}
	}
	if len(b.Ranking) == 0 && b.Winner != "" {
		b.Ranking = []string{b.Winner}
	}
	if b.Winner == "" && len(b.Ranking) > 0 {
		b.Winner = b.Ranking[0]
	}
	return b
}

// Tally combines ballots with a Borda count: a candidate gets (n-1) points for
// each first place, down to 0 for last. Ties break by first-place count, then
// by letter, so the result is deterministic.
func Tally(ballots []Ballot, refs []PlanRef) Result {
	n := len(refs)
	res := Result{Points: map[string]int{}, Firsts: map[string]int{}, Ballots: ballots}
	for _, r := range refs {
		res.Points[r.Letter] = 0
		res.Firsts[r.Letter] = 0
	}

	for _, b := range ballots {
		for pos, l := range b.Ranking {
			res.Points[l] += n - 1 - pos
			if pos == 0 {
				res.Firsts[l]++
			}
		}
	}

	letters := make([]string, 0, n)
	for _, r := range refs {
		letters = append(letters, r.Letter)
	}
	sort.Slice(letters, func(i, j int) bool {
		a, b := letters[i], letters[j]
		if res.Points[a] != res.Points[b] {
			return res.Points[a] > res.Points[b]
		}
		if res.Firsts[a] != res.Firsts[b] {
			return res.Firsts[a] > res.Firsts[b]
		}
		return a < b
	})

	if len(letters) > 0 {
		res.WinnerLetter = letters[0]
		for _, r := range refs {
			if r.Letter == res.WinnerLetter {
				res.WinnerAgent = r.Agent
			}
		}
	}
	return res
}
