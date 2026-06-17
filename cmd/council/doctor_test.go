package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestAgentExtraEnvKeys(t *testing.T) {
	tests := []struct {
		name   string
		global map[string]string
		agent  map[string]string
		want   []string
	}{
		{
			name:  "nil global, per-agent keys are all extra and sorted",
			agent: map[string]string{"OPENAI_BASE_URL": "x", "ANTHROPIC_BASE_URL": "y"},
			want:  []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL"},
		},
		{
			name:   "keys shared with global at the same value are not extra",
			global: map[string]string{"SHARED": "1"},
			agent:  map[string]string{"SHARED": "1", "ONLY": "2"},
			want:   []string{"ONLY"},
		},
		{
			name:   "an override of a global key counts as extra",
			global: map[string]string{"SHARED": "1"},
			agent:  map[string]string{"SHARED": "2"},
			want:   []string{"SHARED"},
		},
		{
			name:   "no extras returns empty",
			global: map[string]string{"A": "1"},
			agent:  map[string]string{"A": "1"},
			want:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agentExtraEnvKeys(tc.global, tc.agent)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("agentExtraEnvKeys() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDoctorMissingConfigIsReadOnly: with no global config and no --fix, doctor
// reports the problem and points at the fix, but must not create anything.
func TestDoctorMissingConfigIsReadOnly(t *testing.T) {
	setTempHome(t)
	chdir(t, t.TempDir())

	out := captureOutput(t, func() {
		if err := doctor(nil); err == nil {
			t.Fatal("doctor should fail when the global config is missing")
		}
	})
	if !strings.Contains(out, "missing") || !strings.Contains(out, "--fix") {
		t.Fatalf("doctor should report the missing config and the --fix hint:\n%s", out)
	}
	cfgPath, _ := config.DefaultPath()
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("default doctor must not create the config (stat err = %v)", err)
	}
}

// TestDoctorFixWritesDefaultConfig: --fix writes the default config when it is
// missing and says so.
func TestDoctorFixWritesDefaultConfig(t *testing.T) {
	setTempHome(t)
	chdir(t, t.TempDir())

	out := captureOutput(t, func() { _ = doctor([]string{"--fix"}) })

	cfgPath, _ := config.DefaultPath()
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("doctor --fix should write the default config: %v", err)
	}
	if !strings.Contains(out, "wrote default config") {
		t.Fatalf("doctor --fix should announce the write:\n%s", out)
	}
}

// TestDoctorFixTightensArtifactPerms: --fix resets broadened run-artifact perms
// back to owner-only.
func TestDoctorFixTightensArtifactPerms(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}

	stampDir := filepath.Join(dir, ".council", "runs", "20260101-000000")
	if err := os.MkdirAll(stampDir, 0o777); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(stampDir, "transcript.txt")
	if err := os.WriteFile(loose, []byte("hi"), 0o666); err != nil {
		t.Fatal(err)
	}
	// Defeat umask so the test starts from genuinely broadened modes.
	if err := os.Chmod(stampDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o666); err != nil {
		t.Fatal(err)
	}

	captureOutput(t, func() { _ = doctor([]string{"--fix"}) })

	if di, _ := os.Stat(stampDir); di.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %o, want 700", di.Mode().Perm())
	}
	if fi, _ := os.Stat(loose); fi.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o, want 600", fi.Mode().Perm())
	}
}

func TestDoctorFixSkipsArtifactSymlinks(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, ".council", "runs", "20260101-000000")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	changed, err := tightenArtifactPerms(filepath.Join(dir, ".council", "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("changed %d paths, want 0", changed)
	}
	if fi, err := os.Stat(outside); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o666 {
		t.Fatalf("symlink target perm = %o, want 666", fi.Mode().Perm())
	}
}

// TestDoctorReadOnlyLeavesPermsUntouched: the default run never changes perms.
func TestDoctorReadOnlyLeavesPermsUntouched(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}

	stampDir := filepath.Join(dir, ".council", "runs", "r1")
	if err := os.MkdirAll(stampDir, 0o777); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(stampDir, "f.txt")
	if err := os.WriteFile(loose, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stampDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o666); err != nil {
		t.Fatal(err)
	}

	captureOutput(t, func() { _ = doctor(nil) })

	if di, _ := os.Stat(stampDir); di.Mode().Perm() != 0o777 {
		t.Fatalf("read-only doctor changed dir perm to %o", di.Mode().Perm())
	}
	if fi, _ := os.Stat(loose); fi.Mode().Perm() != 0o666 {
		t.Fatalf("read-only doctor changed file perm to %o", fi.Mode().Perm())
	}
}

// TestDoctorFixSetsStackCheckCommand: in a go repo with no review gate, --fix
// writes review.check_command into a repo-local .council.yaml.
func TestDoctorFixSetsStackCheckCommand(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	initGitRepo(t, dir)
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() { _ = doctor([]string{"--fix"}) })

	data, err := os.ReadFile(filepath.Join(dir, ".council.yaml"))
	if err != nil {
		t.Fatalf("doctor --fix should create a repo-local .council.yaml: %v", err)
	}
	if !strings.Contains(string(data), "check_command") {
		t.Fatalf(".council.yaml is missing check_command:\n%s", data)
	}
	if !strings.Contains(out, "review.check_command") {
		t.Fatalf("doctor --fix should announce the review gate:\n%s", out)
	}
}

// TestDoctorPrintsGuidance: the default run emits the prescriptive roster and a
// next action.
func TestDoctorPrintsGuidance(t *testing.T) {
	setTempHome(t)
	dir := t.TempDir()
	chdir(t, dir)
	cfgPath, _ := config.DefaultPath()
	if err := config.WriteDefault(cfgPath, false); err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() { _ = doctor(nil) })
	for _, want := range []string{"agents — known CLIs:", "claude", "next:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor guidance missing %q in:\n%s", want, out)
		}
	}
}

func TestPresetInstallHints(t *testing.T) {
	for _, name := range config.PresetNames() {
		if config.PresetInstallHint(name) == "" {
			t.Errorf("preset %q should have an install hint", name)
		}
	}
	if config.PresetInstallHint("not-a-preset") != "" {
		t.Error("unknown preset should have no install hint")
	}
}
