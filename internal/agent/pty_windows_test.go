//go:build windows

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umutarmut38/council/internal/config"
)

func TestBuildWindowsCommandLinePlainExe(t *testing.T) {
	// comspec() resolves to an existing cmd.exe; it is not a batch file, so the
	// command line should be the plainly escaped argv with no interpreter wrap.
	cmd := comspec()
	got, err := buildWindowsCommandLine([]string{cmd, "/c", "echo hi"})
	if err != nil {
		t.Fatalf("buildWindowsCommandLine: %v", err)
	}
	want := joinArgv([]string{cmd, "/c", "echo hi"})
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildWindowsCommandLineBatchIsWrapped(t *testing.T) {
	dir := t.TempDir()
	bat := filepath.Join(dir, "agent shim.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := buildWindowsCommandLine([]string{bat, "--flag", "a b"})
	if err != nil {
		t.Fatalf("buildWindowsCommandLine: %v", err)
	}

	if !strings.HasPrefix(got, joinArgv([]string{comspec(), "/s", "/c"})+" \"") {
		t.Fatalf("batch command line not wrapped in interpreter: %q", got)
	}
	if !strings.HasSuffix(got, "\"") {
		t.Fatalf("batch command line missing closing quote: %q", got)
	}
	// The shim path has a space, so it must be quoted inside the wrapper.
	if !strings.Contains(got, "\""+bat+"\"") {
		t.Fatalf("batch path not quoted inside wrapper: %q", got)
	}
}

func TestBuildWindowsCommandLineEmpty(t *testing.T) {
	if _, err := buildWindowsCommandLine(nil); err == nil {
		t.Fatal("expected error for empty command")
	}
}

// TestSessionRunsCommand exercises the real ConPTY path end to end: spawn a
// command, capture its output, and observe a clean exit.
func TestSessionRunsCommand(t *testing.T) {
	dir := t.TempDir()
	s := NewSession("echo", config.AgentConfig{
		Command: []string{"cmd", "/c", "echo hello-council"},
	}, filepath.Join(dir, "raw.log"))

	var (
		mu       sync.Mutex
		captured strings.Builder
	)
	done := make(chan struct{})
	var exitCode *int
	var exitErr error

	err := s.Start(
		func(name string, data []byte, _ int64) {
			mu.Lock()
			captured.Write(data)
			mu.Unlock()
		},
		func(name string, code *int, err error) {
			exitCode = code
			exitErr = err
			close(done)
		},
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = s.Terminate()
		t.Fatal("timed out waiting for command to exit")
	}

	if exitErr != nil {
		t.Fatalf("exit error: %v", exitErr)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Fatalf("exit code = %v, want 0", exitCode)
	}

	mu.Lock()
	out := captured.String()
	mu.Unlock()
	if !strings.Contains(out, "hello-council") {
		t.Fatalf("output %q does not contain expected text", out)
	}
}
