package setup

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSetEnvKeysSortsAndDropsValues(t *testing.T) {
	s := New()
	s.SetEnvKeys(map[string]string{"ZED": "secret", "API_KEY": "hunter2", "mid": "x"})

	got := s.Snapshot().EnvKeys
	want := []string{"API_KEY", "ZED", "mid"} // ASCII sort: uppercase before lowercase
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("env keys = %v, want %v", got, want)
	}
	for _, k := range got {
		if strings.Contains(k, "secret") || strings.Contains(k, "hunter2") {
			t.Fatalf("env values leaked into keys: %v", got)
		}
	}
}

func TestOneShotLifecycle(t *testing.T) {
	s := New()
	h := s.Begin("migrate", []string{"db", "migrate"}, KindOneShot, 0)

	if got := s.Snapshot().Commands[0].State; got != StatePending {
		t.Fatalf("initial state = %q, want %q", got, StatePending)
	}

	h.Running(0)
	fmt.Fprint(h.Writer(), "applying 3 migrations\n")
	h.Succeeded()

	c := s.Snapshot().Commands[0]
	if c.State != StateDone {
		t.Fatalf("state = %q, want %q", c.State, StateDone)
	}
	if c.Kind != KindOneShot {
		t.Fatalf("kind = %q, want %q", c.Kind, KindOneShot)
	}
	if !strings.Contains(c.Output, "applying 3 migrations") {
		t.Fatalf("output not captured: %q", c.Output)
	}
	if c.PID != 0 {
		t.Fatalf("one-shot pid = %d, want 0", c.PID)
	}
}

func TestBackgroundReadyThenStopped(t *testing.T) {
	s := New()
	h := s.Begin("api", []string{"npm", "start"}, KindBackground, 8080)
	h.Running(4242)
	h.Ready()
	h.Stopped(nil)

	c := s.Snapshot().Commands[0]
	if c.State != StateStopped {
		t.Fatalf("state = %q, want %q", c.State, StateStopped)
	}
	if !c.Ready {
		t.Fatal("ready gate not recorded")
	}
	if c.PID != 4242 {
		t.Fatalf("pid = %d, want 4242", c.PID)
	}
	if c.WaitForPort != 8080 {
		t.Fatalf("waitForPort = %d, want 8080", c.WaitForPort)
	}
}

func TestStoppedDoesNotOverrideFailure(t *testing.T) {
	s := New()
	h := s.Begin("api", []string{"npm", "start"}, KindBackground, 0)
	h.Running(99)
	h.Failed(errors.New("boom"))
	h.Stopped(errors.New("killed after grace period"))

	c := s.Snapshot().Commands[0]
	if c.State != StateFailed {
		t.Fatalf("state = %q, want %q (teardown must not mask failure)", c.State, StateFailed)
	}
	if c.Err != "boom" {
		t.Fatalf("err = %q, want original %q", c.Err, "boom")
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	s := New()
	h := s.Begin("x", []string{"echo", "hi"}, KindOneShot, 0)
	h.Succeeded()

	snap := s.Snapshot()
	snap.Commands[0].Args[0] = "mutated"
	snap.EnvKeys = append(snap.EnvKeys, "INJECTED")

	again := s.Snapshot()
	if again.Commands[0].Args[0] != "echo" {
		t.Fatalf("snapshot Args alias the live state: %q", again.Commands[0].Args[0])
	}
	if len(again.EnvKeys) != 0 {
		t.Fatalf("snapshot EnvKeys alias the live state: %v", again.EnvKeys)
	}
}

func TestOutputLinesTrailingCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	lines := outputLines(b.String())
	if len(lines) != 21 { // 1 omission notice + 20 retained
		t.Fatalf("got %d lines, want 21", len(lines))
	}
	if !strings.Contains(lines[0], "omitted") {
		t.Fatalf("first line = %q, want an omission notice", lines[0])
	}
	if lines[len(lines)-1] != "line 24" {
		t.Fatalf("last line = %q, want %q", lines[len(lines)-1], "line 24")
	}
	if got := outputLines("\n\n"); got != nil {
		t.Fatalf("blank output = %v, want nil", got)
	}
}

func TestReportRender(t *testing.T) {
	s := New()
	s.SetEnvKeys(map[string]string{"API_KEY": "x"})
	h := s.Begin("api", []string{"npm", "start"}, KindBackground, 8080)
	h.Running(321)
	h.Ready()
	fmt.Fprint(h.Writer(), "listening\n")
	out := s.Snapshot().Render()

	for _, want := range []string{
		"exported env keys (values never shown): API_KEY",
		"api",
		"[background, readiness :8080]",
		string(StateReady),
		"pid 321",
		"gate ok",
		"$ npm start",
		"| listening",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

func TestReportRenderEmpty(t *testing.T) {
	out := New().Snapshot().Render()
	if !strings.Contains(out, "exported env keys (values never shown): (none)") {
		t.Fatalf("missing empty env line:\n%s", out)
	}
	if !strings.Contains(out, "setup commands: (none)") {
		t.Fatalf("missing empty commands line:\n%s", out)
	}
}

// TestConcurrentUpdates exercises the lock under -race: writers, state updates,
// and snapshots run together without data races or deadlocks.
func TestConcurrentUpdates(t *testing.T) {
	s := New()
	h := s.Begin("api", []string{"run"}, KindBackground, 0)
	h.Running(1)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				fmt.Fprintf(h.Writer(), "tick %d\n", j)
				_ = s.Snapshot()
				s.SetEnvKeys(map[string]string{"K": "v"})
			}
		}()
	}
	wg.Wait()
	h.Stopped(nil)

	if s.Snapshot().Commands[0].State != StateStopped {
		t.Fatal("final state not recorded")
	}
}
