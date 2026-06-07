//go:build !windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/umutarmut38/council/internal/config"
)

// unixPTY backs a Session with a real pseudo-terminal via creack/pty.
type unixPTY struct {
	ptmx      *os.File
	cmd       *exec.Cmd
	closeOnce sync.Once
}

func startPTY(cfg config.AgentConfig, cwd string, env []string, cols, rows int) (ptyConn, error) {
	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cwd
	cmd.Env = env

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	return &unixPTY{ptmx: ptmx, cmd: cmd}, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.ptmx.Write(b) }

func (p *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (p *unixPTY) Wait() (*int, error) {
	err := p.cmd.Wait()
	return exitCodeFromError(err), err
}

func (p *unixPTY) Interrupt() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}
}

// unblockRead is a no-op: the PTY master reaches EOF on its own once the child
// exits, so the reader drains naturally before Close.
func (p *unixPTY) unblockRead() {}

func (p *unixPTY) Close() error {
	p.closeOnce.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.ptmx.Close()
	})
	return nil
}

func exitCodeFromError(err error) *int {
	if err == nil {
		code := 0
		return &code
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return &code
	}
	return nil
}

func isPTYClosed(err error) bool {
	return errors.Is(err, syscall.EIO)
}
