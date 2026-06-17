package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/umutarmut38/council/internal/fsperm"
	runstore "github.com/umutarmut38/council/internal/session"
)

// Artifact filenames agents are told to write into their worktree (their cwd)
// during each phase. Fixed names mean a single broadcast prompt works for all.
const (
	PlanArtifact = "PLAN.md"
	VoteArtifact = "VOTE.md"
)

// Run is one council run: a directory holding the issue, plans, votes, result.
type Run struct {
	RootDir string
	Stamp   string
	Dir     string
}

type RunSummary struct {
	Stamp         string
	Dir           string
	PromptPreview string
	Winner        string
	Plans         []string
	Votes         []string
	Reviews       []string
	Diffs         []string
	Agents        []string
}

// RunState records the currently active orchestration phase so a fresh TUI can
// resume inside a phase instead of just reopening the run context.
type RunState struct {
	Phase        configPhase `json:"phase,omitempty"`
	Participants []string    `json:"participants,omitempty"`
	PromptSent   bool        `json:"prompt_sent,omitempty"`
	UpdatedAt    string      `json:"updated_at,omitempty"`
}

// configPhase avoids importing internal/config into run.go just for JSON
// persistence. Controller converts to/from config.Phase at the boundary.
type configPhase string

// NewRun creates a fresh timestamped run directory. Run IDs get a numeric
// suffix on collision so two runs started in the same second stay separate.
func NewRun(rootDir string) (*Run, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}
	dir, stamp, err := runstore.CreateRunDir(rootDir)
	if err != nil {
		return nil, err
	}
	r := &Run{RootDir: rootDir, Stamp: stamp, Dir: dir}
	for _, d := range []string{r.PlansDir(), r.VotesDir()} {
		if err := os.MkdirAll(d, fsperm.Dir()); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// OpenRun opens an existing run by stamp, or the latest run if stamp is empty.
func OpenRun(rootDir, stamp string) (*Run, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}
	if strings.TrimSpace(stamp) == "" {
		return LatestRun(rootDir)
	}
	dir := filepath.Join(rootDir, stamp)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("run %q not found in %s", stamp, rootDir)
	}
	return &Run{RootDir: rootDir, Stamp: stamp, Dir: dir}, nil
}

// LatestRun returns the most recent run directory under rootDir.
func LatestRun(rootDir string) (*Run, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("no runs in %s: %w", rootDir, err)
	}
	stamps := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			stamps = append(stamps, e.Name())
		}
	}
	if len(stamps) == 0 {
		return nil, fmt.Errorf("no runs in %s", rootDir)
	}
	sort.Strings(stamps)
	stamp := stamps[len(stamps)-1]
	return &Run{RootDir: rootDir, Stamp: stamp, Dir: filepath.Join(rootDir, stamp)}, nil
}

func ListRuns(rootDir string, limit int) ([]RunSummary, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("no runs in %s: %w", rootDir, err)
	}
	stamps := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			stamps = append(stamps, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stamps)))
	if limit > 0 && len(stamps) > limit {
		stamps = stamps[:limit]
	}
	summaries := make([]RunSummary, 0, len(stamps))
	for _, stamp := range stamps {
		summary, err := SummarizeRun(rootDir, stamp)
		if err == nil {
			summaries = append(summaries, summary)
		}
	}
	return summaries, nil
}

func SummarizeRun(rootDir string, stamp string) (RunSummary, error) {
	run, err := OpenRun(rootDir, stamp)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{
		Stamp:         run.Stamp,
		Dir:           run.Dir,
		PromptPreview: promptPreview(run),
		Plans:         planNames(run.PlansDir()),
		Votes:         voteNames(run.VotesDir()),
		Reviews:       suffixNames(run.BuildsDir(), ".review.md"),
		Diffs:         diffNames(run.BuildsDir()),
	}
	summary.Winner = resultWinner(run.ResultPath())
	agentSet := map[string]bool{}
	for _, group := range [][]string{summary.Plans, summary.Votes, summary.Reviews, summary.Diffs} {
		for _, name := range group {
			if !strings.HasPrefix(name, "plan-") && !strings.HasPrefix(name, "diff-") {
				agentSet[name] = true
			}
		}
	}
	for agent := range agentSet {
		summary.Agents = append(summary.Agents, agent)
	}
	sort.Strings(summary.Agents)
	return summary, nil
}

