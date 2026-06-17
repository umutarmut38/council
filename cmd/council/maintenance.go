package main

// Run lifecycle and project-integration commands: clean-runs, stack presets,
// run reports, scorecards, the batch issue queue, and GitHub helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/umutarmut38/council/internal/cmdrun"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/fsperm"
	"github.com/umutarmut38/council/internal/orchestrate"
)

// councilCleanRuns prunes old run directories:
// council clean-runs [--keep N] [--dry-run] [--yes]
func councilCleanRuns(args []string) error {
	fs, noLocal := newOrchFlagSet("council clean-runs")
	keep := fs.Int("keep", 10, "number of most recent runs to keep")
	dryRun := fs.Bool("dry-run", false, "list what would be removed")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	rootDir := cfg.Sessions.RootDir
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return fmt.Errorf("no runs in %s: %w", rootDir, err)
	}
	stamps := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			stamps = append(stamps, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stamps)))
	if *keep < 0 {
		*keep = 0
	}
	if len(stamps) <= *keep {
		fmt.Printf("Nothing to prune: %d run(s), keeping %d.\n", len(stamps), *keep)
		return nil
	}
	doomed := stamps[*keep:]
	fmt.Printf("Would remove %d run(s) (keeping the %d most recent):\n", len(doomed), *keep)
	for _, stamp := range doomed {
		fmt.Printf("  %s\n", filepath.Join(rootDir, stamp))
	}
	if *dryRun {
		return nil
	}
	if !*yes && cfg.Policy.ConfirmDestructive() {
		if !confirmPrompt("Remove them permanently?") {
			return errors.New("aborted")
		}
	}
	for _, stamp := range doomed {
		if err := os.RemoveAll(filepath.Join(rootDir, stamp)); err != nil {
			return err
		}
	}
	fmt.Printf("Removed %d run(s).\n", len(doomed))
	return nil
}

// stackPresets maps a detected stack to a sensible review.check_command.
var stackPresets = map[string][]string{
	"go":     {"go", "test", "./..."},
	"node":   {"npm", "test"},
	"rust":   {"cargo", "test"},
	"python": {"pytest"},
}

// detectStack inspects marker files in dir and returns the stack name and its
// check command ("" when nothing was recognized).
func detectStack(dir string) (string, []string) {
	markers := []struct {
		file  string
		stack string
	}{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
		{"setup.py", "python"},
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return m.stack, append([]string(nil), stackPresets[m.stack]...)
		}
	}
	return "", nil
}

// councilStack manages the per-repo review gate:
// council stack detect | council stack set <go|node|rust|python>
func councilStack(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: council stack detect | council stack set <go|node|rust|python>")
	}
	cwd, _ := os.Getwd()
	switch args[0] {
	case "detect":
		stack, cmd := detectStack(cwd)
		if stack == "" {
			return errors.New("no known stack detected (looked for go.mod, package.json, Cargo.toml, pyproject.toml, setup.py)")
		}
		fmt.Printf("Detected %s — review gate: %s\n", stack, strings.Join(cmd, " "))
		return writeStackToLocalConfig(cmd)
	case "set":
		if len(args) < 2 {
			return errors.New("usage: council stack set <go|node|rust|python>")
		}
		cmd, ok := stackPresets[strings.ToLower(args[1])]
		if !ok {
			names := make([]string, 0, len(stackPresets))
			for name := range stackPresets {
				names = append(names, name)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown stack %q (known: %s)", args[1], strings.Join(names, ", "))
		}
		return writeStackToLocalConfig(cmd)
	default:
		return fmt.Errorf("unknown stack command %q (detect | set)", args[0])
	}
}

// writeStackToLocalConfig merges review.check_command into the repo-local
// .council.yaml (creating it when missing) and trusts the result — the user
// just authored it.
func writeStackToLocalConfig(cmd []string) error {
	cwd, _ := os.Getwd()
	repoRoot, err := orchestrate.DetectRepoRoot(cwd)
	if err != nil {
		return fmt.Errorf("council stack writes .council.yaml at the repo root: %w", err)
	}
	path := filepath.Join(repoRoot, ".council.yaml")
	if existing := config.FindLocalConfig(); existing != "" {
		path = existing
	}

	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	review, _ := doc["review"].(map[string]any)
	if review == nil {
		review = map[string]any{}
	}
	review["check_command"] = cmd
	doc["review"] = review

	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if err := config.TrustLocalConfig(path, data); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record trust: %v\n", err)
	}
	fmt.Printf("Set review.check_command = %v in %s (trusted)\n", cmd, path)
	return nil
}

