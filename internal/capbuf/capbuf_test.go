package capbuf

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestWriterTruncates(t *testing.T) {
	w := &Writer{Max: 5}
	n, err := w.Write([]byte("0123456789"))
	if err != nil || n != 10 {
		t.Fatalf("Write = (%d, %v), want (10, nil)", n, err)
	}
	// More writes after the cap are accepted but discarded.
	if n, _ := w.Write([]byte("more")); n != 4 {
		t.Fatalf("post-cap Write = %d, want 4", n)
	}
	got := w.String()
	if !strings.HasPrefix(got, "01234") {
		t.Fatalf("retained prefix = %q, want it to start with 01234", got)
	}
	if !strings.Contains(got, "[output truncated]") {
		t.Fatalf("missing truncation marker: %q", got)
	}
	if strings.Contains(got, "56789") || strings.Contains(got, "more") {
		t.Fatalf("data past the cap was retained: %q", got)
	}
}

func TestWriterUnbounded(t *testing.T) {
	w := &Writer{Max: 0}
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(w, "line %d\n", i)
	}
	if !strings.Contains(w.String(), "line 999") {
		t.Fatal("unbounded buffer dropped data")
	}
	if strings.Contains(w.String(), "truncated") {
		t.Fatal("unbounded buffer should never truncate")
	}
}

// TestWriterConcurrentReadWrite exercises the "safe for concurrent use" promise:
// readers (String/Bytes) must not race with writers appending in place. Run with
// -race; it fails if String/Bytes read the buffer outside the lock.
func TestWriterConcurrentReadWrite(t *testing.T) {
	w := &Writer{Max: 0}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				fmt.Fprintf(w, "chunk-%d\n", j)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = w.String()
				_ = w.Bytes()
			}
		}()
	}
	wg.Wait()
}
