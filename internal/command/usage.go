package command

import "strings"

const (
	tagline = "council — run a council of AI coding agents in live terminal panes, driven\n" +
		"through a plan → vote → build → review → adopt workflow."
	usageHeader         = "Usage:"
	orchestrationHeader = "Orchestration (each phase runs in live panes, one git worktree per agent):"

	// flagsBlock and examplesBlock orient a first run: the global flags are only
	// shown inside synopsis lines above, and the command list alone doesn't say
	// where to start. Footer points at config + docs.
	flagsBlock = "Flags:\n" +
		"  --agents claude,codex   only launch these agents (comma-separated)\n" +
		"  --no-local-config       ignore the repo-local .council.yaml"
	examplesBlock = "Examples:\n" +
		"  council config wizard   set up ~/.council.yaml interactively\n" +
		"  council doctor          check agents, git, and run dirs are ready\n" +
		"  council                 launch the interactive multiplexer\n" +
		`  council plan "<issue>"  start a plan → vote → build run`
	footer = "Config: ~/.council.yaml   Docs: https://github.com/umutarmut38/council"

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
	b.WriteString(tagline)
	b.WriteString("\n\n")
	b.WriteString(usageHeader)
	b.WriteByte('\n')
	b.WriteString(renderCLISection(GroupGeneral, generalDescCol))
	b.WriteByte('\n')
	b.WriteString(orchestrationHeader)
	b.WriteByte('\n')
	b.WriteString(renderCLISection(GroupOrchestration, orchDescCol))
	b.WriteByte('\n')
	b.WriteString(flagsBlock)
	b.WriteString("\n\n")
	b.WriteString(examplesBlock)
	b.WriteString("\n\n")
	b.WriteString(footer)
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
