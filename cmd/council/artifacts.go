package main

// `council artifacts scan` inspects run artifacts for likely secrets, reusing
// the redaction patterns behind sessions.redact. It also backs the pre-report
// and pre-pr warnings, since raw PTY logs are never redacted and are the most
// likely place for a pasted credential to land.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/umutarmut38/council/internal/orchestrate"
	"github.com/umutarmut38/council/internal/session"
)

func councilArtifacts(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: council artifacts scan [run] [--all]")
	}
	switch args[0] {
	case "scan":
		return councilArtifactsScan(args[1:])
	default:
		return fmt.Errorf("unknown artifacts command %q (scan)", args[0])
	}
}

// councilArtifactsScan scans one run (latest by default, or a named [run]) or
// every run (--all) and reports likely secrets per file. It exits non-zero when
// anything is found, so it can gate scripts.
func councilArtifactsScan(args []string) error {
	fs, noLocal := newOrchFlagSet("council artifacts scan")
	all := fs.Bool("all", false, "scan every run under the sessions root, not just one")
	positional, err := parseWithTrailingFlags(fs, args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	rootDir := cfg.Sessions.RootDir

	type runScan struct{ stamp, dir string }
	var runs []runScan
	if *all {
		summaries, err := orchestrate.ListRuns(rootDir, 0)
		if err != nil {
			return err
		}
		for _, s := range summaries {
			runs = append(runs, runScan{s.Stamp, s.Dir})
		}
		if len(runs) == 0 {
			fmt.Println("No runs to scan.")
			return nil
		}
	} else {
		if len(positional) > 1 {
			return fmt.Errorf("usage: council artifacts scan [run] [--all]")
		}
		runArg := ""
		if len(positional) == 1 {
			runArg = positional[0]
		}
		run, err := orchestrate.OpenRun(rootDir, runArg)
		if err != nil {
			return err
		}
		runs = append(runs, runScan{run.Stamp, run.Dir})
	}

	total := 0
	for _, r := range runs {
		files, err := scanArtifactDir(r.dir)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			fmt.Printf("run %s: clean\n", r.stamp)
			continue
		}
		fmt.Printf("run %s:\n", r.stamp)
		for _, f := range files {
			fmt.Printf("  %s\n", f.Path)
			for _, finding := range f.Findings {
				fmt.Printf("    line %d: %s\n", finding.Line, finding.Kind)
				total++
			}
		}
	}

	if total > 0 {
		fmt.Printf("\nFound %d potential secret(s). Review and scrub before sharing; raw PTY\n", total)
		fmt.Println("logs are never redacted. Set `sessions.redact: true` to scrub saved transcripts.")
		return fmt.Errorf("artifacts scan found %d potential secret(s)", total)
	}
	fmt.Println("\nNo potential secrets detected.")
	return nil
}

// fileFindings groups secret findings by artifact path (relative to the run dir).
type fileFindings struct {
	Path     string
	Findings []session.SecretFinding
}

// scanArtifactDir walks a run directory and scans every text artifact for
// likely secrets, returning only the files with findings, sorted by path. Raw
// PTY logs are scanned too — they are never redacted, so they matter most.
func scanArtifactDir(runDir string) ([]fileFindings, error) {
	var out []fileFindings
	err := filepath.WalkDir(runDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil || isBinary(data) {
			return nil
		}
		findings := session.Scan(string(data))
		if len(findings) == 0 {
			return nil
		}
		rel, relErr := filepath.Rel(runDir, path)
		if relErr != nil {
			rel = path
		}
		out = append(out, fileFindings{Path: filepath.ToSlash(rel), Findings: findings})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// isBinary reports whether data looks non-textual (a NUL byte in the first
// 8 KiB), so the scanner skips binaries instead of flagging noise.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// warnArtifactSecrets prints a best-effort stderr warning when a run's
// artifacts look like they contain secrets. It never blocks the caller and is
// used before report/pr, which share or publish artifact-derived content.
func warnArtifactSecrets(runDir string) {
	files, err := scanArtifactDir(runDir)
	if err != nil || len(files) == 0 {
		return
	}
	total := 0
	for _, f := range files {
		total += len(f.Findings)
	}
	fmt.Fprintf(os.Stderr, "warning: %d potential secret(s) in %d artifact file(s) under %s\n", total, len(files), runDir)
	fmt.Fprintln(os.Stderr, "         run `council artifacts scan` to review; raw PTY logs are not redacted.")
}
