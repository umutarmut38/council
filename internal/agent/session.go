package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
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
	rawLog      atomic.Pointer[os.File]
	desiredCols int
	desiredRows int
	mu          sync.Mutex

	// Until EnableRawLog attaches a file (the interactive run dir is created
	// lazily on the first prompt), pre-prompt PTY output — banners, auth
	// prompts, startup errors, early exits — is buffered here and flushed into
	// the log once it exists, so it isn't lost. Guarded by rawMu, capped at
	// maxRawBuffer.
	rawMu         sync.Mutex
	rawBuf        []byte
	rawBufFlushed bool
}

// maxRawBuffer bounds the pre-run raw-output buffer per session so an agent that
// streams a lot before the first prompt can't grow it without limit. The
// earliest bytes (startup banner/auth prompt) are the useful ones, so buffering
// stops once full rather than evicting them.
const maxRawBuffer = 1 << 20 // 1 MiB

// maxCoalesceBytes caps how much PTY output the forwarder merges into a single
// onOutput call. The TUI processes one message at a time and pays a large fixed
// cost per AgentOutputMsg (regex transcript-clean + terminal emulation + a Model
// copy, ~15–30µs and ~130 allocs each), so a flooding agent that emits many
// small reads saturates the Update loop and starves keystrokes. Coalescing turns
// a burst of reads into far fewer messages; the cap keeps any one merged message
// small enough (~a few ms to process) that a queued keystroke is never stuck
// behind a huge chunk. See internal/tui benchmarks for the measured effect.
const maxCoalesceBytes = 64 << 10 // 64 KiB

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

	// The raw PTY log lives under the run directory, which the interactive TUI
	// creates lazily on the first prompt. When no path is set yet, start without
	// a log; EnableRawLog wires it up once the run exists. Read RawLogPath under
	// the lock because EnableRawLog may set it concurrently, so a /restart after
	// the first prompt relaunches with logging from process start.
	s.mu.Lock()
	rawPath := s.RawLogPath
	s.mu.Unlock()

	var rawLog *os.File
	if rawPath != "" {
		if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(rawPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		rawLog = f
	}

	cwd, err := expandPath(s.Config.CWD)
	if err != nil {
		closeFile(rawLog)
		return err
	}

	cols, rows := s.startupSize()

	conn, err := startPTY(s.Config, cwd, terminalEnv(s.Config), cols, rows)
	if err != nil {
		closeFile(rawLog)
		return err
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	if rawLog != nil {
		s.rawLog.Store(rawLog)
	}

	go s.run(onOutput, onExit)
	return nil
}

// EnableRawLog opens the raw PTY log once the run directory exists, turning on
// persistence for a session that started before the first prompt. Any output
// buffered before now (startup banner, auth prompt, early errors) is flushed
// into the log first, so nothing emitted before the first prompt is lost. It is
// a no-op if logging is already on or the path is empty, and is safe to call
// from the TUI update loop while the session's reader goroutine is streaming.
func (s *Session) EnableRawLog(path string) error {
	if path == "" || s.rawLog.Load() != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	// Flush the pre-run buffer and publish the file under rawMu, so the reader
	// goroutine (which buffers under the same lock while rawLog is nil) can't
	// append output that lands neither in the buffer nor the file.
	s.rawMu.Lock()
	if s.rawLog.Load() != nil { // lost a race with another EnableRawLog
		s.rawMu.Unlock()
		closeFile(f)
		return nil
	}
	if len(s.rawBuf) > 0 {
		_, _ = f.Write(s.rawBuf)
	}
	s.rawBuf = nil
	s.rawBufFlushed = true
	s.rawLog.Store(f)
	s.rawMu.Unlock()

	// Record the path (for a later /restart to relaunch with the same log) and
	// check whether the session finished before we attached — if so the reader
	// goroutine already swapped nil out and won't close ours, so reclaim it.
	s.mu.Lock()
	s.RawLogPath = path
	done := s.Done
	s.mu.Unlock()
	if done {
		if rl := s.rawLog.Swap(nil); rl != nil {
			closeFile(rl)
		}
	}
	return nil
}

// bufferRawOutput retains PTY output emitted before a raw log is attached. It is
// called from the reader goroutine when rawLog is nil; the rawMu re-check covers
// the window where EnableRawLog attaches the file between the Load and the lock.
func (s *Session) bufferRawOutput(chunk []byte) {
	s.rawMu.Lock()
	defer s.rawMu.Unlock()
	if rl := s.rawLog.Load(); rl != nil {
		_, _ = rl.Write(chunk)
		return
	}
	if s.rawBufFlushed {
		return
	}
	if room := maxRawBuffer - len(s.rawBuf); room > 0 {
		if len(chunk) > room {
			chunk = chunk[:room]
		}
		s.rawBuf = append(s.rawBuf, chunk...)
	}
}

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
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

	// Release the raw log even when the session never started (conn == nil) or
	// already exited: EnableRawLog may have opened it before the first prompt,
	// and an open file blocks Windows from deleting the run directory. The
	// Swap is atomic, so it races safely with the reader goroutine and with
	// run()'s own close.
	if rl := s.rawLog.Swap(nil); rl != nil {
		_ = rl.Close()
	}
	// Drop any pre-run buffer (no run dir was ever attached to flush it to) and
	// stop the reader from buffering more.
	s.rawMu.Lock()
	s.rawBuf = nil
	s.rawBufFlushed = true
	s.rawMu.Unlock()

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
	fwdDone := make(chan struct{})

	var (
		exitCode    *int
		waitErr     error
		lastReadErr error
	)

	// chunks decouples the PTY reader from the TUI: the reader is never blocked
	// by a slow Update loop (up to the buffer), and the forwarder coalesces
	// whatever has piled up into one onOutput call, so a flood becomes few
	// messages instead of one per read. The raw log is still written per read in
	// the reader (order preserved), so coalescing only affects the TUI stream.
	chunks := make(chan []byte, 512)

	// Forwarder: merge-and-emit. Runs onOutput off the reader goroutine.
	go func() {
		defer close(fwdDone)
		coalesceOutput(chunks, maxCoalesceBytes, func(b []byte) {
			if onOutput != nil {
				onOutput(s.Name, b)
			}
		})
	}()

	// Reader: raw-log per read, then hand the chunk to the forwarder.
	go func() {
		defer close(readDone)
		buf := make([]byte, 32768)
		for {
			n, err := s.conn.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				if rl := s.rawLog.Load(); rl != nil {
					_, _ = rl.Write(chunk)
				} else {
					s.bufferRawOutput(chunk)
				}
				chunks <- chunk
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
	// No more reads: let the forwarder flush any coalesced tail before we report
	// the exit, so all output is delivered to the TUI ahead of the exit message.
	close(chunks)
	<-fwdDone

	finalErr := normalizeReadExitError(lastReadErr, waitErr)
	_ = s.conn.Close()
	s.finish(exitCode)
	if rl := s.rawLog.Swap(nil); rl != nil {
		_ = rl.Close()
	}
	if onExit != nil {
		onExit(s.Name, exitCode, finalErr)
	}
}

// coalesceOutput drains chunks from in, merging everything immediately available
// (bounded by maxBytes per emit) into a single emit call. A burst of PTY reads
// becomes far fewer TUI messages, without delaying sparse output (when nothing
// else is queued the first chunk is emitted at once) and without reordering
// bytes (concatenation is exactly what the terminal would have received). It
// returns once in is closed, after emitting any final merged bytes.
func coalesceOutput(in <-chan []byte, maxBytes int, emit func([]byte)) {
	for first := range in {
		merged := first
	drain:
		for len(merged) < maxBytes {
			select {
			case more, ok := <-in:
				if !ok {
					emit(merged)
					return
				}
				merged = append(merged, more...)
			default:
				break drain
			}
		}
		emit(merged)
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
	} else {
		env = append(env, "TERM=dumb", "NO_COLOR=1")
	}
	// Config-supplied env is appended last so it overrides the inherited
	// shell environment (a later KEY=VALUE wins).
	for _, kv := range sortedEnv(cfg.Env) {
		env = append(env, kv)
	}
	return env
}

// sortedEnv renders an env map as KEY=VALUE entries in a deterministic order.
func sortedEnv(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
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