func promptPreview(run *Run) string {
	for _, path := range []string{run.IssuePath(), filepath.Join(run.Dir, "prompt.txt")} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := strings.Join(strings.Fields(string(data)), " ")
		if len([]rune(text)) > 100 {
			return string([]rune(text)[:99]) + "~"
		}
		return text
	}
	return ""
}

func resultWinner(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload struct {
		Winner string `json:"winner_agent"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Winner
}

// ResultLetter returns the winning plan's anonymized ballot letter from result.json,
// or "" when no vote result exists yet. Kept here so the TUI never parses result.json
// itself.
func (r *Run) ResultLetter() string {
	data, err := os.ReadFile(r.ResultPath())
	if err != nil {
		return ""
	}
	var payload struct {
		Letter string `json:"winner_letter"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	return payload.Letter
}

func markdownNames(dir string) []string {
	return suffixNames(dir, ".md")
}

// planNames lists agent plans, skipping the .orig.md backups kept by /refine.
func planNames(dir string) []string {
	names := markdownNames(dir)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".orig") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// diffNames lists agent build diffs, skipping the anonymized diff-<letter>
// copies written for reviewers.
func diffNames(dir string) []string {
	names := suffixNames(dir, ".diff")
	out := make([]string, 0, len(names))
	for _, name := range names {
		if anonDiffName.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func voteNames(dir string) []string {
	names := markdownNames(dir)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "plan-") || name == "result" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func suffixNames(dir string, suffix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), suffix))
	}
	sort.Strings(names)
	return names
}

func LoadTranscripts(runDir string, agentNames []string) map[string]string {
	bySafeName := map[string]string{}
	for _, name := range agentNames {
		bySafeName[safeName(name)] = name
	}

	root := filepath.Join(runDir, "transcripts")
	paths := make([]string, 0)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".txt") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)

	transcripts := map[string]string{}
	for _, path := range paths {
		safe := strings.TrimSuffix(filepath.Base(path), ".txt")
		name, ok := bySafeName[safe]
		if !ok {
			name = safe
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		phase := "session"
		if rel, relErr := filepath.Rel(root, path); relErr == nil {
			dir := filepath.Dir(rel)
			if dir != "." {
				phase = filepath.ToSlash(dir)
			}
		}
		if transcripts[name] != "" {
			transcripts[name] += "\n"
		}
		transcripts[name] += fmt.Sprintf("--- transcript: %s ---\n%s", phase, strings.TrimRight(string(data), "\n"))
	}
	return transcripts
}

func (r *Run) PlansDir() string    { return filepath.Join(r.Dir, "plans") }
func (r *Run) VotesDir() string    { return filepath.Join(r.Dir, "votes") }
func (r *Run) BuildsDir() string   { return filepath.Join(r.Dir, "builds") }
func (r *Run) IssuePath() string   { return filepath.Join(r.Dir, "issue.md") }
func (r *Run) ResultPath() string  { return filepath.Join(r.VotesDir(), "result.json") }
func (r *Run) SummaryPath() string { return filepath.Join(r.VotesDir(), "result.md") }
func (r *Run) StatePath() string   { return filepath.Join(r.Dir, "state.json") }
func (r *Run) VoteRefsPath() string {
	return filepath.Join(r.VotesDir(), "plan-assignments.json")
}
func (r *Run) ReviewRefsPath() string {
	return filepath.Join(r.BuildsDir(), "review-assignments.json")
}

func (r *Run) PlanPath(agent string) string {
	return filepath.Join(r.PlansDir(), safeName(agent)+".md")
}
func (r *Run) VotePath(agent string) string {
	return filepath.Join(r.VotesDir(), safeName(agent)+".md")
}

// ---- review/build-judging paths ----

func (r *Run) BuildResultPath() string { return filepath.Join(r.BuildsDir(), "result.json") }

// BuildDiffPath is the captured diff of an agent's build implementation.
func (r *Run) BuildDiffPath(agent string) string {
	return filepath.Join(r.BuildsDir(), safeName(agent)+".diff")
}

// CheckLogPath is the output of the review check command for an agent's build.
func (r *Run) CheckLogPath(agent string) string {
	return filepath.Join(r.BuildsDir(), safeName(agent)+".check.log")
}

