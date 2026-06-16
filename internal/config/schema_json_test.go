package config

import (
	"encoding/json"
	"testing"
)

func TestSchemaJSONIsDraft202012(t *testing.T) {
	doc := SchemaJSON()
	if got := doc["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("$schema = %v, want the draft 2020-12 URI", got)
	}
	if doc["type"] != "object" {
		t.Fatalf("root type = %v, want object", doc["type"])
	}
}

func TestSchemaJSONNestsAgentTerminal(t *testing.T) {
	doc := SchemaJSON()
	props := doc["properties"].(map[string]any)

	agents, ok := props["agents"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents property: %v", props["agents"])
	}
	agent, ok := agents["additionalProperties"].(map[string]any)
	if !ok {
		t.Fatalf("agents.additionalProperties is not an object schema: %v", agents["additionalProperties"])
	}
	agentProps := agent["properties"].(map[string]any)
	terminal, ok := agentProps["terminal"].(map[string]any)
	if !ok {
		t.Fatalf("agent schema missing nested terminal object: %v", agentProps["terminal"])
	}
	termProps, ok := terminal["properties"].(map[string]any)
	if !ok || termProps["renderer"] == nil {
		t.Fatalf("terminal schema missing properties.renderer: %v", terminal)
	}
	// The enabled flag should be typed as a boolean from the "bool" field type.
	enabled, ok := agentProps["enabled"].(map[string]any)
	if !ok || enabled["type"] != "boolean" {
		t.Fatalf("agent.enabled should be a boolean, got %v", agentProps["enabled"])
	}
}

func TestSchemaJSONConstrainsPolicyMode(t *testing.T) {
	doc := SchemaJSON()
	props := doc["properties"].(map[string]any)
	policy := props["policy"].(map[string]any)
	policyProps := policy["properties"].(map[string]any)
	mode := policyProps["mode"].(map[string]any)
	enum, ok := mode["enum"].([]any)
	if !ok || len(enum) != 3 {
		t.Fatalf("policy.mode.enum = %v, want [safe normal aggressive]", mode["enum"])
	}
	want := map[string]bool{PolicySafe: true, PolicyNormal: true, PolicyAggressive: true}
	for _, v := range enum {
		delete(want, v.(string))
	}
	if len(want) != 0 {
		t.Fatalf("policy.mode.enum missing %v", want)
	}
}

func TestSchemaJSONStringIsDeterministicValidJSON(t *testing.T) {
	first := SchemaJSONString()
	second := SchemaJSONString()
	if first != second {
		t.Fatal("SchemaJSONString should be deterministic across calls")
	}
	var into any
	if err := json.Unmarshal([]byte(first), &into); err != nil {
		t.Fatalf("SchemaJSONString is not valid JSON: %v", err)
	}
}

// TestSchemaJSONCoversTopLevelKeys keeps the JSON schema in lockstep with the
// documented sections: every top-level config key the Markdown schema knows
// about must appear as a JSON property.
func TestSchemaJSONCoversTopLevelKeys(t *testing.T) {
	doc := SchemaJSON()
	props := doc["properties"].(map[string]any)
	for _, key := range []string{
		"agents", "ui", "env", "setup", "experimental", "sessions",
		"review", "files", "policy", "personality_categories", "personalities",
	} {
		if props[key] == nil {
			t.Errorf("JSON schema missing top-level property %q", key)
		}
	}
}
