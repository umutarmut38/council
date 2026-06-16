package main

// Pre-launch setup: run config-declared commands before any agent starts —
// one-shot setup (run to completion) or supervised background services (e.g.
// a proxy) that council keeps alive for the session and stops on exit. This
// is the vendor-agnostic primitive behind integrations like a context-
// compression proxy: council just runs the command and sets env vars; what
// the command is, is entirely the user's choice.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/setup"
)

// bgProc pairs a supervised background process with its observability handle so
// teardown can record the result.
type bgProc struct {
	cmd *exec.Cmd
	h   *setup.Handle
}

// setupSession tracks the background processes started for a council session
// so they can be torn down on exit.
type setupSession struct {
	background []bgProc
}

// One council invocation runs setup at most once, regardless of how many agent
// phases it drives (the interactive multiplexer, or `council run` chaining
// plan→vote→build). stopSetup() tears the background processes down; main()
// calls it once on exit. setupStatus is the observability snapshot the TUI
// renders via /setup.
var (
	setupState  *setupSession
	setupStatus *setup.Status
	setupDone   bool
)

// ensureSetup runs cfg.Setup before agents launch, exactly once per process.
// One-shot commands run to completion (a non-zero exit aborts startup);
// background commands are started and supervised until stopSetup().
func ensureSetup(cfg config.Config) error {
	if setupDone {
		return nil
	}
	setupDone = true
	// Surface the exported env keys (never values) even when no setup commands
	// run, so /setup can show what agents inherit.
	if len(cfg.Env) == 0 && len(cfg.Setup) == 0 {
		return nil
	}
	status := setup.New()
	status.SetEnvKeys(cfg.Env)
	setupStatus = status
	if len(cfg.Setup) == 0 {
		return nil
	}
	sess := &setupSession{}
	setupState = sess

	for _, sc := range cfg.Setup {
		kind := setup.KindOneShot
		if sc.Background {
			kind = setup.KindBackground
		}
		h := status.Begin(sc.Label(), sc.Command, kind, sc.WaitForPort)

		if len(sc.Command) == 0 {
			h.Failed(errors.New("command is required"))
			sess.stop()
			return fmt.Errorf("setup %q: command is required", sc.Label())
		}
		if _, lookErr := exec.LookPath(sc.Command[0]); lookErr != nil {
			h.Failed(fmt.Errorf("%s not found in PATH", sc.Command[0]))
			sess.stop()
			return fmt.Errorf("setup %q: %s not found in PATH", sc.Label(), sc.Command[0])
		}

		// Keep setup output visible off the TUI (stderr) and capture it for
		// /setup. One MultiWriter feeds both streams to keep them interleaved.
		out := io.MultiWriter(os.Stderr, h.Writer())

		if sc.Background {
			fmt.Fprintf(os.Stderr, "council: starting %q\n", sc.Label())
			cmd := exec.Command(sc.Command[0], sc.Command[1:]...)
			cmd.Stdout, cmd.Stderr = out, out
			if startErr := cmd.Start(); startErr != nil {
				h.Failed(startErr)
				sess.stop()
				return fmt.Errorf("setup %q: %w", sc.Label(), startErr)
			}
			h.Running(cmd.Process.Pid)
			sess.background = append(sess.background, bgProc{cmd: cmd, h: h})
			if sc.WaitForPort > 0 {
				if waitErr := waitForPort(sc.WaitForPort, 10*time.Second); waitErr != nil {
					h.Failed(waitErr)
					sess.stop()
					return fmt.Errorf("setup %q: %w", sc.Label(), waitErr)
				}
				h.Ready()
			}
			continue
		}

		fmt.Fprintf(os.Stderr, "council: running %q\n", sc.Label())
		cmd := exec.Command(sc.Command[0], sc.Command[1:]...)
		cmd.Stdout, cmd.Stderr = out, out
		if runErr := cmd.Run(); runErr != nil {
			h.Failed(runErr)
			sess.stop()
			return fmt.Errorf("setup %q failed: %w", sc.Label(), runErr)
		}
		h.Succeeded()
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
	for _, bp := range s.background {
		if bp.cmd.Process == nil {
			continue
		}
		_ = bp.cmd.Process.Signal(os.Interrupt)
		wg.Add(1)
		go func(bp bgProc) {
			defer wg.Done()
			done := make(chan error, 1)
			go func() { done <- bp.cmd.Wait() }()
			select {
			case <-done: // exited within the grace period
				bp.h.Stopped(nil)
			case <-time.After(grace):
				_ = bp.cmd.Process.Kill()
				<-done // reap the killed process
				bp.h.Stopped(errors.New("killed after grace period"))
			}
		}(bp)
	}
	wg.Wait()
	s.background = nil
}

// waitForPort blocks until 127.0.0.1:port accepts a TCP connection, or the
// timeout elapses.
func waitForPort(port int, timeout time.Duration) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid wait_for_port %d: must be between 1 and 65535", port)
	}
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
