package cmdrun

import (
	"context"
	"sync"
)

// Fake is a scripted Runner for tests. It records every invocation and answers
// from Handler (or with an empty success when Handler is nil), so code that runs
// git/gh/etc. can be exercised without touching the real binaries.
type Fake struct {
	// Handler computes the response for a Spec. When nil, calls succeed with no
	// output.
	Handler func(Spec) (Result, error)

	mu    sync.Mutex
	calls []Spec
}

var _ Runner = (*Fake)(nil)

func (f *Fake) dispatch(s Spec) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
	if f.Handler == nil {
		return Result{}, nil
	}
	return f.Handler(s)
}

// Calls returns a copy of the recorded invocations in order.
func (f *Fake) Calls() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Spec(nil), f.calls...)
}

func (f *Fake) Run(_ context.Context, s Spec) (Result, error) { return f.dispatch(s) }

func (f *Fake) Output(_ context.Context, s Spec) ([]byte, error) {
	res, err := f.dispatch(s)
	return res.Stdout, err
}

func (f *Fake) CombinedOutput(_ context.Context, s Spec) ([]byte, error) {
	res, err := f.dispatch(s)
	if res.Combined != nil {
		return res.Combined, err
	}
	combined := append(append([]byte(nil), res.Stdout...), res.Stderr...)
	return combined, err
}
