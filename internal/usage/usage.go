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
	Fingerprint   string `json:"fingerprint,omitempty"`

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

// SessionTotal is one rolled-up row. Rows are phase-aware so a reported build
// event never replaces an estimated plan event.
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

	for _, e := range events {
		e.normalize()
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
	fmt.Fprintln(tw, "Agent\tPhase\tTool\tModel\tPriceModel\tInput\tOutput\tCost\tSource\tConfidence\tNote")
	for _, ses := range s.Sessions {
		note := ses.PriceNote
		if ses.Stale {
			note = joinNote(note, "stale price")
		}
		if len(ses.Notes) > 0 {
			note = joinNote(note, strings.Join(ses.Notes, "; "))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			ses.Agent, ses.Phase, dash(ses.Tool), dash(ses.Model), dash(ses.PriceModel),
			tokens(ses.Tokens.Input), tokens(ses.Tokens.Output+ses.Tokens.Reasoning),
			cost(ses.Cost, ses.Currency), dash(ses.PriceSource), dash(ses.PriceConf), dash(note))
	}
	fmt.Fprintf(tw, "Total\t\t\t\t\t%s\t%s\t%s\t\t\t%s\n",
		tokens(s.Tokens.Input), tokens(s.Tokens.Output+s.Tokens.Reasoning), cost(s.Cost, s.Currency), dash(s.Note))
	tw.Flush()
	if len(s.Hints) > 0 {
		b.WriteString("\nHints:\n")
		for _, h := range s.Hints {
			b.WriteString("- " + h + "\n")
		}
	}
	return b.String()
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
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

var currencySymbols = map[string]string{"USD": "$", "EUR": "EUR ", "GBP": "GBP ", "JPY": "JPY "}

func cost(c *float64, currency string) string {
	if c == nil {
		return "--"
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		cur = "USD"
	}
	if sym, ok := currencySymbols[cur]; ok {
		return fmt.Sprintf("%s%.2f", sym, *c)
	}
	return fmt.Sprintf("%.2f %s", *c, cur)
}
