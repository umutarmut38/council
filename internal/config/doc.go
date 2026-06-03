// Package config defines council's YAML schema and normalization rules.
//
// It keeps runtime behavior configurable: agent commands, terminal delivery
// quirks, worker/reviewer roles, behavioral personalities, per-repo overrides,
// UI layout, and review checks are all represented here before the TUI or
// orchestration layers consume them.
package config
