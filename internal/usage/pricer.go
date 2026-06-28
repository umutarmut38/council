package usage

import (
	"context"
	"time"

	"github.com/umutarmut38/council/internal/usage/internal/pricing"
)

// Pricer resolves a model (and optional user price profile) to a per-token cost.
// It is the usage domain's facade over the pricing engine, so callers (TUI, CLI)
// never import the pricing package directly.
type Pricer struct{ r *pricing.Resolver }

// PriceProfile is a user-configured price, in per-million-token units.
type PriceProfile struct {
	InputPerMillion  float64
	OutputPerMillion float64
	Currency         string
	ReviewedAt       string
}

// PricerOptions configure a Pricer.
type PricerOptions struct {
	CacheDir     string                  // dir holding the LiteLLM cache (.council/usage)
	ModelAliases map[string]string       // usage.model_aliases
	Prices       map[string]PriceProfile // usage.prices
}

// NewPricer builds a Pricer from the bundled snapshot, the on-disk LiteLLM cache,
// and the user's price profiles.
func NewPricer(o PricerOptions) *Pricer {
	up := make(map[string]pricing.UserPrice, len(o.Prices))
	for k, p := range o.Prices {
		up[k] = pricing.UserPrice{
			InputPerMillion:  p.InputPerMillion,
			OutputPerMillion: p.OutputPerMillion,
			Currency:         p.Currency,
			ReviewedAt:       p.ReviewedAt,
		}
	}
	return &Pricer{r: pricing.New(pricing.Options{CacheDir: o.CacheDir, UserAlias: o.ModelAliases, UserPrices: up})}
}

// Rate is a resolved per-token price.
type Rate struct {
	InputPerToken  float64
	OutputPerToken float64
	Source         string
	Found          bool
}

// Rate resolves the price for a model; priceProfile wins when set and known.
func (p *Pricer) Rate(model, priceProfile string) Rate {
	if p == nil {
		return Rate{}
	}
	c, src, ok := p.r.Resolve(model, priceProfile)
	return Rate{InputPerToken: c.Input, OutputPerToken: c.Output, Source: src, Found: ok}
}

// Origin reports the active price table's source and freshness, for UI labels.
func (p *Pricer) Origin() (source string, at time.Time) {
	if p == nil {
		return "", time.Time{}
	}
	return p.r.Origin()
}

// RefreshPrices fetches the LiteLLM price table into the on-disk cache under dir.
func RefreshPrices(ctx context.Context, dir string, now time.Time) error {
	return pricing.RefreshCache(ctx, dir, now)
}
