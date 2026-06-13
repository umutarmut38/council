package main

// Pre-launch setup: run config-declared commands before any agent starts —
// one-shot setup (run to completion) or supervised background services (e.g.
// a proxy) that council keeps alive for the session and stops on exit. This
// is the vendor-agnostic primitive behind integrations like a context-
// compression proxy: council just runs the command and sets env vars; what
// the command is, is entirely the user's choice.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/umutarmut38/council/internal/config"
)

// setupSession tracks the background processes started for a council session
// so they can be torn down on exit.
type setupSession struct {
	background []*exec.Cmd
}

// One council invocation runs setup at most once, regardless of how many agent
// phases it drives (the interactive multiplexer, or `council run` chaining
// plan→vote→build). stopSetup() tears the background processes down; main()
// calls it once on exit.
var (
	setupState *setupSession
	setupDone  bool
)

// ensureSetup runs cfg.Setup before agents launch, exactly once per process.
// One-shot commands run to completion (a non-zero exit aborts startup);
// background commands are started and supervised until stopSetup().
func ensureSetup(cfg config.Config) error {
	if setupDone {
		return nil
	}
	setupDone = true
	if len(cfg.Setup) == 0 {
		return nil
	}
	sess := &setupSession{}
	setupState = sess

	for _, sc := range cfg.Setup {
		if len(sc.Command) == 0 {
			continue
		}
		if _, lookErr := exec.LookPath(sc.Command[0]); lookErr != nil {
			sess.stop()
			return fmt.Errorf("setup %q: %s not found in PATH", sc.Label(), sc.Command[0])
		}

		if sc.Background {
			fmt.Fprintf(os.Stderr, "council: starting %q\n", sc.Label())
			cmd := exec.Command(sc.Command[0], sc.Command[1:]...)
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr // visible, off the TUI
			if startErr := cmd.Start(); startErr != nil {
				sess.stop()
				return fmt.Errorf("setup %q: %w", sc.Label(), startErr)
			}
			sess.background = append(sess.background, cmd)
			if sc.WaitForPort > 0 {
				if waitErr := waitForPort(sc.WaitForPort, 10*time.Second); waitErr != nil {
					sess.stop()
					return fmt.Errorf("setup %q: %w", sc.Label(), waitErr)
				}
			}
			continue
		}

		fmt.Fprintf(os.Stderr, "council: running %q\n", sc.Label())
		cmd := exec.Command(sc.Command[0], sc.Command[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			sess.stop()
			return fmt.Errorf("setup %q failed: %w", sc.Label(), runErr)
		}
	}
	return nil
}

// stopSetup tears down any supervised background setup processes.
func stopSetup() {
	if setupState != nil {
		setupState.stop()
	}
}

// stop terminates every supervised background process: SIGINT, then a short
// grace period to exit cleanly, then Kill for any straggler. Each process is
// reaped with Wait (the only thing that releases it and populates
// ProcessState), so none are left as zombies.
func (s *setupSession) stop() {
	const grace = 800 * time.Millisecond
	var wg sync.WaitGroup
	for _, cmd := range s.background {
		if cmd.Process == nil {
			continue
		}
		_ = cmd.Process.Signal(os.Interrupt)
		wg.Add(1)
		go func(c *exec.Cmd) {
			defer wg.Done()
			done := make(chan error, 1)
			go func() { done <- c.Wait() }()
			select {
			case <-done: // exited within the grace period
			case <-time.After(grace):
				_ = c.Process.Kill()
				<-done // reap the killed process
			}
		}(cmd)
	}
	wg.Wait()
	s.background = nil
}

// waitForPort blocks until 127.0.0.1:port accepts a TCP connection, or the
// timeout elapses.
func waitForPort(port int, timeout time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s after %s", addr, timeout)
}

// setupSummary is a one-line description of configured setup for doctor.
func setupSummary(sc config.SetupCommand) string {
	kind := "run-and-wait"
	if sc.Background {
		kind = "background"
		if sc.WaitForPort > 0 {
			kind += fmt.Sprintf(", wait for :%d", sc.WaitForPort)
		}
	}
	return fmt.Sprintf("%s  [%s]  $ %s", sc.Label(), kind, strings.Join(sc.Command, " "))
}
