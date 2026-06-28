// Package pricing resolves a model name to a per-token price and computes cost.
// The lookup pipeline, alias handling, clamps, and the bundled LiteLLM snapshot
// are a behaviour-preserving Go port of codeburn's src/models.ts (MIT, (c) 2026
// AgentSeal — see NOTICE). Divergences from the TS, all deliberate:
//   - No module-global mutable state: everything lives on a Resolver built once
//     and read-only thereafter, so it is safe to share across goroutines.
//   - An unpriced model returns found=false (caller marks it "unknown"); council
//     never silently treats an unknown paid model as $0.
//   - Local-model-savings and proxy-path logic are dropped (out of scope).
package pricing

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/fsperm"
)

//go:embed data/litellm-snapshot.json
var snapshotJSON []byte

//go:embed data/pricing-fallback.json
var fallbackJSON []byte

// LiteLLMURL is the upstream price table refreshed by RefreshCache.
const LiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

const (
	cacheTTL          = 24 * time.Hour
	webSearchCost     = 0.01
	oneHourCacheMult  = 1.6 // 1-hour cache write rate vs the 5-minute rate
	maxPerTokenRate   = 1.0 // clamp ceiling; well above any real frontier model
	cacheWriteDefault = 1.25
	cacheReadDefault  = 0.1
)

// Source labels where a resolved price came from.
const (
	SourceUser    = "user"            // a user-configured price profile
	SourceCache   = "litellm-cache"   // refreshed LiteLLM data on disk
	SourceBundled = "litellm-bundled" // the embedded snapshot
	SourceUnknown = ""
)

// ModelCosts is a model's per-token rates (port of codeburn ModelCosts).
type ModelCosts struct {
	Input, Output, CacheWrite, CacheRead, WebSearch, FastMultiplier float64
}

// UserPrice is a user-configured price profile, in per-million-token units to
// match the config file. Converted to ModelCosts on resolve.
type UserPrice struct {
	InputPerMillion  float64
	OutputPerMillion float64
	Currency         string
	ReviewedAt       string // RFC3339 date
}

var (
	reDate     = regexp.MustCompile(`-\d{8}$`)
	reProvider = regexp.MustCompile(`^[^/]+/`)
)

// safePerTokenRate clamps a rate to a sane non-negative value, rejecting
// NaN/Inf/negative and capping at $1/token (defends against tampered data).
func safePerTokenRate(n *float64) (float64, bool) {
	if n == nil || math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0 {
		return 0, false
	}
	if *n > maxPerTokenRate {
		return maxPerTokenRate, true
	}
	return *n, true
}

func buildCosts(input, output float64, cacheWrite, cacheRead, fast *float64) ModelCosts {
	cw := input * cacheWriteDefault
	if v, ok := safePerTokenRate(cacheWrite); ok {
		cw = v
	}
	cr := input * cacheReadDefault
	if v, ok := safePerTokenRate(cacheRead); ok {
		cr = v
	}
	// fast is a multiplier (e.g. 6x), not a per-token rate, so it is not clamped.
	fm := 1.0
	if fast != nil && !math.IsNaN(*fast) && !math.IsInf(*fast, 0) && *fast > 0 {
		fm = *fast
	}
	return ModelCosts{Input: input, Output: output, CacheWrite: cw, CacheRead: cr, WebSearch: webSearchCost, FastMultiplier: fm}
}

// tupleToCosts parses a snapshot tuple [input, output, cacheWrite?, cacheRead?, fast?].
func tupleToCosts(raw []*float64) (ModelCosts, bool) {
	if len(raw) < 2 {
		return ModelCosts{}, false
	}
	in, ok := safePerTokenRate(raw[0])
	out, ok2 := safePerTokenRate(raw[1])
	if !ok || !ok2 {
		return ModelCosts{}, false
	}
	get := func(i int) *float64 {
		if i < len(raw) {
			return raw[i]
		}
		return nil
	}
	return buildCosts(in, out, get(2), get(3), get(4)), true
}

func loadTuples(data []byte) map[string]ModelCosts {
	var raw map[string][]*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]ModelCosts{}
	}
	out := make(map[string]ModelCosts, len(raw))
	for name, tup := range raw {
		if c, ok := tupleToCosts(tup); ok {
			out[name] = c
		}
	}
	return out
}

// Resolver answers price lookups. Build once with New; read-only thereafter.
type Resolver struct {
	priced     map[string]ModelCosts
	sortedKeys []string // longest-first, for prefix matching
	lowerIndex map[string]ModelCosts
	userAlias  map[string]string
	userPrices map[string]UserPrice
	origin     string    // SourceCache | SourceBundled
	originAt   time.Time // build/fetch time of the active data
}

