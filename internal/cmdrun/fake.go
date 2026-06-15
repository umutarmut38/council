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
	f.calls = append(f.calls, cloneSpec(s))
	f.mu.Unlock()
	if f.Handler == nil {
		return Result{}, nil
	}
	return f.Handler(s)
}

// Calls returns a copy of the recorded invocations in order. The returned Specs
// own their Args/Env, so callers may inspect or mutate them without affecting
// the recorded history.
func (f *Fake) Calls() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Spec, len(f.calls))
	for i, s := range f.calls {
		out[i] = cloneSpec(s)
	}
	return out
}

// cloneSpec deep-copies a Spec's reference fields (Args slice and Env map) so a
// recorded invocation is decoupled from the caller's inputs (which may be reused
// or mutated after the call) and from any previously returned copy.
func cloneSpec(s Spec) Spec {
	if s.Args != nil {
		s.Args = append([]string(nil), s.Args...)
	}
	if s.Env != nil {
		env := make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			env[k] = v
		}
		s.Env = env
	}
	return s
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
