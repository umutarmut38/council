package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// captureOutput redirects os.Stdout and os.Stderr to a pipe for the duration of
// fn and returns everything written to either. CLI commands print results and
// errors directly to the process streams, so this is how the tests inspect what
// a command emitted without launching a real terminal.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	restore := func() { os.Stdout, os.Stderr = origOut, origErr }
	defer restore()
	fn()
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// setTempHome points HOME and XDG_CONFIG_HOME at a fresh temp directory so
// config.DefaultPath (~/.council.yaml) and the trust store (os.UserConfigDir)
// resolve under the test sandbox instead of the developer's real home.
func setTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

// chdir switches into dir and restores the previous working directory when the
// test finishes. CLI commands resolve repo-local config, run dirs, and the
// issue queue relative to the working directory.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// withNonTerminalStdin replaces os.Stdin with the read end of a closed pipe.
// A pipe reports os.ModeNamedPipe (not os.ModeCharDevice), so stdinIsTerminal
// returns false deterministically — `go test` may otherwise attach /dev/null,
// which *is* a character device and would look like a terminal.
func withNonTerminalStdin(t *testing.T) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig; _ = r.Close() })
}

// initGitRepo turns dir into a git repository with a single commit, matching the
// helper the orchestrate package uses, so commands that need a repo root (stack,
// clean) have one to work against.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}
