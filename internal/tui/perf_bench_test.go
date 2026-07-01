package tui

import (
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

// perfModel builds a minimal 3-pane Model for driving Update with output floods.
func perfModel() (Model, *agent.Session) {
	sessions := []*agent.Session{
		agent.NewSession("a", config.AgentConfig{Command: []string{"true"}}, ""),
		agent.NewSession("b", config.AgentConfig{Command: []string{"true"}}, ""),
		agent.NewSession("c", config.AgentConfig{Command: []string{"true"}}, ""),
	}
	m := NewModelWithConfig(sessions, nil, config.Config{}, "", nil, 0, func(*agent.Session) {}, nil)
	m.Width, m.Height = 200, 50
	m.resizeAgents()
	return m, sessions[0]
}

func textChunk(n int) []byte {
	line := []byte("the quick brown fox jumps over the lazy dog 0123456789\n")
	var b []byte
	for len(b) < n {
		b = append(b, line...)
	}
	return b[:n]
}

// ansiChunk mimics a "broken" pane: dense CSI/OSC escape sequences (color, cursor
// moves, clears) that the transcript-clean regexes and the terminal emulator must
// each parse.
func ansiChunk(n int) []byte {
	unit := []byte("\x1b[31m\x1b[1mX\x1b[0m\x1b[2K\x1b[10;20H\x1b]0;title\x07\x1b[Kdata ")
	var b []byte
	for len(b) < n {
		b = append(b, unit...)
	}
	return b[:n]
}

// feedUpdate pushes one AgentOutputMsg through the real Update loop, returning
// the evolved model (mirroring bubbletea's by-value Update contract).
func feedUpdate(m Model, s *agent.Session, chunk []byte) Model {
	next, _ := m.Update(AgentOutputMsg{Name: s.Name, Session: s, Data: chunk})
	return next.(Model)
}

// Per-message dispatch cost at a small chunk size — the fixed overhead paid once
// per AgentOutputMsg regardless of how little data it carries.
func BenchmarkUpdateOutputSmallText(b *testing.B) {
	m, s := perfModel()
	chunk := textChunk(256)
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = feedUpdate(m, s, chunk)
	}
}

func BenchmarkUpdateOutputLargeText(b *testing.B) {
	m, s := perfModel()
	chunk := textChunk(16384)
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = feedUpdate(m, s, chunk)
	}
}

func BenchmarkUpdateOutputSmallANSI(b *testing.B) {
	m, s := perfModel()
	chunk := ansiChunk(256)
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = feedUpdate(m, s, chunk)
	}
}

func BenchmarkUpdateOutputLargeANSI(b *testing.B) {
	m, s := perfModel()
	chunk := ansiChunk(16384)
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = feedUpdate(m, s, chunk)
	}
}

// benchVolume processes a FIXED total volume split into chunkSize pieces, so the
// ns/op difference across chunk sizes isolates the per-message overhead (more,
// smaller messages = more Update dispatches for the same bytes). This is the
// core evidence that coalescing PTY reads into fewer, larger messages cuts the
// Update-loop work a flood imposes.
func benchVolume(b *testing.B, chunkSize int, ansi bool) {
	const total = 4 << 20 // 4 MiB per op
	var chunk []byte
	if ansi {
		chunk = ansiChunk(chunkSize)
	} else {
		chunk = textChunk(chunkSize)
	}
	n := total / chunkSize
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, s := perfModel()
		for j := 0; j < n; j++ {
			m = feedUpdate(m, s, chunk)
		}
	}
}

func BenchmarkVolumeText_256B(b *testing.B) { benchVolume(b, 256, false) }
func BenchmarkVolumeText_4KB(b *testing.B)  { benchVolume(b, 4096, false) }
func BenchmarkVolumeText_32KB(b *testing.B) { benchVolume(b, 32768, false) }
func BenchmarkVolumeANSI_256B(b *testing.B) { benchVolume(b, 256, true) }
func BenchmarkVolumeANSI_4KB(b *testing.B)  { benchVolume(b, 4096, true) }
func BenchmarkVolumeANSI_32KB(b *testing.B) { benchVolume(b, 32768, true) }
