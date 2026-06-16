package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte{'a', 0x00, 'b'}) {
		t.Fatal("a NUL byte should mark content as binary")
	}
	if isBinary([]byte("plain readable text")) {
		t.Fatal("plain text should not be binary")
	}
}

func TestScanArtifactDirSkipsBinaryAndClean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("api_key=supersecretvalue123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.md"), []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A binary blob that also contains a secret-looking string is skipped.
	blob := append([]byte{0x00, 0x01, 0x02}, []byte("api_key=anothersecretvalue")...)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), blob, 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := scanArtifactDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "secret.txt" {
		t.Fatalf("expected only secret.txt to flag, got %+v", files)
	}
	if len(files[0].Findings) == 0 {
		t.Fatal("secret.txt should have at least one finding")
	}
}

func TestCouncilArtifactsScanReportsSecrets(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(dir, ".council", "runs", "20260101-000000", "raw")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Raw PTY logs are never redacted — the scanner must look inside them.
	if err := os.WriteFile(filepath.Join(runDir, "claude.log"),
		[]byte("export TOKEN=ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() {
		if err := councilArtifacts([]string{"scan"}); err == nil {
			t.Fatal("scan should return a non-zero error when secrets are found")
		}
	})
	if !strings.Contains(out, filepath.ToSlash(filepath.Join("raw", "claude.log"))) {
		t.Fatalf("scan output should name the offending file:\n%s", out)
	}
	if !strings.Contains(out, "GitHub token") {
		t.Fatalf("scan output should name the secret kind:\n%s", out)
	}
}

func TestCouncilArtifactsScanCleanRun(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(dir, ".council", "runs", "20260101-000000")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "issue.md"), []byte("just a normal issue\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() {
		if err := councilArtifacts([]string{"scan"}); err != nil {
			t.Fatalf("a clean run should not error: %v", err)
		}
	})
	if !strings.Contains(out, "No potential secrets") {
		t.Fatalf("clean scan should report no secrets:\n%s", out)
	}
}

func TestCouncilArtifactsUnknownSubcommand(t *testing.T) {
	err := councilArtifacts([]string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "unknown artifacts command") {
		t.Fatalf("error = %v, want unknown artifacts command", err)
	}
}

func TestWarnArtifactSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "log.txt"),
		[]byte("token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() { warnArtifactSecrets(dir) })
	if !strings.Contains(out, "potential secret") || !strings.Contains(out, "not redacted") {
		t.Fatalf("warning missing expected text:\n%s", out)
	}

	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "x.txt"), []byte("nothing to see\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := captureOutput(t, func() { warnArtifactSecrets(clean) }); strings.TrimSpace(out) != "" {
		t.Fatalf("a clean dir should warn nothing, got: %q", out)
	}
}
