// Package usage is council's local, provider-agnostic token/cost ledger. It
// records what council can observe locally -- prompt sizes and transcript sizes
// per agent session -- as append-only JSONL events under a run, and aggregates
// them per session (council member). Cost is derived later via the Pricer
// facade; usage is the raw fact.
package usage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/umutarmut38/council/internal/fsperm"
)

// SchemaVersion tags every event so a future format change can be detected.
const SchemaVersion = 1

// Confidence labels how trustworthy a value is, worst to best.
const (
	Unknown   = "unknown"
	Estimated = "estimated"
	Reported  = "reported"
	Exact     = "exact"
)

// Event sources.
const (
	SourcePrompt     = "council.prompt"
	SourceTranscript = "council.transcript"
	SourceProvider   = "provider.session"
	SourceCodeburn   = "codeburn"
)

// Metadata sources.
const (
	MetaSourceConfig   = "config"
	MetaSourcePreset   = "preset"
	MetaSourceProvider = SourceProvider
	MetaSourceUnknown  = Unknown
)

// Output bases.
const (
	OutputBasisPaneTranscriptDelta = "pane_transcript_delta"
	OutputBasisProviderReported    = "provider_reported"
)

// UnknownValue is the explicit marker used in required event fields when a
// value is unavailable. Empty strings are tolerated when reading older ledgers,
// but new events write "unknown" so unknown never looks like an omitted fact.
const UnknownValue = Unknown

// Event is one recorded usage fact. It intentionally keeps observed model
// identity (Model) separate from the resolved pricing key (PriceModel). Council
// never parses AgentConfig.Command to fill any of these fields.
type Event struct {
	SchemaVersion int    `json:"schema_version"`
	At            string `json:"at"`
	RunID         string `json:"run_id"`
	Agent         string `json:"agent"`
	Phase         string `json:"phase,omitempty"`
	Source        string `json:"source"`
	Confidence    string `json:"confidence"`

	Tool           string `json:"tool"`
	ToolSource     string `json:"tool_source"`
	ToolConfidence string `json:"tool_confidence"`

	Model           string `json:"model"`
	ModelSource     string `json:"model_source"`
	ModelConfidence string `json:"model_confidence"`

	PriceModel          string `json:"price_model"`
	PriceSource         string `json:"price_source"`
	PriceConfidence     string `json:"price_confidence"`
	PriceResolutionNote string `json:"price_resolution_note"`
	PriceProfile        string `json:"price_profile,omitempty"`

	Estimator            string `json:"estimator"`
	UserInputChars       int    `json:"user_input_chars,omitempty"`
	WireInputChars       int    `json:"wire_input_chars,omitempty"`
	InputChars           int    `json:"input_chars,omitempty"`
	InputTokens          int    `json:"input_tokens,omitempty"`
	OutputTokens         int    `json:"output_tokens,omitempty"`
	OutputBasis          string `json:"output_basis,omitempty"`
	TranscriptCharsTotal int    `json:"transcript_chars_total,omitempty"`

	PromptHash    string `json:"prompt_hash,omitempty"`
	PromptPreview string `json:"prompt_preview,omitempty"`

	ProviderSessionID string   `json:"provider_session_id,omitempty"`
	ProviderCallID    string   `json:"provider_call_id,omitempty"`
	ReconcileKey      string   `json:"reconcile_key"`
	Replaces          []string `json:"replaces,omitempty"`
	Note              string   `json:"note,omitempty"`

	CWD string `json:"cwd,omitempty"`

	CacheCreateTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadTokens   int `json:"cache_read_input_tokens,omitempty"`
	ReasoningTokens   int `json:"reasoning_output_tokens,omitempty"`
	WebSearchRequests int `json:"web_search_requests,omitempty"`
	FastInputTokens   int `json:"fast_input_tokens,omitempty"`
	FastOutputTokens  int `json:"fast_output_tokens,omitempty"`
}

// UsageKey is the narrow replacement key for estimated events. A reported event
// may replace one or more exact estimated keys via Event.Replaces, while keeping
// its own observed provider model for pricing.
type UsageKey struct {
	RunID      string `json:"run_id"`
	Agent      string `json:"agent"`
	Phase      string `json:"phase"`
	CWD        string `json:"cwd"`
	Tool       string `json:"tool"`
	Model      string `json:"model"`
	PromptHash string `json:"prompt_hash"`
}

