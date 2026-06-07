//go:build windows

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"

	"github.com/umutarmut38/council/internal/config"
)

// windowsPTY backs a Session with a Windows pseudo console (ConPTY).
type windowsPTY struct {
	cpty      *conpty.ConPty
	closeOnce sync.Once

	mu         sync.Mutex
	terminated bool
}

func startPTY(cfg config.AgentConfig, cwd string, env []string, cols, rows int) (ptyConn, error) {
	cmdLine, err := buildWindowsCommandLine(cfg.Command)
	if err != nil {
		return nil, err
	}

	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyEnv(env),
	}
	if cwd != "" && cwd != "." {
		opts = append(opts, conpty.ConPtyWorkDir(cwd))
	}

	cpty, err := conpty.Start(cmdLine, opts...)
	if err != nil {
		return nil, err
	}
	return &windowsPTY{cpty: cpty}, nil
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.cpty.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.cpty.Write(b) }

func (p *windowsPTY) Resize(cols, rows int) error { return p.cpty.Resize(cols, rows) }

func (p *windowsPTY) Wait() (*int, error) {
	// Closing the pseudo console releases the process handle, so once we have
	// force-terminated there is no exit code left to read.
	if p.isTerminated() {
		return nil, nil
	}

	code, err := p.cpty.Wait(context.Background())

	// A concurrent Close (e.g. Terminate) races with Wait by closing the process
	// handle out from under it; treat that as a clean, code-less exit rather than
	// surfacing the resulting handle error.
	if p.isTerminated() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := int(code)
	return &c, nil
}

func (p *windowsPTY) isTerminated() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.terminated
}

func (p *windowsPTY) Interrupt() {
	// ConPTY has no signal channel; the closest equivalent is feeding Ctrl+C
	// into the console input.
	_, _ = p.cpty.Write([]byte{0x03})
}

// unblockRead closes the pseudo console once the child has exited. The ConPTY
// output pipe never reaches EOF on its own, so a blocked Read only returns after
// the console host is torn down. A short grace gives the reader a chance to
// drain the child's final output before the pipe is severed.
func (p *windowsPTY) unblockRead() {
	if p.isTerminated() {
		return
	}
	time.Sleep(60 * time.Millisecond)
	_ = p.Close()
}

func (p *windowsPTY) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.terminated = true
		p.mu.Unlock()
		// ClosePseudoConsole terminates the attached process and releases the
		// console and its pipe handles.
		_ = p.cpty.Close()
	})
	return nil
}

func isPTYClosed(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_HANDLE_EOF) ||
		errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, windows.ERROR_INVALID_HANDLE) ||
		errors.Is(err, windows.ERROR_OPERATION_ABORTED)
}

// buildWindowsCommandLine turns a configured argv into a command line string for
// CreateProcess. Batch shims (.cmd/.bat) — common for npm-installed agent CLIs
// like claude or codex — cannot be executed directly by CreateProcess, so they
// are wrapped in the command interpreter.
func buildWindowsCommandLine(command []string) (string, error) {
	if len(command) == 0 {
		return "", errors.New("no command configured")
	}

	name := command[0]
	args := command[1:]

	exe, err := exec.LookPath(name)
	if err != nil {
		// Fall back to the raw name and let CreateProcess search the PATH.
		exe = name
	}

	lower := strings.ToLower(exe)
	if strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".cmd") {
		// "cmd.exe /s /c "<command>"" strips exactly the outer pair of quotes,
		// leaving the inner, individually quoted arguments intact.
		inner := joinArgv(append([]string{exe}, args...))
		return joinArgv([]string{comspec(), "/s", "/c"}) + " \"" + inner + "\"", nil
	}

	return joinArgv(append([]string{exe}, args...)), nil
}

func joinArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = syscall.EscapeArg(a)
	}
	return strings.Join(parts, " ")
}

func comspec() string {
	if c := os.Getenv("COMSPEC"); c != "" {
		return c
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		return root + `\System32\cmd.exe`
	}
	return "cmd.exe"
}