// ReviewPath is where a reviewer writes its ranked review of the builds.
func (r *Run) ReviewPath(agent string) string {
	return filepath.Join(r.BuildsDir(), safeName(agent)+".review.md")
}

// AnonDiffPath is an anonymized copy of a build diff that reviewers read.
func (r *Run) AnonDiffPath(letter string) string {
	return filepath.Join(r.BuildsDir(), "diff-"+strings.ToLower(letter)+".diff")
}

// SaveBaseSHA records the commit the build worktrees branched from, so the diff
// and patch can be computed later (even from a separate `council review` run).
func (r *Run) SaveBaseSHA(sha string) error {
	if err := os.MkdirAll(r.BuildsDir(), fsperm.Dir()); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.BuildsDir(), "base.txt"), []byte(strings.TrimSpace(sha)+"\n"), fsperm.File())
}

func (r *Run) BaseSHA() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.BuildsDir(), "base.txt"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// AnonPlanPath is where an anonymized copy of a plan is written for voting, so
// reviewers read "plan-A.md" rather than a file named after the author.
func (r *Run) AnonPlanPath(letter string) string {
	return filepath.Join(r.VotesDir(), "plan-"+strings.ToLower(letter)+".md")
}

func (r *Run) SaveIssue(text string) error {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(r.IssuePath(), []byte(text), fsperm.File())
}

func (r *Run) Issue() (string, error) {
	data, err := os.ReadFile(r.IssuePath())
	if err != nil {
		return "", fmt.Errorf("read issue: %w", err)
	}
	return string(data), nil
}

func (r *Run) SaveState(phase string, participants []string, promptSent bool) error {
	if err := os.MkdirAll(r.Dir, fsperm.Dir()); err != nil {
		return err
	}
	state := RunState{
		Phase:        configPhase(phase),
		Participants: append([]string(nil), participants...),
		PromptSent:   promptSent,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.StatePath(), data, fsperm.File())
}

