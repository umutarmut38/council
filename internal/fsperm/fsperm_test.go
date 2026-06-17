package fsperm

import "testing"

func TestModesFollowPrivate(t *testing.T) {
	t.Cleanup(func() { SetPrivate(true) })

	SetPrivate(true)
	if got := Dir(); got != 0o700 {
		t.Errorf("private Dir() = %o, want 0700", got)
	}
	if got := File(); got != 0o600 {
		t.Errorf("private File() = %o, want 0600", got)
	}

	SetPrivate(false)
	if got := Dir(); got != 0o755 {
		t.Errorf("shared Dir() = %o, want 0755", got)
	}
	if got := File(); got != 0o644 {
		t.Errorf("shared File() = %o, want 0644", got)
	}
}
