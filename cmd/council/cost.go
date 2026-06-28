package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	"github.com/umutarmut38/council/internal/usage"
)

// councilCost prints a run's per-session usage/cost without launching the TUI:
//
//	council cost [run]            summary for a run (default: latest)
//	council cost --since 30d      summary across recent runs
//	council cost prices refresh   refresh the LiteLLM price cache
func councilCost(args []string) error {
	if len(args) > 0 && args[0] == "prices" {
		return councilCostPrices(args[1:])
	}

	fs, noLocal := newOrchFlagSet("council cost")
	since := fs.String("since", "", "aggregate across runs newer than this (e.g. 30d, 7d)")
	source := fs.String("source", "ledger", "ledger | codeburn (machine-wide cross-tool totals)")
	rest, err := parseWithTrailingFlags(fs, args)
	if err != nil {
		return err
	}
	if *source == "codeburn" {
		return councilCostCodeburn()
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	pricer := usage.NewPricer(usage.PricerOptions{
		CacheDir:     usageDir(cfg),
		ModelAliases: cfg.Usage.ModelAliases,
		Prices:       toPriceProfiles(cfg.Usage.Prices),
	})

	var events []usage.Event
	var scope string
	if *since != "" {
		cutoff, derr := sinceCutoff(*since)
		if derr != nil {
			return derr
		}
		if events, err = usage.LoadRunsSince(cfg.Sessions.RootDir, cutoff); err != nil {
			return err
		}
		scope = "since " + *since
	} else {
		runArg := ""
		if len(rest) > 0 {
			runArg = rest[0]
		}
		run, rerr := orchestrate.OpenRun(cfg.Sessions.RootDir, runArg)
		if rerr != nil {
			return rerr
		}
		if events, err = usage.LoadEvents(run.Dir); err != nil {
			return err
		}
		scope = "run " + run.Stamp
	}

	if len(events) == 0 {
		fmt.Printf("no usage recorded for %s\n", scope)
		if !cfg.Usage.Enabled {
			fmt.Println("(usage tracking is off; set usage.enabled: true in .council.yaml)")
		}
		return nil
	}

	events = append(events, usage.Reconcile(events)...)
	summary := usage.Aggregate(events)
	summary.Currency = cfg.Usage.Currency
	summary.Price(pricer)
	fmt.Printf("Usage — %s\n\n%s", scope, usage.FormatTable(summary))
	if src, at := pricer.Origin(); !at.IsZero() {
		fmt.Printf("\nprices: %s (%s)\n", src, at.Format("2006-01-02"))
	}
	return nil
}

// councilCostCodeburn relays machine-wide cross-tool usage from the optional
// codeburn CLI, for tools council does not launch itself.
func councilCostCodeburn() error {
	if !usage.CodeburnAvailable() {
		return fmt.Errorf("codeburn is not installed; install it for cross-tool totals: npm i -g codeburn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := usage.RunCodeburn(ctx)
	if err != nil {
		return fmt.Errorf("codeburn: %w", err)
	}
	fmt.Printf("codeburn — machine-wide usage (%s)\n\n", report.Currency)
	fmt.Printf("  today: %.2f  (%d calls)\n", report.Today.Cost, report.Today.Calls)
	fmt.Printf("  month: %.2f  (%d calls)\n", report.Month.Cost, report.Month.Calls)
	return nil
}

func councilCostPrices(args []string) error {
	if len(args) == 0 || args[0] != "refresh" {
		return fmt.Errorf("usage: council cost prices refresh")
	}
	cfg, err := loadConfig(false)
	if err != nil {
		return err
	}
	dir := usageDir(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := usage.RefreshPrices(ctx, dir, time.Now()); err != nil {
		return fmt.Errorf("refresh pricing: %w", err)
	}
	fmt.Println("refreshed:", filepath.Join(dir, "litellm-pricing-cache.json"))
	return nil
}

// usageDir is .council/usage, a sibling of the runs dir.
func usageDir(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Sessions.RootDir), "usage")
}

// toPriceProfiles converts config price profiles to the usage facade's type.
func toPriceProfiles(prices map[string]config.PriceProfile) map[string]usage.PriceProfile {
	if len(prices) == 0 {
		return nil
	}
	out := make(map[string]usage.PriceProfile, len(prices))
	for name, p := range prices {
		out[name] = usage.PriceProfile{
			InputPerMillion:  p.InputPerMillion,
			OutputPerMillion: p.OutputPerMillion,
			Currency:         p.Currency,
			ReviewedAt:       p.ReviewedAt,
		}
	}
	return out
}

// sinceCutoff parses a "30d"/"7d"/"24h" window into an absolute time.
func sinceCutoff(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return time.Time{}, fmt.Errorf("bad --since %q", s)
		}
		return time.Now().AddDate(0, 0, -days), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad --since %q (use 30d, 7d, 24h)", s)
	}
	return time.Now().Add(-d), nil
}
