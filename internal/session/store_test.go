package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateRunDirSurvivesCollisions(t *testing.T) {
	root := t.TempDir()
	seen := map[string]bool{}
	// Several runs inside the same second must produce distinct directories.
	for i := 0; i < 5; i++ {
		dir, stamp, err := CreateRunDir(root)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if seen[stamp] {
			t.Fatalf("duplicate stamp %q", stamp)
		}
		seen[stamp] = true
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("run dir not created: %v", err)
		}
	}
}

func TestNewDeferredTouchesNothingUntilEnsure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	store := NewDeferred(root, []byte("agents: {}\n"), []byte("{}"))

	if store.Started() {
		t.Fatal("a deferred store must not be started before Ensure")
	}
	// Merely constructing the store must not create the runs directory.
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("runs root created too early (err=%v)", err)
	}

	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if !store.Started() {
		t.Fatal("store should be started after Ensure")
	}
	if _, err := os.Stat(store.RunDir); err != nil {
		t.Fatalf("run dir not created by Ensure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.RunDir, "config.effective.yaml")); err != nil {
		t.Fatalf("config snapshot not written: %v", err)
	}

	// Ensure is idempotent: a second call keeps the same directory.
	dir := store.RunDir
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if store.RunDir != dir {
		t.Fatalf("Ensure not idempotent: %s -> %s", dir, store.RunDir)
	}
}

func TestSavePromptRealizesDeferredStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	store := NewDeferred(root, nil, nil)

	if err := store.SavePrompt("do the thing"); err != nil {
		t.Fatal(err)
	}
	if !store.Started() {
		t.Fatal("SavePrompt should realize the deferred store")
	}
	if _, err := os.Stat(filepath.Join(store.RunDir, "prompt.txt")); err != nil {
		t.Fatalf("prompt.txt not written: %v", err)
	}
}

func TestStorePrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	root := t.TempDir()
	store, err := New(filepath.Join(root, "runs"), []byte("agents: {}\n"), []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTranscript("claude", "hello"); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("run dir mode = %o, want 0700", perm)
	}
	for _, file := range []string{
		filepath.Join(store.RunDir, "config.effective.yaml"),
		store.TranscriptPath("claude"),
	} {
		fi, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s mode = %o, want 0600", file, perm)
		}
	}
}

func TestStoreRawPromptAndPhaseAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	root := t.TempDir()
	store, err := New(filepath.Join(root, "runs"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePrompt("do the thing"); err != nil {
		t.Fatal(err)
	}

	// Raw PTY log dir holds unredacted output; it must be owner-only (0700).
	if fi, err := os.Stat(store.RawDir); err != nil {
		t.Fatal(err)
	} else if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("raw dir mode = %o, want 0700", perm)
	}
	if fi, err := os.Stat(filepath.Join(store.RunDir, "prompt.txt")); err != nil {
		t.Fatal(err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("prompt mode = %o, want 0600", perm)
	}

	// Phase subdirectories (plan/vote/build) must also be owner-only.
	phase, err := OpenAt(store.RunDir, "plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{phase.TranscriptDir, phase.RawDir} {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s mode = %o, want 0700", dir, perm)
		}
	}
}

func TestScanLocatesSecretsByLine(t *testing.T) {
	text := strings.Join([]string{
		"line one is fine",
		"token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB here",
		"also fine",
		"password=hunter2hunter2",
	}, "\n")
	findings := Scan(text)
	if len(findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d: %+v", len(findings), findings)
	}
	if findings[0].Line != 2 {
		t.Fatalf("first finding line = %d, want 2 (findings: %+v)", findings[0].Line, findings)
	}
	for _, f := range findings {
		if strings.Contains(f.Kind, "ghp_") || strings.Contains(f.Kind, "hunter2") {
			t.Fatalf("finding kind leaked a secret value: %q", f.Kind)
		}
	}
	if got := Scan("totally clean text\nno secrets at all"); len(got) != 0 {
		t.Fatalf("clean text should yield no findings, got %+v", got)
	}
}

func TestRedactScrubsCommonSecrets(t *testing.T) {
	in := strings.Join([]string{
		"key AKIAIOSFODNN7EXAMPLE in env",
		"token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB done",
		"Authorization: Bearer abcdef0123456789abcdef0123456789",
		"api_key=super-secret-value-123",
		"plain text stays",
	}, "\n")
	got := Redact(in)
	for _, leaked := range []string{"AKIAIOSFODNN7EXAMPLE", "ghp_0123456789", "super-secret-value-123"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("secret %q survived redaction:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "plain text stays") {
		t.Fatalf("redaction mangled plain text:\n%s", got)
	}
}
