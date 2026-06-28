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
	mk := func(model string, in, out int) Call {
		return Call{Provider: "copilot", SessionID: sid, ProjectPath: cwd, Model: model, InputTokens: in, OutputTokens: out, Timestamp: ts}
	}

	if len(shutdown.Data.ModelMetrics) > 0 { // per-model breakdown is the most accurate
		var calls []Call
		for model, mm := range shutdown.Data.ModelMetrics {
			calls = append(calls, mk(model, mm.Usage.InputTokens, mm.Usage.OutputTokens+mm.Usage.ReasoningTokens))
		}
		return calls
	}
	td := shutdown.Data.TokenDetails
	return []Call{mk(currentModel, td.Input.TokenCount, td.Output.TokenCount)}
}

func (r copilotReader) ReadForCWD(cwd string) ([]Call, error) {
	var calls []Call
	for _, dir := range r.sessionDirs() {
		calls = append(calls, r.parseSession(dir, cwd)...)
	}
	return calls, nil
}

func (r copilotReader) LatestModel(cwd string) (string, error) {
	dirs := r.sessionDirs()
	// newest session dir first
	sort := func() {
		for i := 1; i < len(dirs); i++ {
			for j := i; j > 0; j-- {
				if dirModTime(dirs[j]).After(dirModTime(dirs[j-1])) {
					dirs[j], dirs[j-1] = dirs[j-1], dirs[j]
				}
			}
		}
	}
	sort()
	for _, dir := range dirs {
		if copilotCWD(dir) != cwd {
			continue
		}
		if calls := r.parseSession(dir, cwd); len(calls) > 0 {
			return calls[len(calls)-1].Model, nil
		}
	}
	return "", nil
}

func dirModTime(dir string) time.Time {
	if fi, err := os.Stat(filepath.Join(dir, "events.jsonl")); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func init() { Register("copilot", func() Reader { return Copilot("") }) }
