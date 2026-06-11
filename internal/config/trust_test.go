package config

import (
	"os"
	"path/filepath"
	"testing"
)

// pointTrustStoreAtTempDir redirects the trust store to a temp location by
// overriding the user config dir for the duration of the test.
func pointTrustStoreAtTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// os.UserConfigDir consults XDG_CONFIG_HOME on Linux and HOME elsewhere.
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

func TestLocalConfigTrustLifecycle(t *testing.T) {
	pointTrustStoreAtTempDir(t)

	cfgPath := filepath.Join(t.TempDir(), ".council.yaml")
	body := []byte("ui:\n  page_rows: 3\n")
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := LocalConfigTrust(cfgPath, body); got != TrustUnknown {
		t.Fatalf("fresh config trust = %v, want TrustUnknown", got)
	}
	if err := TrustLocalConfig(cfgPath, body); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if got := LocalConfigTrust(cfgPath, body); got != Trusted {
		t.Fatalf("after trust = %v, want Trusted", got)
	}

	// Content change must demote to TrustChanged — a tampered config is never
	// silently applied.
	changed := []byte("ui:\n  page_rows: 4\n")
	if got := LocalConfigTrust(cfgPath, changed); got != TrustChanged {
		t.Fatalf("after change = %v, want TrustChanged", got)
	}

	if err := RevokeLocalConfigTrust(cfgPath); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := LocalConfigTrust(cfgPath, body); got != TrustUnknown {
		t.Fatalf("after revoke = %v, want TrustUnknown", got)
	}
}

func TestFindLocalConfigStopsAtGitFileWorktree(t *testing.T) {
	// In a linked git worktree, .git is a FILE; discovery must still treat it
	// as the repo boundary instead of walking past it.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), ".council.yaml")
	sub := filepath.Join(root, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = outside // nothing above the boundary should ever be found

	if got := findLocalConfigFrom(sub); got != "" {
		t.Fatalf("found %q, want none (boundary is the .git file)", got)
	}

	// A config at the worktree root itself is still found.
	inRoot := filepath.Join(root, ".council.yaml")
	if err := os.WriteFile(inRoot, []byte("ui: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := findLocalConfigFrom(sub); got != inRoot {
		t.Fatalf("found %q, want %q", got, inRoot)
	}
}
