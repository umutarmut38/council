package tui

import (
	"fmt"
	"testing"
)

func TestInputHistoryRecallAndDraft(t *testing.T) {
	var m Model
	m.recordInputHistory("first")
	m.recordInputHistory("/plan")
	m.recordInputHistory("@all go")

	// User has started typing a new line before browsing.
	m.PromptInput = "draft"

	// Up walks newest -> oldest and stops at the oldest entry.
	want := []string{"@all go", "/plan", "first", "first"}
	for i, w := range want {
		m.historyPrev()
		if m.PromptInput != w {
			t.Fatalf("up #%d: got %q, want %q", i+1, m.PromptInput, w)
		}
	}

	// Down walks back toward newer entries, then restores the live draft.
	wantDown := []string{"/plan", "@all go", "draft", "draft"}
	for i, w := range wantDown {
		m.historyNext()
		if m.PromptInput != w {
			t.Fatalf("down #%d: got %q, want %q", i+1, m.PromptInput, w)
		}
	}
}

func TestInputHistoryDedup(t *testing.T) {
	var m Model
	m.recordInputHistory("a")
	m.recordInputHistory("a") // consecutive duplicate dropped
	m.recordInputHistory("b")
	m.recordInputHistory("a") // non-consecutive duplicate kept

	if got, want := len(m.inputHistory), 3; got != want {
		t.Fatalf("history len: got %d, want %d (%v)", got, want, m.inputHistory)
	}
}

func TestInputHistoryCap(t *testing.T) {
	var m Model
	for i := 0; i < maxInputHistory+50; i++ {
		m.recordInputHistory(fmt.Sprintf("line-%d", i))
	}
	if got := len(m.inputHistory); got != maxInputHistory {
		t.Fatalf("history len: got %d, want %d", got, maxInputHistory)
	}
	// Oldest entries dropped; newest retained.
	if m.inputHistory[len(m.inputHistory)-1] != fmt.Sprintf("line-%d", maxInputHistory+49) {
		t.Fatalf("newest entry not retained: %q", m.inputHistory[len(m.inputHistory)-1])
	}
}

func TestInputHistoryEmptyNoop(t *testing.T) {
	var m Model
	m.PromptInput = "typing"
	m.historyPrev()
	m.historyNext()
	if m.PromptInput != "typing" {
		t.Fatalf("empty-history nav mutated draft: %q", m.PromptInput)
	}
}
