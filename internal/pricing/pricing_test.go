package pricing

import (
	"math"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestResolveExactAndCost(t *testing.T) {
	r := New(Options{})
	costs, src, ok := r.Resolve("gpt-5", "")
	if !ok || src != SourceBundled {
		t.Fatalf("gpt-5 resolve: ok=%v src=%q", ok, src)
	}
	if !approx(costs.Input, 1.25e-06) {
		t.Fatalf("gpt-5 input = %v, want 1.25e-06", costs.Input)
	}
	c, ok := r.CalculateCost("gpt-5", 1_000_000, 0, 0, 0, 0, false, 0)
	if !ok || !approx(c, 1.25) {
		t.Fatalf("gpt-5 cost = %v, want 1.25", c)
	}
}

// gpt-5-mini-foo must match gpt-5-mini, not collapse to the shorter gpt-5.
func TestLongestPrefix(t *testing.T) {
	r := New(Options{})
	costs, _, ok := r.Resolve("gpt-5-mini-foo", "")
	if !ok || !approx(costs.Input, 2.5e-07) {
		t.Fatalf("longest-prefix input = %v, want gpt-5-mini 2.5e-07", costs.Input)
	}
}

func TestAliasResolution(t *testing.T) {
	r := New(Options{})
	costs, _, ok := r.Resolve("claude-4.6-sonnet", "") // alias -> claude-sonnet-4-6
	if !ok || !approx(costs.Input, 3e-06) {
		t.Fatalf("alias input = %v, want claude-sonnet-4-6 3e-06", costs.Input)
	}
}

func TestAliasWinsOverResellerKey(t *testing.T) {
	r := New(Options{})
	costs, _, ok := r.Resolve("claude-4-opus", "") // curated alias -> claude-opus-4
	if !ok || !approx(costs.Input, 1.5e-05) {
		t.Fatalf("alias-wins input = %v, want claude-opus-4 1.5e-05", costs.Input)
	}
}

func TestUserAliasOverridesBuiltin(t *testing.T) {
	r := New(Options{UserAlias: map[string]string{"my-model": "gpt-5"}})
	costs, _, ok := r.Resolve("my-model", "")
	if !ok || !approx(costs.Input, 1.25e-06) {
		t.Fatalf("user alias input = %v, want gpt-5", costs.Input)
	}
}

// An unknown model is never silently $0 — it reports found=false.
func TestUnknownModelIsNotFree(t *testing.T) {
	r := New(Options{})
	if _, _, ok := r.Resolve("totally-made-up-model-xyz", ""); ok {
		t.Fatal("unknown model resolved; want found=false")
	}
	if c, ok := r.CalculateCost("totally-made-up-model-xyz", 1_000_000, 0, 0, 0, 0, false, 0); ok || c != 0 {
		t.Fatalf("unknown CalculateCost = %v ok=%v, want 0/false", c, ok)
	}
}

func TestUserPriceProfileWins(t *testing.T) {
	r := New(Options{UserPrices: map[string]UserPrice{
		"p": {InputPerMillion: 2, OutputPerMillion: 10},
	}})
	costs, src, ok := r.Resolve("gpt-5", "p")
	if !ok || src != SourceUser {
		t.Fatalf("profile resolve: ok=%v src=%q", ok, src)
	}
	if !approx(costs.Input, 2e-06) || !approx(costs.Output, 1e-05) {
		t.Fatalf("profile rates = %v/%v, want 2e-06/1e-05", costs.Input, costs.Output)
	}
}

func TestFastMultiplier(t *testing.T) {
	r := New(Options{})
	std, _ := r.CalculateCost("claude-opus-4-6", 1_000_000, 0, 0, 0, 0, false, 0)
	fast, _ := r.CalculateCost("claude-opus-4-6", 1_000_000, 0, 0, 0, 0, true, 0)
	if !approx(std, 5.0) || !approx(fast, 30.0) { // input 5e-06, fast x6
		t.Fatalf("std=%v fast=%v, want 5 and 30", std, fast)
	}
}

func TestNegativeTokensClampToZero(t *testing.T) {
	r := New(Options{})
	c, ok := r.CalculateCost("gpt-5", -100, -100, 0, 0, 0, false, 0)
	if !ok || c != 0 {
		t.Fatalf("negative tokens cost = %v, want 0", c)
	}
}

func TestUserPriceStale(t *testing.T) {
	now := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	old := UserPrice{ReviewedAt: "2026-01-01"}
	fresh := UserPrice{ReviewedAt: "2026-06-20"}
	if !old.IsStale(now, 60) {
		t.Fatal("177-day-old profile should be stale at 60d")
	}
	if fresh.IsStale(now, 60) {
		t.Fatal("7-day-old profile should not be stale at 60d")
	}
}
