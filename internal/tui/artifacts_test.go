package tui

import (
	"strings"
	"testing"
)

func TestTranscriptPrivacyNote(t *testing.T) {
	on := transcriptPrivacyNote(true)
	off := transcriptPrivacyNote(false)

	if !strings.Contains(on, "transcripts here are redacted") {
		t.Fatalf("redact-on note should say transcripts are redacted: %q", on)
	}
	if !strings.Contains(off, "NOT redacted") {
		t.Fatalf("redact-off note should warn transcripts are not redacted: %q", off)
	}
	for _, note := range []string{on, off} {
		if !strings.Contains(note, "raw PTY logs") {
			t.Fatalf("note should distinguish raw PTY logs: %q", note)
		}
	}
}
