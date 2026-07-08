package main

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/usage"
)

func TestFormatModelsList(t *testing.T) {
	models := []usage.ListedModel{
		{Name: "claude-haiku-4-5", InputPerToken: 1e-6, OutputPerToken: 5e-6, CacheCreatePerToken: 1.25e-6, CacheReadPerToken: 1e-7},
		{Name: "gpt-5.4-mini", InputPerToken: 1.5e-7, OutputPerToken: 6e-7},
	}
	builtin := map[string]string{"claude-4.5-haiku": "claude-haiku-4-5", "gpt-5-mini": "gpt-5.4-mini"}
	user := map[string]string{"claude-haiku-4.5": "claude-haiku-4-5"}

	// Filter on "haiku" hits the model, the built-in alias, and the user alias.
	out := formatModelsList(models, builtin, user, "haiku")
	if !strings.Contains(out, "claude-haiku-4-5") {
		t.Fatalf("filtered list missing the model row:\n%s", out)
	}
	if !strings.Contains(out, "1.00") || !strings.Contains(out, "5.00") {
		t.Fatalf("per-MTok rates not rendered:\n%s", out)
	}
	if !strings.Contains(out, "claude-haiku-4.5 -> claude-haiku-4-5  (user)") {
		t.Fatalf("user alias should be listed and marked (user):\n%s", out)
	}
	if !strings.Contains(out, "claude-4.5-haiku -> claude-haiku-4-5") {
		t.Fatalf("matching built-in alias should be listed:\n%s", out)
	}
	// The unrelated gpt model and its alias must be filtered out.
	if strings.Contains(out, "gpt-5.4-mini") || strings.Contains(out, "gpt-5-mini ->") {
		t.Fatalf("filter leaked non-matching rows:\n%s", out)
	}

	// Zero cache rates render as "--", not 0.00.
	full := formatModelsList(models, nil, nil, "gpt-5.4-mini")
	if !strings.Contains(full, "gpt-5.4-mini") || !strings.Contains(full, "--") {
		t.Fatalf("zero cache rate should show --:\n%s", full)
	}

	// A miss reports it and lists no aliases.
	miss := formatModelsList(models, builtin, user, "no-such-model")
	if !strings.Contains(miss, "no models match") || strings.Contains(miss, "Aliases:") {
		t.Fatalf("miss output wrong:\n%s", miss)
	}
}
