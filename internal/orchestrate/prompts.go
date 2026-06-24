package orchestrate

import (
	"fmt"
	"strings"
)

// PlanPrompt asks an agent to write a markdown plan (only) to artifactPath.
func PlanPrompt(issue string, artifactPath string) string {
	return fmt.Sprintf(`You are one member of an engineering council, working independently.

ISSUE:
%s

Produce a concrete, detailed implementation plan in Markdown: the approach, the specific files/functions to change, edge cases, risks, and a test strategy. Do NOT implement the change and do NOT edit any source files yet.

Write ONLY your plan to this file path (overwrite if it exists):
%s

Do not write anything else.`, strings.TrimSpace(issue), artifactPath)
}

// VotePrompt asks for a ranked vote. It references the anonymized plan files by
// path (rather than inlining them) so the prompt stays small and reliably posts
// into an interactive agent instead of becoming a giant paste. planPaths maps a
// letter to the file the reviewer should read.
func VotePrompt(issue string, refs []PlanRef, planPaths map[string]string, artifactPath string) string {
	var b strings.Builder
	b.WriteString("You are one member of an engineering council. Read the competing plans for the issue below and vote. Judge on merit only — do not try to identify or favor any author (including yourself).\n\nISSUE:\n")
	b.WriteString(strings.TrimSpace(issue))
	b.WriteString("\n\nRead each candidate plan from its file:\n")
	for _, ref := range refs {
		fmt.Fprintf(&b, "- Plan %s: %s\n", ref.Letter, planPaths[ref.Letter])
	}
	fmt.Fprintf(&b, `
Rank the plans from best to worst, note the main correctness issues and risks, and pick the single best plan. Write your review to this file:
%s
End the file with exactly these two lines:
RANKING: <letters best to worst, e.g. B > A > C>
WINNER: <single letter>`, artifactPath)
	return b.String()
}

// ReviewPrompt asks for a ranked review of the built implementations. Like the
// vote, it references the (anonymized) diff files by path so the prompt stays
// small. diffPaths maps a letter to the diff file the reviewer should read.
func ReviewPrompt(issue string, refs []PlanRef, diffPaths map[string]string, artifactPath string) string {
	var b strings.Builder
	b.WriteString("You are one member of an engineering council reviewing competing implementations of the issue below. Each candidate is a git diff. Judge them on correctness, completeness, and quality — do not try to identify or favor any author (including yourself).\n\nISSUE:\n")
	b.WriteString(strings.TrimSpace(issue))
	b.WriteString("\n\nRead each candidate implementation from its diff file:\n")
	for _, ref := range refs {
		fmt.Fprintf(&b, "- Implementation %s: %s\n", ref.Letter, diffPaths[ref.Letter])
	}
	fmt.Fprintf(&b, `
Rank the implementations from best to worst, note the key correctness issues and risks, and pick the single best one. Write your review to this file:
%s
End the file with exactly these two lines:
RANKING: <letters best to worst, e.g. B > A > C>
WINNER: <single letter>`, artifactPath)
	return b.String()
}

// BuildPrompt asks each agent to implement the winning plan in its worktree.
func BuildPrompt(issue, planPath string) string {
	return fmt.Sprintf(`The council selected a winning plan. Read it from the file below, then implement it fully and correctly in this repository (your own isolated git worktree). Make all required code changes, keep the build/tests working, and stay within this directory.

ISSUE:
%s

WINNING PLAN (read this file):
%s`, strings.TrimSpace(issue), strings.TrimSpace(planPath))
}

// RefinePrompt asks the winning planner to absorb the reviewers' critiques and
// rewrite its plan before the build starts.
func RefinePrompt(issue, origPlanPath string, votePaths []string, artifactPath, note string) string {
	var b strings.Builder
	if len(votePaths) > 0 {
		b.WriteString("Your plan won the council vote. Before the build starts, refine it using the reviewers' critiques.\n\nISSUE:\n")
	} else {
		b.WriteString("Refine your plan before the build starts.\n\nISSUE:\n")
	}
	b.WriteString(strings.TrimSpace(issue))
	fmt.Fprintf(&b, "\n\nYOUR ORIGINAL PLAN (read this file):\n%s\n", origPlanPath)
	if len(votePaths) > 0 {
		b.WriteString("\nREVIEWER CRITIQUES (read each file):\n")
		for _, path := range votePaths {
			fmt.Fprintf(&b, "- %s\n", path)
		}
	}
	if n := strings.TrimSpace(note); n != "" {
		fmt.Fprintf(&b, "\nREQUESTED CHANGES:\n%s\n", n)
	} else if len(votePaths) == 0 {
		b.WriteString("\nThere are no reviewer critiques (single plan). Tighten and clarify the plan: sharpen the approach, surface the risks, and make the steps concrete.\n")
	}
	fmt.Fprintf(&b, `
Write the REFINED implementation plan to this file (overwrite if it exists):
%s

The refined plan must keep your approach but address the valid objections. End it with two sections: "## Risks" (the failure modes and how you mitigate them) and "## Test checklist" (concrete tests the build must pass). Do NOT implement anything yet.`, artifactPath)
	return b.String()
}

// LetterContents maps anonymized letters to plan contents, given agent->content.
func LetterContents(refs []PlanRef, byAgent map[string]string) map[string]string {
	out := map[string]string{}
	for _, ref := range refs {
		out[ref.Letter] = byAgent[ref.Agent]
	}
	return out
}
