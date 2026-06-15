package main

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/umutarmut38/council/internal/config"
)

func resetSetup() {
	setupDone = false
	setupState = nil
}

func TestEnsureSetupRunsOneShotAndIsIdempotent(t *testing.T) {
	resetSetup()
	t.Cleanup(func() { stopSetup(); resetSetup() })

	cfg := config.Config{Setup: []config.SetupCommand{
		{Name: "noop", Command: []string{"true"}},
	}}
	if err := ensureSetup(cfg); err != nil {
		t.Fatalf("one-shot setup: %v", err)
	}
	// Second call is a no-op (guard), even with a command that would fail.
	if err := ensureSetup(config.Config{Setup: []config.SetupCommand{
		{Command: []string{"false"}},
	}}); err != nil {
		t.Fatalf("second ensureSetup should be a no-op, got %v", err)
	}
}

func TestEnsureSetupAbortsOnFailingOneShot(t *testing.T) {
	resetSetup()
	t.Cleanup(func() { stopSetup(); resetSetup() })

	err := ensureSetup(config.Config{Setup: []config.SetupCommand{
		{Name: "boom", Command: []string{"false"}},
	}})
	if err == nil {
		t.Fatal("a one-shot command exiting non-zero must abort setup")
	}
}

func TestEnsureSetupRejectsEmptyCommand(t *testing.T) {
	resetSetup()
	t.Cleanup(func() { stopSetup(); resetSetup() })

	err := ensureSetup(config.Config{Setup: []config.SetupCommand{
		{Name: "missing"},
	}})
	if err == nil {
		t.Fatal("empty setup command should fail")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("empty setup command error should explain the problem, got %v", err)
	}
}

func TestEnsureSetupBackgroundWaitsForPortAndStops(t *testing.T) {
	resetSetup()
	t.Cleanup(func() { stopSetup(); resetSetup() })

	// A listener stands in for the daemon's port being ready.
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	port := srv.Addr().(*net.TCPAddr).Port

	cfg := config.Config{Setup: []config.SetupCommand{
		{Name: "daemon", Command: []string{"sleep", "30"}, Background: true, WaitForPort: port},
	}}
	start := time.Now()
	if err := ensureSetup(cfg); err != nil {
		t.Fatalf("background setup with ready port: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("readiness gate took too long for an already-listening port")
	}
	if setupState == nil || len(setupState.background) != 1 {
		t.Fatalf("background process not tracked: %+v", setupState)
	}
	cmd := setupState.background[0]
	stopSetup() // must terminate AND reap the supervised sleeper

	// stop() Waits on each process, so once it returns the sleeper is reaped:
	// ProcessState is non-nil (a leaked zombie would leave it nil) and it did
	// not exit on its own (it was signaled/killed).
	if cmd.ProcessState == nil {
		t.Fatal("background process not reaped after stopSetup (ProcessState is nil)")
	}
	if cmd.ProcessState.Success() {
		t.Fatal("supervised `sleep 30` should have been signaled/killed, not exited successfully")
	}
}

func TestWaitForPortTimesOut(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // nothing listening now

	if err := waitForPort(port, 600*time.Millisecond); err == nil {
		t.Fatal("waitForPort should time out when nothing is listening")
	}
}

func TestWaitForPortRejectsInvalidPort(t *testing.T) {
	if err := waitForPort(70000, time.Minute); err == nil {
		t.Fatal("waitForPort should reject invalid ports")
	} else if !strings.Contains(err.Error(), "invalid wait_for_port") {
		t.Fatalf("invalid port error should explain the problem, got %v", err)
	}
}