// councilReport writes report.md for a run; --post comments it on the GitHub
// issue (requires the run to have been started with --issue and gh installed).
func councilReport(args []string) error {
	fs, noLocal := newOrchFlagSet("council report")
	post := fs.Int("post", 0, "post the report as a comment on this GitHub issue number (via gh)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	run, err := orchestrate.OpenRun(cfg.Sessions.RootDir, fs.Arg(0))
	if err != nil {
		return err
	}
	warnArtifactSecrets(run.Dir)
	path, err := orchestrate.WriteReport(run)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", path)
	if *post > 0 {
		_, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "gh", Args: []string{"issue", "comment", fmt.Sprint(*post), "--body-file", path}})
		if err != nil {
			return fmt.Errorf("gh issue comment: %w", err)
		}
		fmt.Printf("Posted report to issue #%d\n", *post)
	}
	return nil
}

// councilPR opens a pull request from a build branch via gh:
// council pr [run] [agent]
func councilPR(args []string) error {
	fs, noLocal := newOrchFlagSet("council pr")
	positional, err := parseWithTrailingFlags(fs, args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		return err
	}
	runArg, agentArg := "", ""
	switch len(positional) {
	case 0:
	case 1:
		if err := ctrl.UseRun(positional[0]); err == nil {
			runArg = positional[0]
		} else {
			agentArg = positional[0]
		}
	default:
		runArg, agentArg = positional[0], positional[1]
	}
	if ctrl.Run() == nil {
		if err := ctrl.UseRun(runArg); err != nil {
			return err
		}
	}
	agentName := strings.TrimSpace(agentArg)
	if agentName == "" {
		agentName, err = ctrl.BuildWinner()
		if err != nil {
			return err
		}
	}
	branch := fmt.Sprintf("council/%s/%s", agentName, ctrl.Run().Stamp)

	warnArtifactSecrets(ctrl.Run().Dir)

	// Make sure the report exists; it becomes the PR body.
	reportPath, err := orchestrate.WriteReport(ctrl.Run())
	if err != nil {
		return err
	}
	if _, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"push", "-u", "origin", branch}}); err != nil {
		return fmt.Errorf("git push %s: %w", branch, err)
	}
	title := fmt.Sprintf("council: %s implementation (run %s)", agentName, ctrl.Run().Stamp)
	out, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "gh", Args: []string{"pr", "create", "--head", branch, "--title", title, "--body-file", reportPath}})
	if err != nil {
		return fmt.Errorf("gh pr create: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

// ---- scorecard ----

type score struct {
	Runs       int
	PlanWins   int
	BuildWins  int
	ChecksRun  int
	ChecksPass int
	Plans      int
	Votes      int
	Reviews    int
	Adopted    int
}

// councilScorecard aggregates agent performance across all runs on disk.
func councilScorecard(args []string) error {
	fs, noLocal := newOrchFlagSet("council scorecard")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	summaries, err := orchestrate.ListRuns(cfg.Sessions.RootDir, 0)
	if err != nil {
		return err
	}
	scores := map[string]*score{}
	get := func(agent string) *score {
		s, ok := scores[agent]
		if !ok {
			s = &score{}
			scores[agent] = s
		}
		return s
	}
	for _, summary := range summaries {
		run, err := orchestrate.OpenRun(cfg.Sessions.RootDir, summary.Stamp)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		mark := func(agent string) {
			if !seen[agent] {
				seen[agent] = true
				get(agent).Runs++
			}
		}
		for _, a := range summary.Plans {
			mark(a)
			get(a).Plans++
		}
		for _, a := range summary.Votes {
			mark(a)
			get(a).Votes++
		}
		for _, a := range summary.Reviews {
			mark(a)
			get(a).Reviews++
		}
		if summary.Winner != "" {
			mark(summary.Winner)
			get(summary.Winner).PlanWins++
		}
		for _, a := range summary.Diffs {
			mark(a)
			if log, err := os.ReadFile(run.CheckLogPath(a)); err == nil {
				get(a).ChecksRun++
				if strings.HasSuffix(strings.TrimSpace(string(log)), "PASS") {
					get(a).ChecksPass++
				}
			}
		}
		if data, err := os.ReadFile(run.BuildResultPath()); err == nil {
			var payload struct {
				Winner string `json:"winner_agent"`
			}
			if json.Unmarshal(data, &payload) == nil && payload.Winner != "" {
				mark(payload.Winner)
				get(payload.Winner).BuildWins++
			}
		}
		if adopted, ok := run.Adoption(); ok {
			mark(adopted)
			get(adopted).Adopted++
		}
	}
	if len(scores) == 0 {
		fmt.Println("No run data yet.")
		return nil
	}
	names := make([]string, 0, len(scores))
	for name := range scores {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := scores[names[i]], scores[names[j]]
		if a.PlanWins+a.BuildWins != b.PlanWins+b.BuildWins {
			return a.PlanWins+a.BuildWins > b.PlanWins+b.BuildWins
		}
		return names[i] < names[j]
	})
	fmt.Printf("%-16s %5s %9s %10s %11s %6s %6s %8s %8s\n",
		"AGENT", "RUNS", "PLAN-WINS", "BUILD-WINS", "CHECK-PASS", "PLANS", "VOTES", "REVIEWS", "ADOPTED")
	for _, name := range names {
		s := scores[name]
		checks := "—"
		if s.ChecksRun > 0 {
			checks = fmt.Sprintf("%d/%d", s.ChecksPass, s.ChecksRun)
		}
		fmt.Printf("%-16s %5d %9d %10d %11s %6d %6d %8d %8d\n",
			name, s.Runs, s.PlanWins, s.BuildWins, checks, s.Plans, s.Votes, s.Reviews, s.Adopted)
	}
	return nil
}

// ---- batch issue queue ----

type queueItem struct {
	Issue  int    `json:"issue,omitempty"`
	File   string `json:"file,omitempty"`
	Inline string `json:"inline,omitempty"`
}

func queuePath() string { return filepath.Join(".council", "queue.json") }

func loadQueue() ([]queueItem, error) {
	data, err := os.ReadFile(queuePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var items []queueItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", queuePath(), err)
	}
	return items, nil
}

func saveQueue(items []queueItem) error {
	if err := os.MkdirAll(filepath.Dir(queuePath()), fsperm.Dir()); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(queuePath(), data, fsperm.File())
}

func (q queueItem) String() string {
	switch {
	case q.Issue > 0:
		return fmt.Sprintf("issue #%d", q.Issue)
	case q.File != "":
		return "file " + q.File
	default:
		preview := q.Inline
		if len(preview) > 60 {
			preview = preview[:59] + "~"
		}
		return fmt.Sprintf("%q", preview)
	}
}

// councilQueue batches several tasks through council, one full run each:
// council queue add --issue 123 | add --file task.md | add "<text>"
// council queue list | run | clear
func councilQueue(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: council queue add|list|run|clear")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("council queue add", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		issueNum := fs.Int("issue", 0, "GitHub issue number")
		file := fs.String("file", "", "issue file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		item := queueItem{Issue: *issueNum, File: *file, Inline: strings.Join(fs.Args(), " ")}
		if item.Issue == 0 && item.File == "" && strings.TrimSpace(item.Inline) == "" {
			return errors.New("usage: council queue add --issue N | --file task.md | \"<text>\"")
		}
		items, err := loadQueue()
		if err != nil {
			return err
		}
		items = append(items, item)
		if err := saveQueue(items); err != nil {
			return err
		}
		fmt.Printf("Queued %s (%d item(s) total)\n", item, len(items))
		return nil
	case "list":
		items, err := loadQueue()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("Queue is empty.")
			return nil
		}
		for i, item := range items {
			fmt.Printf("%2d. %s\n", i+1, item)
		}
		return nil
	case "clear":
		if err := saveQueue(nil); err != nil {
			return err
		}
		fmt.Println("Queue cleared.")
		return nil
	case "run":
		items, err := loadQueue()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errors.New("queue is empty; `council queue add` first")
		}
		for len(items) > 0 {
			item := items[0]
			fmt.Printf("\n=== council queue: %s (%d remaining) ===\n", item, len(items))
			runArgs := []string{}
			switch {
			case item.Issue > 0:
				runArgs = append(runArgs, "--issue", fmt.Sprint(item.Issue))
			case item.File != "":
				runArgs = append(runArgs, "--file", item.File)
			default:
				runArgs = append(runArgs, item.Inline)
			}
			if err := councilRunAll(runArgs); err != nil {
				return fmt.Errorf("queue item %s failed (left at the head of the queue): %w", item, err)
			}
			items = items[1:]
			if err := saveQueue(items); err != nil {
				return err
			}
		}
		fmt.Println("Queue finished.")
		return nil
	default:
		return fmt.Errorf("unknown queue command %q (add|list|run|clear)", args[0])
	}
}
