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

// LetterContents maps anonymized letters to plan contents, given agent->content.
func LetterContents(refs []PlanRef, byAgent map[string]string) map[string]string {
	out := map[string]string{}
	for _, ref := range refs {
		out[ref.Letter] = byAgent[ref.Agent]
	}
	return out
}