// Key returns this event's aggregation/replacement key.
func (e Event) Key() UsageKey {
	return UsageKey{
		RunID:      e.RunID,
		Agent:      e.Agent,
		Phase:      valueOr(e.Phase, "session"),
		CWD:        e.CWD,
		Tool:       valueOr(e.Tool, UnknownValue),
		Model:      valueOr(e.Model, UnknownValue),
		PromptHash: e.PromptHash,
	}
}

// String renders a compact stable key. The hash keeps paths/prompts out of
// table output while still binding all UsageKey fields.
func (k UsageKey) String() string {
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return "usagekey:v1:" + hex.EncodeToString(sum[:12])
}

func (e *Event) normalize() {
	e.SchemaVersion = SchemaVersion
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	if e.Source == "" {
		e.Source = SourcePrompt
	}
	if e.Confidence == "" {
		e.Confidence = Unknown
	}
	if e.Tool == "" {
		e.Tool = UnknownValue
	}
	if e.ToolSource == "" {
		e.ToolSource = MetaSourceUnknown
	}
	if e.ToolConfidence == "" {
		e.ToolConfidence = Unknown
	}
	if e.Model == "" {
		e.Model = UnknownValue
	}
	if e.ModelSource == "" {
		e.ModelSource = MetaSourceUnknown
	}
	if e.ModelConfidence == "" {
		e.ModelConfidence = Unknown
	}
	if e.PriceModel == "" {
		e.PriceModel = UnknownValue
	}
	if e.PriceSource == "" {
		e.PriceSource = Unknown
	}
	if e.PriceConfidence == "" {
		e.PriceConfidence = Unknown
	}
	if e.Estimator == "" && e.Source != SourceProvider {
		e.Estimator = DefaultEstimator
	}
	if e.ReconcileKey == "" {
		e.ReconcileKey = e.Key().String()
	}
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// Estimator names the local token estimator.
type Estimator struct {
	Name  string
	Units func(string) int
}

const (
	EstimatorBytes4  = "bytes4"
	EstimatorRunes4  = "runes4"
	DefaultEstimator = EstimatorBytes4
)

// EstimatorFor returns the configured estimator. Unknown names fall back to
// bytes4 and keep that explicit label, avoiding the old ambiguous chars4 name.
func EstimatorFor(name string) Estimator {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case EstimatorRunes4:
		return Estimator{Name: EstimatorRunes4, Units: utf8.RuneCountInString}
	default:
		return Estimator{Name: EstimatorBytes4, Units: func(s string) int { return len([]byte(s)) }}
	}
}

// Estimate returns the token estimate and measured input character/unit count.
func (e Estimator) Estimate(s string) (tokens int, units int) {
	if e.Units == nil {
		e = EstimatorFor("")
	}
	units = e.Units(s)
	return units / 4, units
}

// EstimateTokens preserves the old package helper as the default bytes/4
// estimator for tests and simple callers.
func EstimateTokens(s string) int {
	t, _ := EstimatorFor(DefaultEstimator).Estimate(s)
	return t
}

// PromptHash returns a stable short hash of the agent-visible prompt body (not
// the launch command and not terminal control bytes).
func PromptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:12])
}

// PromptPreview returns a compact user-message preview for ledgers and
// reconciliation hints.
func PromptPreview(prompt string) string {
	text := strings.Join(strings.Fields(prompt), " ")
	runes := []rune(text)
	if len(runes) > 160 {
		return string(runes[:160])
	}
	return text
}

// Ledger appends events to <runDir>/usage/events.jsonl and keeps a live
// per-agent token tally for the header/border. It is owned by the TUI model and
// touched only from Update, so it needs no locking.
type Ledger struct {
	f      *os.File
	Tokens map[string]TokenTotals
}

// TokenTotals is a token/request count. TokenPair is kept as an alias for older
// call sites that only care about input/output.
type TokenTotals struct {
	Input       int
	Output      int
	Reasoning   int
	CacheCreate int
	CacheRead   int
	WebSearch   int
	FastInput   int
	FastOutput  int
}

type TokenPair = TokenTotals

