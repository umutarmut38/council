package command

import (
	"fmt"
	"strings"
)

// CLIReferenceRegions renders the CLI command reference as aligned synopsis
// blocks, one per group, keyed by the doc region id they fill in
// docs/commands.md. Generating these from the registry keeps the shell
// reference from drifting from `council help`.
func CLIReferenceRegions() map[string]string {
	return map[string]string{
		"cli-general":       cliSynopsisBlock(GroupGeneral),
		"cli-orchestration": cliSynopsisBlock(GroupOrchestration),
	}
}

// CLIDocsRegions renders the full per-command CLI reference for docs/cli.md, one
// region per group. Unlike CLIReferenceRegions (a one-line synopsis table in
// docs/commands.md), this expands each command with its summary, verbs, and
// flags, mirroring `council <cmd> --help` so the page and the tool never drift.
func CLIDocsRegions() map[string]string {
	return map[string]string{
		"cli-ref-general":       cliDocsBlock(GroupGeneral),
		"cli-ref-orchestration": cliDocsBlock(GroupOrchestration),
	}
}

// cliDocsBlock renders one group as a sequence of `### council <name>` sections,
// each with the synopsis, the Long paragraph (verb-style commands list their
// verbs), and a bullet list of flags.
func cliDocsBlock(g Group) string {
	var b strings.Builder
	for _, c := range cliCommands {
		if c.Group != g {
			continue
		}
		fmt.Fprintf(&b, "### council %s\n\n", c.Name)
		if c.Summary != "" {
			fmt.Fprintf(&b, "%s\n\n", c.Summary)
		}
		fmt.Fprintf(&b, "```text\ncouncil %s\n```\n", c.Use)
		if c.Long != "" {
			// Multi-line Long (the verb listings) keeps its alignment inside a
			// fence; a one-line Long renders as prose.
			if strings.Contains(c.Long, "\n") {
				fmt.Fprintf(&b, "\n```text\n%s\n```\n", c.Long)
			} else {
				fmt.Fprintf(&b, "\n%s\n", c.Long)
			}
		}
		if flags := c.HelpFlags(); len(flags) > 0 {
			b.WriteString("\n")
			for _, f := range flags {
				label := f.Name
				if f.Arg != "" {
					label += " " + f.Arg
				}
				if f.Desc != "" {
					fmt.Fprintf(&b, "- `%s` — %s\n", label, f.Desc)
				} else {
					fmt.Fprintf(&b, "- `%s`\n", label)
				}
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// cliSynopsisBlock renders one group as a fenced, column-aligned synopsis list:
//
//	council <use>   <summary>
func cliSynopsisBlock(g Group) string {
	type row struct{ left, desc string }
	var rows []row
	width := 0
	for _, c := range cliCommands {
		if c.Group != g {
			continue
		}
		left := "council " + c.Use
		if len(left) > width {
			width = len(left)
		}
		rows = append(rows, row{left, c.Summary})
	}

	var b strings.Builder
	b.WriteString("```text\n")
	for _, r := range rows {
		if r.desc == "" {
			b.WriteString(r.left + "\n")
			continue
		}
		b.WriteString(fmt.Sprintf("%-*s   %s\n", width, r.left, r.desc))
	}
	b.WriteString("```")
	return b.String()
}
