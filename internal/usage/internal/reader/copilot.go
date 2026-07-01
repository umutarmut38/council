package reader

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// copilotReader reads the GitHub Copilot CLI's sessions from
// ~/.copilot/session-state/<id>/. Each session dir has workspace.yaml (carrying
// the cwd) and events.jsonl; the session.shutdown event reports cumulative,
// per-model token usage — real reported counts, not estimates.
type copilotReader struct{ root string }

// Copilot returns a reader for the Copilot CLI. Empty path uses
// ~/.copilot/session-state.
func Copilot(root string) Reader {
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".copilot", "session-state")
	}
	return copilotReader{root: root}
}

func (copilotReader) Name() string { return "copilot" }

// copilotCWD reads `cwd:` out of a session's workspace.yaml (a one-line scan
// avoids a YAML dependency for a single scalar field).
func copilotCWD(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "workspace.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "cwd:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(s, "cwd:")), `"'`)
		}
	}
	return ""
}

type copilotUsage struct {
	InputTokens     int `json:"inputTokens"`
	OutputTokens    int `json:"outputTokens"`
	ReasoningTokens int `json:"reasoningTokens"`
	CacheReadTokens int `json:"cacheReadTokens"`
}

type copilotEvent struct {
	Type string `json:"type"`
	Data struct {
		CurrentModel string `json:"currentModel"`
		ModelMetrics map[string]struct {
			Usage copilotUsage `json:"usage"`
		} `json:"modelMetrics"`
		TokenDetails struct {
			Input struct {
				TokenCount int `json:"tokenCount"`
			} `json:"input"`
			CacheRead struct {
				TokenCount int `json:"tokenCount"`
			} `json:"cache_read"`
			Output struct {
				TokenCount int `json:"tokenCount"`
			} `json:"output"`
		} `json:"tokenDetails"`
	} `json:"data"`
}

func (r copilotReader) sessionDirs() []string {
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(r.root, e.Name()))
		}
	}
	return out
}

// parseSession returns one Call per model used in the session (from the
// session.shutdown metrics), or nil when the session isn't in cwd or hasn't
// reported totals yet (still running → council's estimated floor covers it).
func (r copilotReader) parseSession(dir, cwd string) []Call {
	if copilotCWD(dir) != cwd {
		return nil
	}
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var shutdown *copilotEvent
	currentModel := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var e copilotEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Data.CurrentModel != "" {
			currentModel = e.Data.CurrentModel
		}
		if e.Type == "session.shutdown" {
			ec := e
			shutdown = &ec
		}
	}
	if shutdown == nil {
		return nil
	}

	var ts time.Time
	if fi, e := os.Stat(path); e == nil {
		ts = fi.ModTime()
	}
	sid := filepath.Base(dir)
	// Copilot's modelMetrics.inputTokens is cache-INCLUSIVE: it equals the fresh
	// input plus cacheReadTokens (verified: 17902 fresh + 17408 cached = 35310).
	// Record only the fresh input and keep the cached part separate (priced at the
	// cache-read rate) — matching the codex/claude convention — so re-sent context
	// isn't double-charged at the full input rate.
	mk := func(model string, in, out, reasoning, cacheRead int) Call {
		// cacheRead is a subset of the cache-inclusive input, so clamp it into
		// [0, in]. A malformed record with cacheRead > in would otherwise leave
		// fresh+cache greater than the reported input and overcharge; clamping
		// keeps fresh (in-cacheRead) >= 0 and fresh+cache == in.
		if cacheRead < 0 {
			cacheRead = 0
		}
		if cacheRead > in {
			cacheRead = in
		}
		return Call{Provider: "copilot", SessionID: sid, CallID: sid + ":" + model, ProjectPath: cwd, Model: model, InputTokens: in - cacheRead, CacheRead: cacheRead, OutputTokens: out, ReasoningTokens: reasoning, Timestamp: ts}
	}

	if len(shutdown.Data.ModelMetrics) > 0 { // per-model breakdown is the most accurate
		var calls []Call
		for model, mm := range shutdown.Data.ModelMetrics {
			calls = append(calls, mk(model, mm.Usage.InputTokens, mm.Usage.OutputTokens, mm.Usage.ReasoningTokens, mm.Usage.CacheReadTokens))
		}
		return calls
	}
	// tokenDetails.input is already cache-EXCLUSIVE (fresh); pass it as an inclusive
	// total plus cache_read so mk's subtraction leaves the fresh input intact.
	td := shutdown.Data.TokenDetails
	return []Call{mk(currentModel, td.Input.TokenCount+td.CacheRead.TokenCount, td.Output.TokenCount, 0, td.CacheRead.TokenCount)}
}

func (r copilotReader) ReadForCWD(cwd string) ([]Call, error) {
	var calls []Call
	for _, dir := range r.sessionDirs() {
		calls = append(calls, r.parseSession(dir, cwd)...)
	}
	return calls, nil
}

func init() { Register("copilot", func() Reader { return Copilot("") }) }
