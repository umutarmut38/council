package usage

import "sort"

// Price fills each session's Cost (and the grand total when every priced row
// shares one currency) from a Pricer. A session whose model has no known price
// keeps Cost nil, which renders as "--" (cost unknown) -- never a silent $0.
func (s *Summary) Price(p *Pricer) {
	if p == nil {
		return
	}
	var total float64
	totalCurrency := ""
	priced, unknown, mixed := false, false, false
	for i := range s.Sessions {
		ses := &s.Sessions[i]
		r := p.Rate(ses.Model, ses.PriceProfile)
		if !r.Found {
			unknown = true
			ses.PriceSource = Unknown
			ses.PriceConf = Unknown
			ses.PriceModel = UnknownValue
			ses.PriceNote = joinNote(ses.PriceNote, "price unknown")
			hint := ses.Agent + "/" + ses.Phase + ": price unknown for model " + dash(ses.Model)
			// Point at the fix, but only when there's a real name to alias. An
			// unknown ("--") model can't be aliased — the "usage.model is not
			// configured" hint already covers that case.
			if ses.Model != "" && ses.Model != UnknownValue {
				hint += `; map it in usage.model_aliases ("` + ses.Model + `: <canonical>")`
			}
			s.Hints = appendUnique(s.Hints, hint)
			continue
		}
		c, _ := r.Cost(ses.Tokens)
		ses.Cost = &c
		ses.Currency = r.Currency
		ses.PriceSource = r.Source
		ses.PriceConf = r.Confidence
		ses.PriceModel = r.PriceModel
		ses.PriceNote = joinNote(ses.PriceNote, r.Note)
		ses.Stale = r.Stale
		if r.Stale {
			s.Hints = appendUnique(s.Hints, ses.Agent+"/"+ses.Phase+": price profile is stale")
		}
		if totalCurrency == "" {
			totalCurrency = r.Currency
		} else if totalCurrency != r.Currency {
			mixed = true
		}
		total += c
		priced = true
	}
	sortStrings(s.Hints)
	if priced && !unknown && !mixed {
		s.Cost = &total
		s.Currency = totalCurrency
		return
	}
	if mixed {
		s.Note = "mixed currencies; total omitted"
		return
	}
	if unknown && priced {
		s.Note = "partial pricing; total omitted"
	}
}

func appendUnique(xs []string, x string) []string {
	for _, existing := range xs {
		if existing == x {
			return xs
		}
	}
	return append(xs, x)
}

func sortStrings(xs []string) { sort.Strings(xs) }
