package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/usage"
)

// UsageConfig configures council's local cost/usage ledger. Off by default:
// when disabled, council records nothing and shows no cost UI.
type UsageConfig struct {
	// Enabled turns on the ledger, the header/border cost, and the /cost view.
	Enabled bool `yaml:"enabled,omitempty"`
	// Estimator names the local token estimator: "bytes4" or "runes4".
	Estimator string `yaml:"estimator,omitempty"`
	// ShowTotalInHeader shows a compact run total in the top status line.
	// Tri-state: unset (nil) defaults to on when usage is enabled.
	ShowTotalInHeader *bool `yaml:"show_total_in_header,omitempty"`
	// ShowAgentCostInBorder shows each session's cost in its pane border.
	// Tri-state: unset (nil) defaults to on when usage is enabled.
	ShowAgentCostInBorder *bool `yaml:"show_agent_cost_in_border,omitempty"`
	// StalePriceAfterDays warns when a user price profile's reviewed_at is older
	// than this many days (default 60).
	StalePriceAfterDays int `yaml:"stale_price_after_days,omitempty"`
	// ModelAliases maps a model name council sees to one the price tables know.
	ModelAliases map[string]string `yaml:"model_aliases,omitempty"`
	// Prices are user-reviewed price profiles, keyed by profile name.
	Prices map[string]PriceProfile `yaml:"prices,omitempty"`
}

// PriceProfile is a user-configured price, in per-million-token units. Cache
// rates left unset derive from the input rate (write 1.25x, read 0.1x) so
// cache-heavy sessions are never silently priced at $0.
type PriceProfile struct {
	InputPerMillion      float64 `yaml:"input_per_million"`
	OutputPerMillion     float64 `yaml:"output_per_million"`
	CacheWritePerMillion float64 `yaml:"cache_write_per_million,omitempty"`
	CacheReadPerMillion  float64 `yaml:"cache_read_per_million,omitempty"`
	Currency             string  `yaml:"currency,omitempty"`
	Source               string  `yaml:"source,omitempty"`
	ReviewedAt           string  `yaml:"reviewed_at,omitempty"`
}

// AgentUsageConfig is the per-agent cost binding.
type AgentUsageConfig struct {
	// Model is the configured observed model name used for price lookup.
	Model string `yaml:"model,omitempty"`
	// PriceProfile selects a usage.prices entry; when set it wins over the tables.
	PriceProfile string `yaml:"price_profile,omitempty"`
	// Tool selects which native provider-session reader may reconcile this agent.
	// AgentConfig.Command is opaque and is never inspected for tool detection.
	Tool string `yaml:"tool,omitempty"`
}

// HeaderTotalEnabled reports whether the run cost shows in the header (default
// on when usage is enabled).
func (u UsageConfig) HeaderTotalEnabled() bool {
	return u.ShowTotalInHeader == nil || *u.ShowTotalInHeader
}

// BorderCostEnabled reports whether per-agent cost shows in pane borders
// (default on when usage is enabled).
func (u UsageConfig) BorderCostEnabled() bool {
	return u.ShowAgentCostInBorder == nil || *u.ShowAgentCostInBorder
}

// normalizeUsage fills usage defaults; a no-op when usage is off.
func (c *Config) normalizeUsage() {
	if !c.Usage.Enabled {
		return
	}
	if c.Usage.Estimator == "" {
		c.Usage.Estimator = "bytes4"
	}
	if c.Usage.StalePriceAfterDays <= 0 {
		c.Usage.StalePriceAfterDays = 60
	}
}

// validateUsage checks price profiles and per-agent bindings resolve.
func (c Config) validateUsage() error {
	if !c.Usage.Enabled {
		return nil
	}
	switch c.Usage.Estimator {
	case "", "bytes4", "runes4":
	default:
		return fmt.Errorf("usage.estimator must be bytes4 or runes4")
	}
	profiles := make([]string, 0, len(c.Usage.Prices))
	for name, p := range c.Usage.Prices {
		profiles = append(profiles, name)
		if p.InputPerMillion < 0 || p.OutputPerMillion < 0 || p.CacheWritePerMillion < 0 || p.CacheReadPerMillion < 0 {
			return fmt.Errorf("usage.prices.%s has a negative rate", name)
		}
	}
	sort.Strings(profiles)

	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		au := c.Agents[name].Usage
		if pp := au.PriceProfile; pp != "" {
			if _, ok := c.Usage.Prices[pp]; !ok {
				return fmt.Errorf("agent %q references unknown usage.price_profile %q", name, pp)
			}
		}
		// The tool's only job is selecting a provider-session reader, so an
		// unknown value can never work — a typo would just disable
		// reconciliation silently. Reject it up front.
		if tool := au.Tool; tool != "" && !slices.Contains(usage.KnownTools(), tool) {
			return fmt.Errorf("agent %q has unknown usage.tool %q (known: %s)",
				name, tool, strings.Join(usage.KnownTools(), ", "))
		}
	}
	return nil
}
