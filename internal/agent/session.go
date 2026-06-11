package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/umutarmut38/council/internal/config"
)

type OutputFunc func(name string, data []byte)
type ExitFunc func(name string, exitCode *int, err error)

// ptyConn is the platform-specific pseudo-terminal backing a Session. Unix
// builds back it with creack/pty; Windows builds back it with the ConPTY API.
type ptyConn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	// Wait blocks until the child process exits and returns its exit code
	// (nil when unknown) along with any wait error.
	Wait() (*int, error)
	// Interrupt makes a best-effort attempt to stop the child gracefully.
	Interrupt()
	// unblockRead releases a Read that will not end on its own once the child
	// has exited. ConPTY needs this (its output pipe never reaches EOF); a Unix
	// PTY master reaches EOF by itself, so its implementation is a no-op and the
	// reader is allowed to drain fully before Close.
	unblockRead()
	// Close terminates the child and releases the pseudo-terminal. It is safe
	// to call more than once.
	Close() error
}

type Session struct {
	Name       string
	Config     config.AgentConfig
	RawLogPath string
	StartError error
	Done       bool
	ExitCode   *int

	conn        ptyConn
	rawLog      *os.File
	desiredCols int
	desiredRows int
	mu          sync.Mutex
}

func NewSession(name string, cfg config.AgentConfig, rawLogPath string) *Session {
	return &Session{
		Name:       name,
		Config:     cfg,
		RawLogPath: rawLogPath,
	}
}

func (s *Session) Start(onOutput OutputFunc, onExit ExitFunc) error {
	if len(s.Config.Command) == 0 {
		return errors.New("no command configured")
	}

	if err := os.MkdirAll(filepath.Dir(s.RawLogPath), 0o755); err != nil {
		return err
	}

	rawLog, err := os.OpenFile(s.RawLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	cwd, err := expandPath(s.Config.CWD)
	if err != nil {
		_ = rawLog.Close()
		return err
	}

	cols, rows := s.startupSize()

	conn, err := startPTY(s.Config, cwd, terminalEnv(s.Config), cols, rows)
	if err != nil {
		_ = rawLog.Close()
		return err
	}

	s.mu.Lock()
	s.conn = conn
	s.rawLog = rawLog
	s.mu.Unlock()

	go s.run(onOutput, onExit)
	return nil
}

// startupSize resolves the initial pseudo-terminal dimensions, honoring a fixed
// PTY size when configured and falling back to sane defaults.
func (s *Session) startupSize() (int, int) {
	s.mu.Lock()
	cols := s.desiredCols
	rows := s.desiredRows
	s.mu.Unlock()

	if s.Config.Terminal.PTYSize == "fixed" {
		cols = s.Config.Terminal.Cols
		rows = s.Config.Terminal.Rows
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}
	return cols, rows
}

func (s *Session) MarkStartError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.StartError = err
	s.Done = true
}

func (s *Session) WriteString(value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Done {
		return errors.New("agent has exited")
	}
	if s.conn == nil {
		return errors.New("agent pty is not available")
	}
	_, err := s.conn.Write([]byte(value))
	return err
}

func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	if s.Config.Terminal.PTYSize == "fixed" {
		s.desiredCols = s.Config.Terminal.Cols
		s.desiredRows = s.Config.Terminal.Rows
	} else {
		s.desiredCols = cols
		s.desiredRows = rows
	}
	conn := s.conn
	done := s.Done
	s.mu.Unlock()

	if done || conn == nil || cols <= 0 || rows <= 0 || !resizeEnabled(s.Config) {
		return nil
	}
	return conn.Resize(cols, rows)
}

func (s *Session) Terminate() error {
	s.mu.Lock()
	conn := s.conn
	done := s.Done
	s.mu.Unlock()

	if done || conn == nil {
		return nil
	}

	conn.Interrupt()
	time.Sleep(120 * time.Millisecond)
	_ = conn.Close()
	return nil
}

// run drives the session lifecycle. The reader and the process waiter run
// concurrently because a ConPTY output pipe does not reach EOF when the child
// exits — the waiter has to release the reader once the process is gone.
func (s *Session) run(onOutput OutputFunc, onExit ExitFunc) {
	readDone := make(chan struct{})
	procDone := make(chan struct{})

	var (
		exitCode    *int
		waitErr     error
		lastReadErr error
	)

	go func() {
		defer close(readDone)
		// A larger read buffer coalesces bursty PTY output into fewer messages,
		// which keeps the TUI smooth while agents stream.
		buf := make([]byte, 32768)
		for {
			n, err := s.conn.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				if s.rawLog != nil {
					_, _ = s.rawLog.Write(chunk)
				}
				if onOutput != nil {
					onOutput(s.Name, chunk)
				}
			}
			if err != nil {
				lastReadErr = err
				return
			}
		}
	}()

	go func() {
		defer close(procDone)
		exitCode, waitErr = s.conn.Wait()
		s.conn.unblockRead()
	}()

	<-procDone
	<-readDone

	finalErr := normalizeReadExitError(lastReadErr, waitErr)
	_ = s.conn.Close()
	s.finish(exitCode)
	if s.rawLog != nil {
		_ = s.rawLog.Close()
	}
	if onExit != nil {
		onExit(s.Name, exitCode, finalErr)
	}
}

func (s *Session) finish(exitCode *int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Done = true
	s.ExitCode = exitCode
}

func normalizeReadExitError(readErr error, waitErr error) error {
	if waitErr != nil {
		return waitErr
	}
	if readErr == nil || errors.Is(readErr, io.EOF) || errors.Is(readErr, os.ErrClosed) || isPTYClosed(readErr) {
		return nil
	}
	return readErr
}

func resizeEnabled(cfg config.AgentConfig) bool {
	if cfg.Terminal.PTYSize == "fixed" {
		return false
	}
	if cfg.Terminal.Resize == nil {
		return true
	}
	return *cfg.Terminal.Resize
}

func colorEnabled(cfg config.AgentConfig) bool {
	if cfg.Terminal.Color == nil {
		return true
	}
	return *cfg.Terminal.Color
}

func terminalEnv(cfg config.AgentConfig) []string {
	env := append([]string{}, os.Environ()...)
	if colorEnabled(cfg) {
		env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
		return env
	}
	env = append(env, "TERM=dumb", "NO_COLOR=1")
	return env
}

func expandPath(path string) (string, error) {
	if path == "" {
		return ".", nil
	}
	if path == "~" {
		return os.UserHomeDir()
	}
	if len(path) > 2 && path[0] == '~' && os.IsPathSeparator(path[1]) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve cwd %q: %w", path, err)
	}
	return abs, nil
}
