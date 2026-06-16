// Package setup models the observability state of council's pre-launch setup
// commands: which commands ran, their kind, PID and lifecycle state, the
// readiness-gate result, captured stdout/stderr, the env keys exported to
// agents (keys only — never values, to respect the privacy posture), and the
// teardown outcome.
//
// The state model lives here, separate from the process supervision in
// cmd/council, so the TUI can render it without importing package main. It is
// safe for concurrent use: the supervisor updates it (including from teardown
// goroutines) while the UI reads snapshots.
package setup

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// maxCaptureBytes caps the stdout/stderr retained per setup command, so a
// chatty background daemon can't grow the buffer without bound.
const maxCaptureBytes = 64 << 10

// State is a setup command's lifecycle stage.
type State string

const (
	StatePending State = "pending" // declared, not yet started
	StateRunning State = "running" // started; one-shot in progress or background supervised
	StateReady   State = "ready"   // background command passed its readiness gate
	StateDone    State = "done"    // one-shot completed successfully
	StateFailed  State = "failed"  // failed to start, exited non-zero, or readiness timed out
	StateStopped State = "stopped" // background command torn down on exit
)

// Kind classifies how a setup command runs.
type Kind string

const (
	KindOneShot    Kind = "one-shot"
	KindBackground Kind = "background"
)

// command is the mutable observability record for one setup command.
type command struct {
	label       string
	args        []string
	kind        Kind
	waitForPort int
	pid         int
	state       State
	ready       bool
	err         string
	output      *capBuffer
}

// CommandView is an immutable snapshot of one setup command's state.
type CommandView struct {
	Label       string
	Args        []string
	Kind        Kind
	WaitForPort int
	PID         int
	State       State
	Ready       bool
	Err         string
	Output      string
}

// Status is the live observability record for a session's pre-launch setup.
type Status struct {
	mu       sync.Mutex
	commands []*command
	envKeys  []string
}

// New returns an empty Status.
func New() *Status { return &Status{} }

// SetEnvKeys records the env keys exported to every agent. Only the keys are
// stored — never the values.
func (s *Status) SetEnvKeys(env map[string]string) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s.mu.Lock()
	s.envKeys = keys
	s.mu.Unlock()
}

// Handle updates one command's record as it runs.
type Handle struct {
	s   *Status
	idx int
}

// Begin registers a setup command in the pending state and returns a handle to
// update it.
func (s *Status) Begin(label string, args []string, kind Kind, waitForPort int) *Handle {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := &command{
		label:       label,
		args:        append([]string(nil), args...),
		kind:        kind,
		waitForPort: waitForPort,
		state:       StatePending,
		output:      &capBuffer{max: maxCaptureBytes},
	}
	s.commands = append(s.commands, c)
	return &Handle{s: s, idx: len(s.commands) - 1}
}

// Writer returns the sink for this command's stdout/stderr. It is safe for
// concurrent writes.
func (h *Handle) Writer() *capBuffer {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.commands[h.idx].output
}

func (h *Handle) set(fn func(c *command)) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	fn(h.s.commands[h.idx])
}

// Running marks the command started with the given PID.
func (h *Handle) Running(pid int) {
	h.set(func(c *command) { c.state = StateRunning; c.pid = pid })
}

// Ready marks a background command's readiness gate as passed.
func (h *Handle) Ready() {
	h.set(func(c *command) { c.ready = true; c.state = StateReady })
}

// Succeeded marks a one-shot command as completed successfully.
func (h *Handle) Succeeded() {
	h.set(func(c *command) { c.state = StateDone })
}

// Failed marks the command as failed and records why.
func (h *Handle) Failed(err error) {
	h.set(func(c *command) {
		c.state = StateFailed
		if err != nil {
			c.err = err.Error()
		}
	})
}

// Stopped records a background command's teardown. It does not override a
// recorded failure, so the original failure reason survives.
func (h *Handle) Stopped(err error) {
	h.set(func(c *command) {
		if c.state == StateFailed {
			return
		}
		c.state = StateStopped
		if err != nil && c.err == "" {
			c.err = err.Error()
		}
	})
}

// Report is an immutable snapshot of the whole setup status.
type Report struct {
	EnvKeys  []string
	Commands []CommandView
}

// Snapshot returns a deep copy of the current state, safe to read off-thread.
func (s *Status) Snapshot() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Report{EnvKeys: append([]string(nil), s.envKeys...)}
	for _, c := range s.commands {
		r.Commands = append(r.Commands, CommandView{
			Label:       c.label,
			Args:        append([]string(nil), c.args...),
			Kind:        c.kind,
			WaitForPort: c.waitForPort,
			PID:         c.pid,
			State:       c.state,
			Ready:       c.ready,
			Err:         c.err,
			Output:      c.output.String(),
		})
	}
	return r
}

// Render formats the report as a human-readable block for the TUI and logs.
func (r Report) Render() string {
	var b strings.Builder
	b.WriteString("Pre-launch setup\n")

	b.WriteString("\nexported env keys (values never shown): ")
	if len(r.EnvKeys) == 0 {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(strings.Join(r.EnvKeys, ", ") + "\n")
	}

	if len(r.Commands) == 0 {
		b.WriteString("\nsetup commands: (none)\n")
		return b.String()
	}

	b.WriteString("\nsetup commands:\n")
	for _, c := range r.Commands {
		b.WriteString("  • " + c.headline() + "\n")
		b.WriteString("      $ " + strings.Join(c.Args, " ") + "\n")
		if c.Err != "" {
			b.WriteString("      error: " + c.Err + "\n")
		}
		for _, line := range outputLines(c.Output) {
			b.WriteString("      | " + line + "\n")
		}
	}
	return b.String()
}

// headline is the one-line state summary for a command.
func (c CommandView) headline() string {
	kind := string(c.Kind)
	if c.Kind == KindBackground && c.WaitForPort > 0 {
		kind = fmt.Sprintf("%s, readiness :%d", kind, c.WaitForPort)
	}
	parts := []string{c.Label, "[" + kind + "]", string(c.State)}
	if c.PID > 0 {
		parts = append(parts, fmt.Sprintf("pid %d", c.PID))
	}
	if c.Ready {
		parts = append(parts, "gate ok")
	}
	return strings.Join(parts, "  ")
}

// outputLines splits captured output into trimmed, non-empty trailing lines for
// display, capping how many are shown so a long log doesn't flood the pager.
func outputLines(out string) []string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	const maxLines = 20
	if len(lines) > maxLines {
		trimmed := []string{fmt.Sprintf("… %d earlier line(s) omitted", len(lines)-maxLines)}
		lines = append(trimmed, lines[len(lines)-maxLines:]...)
	}
	return lines
}
