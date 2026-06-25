// Package cmdrun runs external commands with a working directory, environment
// overrides, output size caps, context-based timeouts, and structured errors
// that carry the command, its arguments, the exit code, and the captured
// output. It centralizes the behavior the codebase previously repeated around
// every exec.Command call.
//
// It deliberately does NOT cover the PTY/agent-startup path, which needs raw
// terminal handling, nor long-lived supervised processes.
package cmdrun

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/umutarmut38/council/internal/capbuf"
)

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
	// is truncated, capbuf.TruncationMarker is appended.
	MaxOutput int
}

// Result is the captured outcome of a command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	Combined []byte // populated by CombinedOutput; nil otherwise
	ExitCode int    // process exit code, or -1 when the process did not run
}

// OS runs external commands via os/exec. The package-level Run/Output/
// CombinedOutput helpers are the usual entry points; OS is exposed so tests can
// call the methods directly.
type OS struct{}

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
	stdout, stderr := &capbuf.Writer{Max: s.MaxOutput}, &capbuf.Writer{Max: s.MaxOutput}
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
	stdout, stderr := &capbuf.Writer{Max: s.MaxOutput}, &capbuf.Writer{Max: s.MaxOutput}
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
	buf := &capbuf.Writer{Max: s.MaxOutput}
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

// Package-level helpers run a Spec with the real OS runner. They are the drop-in
// replacements for one-off exec.Command(...).Output()/.CombinedOutput()/.Run()
// calls.

// Run executes s with the OS runner.
func Run(ctx context.Context, s Spec) (Result, error) { return OS{}.Run(ctx, s) }

// Output runs s with the OS runner and returns its standard output.
func Output(ctx context.Context, s Spec) ([]byte, error) { return OS{}.Output(ctx, s) }

// CombinedOutput runs s with the OS runner and returns interleaved output.
func CombinedOutput(ctx context.Context, s Spec) ([]byte, error) { return OS{}.CombinedOutput(ctx, s) }
