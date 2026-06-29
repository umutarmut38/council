package usage

import "testing"

func TestSummaryPriceUnknownIsNotZero(t *testing.T) {
	s := Aggregate([]Event{{Agent: "a", Model: "not-a-real-model", Confidence: Estimated, InputTokens: 1000}})
	s.Price(NewPricer(PricerOptions{}))
	if len(s.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(s.Sessions))
	}
	if s.Sessions[0].Cost != nil || s.Cost != nil {
		t.Fatalf("unknown model got a cost: row=%v total=%v", s.Sessions[0].Cost, s.Cost)
	}
}

func TestSummaryPriceMixedCurrencyOmitsTotal(t *testing.T) {
	p := NewPricer(PricerOptions{Prices: map[string]PriceProfile{
		"usd": {InputPerMillion: 1, OutputPerMillion: 1, Currency: "USD"},
		"eur": {InputPerMillion: 1, OutputPerMillion: 1, Currency: "EUR"},
	}})
	s := Aggregate([]Event{
		{Agent: "a", Model: "x", PriceProfile: "usd", Confidence: Estimated, InputTokens: 1_000_000},
		{Agent: "b", Model: "x", PriceProfile: "eur", Confidence: Estimated, InputTokens: 1_000_000},
	})
	s.Price(p)
	if s.Sessions[0].Cost == nil || s.Sessions[1].Cost == nil {
		t.Fatalf("rows should be individually priced: %+v", s.Sessions)
	}
	if s.Cost != nil || s.Note != "mixed currencies; total omitted" {
		t.Fatalf("mixed currency total = %v note=%q, want omitted", s.Cost, s.Note)
	}
}
