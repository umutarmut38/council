package setup

import "sync"

// capBuffer is a concurrency-safe io.Writer that retains only the first max
// bytes written to it (then appends a single truncation marker) while still
// accepting and discarding the rest, so capturing a chatty daemon's output
// stays bounded and never blocks the child process. A max of <= 0 means no
// limit. Both stdout and stderr copiers write to the same buffer, so the lock
// is required.
type capBuffer struct {
	mu        sync.Mutex
	max       int
	buf       []byte
	truncated bool
}

const truncationMarker = "\n[output truncated]\n"

func (w *capBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.max <= 0 {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	if remaining := w.max - len(w.buf); remaining > 0 {
		if remaining >= len(p) {
			w.buf = append(w.buf, p...)
			return len(p), nil
		}
		w.buf = append(w.buf, p[:remaining]...)
	}
	if !w.truncated {
		w.buf = append(w.buf, truncationMarker...)
		w.truncated = true
	}
	return len(p), nil
}

// String returns the retained output.
func (w *capBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
