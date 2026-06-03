package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/umutarmut38/council/internal/config"
)

type OutputFunc func(name string, data []byte)
type ExitFunc func(name string, exitCode *int, err error)

type Session struct {
	Name       string
	Config     config.AgentConfig
	Cmd        *exec.Cmd
	PTY        *os.File
	RawLogPath string
	StartError error
	Done       bool
	ExitCode   *int

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

	cmd := exec.Command(s.Config.Command[0], s.Config.Command[1:]...)
	cmd.Dir = cwd
	cmd.Env = terminalEnv(s.Config)

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

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		_ = rawLog.Close()
		return err
	}

	s.mu.Lock()
	s.Cmd = cmd
	s.PTY = ptmx
	s.rawLog = rawLog
	s.mu.Unlock()

	go s.readLoop(onOutput, onExit)
	return nil
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
	if s.PTY == nil {
		return errors.New("agent pty is not available")
	}
	_, err := s.PTY.Write([]byte(value))
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
	ptmx := s.PTY
	done := s.Done
	s.mu.Unlock()

	if done || ptmx == nil || cols <= 0 || rows <= 0 || !resizeEnabled(s.Config) {
		return nil
	}
	return pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (s *Session) Terminate() error {
	s.mu.Lock()
	cmd := s.Cmd
	ptmx := s.PTY
	done := s.Done
	s.mu.Unlock()

	if done {
		return nil
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		time.Sleep(120 * time.Millisecond)
		_ = cmd.Process.Kill()
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
	return nil
}

func (s *Session) readLoop(onOutput OutputFunc, onExit ExitFunc) {
	buf := make([]byte, 4096)
	for {
		n, readErr := s.PTY.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if s.rawLog != nil {
				_, _ = s.rawLog.Write(chunk)
			}
			if onOutput != nil {
				onOutput(s.Name, chunk)
			}
		}
		if readErr != nil {
			waitErr := s.wait()
			exitCode := exitCodeFromError(waitErr)
			finalErr := normalizeReadExitError(readErr, waitErr)
			s.finish(exitCode)
			if s.rawLog != nil {
				_ = s.rawLog.Close()
			}
			if onExit != nil {
				onExit(s.Name, exitCode, finalErr)
			}
			return
		}
	}
}

func (s *Session) wait() error {
	s.mu.Lock()
	cmd := s.Cmd
	s.mu.Unlock()

	if cmd == nil {
		return nil
	}
	return cmd.Wait()
}

func (s *Session) finish(exitCode *int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Done = true
	s.ExitCode = exitCode
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

func normalizeReadExitError(readErr error, waitErr error) error {
	if waitErr != nil {
		return waitErr
	}
	if readErr == nil || errors.Is(readErr, io.EOF) || errors.Is(readErr, os.ErrClosed) || errors.Is(readErr, syscall.EIO) {
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
