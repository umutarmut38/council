package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/fsperm"
)

// WriteReport assembles a human-readable report.md from whatever artifacts the
// run has produced so far. It is safe to call at any point in a run; sections
// without data are omitted.
func WriteReport(run *Run) (string, error) {
	if run == nil {
		return "", fmt.Errorf("no run")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Council run %s\n", run.Stamp)

	if issue, err := run.Issue(); err == nil {
		fmt.Fprintf(&b, "\n## Issue\n\n%s\n", strings.TrimSpace(issue))
	}

	summary, err := SummarizeRun(run.RootDir, run.Stamp)
	if err != nil {
		return "", err
	}

	if len(summary.Plans) > 0 {
		fmt.Fprintf(&b, "\n## Plans (%d)\n\n", len(summary.Plans))
		for _, name := range summary.Plans {
			fmt.Fprintf(&b, "- %s — `%s`\n", name, filepath.Join(run.PlansDir(), name+".md"))
		}
	}

	writeVoteSection(&b, run)
	writeBuildSection(&b, run, summary)
	writeReviewSection(&b, run)

	if adopted, ok := run.Adoption(); ok {
		fmt.Fprintf(&b, "\n## Adopted\n\n%s's implementation was applied to the working tree (`%s`).\n", adopted, run.BuildDiffPath(adopted))
	}

	writeTimingSection(&b, run)

	fmt.Fprintf(&b, "\n## Artifacts\n\n- Run dir: `%s`\n- Plans: `%s`\n- Votes: `%s`\n- Builds: `%s`\n", run.Dir, run.PlansDir(), run.VotesDir(), run.BuildsDir())

	path := filepath.Join(run.Dir, "report.md")
	if err := os.WriteFile(path, []byte(b.String()), fsperm.File()); err != nil {
		return "", err
	}
	return path, nil
}

func writeVoteSection(b *strings.Builder, run *Run) {
	data, err := os.ReadFile(run.ResultPath())
	if err != nil {
		return
	}
	var payload struct {
		Winner      string         `json:"winner_agent"`
		Letter      string         `json:"winner_letter"`
		Points      map[string]int `json:"points"`
		Firsts      map[string]int `json:"first_place"`
		Assignments []PlanRef      `json:"plans"`
		Ballots     []Ballot       `json:"ballots"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Winner == "" {
		return
	}
	fmt.Fprintf(b, "\n## Plan vote\n\nWinner: **%s**", payload.Winner)
	if payload.Letter != "" {
		fmt.Fprintf(b, " (Plan %s)", payload.Letter)
	}
	b.WriteString("\n")
	if len(payload.Assignments) > 0 {
		b.WriteString("\n| Plan | Agent | Points | First-place |\n|---|---|---|---|\n")
		for _, ref := range payload.Assignments {
			fmt.Fprintf(b, "| %s | %s | %d | %d |\n", ref.Letter, ref.Agent, payload.Points[ref.Letter], payload.Firsts[ref.Letter])
		}
	}
	if len(payload.Ballots) > 0 {
		b.WriteString("\nBallots:\n\n")
		for _, ballot := range payload.Ballots {
			fmt.Fprintf(b, "- %s: %s\n", ballot.Voter, strings.Join(ballot.Ranking, " > "))
		}
	}
}

func writeBuildSection(b *strings.Builder, run *Run, summary RunSummary) {
	if len(summary.Diffs) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Builds (%d candidates)\n\n| Agent | Diff | Check |\n|---|---|---|\n", len(summary.Diffs))
	for _, agent := range summary.Diffs {
		check := "—"
		if log, err := os.ReadFile(run.CheckLogPath(agent)); err == nil {
			check = "FAIL"
			if strings.Contains(string(log), "\nPASS\n") || strings.HasSuffix(strings.TrimSpace(string(log)), "PASS") {
				check = "PASS"
			}
		}
		fmt.Fprintf(b, "| %s | `%s` | %s |\n", agent, run.BuildDiffPath(agent), check)
	}
	if warnings, err := os.ReadFile(filepath.Join(run.BuildsDir(), "warnings.log")); err == nil && len(warnings) > 0 {
		fmt.Fprintf(b, "\nBest-effort warnings during checks:\n\n```\n%s```\n", string(warnings))
	}
}

func writeReviewSection(b *strings.Builder, run *Run) {
	data, err := os.ReadFile(run.BuildResultPath())
	if err != nil {
		return
	}
	var payload struct {
		Winner string         `json:"winner_agent"`
		Letter string         `json:"winner_letter"`
		Points map[string]int `json:"points"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Winner == "" {
		return
	}
	fmt.Fprintf(b, "\n## Build review\n\nWinning implementation: **%s**\n", payload.Winner)
	if len(payload.Points) > 0 {
		letters := make([]string, 0, len(payload.Points))
		for letter := range payload.Points {
			letters = append(letters, letter)
		}
		sort.Strings(letters)
		b.WriteString("\n| Implementation | Points |\n|---|---|\n")
		for _, letter := range letters {
			fmt.Fprintf(b, "| %s | %d |\n", letter, payload.Points[letter])
		}
	}
}

func writeTimingSection(b *strings.Builder, run *Run) {
	timings := run.LoadTimings()
	if len(timings) == 0 {
		return
	}
	phases := make([]string, 0, len(timings))
	for phase := range timings {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	b.WriteString("\n## Timings\n\n| Phase | Start | End | Participants | Restarts |\n|---|---|---|---|---|\n")
	for _, phase := range phases {
		t := timings[phase]
		fmt.Fprintf(b, "| %s | %s | %s | %s | %d |\n", t.Phase, dash(t.Start), dash(t.End), dash(strings.Join(t.Participants, ", ")), t.Restarts)
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
