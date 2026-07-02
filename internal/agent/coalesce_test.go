package agent

import (
	"bytes"
	"testing"
)

// A burst of chunks already queued is merged into a single emit, in order, with
// no bytes lost — the coalescing that keeps a flood from becoming one TUI
// message per PTY read.
func TestCoalesceOutputMergesBurst(t *testing.T) {
	in := make(chan []byte, 100)
	for i := 0; i < 50; i++ {
		in <- []byte("abc")
	}
	close(in)

	var emits [][]byte
	coalesceOutput(in, 64<<10, func(b []byte) {
		emits = append(emits, append([]byte(nil), b...))
	})

	got := bytes.Join(emits, nil)
	want := bytes.Repeat([]byte("abc"), 50)
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes not preserved:\n got %q\nwant %q", got, want)
	}
	if len(emits) != 1 {
		t.Fatalf("a fully-queued burst should coalesce to 1 emit, got %d", len(emits))
	}
}

// The cap bounds each merged emit so a queued keystroke never waits behind an
// arbitrarily large chunk; all bytes still arrive in order.
func TestCoalesceOutputRespectsCap(t *testing.T) {
	in := make(chan []byte, 100)
	unit := bytes.Repeat([]byte("x"), 100)
	for i := 0; i < 10; i++ {
		in <- unit
	}
	close(in)

	var emits [][]byte
	coalesceOutput(in, 250, func(b []byte) {
		emits = append(emits, append([]byte(nil), b...))
	})

	if got := bytes.Join(emits, nil); len(got) != 1000 || !bytes.Equal(got, bytes.Repeat([]byte("x"), 1000)) {
		t.Fatalf("bytes not preserved under a cap: %d", len(got))
	}
	if len(emits) < 2 {
		t.Fatalf("a small cap should split into multiple emits, got %d", len(emits))
	}
	for i, e := range emits {
		if len(e) > 250+len(unit) { // cap is checked before draining one more chunk
			t.Fatalf("emit %d = %d bytes, exceeds cap+chunk", i, len(e))
		}
	}
}

// Sparse output isn't delayed: a lone chunk is emitted immediately once the
// source closes.
func TestCoalesceOutputSparse(t *testing.T) {
	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)
	var emits [][]byte
	coalesceOutput(in, 64<<10, func(b []byte) { emits = append(emits, append([]byte(nil), b...)) })
	if len(emits) != 1 || string(emits[0]) != "hello" {
		t.Fatalf("sparse output = %v, want one 'hello' emit", emits)
	}
}
