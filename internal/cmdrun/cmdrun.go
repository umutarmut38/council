// Package cmdrun runs external commands behind a small, fakeable interface.
//
// It centralizes the behavior the codebase previously repeated around every
// exec.Command call: a working directory, environment overrides, output size
// caps, context-based timeouts, and structured errors that carry the command,
// its arguments, the exit code, and the captured output. The OS implementation
// talks to the real operating system; Fake is a scripted stand-in for tests.
//
// It deliberately does NOT cover the PTY/agent-startup path, which needs raw
// terminal handling, nor long-lived supervised processes.
package cmdrun

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// truncationMarker is appended to captured output that exceeds Spec.MaxOutput.
const truncationMarker = "\n[output truncated]\n"

// Spec describes a single external command invocation.
type Spec struct {
	// Name is the executable to run (looked up on PATH). Required.
	Name string
	// Args are the command arguments (excluding Name).
	Args []string
	// Dir is the working directory; "" inherits the caller's.
	Dir string
	// Env holds environment overrides merged onto the inherited environment
	// (a key already present is replaced). nil inherits the environment as-is.
	Env map[string]string
	// MaxOutput caps captured output in bytes; <= 0 means no cap. When output
	// is truncated, truncationMarker is appended.
	MaxOutput int
}

// Result is the captured outcome of a command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	Combined []byte // populated by CombinedOutput; nil otherwise
	ExitCode int    // process exit code, or -1 when the process did not run
}

// Runner runs external commands. OS is the real implementation; Fake is for
// tests.
type Runner interface {
	// Run executes the command, capturing stdout and stderr separately.
	Run(ctx context.Context, s Spec) (Result, error)
	// Output runs the command and returns its standard output. On failure the
	// returned error is a *Error whose Output carries the captured stderr.
	Output(ctx context.Context, s Spec) ([]byte, error)
	// CombinedOutput runs the command and returns stdout and stderr interleaved.
	CombinedOutput(ctx context.Context, s Spec) ([]byte, error)
}

// OS is a Runner backed by os/exec.
type OS struct{}

// compile-time assertion that OS satisfies Runner.
var _ Runner = OS{}

func (s Spec) command(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.Name, s.Args...)
	cmd.Dir = s.Dir
	if len(s.Env) > 0 {
		cmd.Env = mergedEnv(s.Env)
	}
	return cmd
}

// mergedEnv overlays overrides onto the current process environment, replacing
// any keys that already exist.
func mergedEnv(overrides map[string]string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			if _, replaced := overrides[kv[:i]]; replaced {
				continue
			}
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func (OS) Run(ctx context.Context, s Spec) (Result, error) {
	cmd := s.command(ctx)
	stdout, stderr := &capWriter{max: s.MaxOutput}, &capWriter{max: s.MaxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	res := Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode(cmd, err),
	}
	if err != nil {
		out := res.Stderr
		if len(out) == 0 {
			out = res.Stdout
		}
		return res, s.wrap(out, res.ExitCode, err)
	}
	return res, nil
}

func (OS) Output(ctx context.Context, s Spec) ([]byte, error) {
	cmd := s.command(ctx)
	stdout, stderr := &capWriter{max: s.MaxOutput}, &capWriter{max: s.MaxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), s.wrap(stderr.Bytes(), exitCode(cmd, err), err)
	}
	return stdout.Bytes(), nil
}

func (OS) CombinedOutput(ctx context.Context, s Spec) ([]byte, error) {
	cmd := s.command(ctx)
	// A single writer for both streams keeps stdout/stderr interleaved; os/exec
	// shares one pipe when Stdout == Stderr, so there is no concurrent write.
	buf := &capWriter{max: s.MaxOutput}
	cmd.Stdout, cmd.Stderr = buf, buf
	err := cmd.Run()
	out := buf.Bytes()
	if err != nil {
		return out, s.wrap(out, exitCode(cmd, err), err)
	}
	return out, nil
}

func (s Spec) wrap(output []byte, code int, err error) error {
	return &Error{
		Name:     s.Name,
		Args:     append([]string(nil), s.Args...),
		Dir:      s.Dir,
		ExitCode: code,
		Output:   output,
		Err:      err,
	}
}

// exitCode extracts the process exit code, returning -1 when the process never
// produced one (e.g. it could not start or was killed).
func exitCode(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return 0
	}
	return -1
}

// capWriter is an io.Writer that retains only the first max bytes written to it
// (followed by a single truncationMarker once the limit is exceeded) while still
// accepting and discarding the rest. Capturing through it keeps memory bounded
// for chatty commands instead of buffering everything and truncating afterward,
// while continuing to drain the pipe so the child process never blocks. A max
// of <= 0 means no limit.
type capWriter struct {
	max       int
	buf       []byte
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
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

// Bytes returns the retained output (capped, with a trailing marker when the
// output was truncated).
func (w *capWriter) Bytes() []byte { return w.buf }

// Package-level helpers run a Spec with the real OS runner. They are the drop-in
// replacements for one-off exec.Command(...).Output()/.CombinedOutput()/.Run()
// calls.

// Run executes s with the OS runner.
func Run(ctx context.Context, s Spec) (Result, error) { return OS{}.Run(ctx, s) }

// Output runs s with the OS runner and returns its standard output.
func Output(ctx context.Context, s Spec) ([]byte, error) { return OS{}.Output(ctx, s) }

// CombinedOutput runs s with the OS runner and returns interleaved output.
func CombinedOutput(ctx context.Context, s Spec) ([]byte, error) { return OS{}.CombinedOutput(ctx, s) }
