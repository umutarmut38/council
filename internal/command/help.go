package command

import (
	"fmt"
	"strings"
)

// HelpString renders `council <cmd> --help`: the synopsis, the summary, the
// optional Long paragraph, and the flag list. It reads only registry metadata,
// so per-command help, `council help`, and the generated CLI reference all share
// one source and cannot drift apart.
func HelpString(c CLI) string {
	var b strings.Builder
	if c.Summary != "" {
		fmt.Fprintf(&b, "council %s — %s\n\n", c.Name, c.Summary)
	}
	b.WriteString("Usage:\n  council ")
	b.WriteString(c.Use)
	b.WriteByte('\n')
	if c.Long != "" {
		b.WriteString("\n")
		b.WriteString(c.Long)
		b.WriteString("\n")
	}
	if flags := c.HelpFlags(); len(flags) > 0 {
		b.WriteString("\nFlags:\n")
		b.WriteString(renderFlags(flags))
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderFlags renders one aligned "  --name arg   desc" row per flag.
func renderFlags(flags []Flag) string {
	width := 0
	labels := make([]string, len(flags))
	for i, f := range flags {
		label := f.Name
		if f.Arg != "" {
			label += " " + f.Arg
		}
		labels[i] = label
		if len(label) > width {
			width = len(label)
		}
	}
	var b strings.Builder
	for i, f := range flags {
		if f.Desc == "" {
			fmt.Fprintf(&b, "  %s\n", labels[i])
			continue
		}
		fmt.Fprintf(&b, "  %-*s   %s\n", width, labels[i], f.Desc)
	}
	return b.String()
}