func (r *Run) ClearState() error {
	err := os.Remove(r.StatePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (r *Run) LoadState() (RunState, error) {
	data, err := os.ReadFile(r.StatePath())
	if err != nil {
		return RunState{}, err
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return RunState{}, err
	}
	return state, nil
}

// ---- timings and adoption metadata ----

// PhaseTiming records when a phase ran, who participated, and how many pane
// restarts it needed — cheap, vendor-neutral signals for tuning a council.
type PhaseTiming struct {
	Phase        string   `json:"phase"`
	Start        string   `json:"start,omitempty"`
	End          string   `json:"end,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Restarts     int      `json:"restarts,omitempty"`
}

func (r *Run) TimingsPath() string { return filepath.Join(r.Dir, "timings.json") }

func (r *Run) LoadTimings() map[string]*PhaseTiming {
	timings := map[string]*PhaseTiming{}
	data, err := os.ReadFile(r.TimingsPath())
	if err == nil {
		_ = json.Unmarshal(data, &timings)
	}
	return timings
}

func (r *Run) saveTimings(timings map[string]*PhaseTiming) error {
	data, err := json.MarshalIndent(timings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.TimingsPath(), data, fsperm.File())
}

// RecordPhaseStart stamps a phase's start time (kept if already set, so a
// resumed phase keeps its original start).
func (r *Run) RecordPhaseStart(phase string, participants []string) {
	timings := r.LoadTimings()
	t, ok := timings[phase]
	if !ok {
		t = &PhaseTiming{Phase: phase}
		timings[phase] = t
	}
	if t.Start == "" {
		t.Start = time.Now().Format(time.RFC3339)
	}
	if len(participants) > 0 {
		t.Participants = participants
	}
	_ = r.saveTimings(timings)
}

// RecordPhaseEnd stamps a phase's end time.
func (r *Run) RecordPhaseEnd(phase string) {
	timings := r.LoadTimings()
	t, ok := timings[phase]
	if !ok {
		t = &PhaseTiming{Phase: phase}
		timings[phase] = t
	}
	t.End = time.Now().Format(time.RFC3339)
	_ = r.saveTimings(timings)
}

// RecordRestart counts a pane restart against the current phase.
func (r *Run) RecordRestart(phase string) {
	if phase == "" {
		phase = "session"
	}
	timings := r.LoadTimings()
	t, ok := timings[phase]
	if !ok {
		t = &PhaseTiming{Phase: phase}
		timings[phase] = t
	}
	t.Restarts++
	_ = r.saveTimings(timings)
}

// AdoptionPath records which build the user applied to the working tree.
func (r *Run) AdoptionPath() string { return filepath.Join(r.Dir, "adopted.json") }

func (r *Run) RecordAdoption(agent string, files []string) error {
	payload := struct {
		Agent     string   `json:"agent"`
		Files     []string `json:"files,omitempty"`
		AdoptedAt string   `json:"adopted_at"`
	}{agent, files, time.Now().Format(time.RFC3339)}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.AdoptionPath(), data, fsperm.File())
}

func (r *Run) Adoption() (agent string, ok bool) {
	data, err := os.ReadFile(r.AdoptionPath())
	if err != nil {
		return "", false
	}
	var payload struct {
		Agent string `json:"agent"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Agent == "" {
		return "", false
	}
	return payload.Agent, true
}

func (r *Run) SaveVoteRefs(refs []PlanRef) error {
	return saveRefs(r.VoteRefsPath(), refs)
}

func (r *Run) LoadVoteRefs() ([]PlanRef, error) {
	return loadRefs(r.VoteRefsPath())
}

func (r *Run) SaveReviewRefs(refs []PlanRef) error {
	return saveRefs(r.ReviewRefsPath(), refs)
}

func (r *Run) LoadReviewRefs() ([]PlanRef, error) {
	return loadRefs(r.ReviewRefsPath())
}

func saveRefs(path string, refs []PlanRef) error {
	if err := os.MkdirAll(filepath.Dir(path), fsperm.Dir()); err != nil {
		return err
	}
	data, err := json.MarshalIndent(refs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, fsperm.File())
}

func loadRefs(path string) ([]PlanRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var refs []PlanRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

// CollectArtifact copies each agent's worktree artifact (PLAN.md / VOTE.md) into
// the run dir and returns agent -> contents. Agents that produced nothing are
// reported in missing.
func CollectArtifact(worktrees map[string]string, filename string, destFor func(agent string) string) (found map[string]string, missing []string, err error) {
	found = map[string]string{}
	for agent, wtPath := range worktrees {
		data, readErr := os.ReadFile(filepath.Join(wtPath, filename))
		if readErr != nil {
			missing = append(missing, agent)
			continue
		}
		if writeErr := os.WriteFile(destFor(agent), data, fsperm.File()); writeErr != nil {
			return found, missing, writeErr
		}
		found[agent] = string(data)
	}
	sort.Strings(missing)
	return found, missing, nil
}

// CollectRunArtifacts reads artifacts that agents wrote directly into the run
// directory.
func CollectRunArtifacts(agents []string, pathFor func(agent string) string) (found map[string]string, missing []string, err error) {
	found = map[string]string{}
	for _, agent := range agents {
		data, readErr := os.ReadFile(pathFor(agent))
		if readErr != nil {
			missing = append(missing, agent)
			continue
		}
		found[agent] = string(data)
	}
	sort.Strings(missing)
	return found, missing, nil
}

// WriteResult persists the tally as JSON and a readable Markdown summary.
func (r *Run) WriteResult(res Result, refs []PlanRef) error {
	payload := struct {
		Winner      string         `json:"winner_agent"`
		WinnerPlan  string         `json:"winner_letter"`
		Points      map[string]int `json:"points"`
		Firsts      map[string]int `json:"first_place"`
		Assignments []PlanRef      `json:"plans"`
		Ballots     []Ballot       `json:"ballots"`
	}{res.WinnerAgent, res.WinnerLetter, res.Points, res.Firsts, refs, res.Ballots}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(r.ResultPath(), data, fsperm.File()); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Council result\n\nWinner: **%s** (Plan %s)\n\n## Tally (Borda)\n\n", res.WinnerAgent, res.WinnerLetter)
	for _, ref := range refs {
		fmt.Fprintf(&b, "- Plan %s — %s: %d points, %d first-place\n", ref.Letter, ref.Agent, res.Points[ref.Letter], res.Firsts[ref.Letter])
	}
	return os.WriteFile(r.SummaryPath(), []byte(b.String()), fsperm.File())
}

func safeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "agent"
	}
	return b.String()
}