// Options configure a Resolver.
type Options struct {
	CacheDir   string               // dir holding litellm-pricing-cache.json (optional)
	UserAlias  map[string]string    // usage.model_aliases
	UserPrices map[string]UserPrice // usage.prices
}

// New builds a Resolver from the bundled snapshot, overlaying a fresh on-disk
// LiteLLM cache when present and within TTL.
func New(opts Options) *Resolver {
	snap := loadTuples(snapshotJSON)
	origin, at := SourceBundled, snapshotBuildDate()

	if opts.CacheDir != "" {
		if cached, t, ok := loadCache(filepath.Join(opts.CacheDir, cacheFile)); ok {
			for k, v := range snap { // snapshot fills gaps the cache lacks
				if _, exists := cached[k]; !exists {
					cached[k] = v
				}
			}
			snap, origin, at = cached, SourceCache, t
		}
	}

	r := &Resolver{
		priced:     snap,
		lowerIndex: buildLowerIndex(snap),
		userAlias:  opts.UserAlias,
		userPrices: opts.UserPrices,
		origin:     origin,
		originAt:   at,
	}
	r.sortedKeys = make([]string, 0, len(snap))
	for k := range snap {
		r.sortedKeys = append(r.sortedKeys, k)
	}
	sort.Slice(r.sortedKeys, func(i, j int) bool { return len(r.sortedKeys[i]) > len(r.sortedKeys[j]) })
	return r
}

func buildLowerIndex(priced map[string]ModelCosts) map[string]ModelCosts {
	idx := map[string]ModelCosts{}
	add := func(name string, c ModelCosts) {
		if c.Input <= 0 && c.Output <= 0 { // skip zero-priced stubs
			return
		}
		lk := strings.ToLower(name)
		if _, ok := idx[lk]; !ok {
			idx[lk] = c
		}
	}
	for k, v := range priced {
		add(k, v)
	}
	for k, v := range loadTuples(fallbackJSON) {
		add(k, v)
	}
	return idx
}

func (r *Resolver) resolveAlias(model string) string {
	if v, ok := r.userAlias[model]; ok {
		return v
	}
	if v, ok := builtinAliases[model]; ok {
		return v
	}
	return model
}

func canonicalName(model string) string {
	s := stripPin(model)
	s = reDate.ReplaceAllString(s, "")
	return reProvider.ReplaceAllString(s, "")
}

func stripPin(model string) string {
	if i := strings.IndexByte(model, '@'); i >= 0 {
		return model[:i]
	}
	return model
}

// getModelCosts ports codeburn's lookup pipeline.
func (r *Resolver) getModelCosts(model string) (ModelCosts, bool) {
	withPrefix := reDate.ReplaceAllString(stripPin(model), "")
	canonicalNm := canonicalName(model)
	canonical := r.resolveAlias(canonicalNm)

	// A curated alias for a bare name beats a coincidental stripped reseller key.
	if canonical != canonicalNm && withPrefix == canonicalNm {
		if c, ok := r.priced[canonical]; ok {
			return c, true
		}
	}
	if c, ok := r.priced[withPrefix]; ok {
		return c, true
	}
	if c, ok := r.priced[canonical]; ok {
		return c, true
	}
	// Longest-prefix: gpt-5-mini must not collapse to gpt-5.
	for _, key := range r.sortedKeys {
		if canonical == key || strings.HasPrefix(canonical, key+"-") {
			return r.priced[key], true
		}
	}
	// Case-insensitive fallback (gap-filled lowercase slugs).
	if c, ok := r.lowerIndex[strings.ToLower(canonical)]; ok {
		return c, true
	}
	if c, ok := r.lowerIndex[strings.ToLower(withPrefix)]; ok {
		return c, true
	}
	return ModelCosts{}, false
}

// Resolve returns the price for an agent's model. priceProfile, when set and
// known, wins over the LiteLLM tables. found=false means unknown (no cost).
func (r *Resolver) Resolve(model, priceProfile string) (costs ModelCosts, source string, found bool) {
	if priceProfile != "" {
		if up, ok := r.userPrices[priceProfile]; ok {
			return ModelCosts{Input: up.InputPerMillion / 1e6, Output: up.OutputPerMillion / 1e6}, SourceUser, true
		}
	}
	if c, ok := r.getModelCosts(model); ok {
		return c, r.origin, true
	}
	return ModelCosts{}, SourceUnknown, false
}

