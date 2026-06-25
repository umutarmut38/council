// Package capbuf provides a bounded, concurrency-safe io.Writer that retains
// only the first Max bytes written to it (then appends a single truncation
// marker) while still accepting and discarding the rest. Capturing through it
// keeps memory bounded for chatty commands or daemons instead of buffering
// everything and truncating afterward, while continuing to drain the writer so
// the producing process never blocks.
package capbuf

import "sync"

// TruncationMarker is appended once output exceeds Writer.Max.
const TruncationMarker = "\n[output truncated]\n"

// Writer retains the first Max bytes written to it; Max <= 0 means no limit. It
// is safe for concurrent use, so stdout and stderr copiers may share one.
type Writer struct {
	Max int

	mu        sync.Mutex
	buf       []byte
	truncated bool
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Max <= 0 {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	if remaining := w.Max - len(w.buf); remaining > 0 {
		if remaining >= len(p) {
			w.buf = append(w.buf, p...)
			return len(p), nil
		}
		w.buf = append(w.buf, p[:remaining]...)
	}
	if !w.truncated {
		w.buf = append(w.buf, TruncationMarker...)
		w.truncated = true
	}
	return len(p), nil
}

// Bytes returns a copy of the retained output (capped, with a trailing marker
// when the output was truncated). It returns a copy, not the internal buffer,
// so the result stays valid and race-free even if a concurrent Write appends in
// place afterward.
func (w *Writer) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf...)
}

// String returns the retained output as a string. The conversion happens under
// the lock so it is safe to call while another goroutine is still writing.
func (w *Writer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
