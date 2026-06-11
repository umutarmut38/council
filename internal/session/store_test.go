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
