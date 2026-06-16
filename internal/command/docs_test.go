package command

import (
	"strings"
	"testing"
)

func TestCLIReferenceRegions(t *testing.T) {
	regions := CLIReferenceRegions()
	gen, ok := regions["cli-general"]
	if !ok {
		t.Fatal("missing cli-general region")
	}
	orch, ok := regions["cli-orchestration"]
	if !ok {
		t.Fatal("missing cli-orchestration region")
	}

	for _, block := range []string{gen, orch} {
		if !strings.HasPrefix(block, "```text\n") || !strings.HasSuffix(block, "```") {
			t.Fatalf("block is not a fenced text block:\n%s", block)
		}
	}

	for _, c := range CLIs() {
		line := "council " + c.Use
		block := gen
		if c.Group == GroupOrchestration {
			block = orch
		}
		if !strings.Contains(block, line) {
			t.Errorf("command %q (%q) missing from its group block", c.Name, line)
		}
		if c.Summary != "" && !strings.Contains(block, c.Summary) {
			t.Errorf("command %q summary missing from its block", c.Name)
		}
	}

	if strings.Contains(gen, "council plan ") {
		t.Error("orchestration command leaked into the general block")
	}
}
