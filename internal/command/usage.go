package command

import "strings"

const (
	usageHeader         = "Usage:"
	orchestrationHeader = "Orchestration (each phase runs in live panes, one git worktree per agent):"

	// Description columns chosen to match the historical help layout. Synopsis
	// lines and entries wider than the column fall back to a two-space gap.
	generalDescCol = 38
	orchDescCol    = 33
	minDescGap     = 2
)

// UsageString renders the `council help` text from the CLI registry. It is the
// single source for the shell usage so help can never drift from the
// dispatcher or the docs.
func UsageString() string {
	var b strings.Builder
	b.WriteString(usageHeader)
	b.WriteByte('\n')
	b.WriteString(renderCLISection(GroupGeneral, generalDescCol))
	b.WriteByte('\n')
	b.WriteString(orchestrationHeader)
	b.WriteByte('\n')
	b.WriteString(renderCLISection(GroupOrchestration, orchDescCol))
	return strings.TrimRight(b.String(), "\n")
}

func renderCLISection(group Group, descCol int) string {
	var b strings.Builder
	for _, c := range cliCommands {
		if c.Group != group {
			continue
		}
		b.WriteString(cliHelpLine(c, descCol))
		b.WriteByte('\n')
	}
	return b.String()
}

// cliHelpLine renders one help row: the synopsis, padded so the description
// starts at descCol (or after a two-space gap when the synopsis is longer).
func cliHelpLine(c CLI, descCol int) string {
	prefix := "  council " + c.Use
	if c.SynopsisOnly || c.Summary == "" {
		return prefix
	}
	gap := descCol - len(prefix)
	if gap < minDescGap {
		gap = minDescGap
	}
	return prefix + strings.Repeat(" ", gap) + c.Summary
}
