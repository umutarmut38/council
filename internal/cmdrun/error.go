package cmdrun

import "strings"

// Error is the structured error returned when a command fails to start or exits
// non-zero. It carries the command line, working directory, exit code, and the
// captured output so callers (and logs) get a consistent, debuggable message,
// while still unwrapping to the underlying error for errors.Is/errors.As (e.g.
// *exec.ExitError or exec.ErrNotFound).
type Error struct {
	Name     string
	Args     []string
	Dir      string
	ExitCode int    // -1 when the process did not run
	Output   []byte // captured output (stderr for Output, combined elsewhere), already capped
	Err      error  // underlying error from os/exec
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Name)
	for _, a := range e.Args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	if e.Dir != "" {
		b.WriteString(" (in ")
		b.WriteString(e.Dir)
		b.WriteByte(')')
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	if out := strings.TrimSpace(string(e.Output)); out != "" {
		b.WriteString(": ")
		b.WriteString(out)
	}
	return b.String()
}

// Unwrap exposes the underlying os/exec error so errors.Is and errors.As keep
// working (e.g. errors.As(&exec.ExitError{}) or errors.Is(exec.ErrNotFound)).
func (e *Error) Unwrap() error { return e.Err }