// CalculateCost is the faithful full port (cache/web/fast aware); council passes
// only input/output and 0 for the rest. found=false → 0 cost and unknown.
func (r *Resolver) CalculateCost(model string, in, out, cacheCreate, cacheRead, webReq int, fast bool, oneHourCache int) (float64, bool) {
	c, ok := r.getModelCosts(model)
	if !ok {
		return 0, false
	}
	mult := 1.0
	if fast {
		mult = c.FastMultiplier
	}
	safe := func(n int) float64 {
		if n > 0 {
			return float64(n)
		}
		return 0
	}
	oneHour := safe(oneHourCache)
	totalCacheCreate := math.Max(safe(cacheCreate), oneHour)
	fiveMin := math.Max(0, totalCacheCreate-oneHour)
	return mult * (safe(in)*c.Input +
		safe(out)*c.Output +
		fiveMin*c.CacheWrite +
		oneHour*c.CacheWrite*oneHourCacheMult +
		safe(cacheRead)*c.CacheRead +
		safe(webReq)*c.WebSearch), true
}

// Origin reports the active table source and its freshness for UI labels.
func (r *Resolver) Origin() (source string, at time.Time) { return r.origin, r.originAt }

// IsStale reports whether a user price profile is older than the given window.
func (up UserPrice) IsStale(now time.Time, days int) bool {
	if days <= 0 || up.ReviewedAt == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", up.ReviewedAt)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, up.ReviewedAt); err != nil {
			return false
		}
	}
	return now.Sub(t) > time.Duration(days)*24*time.Hour
}

// ---- LiteLLM cache refresh ----

const cacheFile = "litellm-pricing-cache.json"

type cacheDoc struct {
	Timestamp int64                 `json:"timestamp"` // unix ms
	Data      map[string][]*float64 `json:"data"`
}

type litellmEntry struct {
	Input            *float64 `json:"input_cost_per_token"`
	Output           *float64 `json:"output_cost_per_token"`
	CacheCreate      *float64 `json:"cache_creation_input_token_cost"`
	CacheRead        *float64 `json:"cache_read_input_token_cost"`
	ProviderSpecific *struct {
		Fast *float64 `json:"fast"`
	} `json:"provider_specific_entry"`
}

// FetchLiteLLM downloads the upstream LiteLLM price table and parses it into the
// tuple form council stores. Shared by RefreshCache (live cache) and the
// bundled-snapshot generator (internal/pricing/gen).
func FetchLiteLLM(ctx context.Context) (map[string][]*float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LiteLLMURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("litellm fetch: HTTP %d", resp.StatusCode)
	}
	var raw map[string]litellmEntry
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	data := map[string][]*float64{}
	for name, e := range raw {
		in, ok := safePerTokenRate(e.Input)
		out, ok2 := safePerTokenRate(e.Output)
		if !ok || !ok2 {
			continue
		}
		var fast *float64
		if e.ProviderSpecific != nil {
			fast = e.ProviderSpecific.Fast
		}
		tup := []*float64{&in, &out, e.CacheCreate, e.CacheRead, fast}
		data[name] = tup
		// Also index the provider-prefix-stripped name (first write wins, so a
		// direct-provider entry beats a re-hoster).
		if stripped := reProvider.ReplaceAllString(name, ""); stripped != name {
			if _, exists := data[stripped]; !exists {
				data[stripped] = tup
			}
		}
	}
	return data, nil
}

// RefreshCache fetches the LiteLLM table and writes the tuple cache atomically
// (owner-only). dir is .council/usage. now is injected for testability.
func RefreshCache(ctx context.Context, dir string, now time.Time) error {
	data, err := FetchLiteLLM(ctx)
	if err != nil {
		return err
	}
	doc, err := json.Marshal(cacheDoc{Timestamp: now.UnixMilli(), Data: data})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, fsperm.Dir()); err != nil {
		return err
	}
	dest := filepath.Join(dir, cacheFile)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, doc, fsperm.File()); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func loadCache(path string) (map[string]ModelCosts, time.Time, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	var doc cacheDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, time.Time{}, false
	}
	t := time.UnixMilli(doc.Timestamp)
	if time.Since(t) > cacheTTL {
		return nil, time.Time{}, false
	}
	out := make(map[string]ModelCosts, len(doc.Data))
	for name, tup := range doc.Data {
		if c, ok := tupleToCosts(tup); ok {
			out[name] = c
		}
	}
	return out, t, true
}

//go:embed data/snapshot-date.txt
var snapshotDate string

func snapshotBuildDate() time.Time {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(snapshotDate)); err == nil {
		return t
	}
	return time.Time{}
}