// AddEvent adds the event's quantities to the totals.
func (t *TokenTotals) AddEvent(e Event) {
	t.Input += e.InputTokens
	t.Output += e.OutputTokens
	t.Reasoning += e.ReasoningTokens
	t.CacheCreate += e.CacheCreateTokens
	t.CacheRead += e.CacheReadTokens
	t.WebSearch += e.WebSearchRequests
	t.FastInput += e.FastInputTokens
	t.FastOutput += e.FastOutputTokens
}

func (t TokenTotals) Any() bool {
	return t.Input != 0 || t.Output != 0 || t.Reasoning != 0 || t.CacheCreate != 0 ||
		t.CacheRead != 0 || t.WebSearch != 0 || t.FastInput != 0 || t.FastOutput != 0
}

// Open creates the run's usage dir and opens events.jsonl for appending.
func Open(runDir string) (*Ledger, error) {
	dir := filepath.Join(runDir, "usage")
	if err := os.MkdirAll(dir, fsperm.Dir()); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fsperm.File())
	if err != nil {
		return nil, err
	}
	return &Ledger{f: f, Tokens: map[string]TokenTotals{}}, nil
}

// Record stamps and appends an event, then updates the live tally.
func (l *Ledger) Record(e Event) error {
	e.normalize()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(append(b, '\n')); err != nil {
		return err
	}
	t := l.Tokens[e.Agent]
	t.AddEvent(e)
	l.Tokens[e.Agent] = t
	return nil
}

// Close flushes the ledger file.
func (l *Ledger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// Append records events to a run's ledger without holding a handle open.
func Append(runDir string, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	l, err := Open(runDir)
	if err != nil {
		return err
	}
	for _, e := range events {
		if err := l.Record(e); err != nil {
			l.Close()
			return err
		}
	}
	return l.Close()
}

// LoadEvents reads a run's events.jsonl. Missing file -> no events, no error.
func LoadEvents(runDir string) ([]Event, error) {
	f, err := os.Open(filepath.Join(runDir, "usage", "events.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // tolerate a torn/partial trailing line rather than losing all events
		}
		e.normalize()
		out = append(out, e)
	}
	return out, sc.Err()
}

// LoadRunsSince loads events from every run under rootDir, keeping only events
// timestamped at or after cutoff (zero cutoff = all).
func LoadRunsSince(rootDir string, cutoff time.Time) ([]Event, error) {
	matches, err := filepath.Glob(filepath.Join(rootDir, "*", "usage", "events.jsonl"))
	if err != nil {
		return nil, err
	}
	var all []Event
	for _, m := range matches {
		runDir := filepath.Dir(filepath.Dir(m)) // .../usage/events.jsonl -> run dir
		evs, err := LoadEvents(runDir)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if cutoff.IsZero() {
				all = append(all, e)
				continue
			}
			if t, perr := time.Parse(time.RFC3339, e.At); perr == nil && !t.Before(cutoff) {
				all = append(all, e)
			}
		}
	}
	return all, nil
}

// SessionTotal is one rolled-up row. Estimated rows are keyed per phase;
// provider-reported rows carry whole-session totals (phase "session") and
// replace exactly the estimates listed in their Replaces keys — which may span
// several phases, because provider stores only report per-session cumulative
// counts.
type SessionTotal struct {
	Agent        string
	Phase        string
	Tool         string
	Model        string
	PriceModel   string
	PriceProfile string
	Confidence   string
	Source       string
	OutputBasis  string
	Input        int
	Output       int
	Tokens       TokenTotals
	Cost         *float64
	Currency     string
	PriceSource  string
	PriceConf    string
	PriceNote    string
	Stale        bool
	Notes        []string
}

// Summary is a run's usage aggregated per session/phase/model.
type Summary struct {
	RunID    string
	Sessions []SessionTotal
	Input    int
	Output   int
	Tokens   TokenTotals
	Cost     *float64
	Currency string
	Note     string
	Hints    []string
}

var confTier = map[string]int{"": 0, Unknown: 0, Estimated: 1, Reported: 2, Exact: 3}

