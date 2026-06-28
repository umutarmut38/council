package usage

// Price fills each session's CostUSD (and the grand total) from a Pricer. A
// session whose model has no known price keeps CostUSD nil, which renders as
// "—" (cost unknown) — never a silent $0.
func (s *Summary) Price(p *Pricer) {
	if p == nil {
		return
	}
	var total float64
	priced := false
	for i := range s.Sessions {
		ses := &s.Sessions[i]
		r := p.Rate(ses.Model, ses.PriceProfile)
		if !r.Found {
			continue
		}
		c := float64(ses.Input)*r.InputPerToken + float64(ses.Output)*r.OutputPerToken
		ses.CostUSD = &c
		total += c
		priced = true
	}
	if priced {
		s.CostUSD = &total
	}
}
