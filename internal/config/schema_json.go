package config

import (
	"encoding/json"
	"fmt"
)

// SchemaJSON builds a JSON Schema (draft 2020-12) describing ~/.council.yaml and
// repo-local .council.yaml files. It is derived from the same Schema() sections
// that power `council config schema` and the generated Markdown tables, so the
// machine-readable schema cannot drift from the documented one. The result is a
// plain map so callers can marshal it (json.Marshal sorts keys, keeping the
// output deterministic).
func SchemaJSON() map[string]any {
	sec := sectionByTitle()

	terminal := objectSchema(sec["agents.<name>.terminal"], nil)
	orchestration := objectSchema(sec["agents.<name>.orchestration"], nil)
	agent := objectSchema(sec["agents.<name>"], map[string]map[string]any{
		"terminal":      terminal,
		"orchestration": orchestration,
	})

	policy := objectSchema(sec["policy"], nil)
	// Constrain policy.mode to the documented risk postures.
	if props, ok := policy["properties"].(map[string]any); ok {
		if mode, ok := props["mode"].(map[string]any); ok {
			mode["enum"] = []any{PolicySafe, PolicyNormal, PolicyAggressive}
		}
	}

	// The env/setup/experimental keys live under one documentation section but
	// are three distinct top-level keys in the file.
	expSec := sec["env, setup, experimental"]
	experimental := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"setup_env": withDescription(
				map[string]any{"type": "boolean"},
				fieldDesc(expSec, "experimental.setup_env"),
			),
		},
		"additionalProperties": false,
	}

	properties := map[string]any{
		"agents": map[string]any{
			"type":                 "object",
			"description":          sec["agents.<name>"].Intro,
			"additionalProperties": agent,
		},
		"ui": objectSchema(sec["ui"], nil),
		"env": withDescription(
			map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			fieldDesc(expSec, "env"),
		),
		"setup": withDescription(
			map[string]any{"type": "array", "items": objectSchema(sec["setup[]"], nil)},
			fieldDesc(expSec, "setup"),
		),
		"experimental": experimental,
		"sessions":     objectSchema(sec["sessions"], nil),
		"review":       objectSchema(sec["review"], nil),
		"files":        objectSchema(sec["files"], nil),
		"policy":       policy,
		"personality_categories": map[string]any{
			"type":                 "object",
			"additionalProperties": objectSchema(sec["personality_categories.<name>"], nil),
		},
		"personalities": map[string]any{
			"type":                 "object",
			"additionalProperties": objectSchema(sec["personalities.<name>"], nil),
		},
	}

	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://github.com/umutarmut38/council/council.schema.json",
		"title":                "council configuration",
		"description":          "Schema for council's global ~/.council.yaml and repo-local .council.yaml files.",
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
}

// SchemaJSONString renders SchemaJSON as indented JSON. It is what
// `council config schema --json` prints.
func SchemaJSONString() string {
	data, err := json.MarshalIndent(SchemaJSON(), "", "  ")
	if err != nil {
		// SchemaJSON only contains JSON-native types, so marshaling cannot fail
		// in practice; surface the error rather than panic if that ever changes.
		return fmt.Sprintf("{\n  \"error\": %q\n}", err.Error())
	}
	return string(data)
}

func sectionByTitle() map[string]SchemaSection {
	m := make(map[string]SchemaSection, len(Schema()))
	for _, s := range Schema() {
		m[s.Title] = s
	}
	return m
}

// objectSchema renders one SchemaSection as a closed JSON Schema object.
// overrides replaces the generated property schema for the named keys, used to
// nest the terminal/orchestration objects under an agent.
func objectSchema(sec SchemaSection, overrides map[string]map[string]any) map[string]any {
	props := make(map[string]any, len(sec.Fields))
	for _, f := range sec.Fields {
		schema, ok := overrides[f.Key]
		if !ok {
			schema = fieldType(f.Type)
		}
		if f.Description != "" {
			schema = withDescription(schema, f.Description)
		}
		props[f.Key] = schema
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
}

// fieldType maps a SchemaField type token to a JSON Schema type fragment.
func fieldType(t string) map[string]any {
	switch t {
	case "bool":
		return map[string]any{"type": "boolean"}
	case "int":
		return map[string]any{"type": "integer"}
	case "string":
		return map[string]any{"type": "string"}
	case "list":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "map":
		return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	default: // "object" and anything unrecognized: an unconstrained object.
		return map[string]any{"type": "object"}
	}
}

// withDescription returns a shallow copy of schema with description set, so the
// shared override fragments are never mutated.
func withDescription(schema map[string]any, desc string) map[string]any {
	out := make(map[string]any, len(schema)+1)
	for k, v := range schema {
		out[k] = v
	}
	out["description"] = desc
	return out
}

func fieldDesc(sec SchemaSection, key string) string {
	for _, f := range sec.Fields {
		if f.Key == key {
			return f.Description
		}
	}
	return ""
}