// providerSupersessions returns the set of event indices for provider.session
// events that are superseded by a richer sweep of the same (run, tool, model,
// cwd) group. Reconcile (see correlate.go) sums every in-window session for a
// (tool, cwd) into exactly ONE event per model per sweep, so two provider
// events sharing this key are always re-computations of the same cumulative
// total — never independent additive sessions. That invariant is why no
// session/call id is needed in the key: totals only grow, so the max-token event
// is the latest and most complete, and the rest must not be summed. Distinct
// models keep distinct keys, so a genuinely additive second model is preserved.
//
// The agent label and phase are deliberately NOT part of the key: a group's
// label flips from the pane name to the tool name when a second same-tool pane
// joins the cwd mid-run, and older ledgers stamped provider events with the
// first estimate's phase — keying on either would keep the stale sweep alive
// and double-count. RunID IS in the key: the same (tool, cwd, model) group in
// two different runs is genuinely additive when aggregating across runs.
func providerSupersessions(events []Event) map[int]bool {
	type provKey struct{ runID, tool, model, cwd string }
	provTotal := func(e Event) int {
		return e.InputTokens + e.OutputTokens + e.ReasoningTokens +
			e.CacheCreateTokens + e.CacheReadTokens +
			e.FastInputTokens + e.FastOutputTokens + e.WebSearchRequests
	}
	best := map[provKey]int{}
	for i, e := range events {
		if e.Source != SourceProvider {
			continue
		}
		e.normalize()
		k := provKey{e.RunID, e.Tool, e.Model, e.CWD}
		if j, ok := best[k]; !ok || provTotal(events[i]) > provTotal(events[j]) {
			best[k] = i
		}
	}
	superseded := map[int]bool{}
	for i, e := range events {
		if e.Source != SourceProvider {
			continue
		}
		e.normalize()
		k := provKey{e.RunID, e.Tool, e.Model, e.CWD}
		if best[k] != i {
			superseded[i] = true
		}
	}
	return superseded
}

