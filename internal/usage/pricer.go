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
	Source           string
	ReviewedAt       string
}

// PricerOptions configure a Pricer.
type PricerOptions struct {
	CacheDir       string                  // dir holding the LiteLLM cache (.council/usage)
	ModelAliases   map[string]string       // usage.model_aliases
	Prices         map[string]PriceProfile // usage.prices
	StaleAfterDays int
	Now            time.Time
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
			Source:           p.Source,
			ReviewedAt:       p.ReviewedAt,
		}
	}
	return &Pricer{r: pricing.New(pricing.Options{
		CacheDir: o.CacheDir, UserAlias: o.ModelAliases, UserPrices: up,
		StaleAfterDays: o.StaleAfterDays, Now: o.Now,
	})}
}

// Rate is a resolved per-token price.
type Rate struct {
	InputPerToken       float64
	OutputPerToken      float64
	CacheCreatePerToken float64
	CacheReadPerToken   float64
	WebSearchPerRequest float64
	FastMultiplier      float64
	Source              string
	SourceDate          time.Time
	ReviewedAt          string
	Stale               bool
	Found               bool
	PriceModel          string
	Note                string
	Currency            string
	Confidence          string
}

// Rate resolves the price for a model; priceProfile wins when set and known.
func (p *Pricer) Rate(model, priceProfile string) Rate {
	if p == nil {
		return Rate{}
	}
	res := p.r.ResolveDetailed(model, priceProfile)
	return Rate{
		InputPerToken: res.Costs.Input, OutputPerToken: res.Costs.Output,
		CacheCreatePerToken: res.Costs.CacheWrite, CacheReadPerToken: res.Costs.CacheRead,
		WebSearchPerRequest: res.Costs.WebSearch, FastMultiplier: res.Costs.FastMultiplier,
		Source: res.Source, SourceDate: res.SourceDate, ReviewedAt: res.ReviewedAt, Stale: res.Stale,
		Found: res.Found, PriceModel: res.PriceModel, Note: res.Note, Currency: res.Currency,
		Confidence: res.Confidence,
	}
}

// Cost returns the price for full token quantities.
func (r Rate) Cost(t TokenTotals) (float64, bool) {
	if !r.Found {
		return 0, false
	}
	mult := r.FastMultiplier
	if mult <= 0 {
		mult = 1
	}
	fastCost := float64(t.FastInput)*r.InputPerToken*mult + float64(t.FastOutput)*r.OutputPerToken*mult
	return float64(t.Input)*r.InputPerToken +
		float64(t.Output+t.Reasoning)*r.OutputPerToken +
		float64(t.CacheCreate)*r.CacheCreatePerToken +
		float64(t.CacheRead)*r.CacheReadPerToken +
		float64(t.WebSearch)*r.WebSearchPerRequest +
		fastCost, true
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
