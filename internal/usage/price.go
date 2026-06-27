package usage

import "github.com/umutarmut38/council/internal/pricing"

// Price fills each session's CostUSD (and the grand total) from a pricing
// resolver. A session whose model has no known price keeps CostUSD nil, which
// renders as "—" (cost unknown) — never a silent $0.
func (s *Summary) Price(r *pricing.Resolver) {
	if r == nil {
		return
	}
	var total float64
	priced := false
	for i := range s.Sessions {
		ses := &s.Sessions[i]
		costs, _, ok := r.Resolve(ses.Model, ses.PriceProfile)
		if !ok {
			continue
		}
		c := float64(ses.Input)*costs.Input + float64(ses.Output)*costs.Output
		ses.CostUSD = &c
		total += c
		priced = true
	}
	if priced {
		s.CostUSD = &total
	}
}