// Aggregate rolls events up per session using explicit replacement keys. A
// reported event replaces only the estimated events listed in Replaces; all
// other estimates remain part of the floor.
func Aggregate(events []Event) Summary {
	replaced := map[string]bool{}
	for _, e := range events {
		for _, key := range e.Replaces {
			replaced[key] = true
		}
	}

	// Each provider reconcile sweep re-computes the CUMULATIVE reported total for
	// a (tool, cwd, model) group from the provider's session files, so a later
	// sweep supersedes an earlier one rather than adding to it. Its identity
	// (reportedID) embeds the Replaces set, which grows every time a new estimate
	// appears (a new prompt, amplified by the periodic auto-reconcile), so each
	// sweep is persisted as a distinct event. Without collapsing them here,
	// Aggregate would SUM N sweeps for the same group and multiply the reported
	// Input/Output/Cost by N. Keep only the richest (max-token) provider event
	// per group; because cumulative session totals only grow, richest == latest.
	supersededProvider := providerSupersessions(events)

	type rowKey struct {
		agent, phase, tool, model, priceModel, priceProfile string
	}
	totals := map[rowKey]*SessionTotal{}
	hintSet := map[string]bool{}
	addHint := func(s string) {
		if s != "" && !hintSet[s] {
			hintSet[s] = true
		}
	}

	for i, e := range events {
		e.normalize()
		if e.Source == SourceProvider && supersededProvider[i] {
			continue
		}
		if replaced[e.ReconcileKey] && e.Source != SourceProvider {
			continue
		}
		var qty TokenTotals
		qty.AddEvent(e)
		if !qty.Any() {
			continue
		}
		phase := valueOr(e.Phase, "session")
		k := rowKey{e.Agent, phase, valueOr(e.Tool, UnknownValue), valueOr(e.Model, UnknownValue), valueOr(e.PriceModel, UnknownValue), e.PriceProfile}
		st := totals[k]
		if st == nil {
			st = &SessionTotal{
				Agent: k.agent, Phase: k.phase, Tool: k.tool, Model: k.model, PriceModel: k.priceModel,
				PriceProfile: k.priceProfile, Confidence: e.Confidence, Source: e.Source,
				OutputBasis: e.OutputBasis, PriceSource: e.PriceSource, PriceConf: e.PriceConfidence,
				PriceNote: e.PriceResolutionNote,
			}
			totals[k] = st
		}
		st.Tokens.Input += e.InputTokens
		st.Tokens.Output += e.OutputTokens
		st.Tokens.Reasoning += e.ReasoningTokens
		st.Tokens.CacheCreate += e.CacheCreateTokens
		st.Tokens.CacheRead += e.CacheReadTokens
		st.Tokens.WebSearch += e.WebSearchRequests
		st.Tokens.FastInput += e.FastInputTokens
		st.Tokens.FastOutput += e.FastOutputTokens
		st.Input = st.Tokens.Input
		st.Output = st.Tokens.Output
		if confTier[e.Confidence] < confTier[st.Confidence] {
			st.Confidence = e.Confidence
		}
		if st.Source == "" || confTier[e.Confidence] > confTier[st.Confidence] {
			st.Source = e.Source
		}
		if e.OutputBasis != "" {
			st.OutputBasis = e.OutputBasis
		}
		if e.PriceResolutionNote != "" {
			st.PriceNote = e.PriceResolutionNote
		}
		if e.Tool == UnknownValue {
			addHint(fmt.Sprintf("%s/%s: usage.tool is not configured; provider-session reconciliation is disabled", e.Agent, phase))
		}
		if e.Model == UnknownValue {
			addHint(fmt.Sprintf("%s/%s: usage.model is not configured; estimated cost stays unknown until a model alias/config or provider-session report is available", e.Agent, phase))
		}
		if e.Note != "" {
			st.Notes = append(st.Notes, e.Note)
		}
	}

	out := Summary{RunID: runID(events)}
	for _, st := range totals {
		out.Sessions = append(out.Sessions, *st)
		out.Tokens.Input += st.Tokens.Input
		out.Tokens.Output += st.Tokens.Output
		out.Tokens.Reasoning += st.Tokens.Reasoning
		out.Tokens.CacheCreate += st.Tokens.CacheCreate
		out.Tokens.CacheRead += st.Tokens.CacheRead
		out.Tokens.WebSearch += st.Tokens.WebSearch
		out.Tokens.FastInput += st.Tokens.FastInput
		out.Tokens.FastOutput += st.Tokens.FastOutput
		out.Input = out.Tokens.Input
		out.Output = out.Tokens.Output
	}
	sort.Slice(out.Sessions, func(i, j int) bool {
		a, b := out.Sessions[i], out.Sessions[j]
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		if a.Phase != b.Phase {
			return a.Phase < b.Phase
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Tool < b.Tool
	})
	for h := range hintSet {
		out.Hints = append(out.Hints, h)
	}
	sort.Strings(out.Hints)
	return out
}

func runID(events []Event) string {
	for _, e := range events {
		if e.RunID != "" {
			return e.RunID
		}
	}
	return ""
}

// FormatTable renders a summary as an aligned text table for the CLI and the
// /cost view. Cost shows "--" until pricing fills Cost.
func FormatTable(s Summary) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "Agent\tPhase\tTool\tModel\tPriceModel\tInput\tCache\tOutput\tCost\tSource\tConfidence\tNote")
	anyEstimated := false
	for _, ses := range s.Sessions {
		note := displayNote(ses)
		// Only a *priced* estimate makes the summed Total an estimate: an unpriced
		// session adds no cost to s.Cost, so it can't turn the figure into one
		// (it surfaces as a "--" row and a hint). Matches the header's logic.
		if ses.Cost != nil && ses.Confidence == Estimated {
			anyEstimated = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			ses.Agent, ses.Phase, dash(ses.Tool), dash(ses.Model), dash(ses.PriceModel),
			tokens(ses.Tokens.Input), cacheTokens(ses.Tokens), tokens(ses.Tokens.Output+ses.Tokens.Reasoning),
			costCell(ses.Cost, ses.Currency, ses.Confidence), dash(ses.PriceSource), dash(ses.PriceConf), dash(note))
	}
	// The Total cost carries ~ when it includes an estimate-derived component —
	// any priced session whose tokens are still estimated (e.g. Copilot mid-run,
	// which only reports on exit), so the figure reads as the lower bound it is.
	totalConf := Reported
	if anyEstimated {
		totalConf = Estimated
	}
	fmt.Fprintf(tw, "Total\t\t\t\t\t%s\t%s\t%s\t%s\t\t\t%s\n",
		tokens(s.Tokens.Input), cacheTokens(s.Tokens), tokens(s.Tokens.Output+s.Tokens.Reasoning),
		costCell(s.Cost, s.Currency, totalConf), dash(s.Note))
	tw.Flush()
	if len(s.Hints) > 0 {
		b.WriteString("\nHints:\n")
		for _, h := range compactHints(s.Hints) {
			b.WriteString("- " + h + "\n")
		}
	}
	return b.String()
}

