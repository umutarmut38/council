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
