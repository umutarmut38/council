// Package usage is council's local, provider-agnostic token/cost ledger. It
// records what council can observe locally — prompt sizes and transcript sizes
// per agent session — as append-only JSONL events under a run, and aggregates
// them per session (council member) so two instances of the same CLI stay
// distinct. Cost is derived later via the Pricer facade; usage is the raw fact.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/umutarmut38/council/internal/fsperm"
)

// SchemaVersion tags every event so a future format change can be detected.
const SchemaVersion = 1

// Confidence labels how trustworthy an event's token counts are, worst to best.
const (
	Unknown   = "unknown"   // usage metadata but nothing to size from
	Estimated = "estimated" // council estimated tokens from local text (chars/4)
	Reported  = "reported"  // real token counts read from the tool's session file
	Exact     = "exact"     // the tool/CLI reported a cost figure directly
)

// Event sources.
const (
	SourcePrompt     = "council.prompt"     // a prompt council sent (input)
	SourceTranscript = "council.transcript" // an agent transcript council captured (output)
	SourceProvider   = "provider.session"   // reconciled from a tool's own session file
	SourceCodeburn   = "codeburn"           // imported via the codeburn CLI
)

// Event is one recorded usage fact. Agent is the council member name, which is
// unique per pane and therefore the per-session key. No cost field: cost is
// derived from current pricing at view time, so price changes apply retroactively.
type Event struct {
	SchemaVersion int    `json:"schema_version"`
	At            string `json:"at"`
	RunID         string `json:"run_id"`
	Agent         string `json:"agent"`
	Phase         string `json:"phase,omitempty"`
	Source        string `json:"source"`
	Confidence    string `json:"confidence"`
	Model         string `json:"model,omitempty"`
	PriceProfile  string `json:"price_profile,omitempty"`
	Tool          string `json:"tool,omitempty"` // which tool's reader can reconcile this agent
	CWD           string `json:"cwd,omitempty"`  // correlation key for provider session files
	InputChars    int    `json:"input_chars,omitempty"`
	InputTokens   int    `json:"input_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
}

// EstimateTokens is the local input/output sizer used when no tool reports real
// counts. ponytail: chars/4 heuristic — swap in a real tokenizer if the
// estimate-vs-reported delta (shown in /cost) proves too wide.
func EstimateTokens(s string) int { return len(s) / 4 }

// Ledger appends events to <runDir>/usage/events.jsonl and keeps a live
// per-agent token tally for the header/border. It is owned by the TUI model and
// touched only from Update, so it needs no locking.
type Ledger struct {
	f      *os.File
	Tokens map[string]TokenPair // per agent, in-memory, for live display
}

// TokenPair is an input/output token count.
type TokenPair struct{ Input, Output int }

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
	return &Ledger{f: f, Tokens: map[string]TokenPair{}}, nil
}

// Record stamps and appends an event, then updates the live tally.
func (l *Ledger) Record(e Event) error {
	e.SchemaVersion = SchemaVersion
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(append(b, '\n')); err != nil {
		return err
	}
	t := l.Tokens[e.Agent]
	t.Input += e.InputTokens
	t.Output += e.OutputTokens
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

// Append records events to a run's ledger without holding a handle open. Used by
// the TUI, where each record is self-contained on the Update goroutine and the
// extra open/close is cheap at prompt/transcript frequency.
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

// LoadEvents reads a run's events.jsonl. Missing file → no events, no error.
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
		out = append(out, e)
	}
	return out, sc.Err()
}

// LoadRunsSince loads events from every run under rootDir, keeping only events
// timestamped at or after cutoff (zero cutoff = all). Used by `council cost
// --since` for cross-run totals; a missing/old run simply contributes nothing.
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

// SessionTotal is one council session's (one pane's) rolled-up usage.
type SessionTotal struct {
	Agent        string
	Model        string
	PriceProfile string
	Confidence   string
	Input        int
	Output       int
	CostUSD      *float64 // nil until priced via Summary.Price
}

// Summary is a run's usage aggregated per session.
type Summary struct {
	RunID    string
	Sessions []SessionTotal
	Input    int
	Output   int
	CostUSD  *float64
	Currency string
}

var confTier = map[string]int{Unknown: 0, Estimated: 1, Reported: 2, Exact: 3}

// Aggregate rolls events up per session, keyed so two instances of the same CLI
// stay separate. Two-level keying keeps the money math honest:
//
//   - The reported-beats-estimated decision is made per (agent, cwd): a tool's
//     reported total for one working directory supersedes the estimates for that
//     same directory only. Estimates for a directory that never reconciled (an
//     ambiguous shared cwd, or a tool with no reader) are kept, so a partial
//     reconciliation can't erase real spend.
//   - Token totals accumulate per (agent, model) so a session that used several
//     models (e.g. Copilot switching models) is priced at each model's own rate
//     rather than billing everything at one. Rows display per agent+model; a
//     single-model agent is one row, as before.
//
// A row's confidence is its weakest contributing tier, so a partly-estimated
// agent never shows as fully reported.
func Aggregate(events []Event) Summary {
	type acKey struct{ agent, cwd string }
	byAC := map[acKey][]Event{}
	for _, e := range events {
		byAC[acKey{e.Agent, e.CWD}] = append(byAC[acKey{e.Agent, e.CWD}], e)
	}

	type amKey struct{ agent, model string }
	totals := map[amKey]*SessionTotal{}
	for k, evs := range byAC {
		best := ""
		for _, e := range evs {
			if confTier[e.Confidence] > confTier[best] {
				best = e.Confidence
			}
		}
		for _, e := range evs {
			if e.Confidence != best {
				continue // a reported total for this cwd supersedes its estimates
			}
			mk := amKey{k.agent, e.Model}
			st := totals[mk]
			if st == nil {
				st = &SessionTotal{Agent: k.agent, Model: e.Model, Confidence: best}
				totals[mk] = st
			}
			st.Input += e.InputTokens
			st.Output += e.OutputTokens
			if e.PriceProfile != "" {
				st.PriceProfile = e.PriceProfile
			}
			if confTier[best] < confTier[st.Confidence] {
				st.Confidence = best // weakest tier wins for the row label
			}
		}
	}

	out := Summary{RunID: runID(events)}
	for _, st := range totals {
		out.Sessions = append(out.Sessions, *st)
		out.Input += st.Input
		out.Output += st.Output
	}
	sort.Slice(out.Sessions, func(i, j int) bool {
		if out.Sessions[i].Agent != out.Sessions[j].Agent {
			return out.Sessions[i].Agent < out.Sessions[j].Agent
		}
		return out.Sessions[i].Model < out.Sessions[j].Model
	})
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
// /cost view. Cost shows "—" until pricing fills CostUSD.
func FormatTable(s Summary) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "Agent\tModel\tInput\tOutput\tCost\tConfidence")
	for _, ses := range s.Sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			ses.Agent, dash(ses.Model), tokens(ses.Input), tokens(ses.Output), cost(ses.CostUSD, s.Currency), ses.Confidence)
	}
	fmt.Fprintf(tw, "Total\t\t%s\t%s\t%s\t\n", tokens(s.Input), tokens(s.Output), cost(s.CostUSD, s.Currency))
	tw.Flush()
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func tokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

var currencySymbols = map[string]string{"USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥"}

func cost(c *float64, currency string) string {
	if c == nil {
		return "—"
	}
	if sym, ok := currencySymbols[strings.ToUpper(currency)]; ok {
		return fmt.Sprintf("%s%.2f", sym, *c)
	}
	if currency == "" {
		return fmt.Sprintf("$%.2f", *c)
	}
	return fmt.Sprintf("%.2f %s", *c, strings.ToUpper(currency)) // unknown code: amount + code
}