func displayNote(ses SessionTotal) string {
	note := ""
	for _, p := range noteParts(ses.PriceNote) {
		if p == "price unknown" {
			continue
		}
		note = joinNote(note, p)
	}
	if ses.Stale {
		note = joinNote(note, "stale price")
	}
	if len(ses.Notes) > 0 {
		note = joinNote(note, strings.Join(ses.Notes, "; "))
	}
	return note
}

func joinNote(a, b string) string {
	parts := noteParts(a)
	seen := make(map[string]bool, len(parts)+1)
	out := make([]string, 0, len(parts)+1)
	for _, p := range append(parts, noteParts(b)...) {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, "; ")
}

func noteParts(s string) []string {
	raw := strings.Split(s, ";")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func compactHints(hints []string) []string {
	if len(hints) == 0 {
		return nil
	}
	type group struct {
		msg      string
		sessions []string
	}
	groups := map[string]*group{}
	var passthrough []string
	for _, h := range hints {
		session, msg, ok := strings.Cut(h, ": ")
		if !ok || !strings.Contains(session, "/") || msg == "" {
			passthrough = append(passthrough, h)
			continue
		}
		g := groups[msg]
		if g == nil {
			g = &group{msg: msg}
			groups[msg] = g
		}
		g.sessions = appendUnique(g.sessions, session)
	}
	for _, g := range groups {
		sort.Strings(g.sessions)
		if len(g.sessions) == 1 {
			passthrough = append(passthrough, g.sessions[0]+": "+g.msg)
			continue
		}
		passthrough = append(passthrough, fmt.Sprintf("%d sessions: %s", len(g.sessions), g.msg))
	}
	sort.Strings(passthrough)
	return passthrough
}

func dash(s string) string {
	if s == "" || s == UnknownValue {
		return "--"
	}
	return s
}

func tokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// cacheTokens renders a row's cached input (read + write). The Input column is
// FRESH input only, so without this column two sessions with identical real
// usage but different cache hits look inexplicably different.
func cacheTokens(t TokenTotals) string {
	n := t.CacheRead + t.CacheCreate
	if n == 0 {
		return "--"
	}
	return tokens(n)
}

var currencySymbols = map[string]string{"USD": "$", "EUR": "EUR ", "GBP": "GBP ", "JPY": "JPY "}

// FormatMoney renders an amount with two decimals, switching to four below a
// dime. Two decimals hides real sub-cent costs ("never silently $0") and, worse,
// makes several small rows visibly disagree with their total — three $0.006 rows
// each round to $0.01 (reads as $0.03) but truly sum to $0.02. Four decimals
// under $0.10 lets small rows and their small total reconcile.
//
// Note: at or above a dime, 2-decimal rows can still be a cent off from a
// many-row total; that is inherent to cent display and not the confusing case
// this addresses.
func FormatMoney(v float64) string {
	if v > 0 && v < 0.10 {
		return fmt.Sprintf("%.4f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// costCell renders a cost for the /cost table, adding the ~ estimate prefix when
// the token count is still estimated (matching the header/badge vocabulary). The
// table's Confidence column is *price* confidence, so without this an exit-only
// reporter like Copilot mid-run — priced "exact" but not yet reported — reads as
// final even though its tokens are only the estimated floor.
func costCell(c *float64, currency, tokenConfidence string) string {
	s := cost(c, currency)
	if c != nil && tokenConfidence == Estimated {
		return "~" + s
	}
	return s
}

func cost(c *float64, currency string) string {
	if c == nil {
		return "--"
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		cur = "USD"
	}
	// Yen has no minor unit — never show decimals (matches the TUI's compactCost).
	if cur == "JPY" {
		return currencySymbols[cur] + fmt.Sprintf("%.0f", *c)
	}
	if sym, ok := currencySymbols[cur]; ok {
		return sym + FormatMoney(*c)
	}
	return FormatMoney(*c) + " " + cur
}
