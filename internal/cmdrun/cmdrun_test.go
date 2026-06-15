package cmdrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// helperEnv gates TestHelperProcess: it only acts as a scripted command when set
// to "1" in a re-executed child, and is a no-op during the normal test run.
const helperEnv = "CMDRUN_HELPER_PROCESS"

// helperSpec builds a Spec that re-executes this test binary as a scripted
// command (handled by TestHelperProcess). This keeps the runner tests portable
// across operating systems instead of depending on Unix utilities like sh/cat.
func helperSpec(args ...string) Spec {
	return Spec{
		Name: os.Args[0],
		Args: append([]string{"-test.run=^TestHelperProcess$", "--"}, args...),
		Env:  map[string]string{helperEnv: "1"},
	}
}

func TestOutputSuccess(t *testing.T) {
	out, err := OS{}.Output(context.Background(), helperSpec("stdout", "hello"))
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("Output = %q, want %q", out, "hello")
	}
}

func TestCombinedOutputInterleavesStreams(t *testing.T) {
	out, err := OS{}.CombinedOutput(context.Background(), helperSpec("streams", "out", "err"))
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Fatalf("CombinedOutput = %q, want it to contain both streams", got)
	}
}

func TestRunCapturesStreamsSeparately(t *testing.T) {
	res, err := OS{}.Run(context.Background(), helperSpec("streams", "to-out", "to-err"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "to-out") {
		t.Fatalf("Stdout = %q", res.Stdout)
	}
	if !strings.Contains(string(res.Stderr), "to-err") {
		t.Fatalf("Stderr = %q", res.Stderr)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestNonZeroExitStructuredError(t *testing.T) {
	out, err := OS{}.Output(context.Background(), helperSpec("fail", "7", "boom"))
	if err == nil {
		t.Fatal("expected an error for a non-zero exit")
	}
	if len(out) != 0 {
		t.Fatalf("stdout = %q, want empty", out)
	}

	var cmdErr *Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error %T is not a *cmdrun.Error", err)
	}
	if cmdErr.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", cmdErr.ExitCode)
	}
	if cmdErr.Name != os.Args[0] {
		t.Fatalf("Name = %q, want %q", cmdErr.Name, os.Args[0])
	}
	if !strings.Contains(string(cmdErr.Output), "boom") {
		t.Fatalf("captured Output = %q, want it to contain stderr", cmdErr.Output)
	}
	if !strings.Contains(cmdErr.Error(), "boom") || !strings.Contains(cmdErr.Error(), "TestHelperProcess") {
		t.Fatalf("Error() = %q, want it to mention the command and output", cmdErr.Error())
	}

	// The structured error still unwraps to *exec.ExitError.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error does not unwrap to *exec.ExitError: %v", err)
	}
}

func TestErrorMessageIncludesDir(t *testing.T) {
	withDir := &Error{Name: "git", Args: []string{"status"}, Dir: "/tmp/work", Err: errors.New("boom")}
	if got := withDir.Error(); !strings.Contains(got, "/tmp/work") || !strings.Contains(got, "git status") {
		t.Fatalf("Error() = %q, want it to include the dir and command", got)
	}
	noDir := &Error{Name: "git", Args: []string{"status"}, Err: errors.New("boom")}
	if got := noDir.Error(); strings.Contains(got, "(in ") {
		t.Fatalf("Error() = %q, want no dir clause when Dir is empty", got)
	}
}

func TestExecutableNotFound(t *testing.T) {
	_, err := OS{}.Output(context.Background(), Spec{Name: "cmdrun-definitely-missing-binary-xyz"})
	if err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error %v should unwrap to exec.ErrNotFound", err)
	}
	var cmdErr *Error
	if !errors.As(err, &cmdErr) || cmdErr.ExitCode != -1 {
		t.Fatalf("want a *cmdrun.Error with ExitCode -1, got %v", err)
	}
}

func TestContextTimeoutCancelsCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := OS{}.CombinedOutput(ctx, helperSpec("sleep", "10s"))
	if err == nil {
		t.Fatal("expected an error when the context times out")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("command was not cancelled promptly: took %s", elapsed)
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("ctx.Err() = %v, want context.DeadlineExceeded", ctx.Err())
	}
}

func TestOutputCapTruncates(t *testing.T) {
	s := helperSpec("stdout", "aaaaaaaaaa") // 10 bytes
	s.MaxOutput = 4
	out, err := OS{}.Output(context.Background(), s)
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if want := "aaaa" + truncationMarker; string(out) != want {
		t.Fatalf("capped output = %q, want %q", out, want)
	}
}

func TestEnvOverride(t *testing.T) {
	s := helperSpec("echoenv", "CMDRUN_TEST_VAR")
	s.Env["CMDRUN_TEST_VAR"] = "injected"
	out, err := OS{}.Output(context.Background(), s)
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "injected" {
		t.Fatalf("env override = %q, want injected", out)
	}
}

func TestWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("in-workdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := helperSpec("readfile", "marker.txt")
	s.Dir = dir
	out, err := OS{}.Output(context.Background(), s)
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "in-workdir" {
		t.Fatalf("workdir read = %q, want in-workdir", out)
	}
}

func TestPackageHelpersUseOSRunner(t *testing.T) {
	out, err := Output(context.Background(), helperSpec("stdout", "ok"))
	if err != nil || string(out) != "ok" {
		t.Fatalf("Output helper = %q, %v", out, err)
	}
	if _, err := Run(context.Background(), helperSpec("stdout", "ignored")); err != nil {
		t.Fatalf("Run helper: %v", err)
	}
}

func TestFakeRecordsAndScriptsCalls(t *testing.T) {
	fake := &Fake{Handler: func(s Spec) (Result, error) {
		if s.Name == "git" && len(s.Args) > 0 && s.Args[0] == "status" {
			return Result{Stdout: []byte("clean")}, nil
		}
		return Result{Stderr: []byte("nope")}, errors.New("unexpected")
	}}

	out, err := fake.Output(context.Background(), Spec{Name: "git", Args: []string{"status"}})
	if err != nil || string(out) != "clean" {
		t.Fatalf("fake Output = %q, %v", out, err)
	}

	combined, err := fake.CombinedOutput(context.Background(), Spec{Name: "git", Args: []string{"push"}})
	if err == nil {
		t.Fatal("expected the scripted error for git push")
	}
	if !strings.Contains(string(combined), "nope") {
		t.Fatalf("fake CombinedOutput = %q, want stderr text", combined)
	}

	calls := fake.Calls()
	if len(calls) != 2 || calls[0].Args[0] != "status" || calls[1].Args[0] != "push" {
		t.Fatalf("recorded calls = %+v", calls)
	}
}

func TestFakeRecordsAreDecoupledFromCaller(t *testing.T) {
	fake := &Fake{}
	args := []string{"status"}
	env := map[string]string{"KEY": "before"}
	if _, err := fake.Run(context.Background(), Spec{Name: "git", Args: args, Env: env}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Mutating the caller's inputs after the call must not change history.
	args[0] = "push"
	env["KEY"] = "after"
	// Mutating a returned copy must not change history either.
	if got := fake.Calls(); got[0].Args[0] != "status" || got[0].Env["KEY"] != "before" {
		t.Fatalf("recorded call mutated by caller: %+v", got[0])
	}
	got := fake.Calls()
	got[0].Args[0] = "mutated"
	got[0].Env["KEY"] = "mutated"
	if again := fake.Calls(); again[0].Args[0] != "status" || again[0].Env["KEY"] != "before" {
		t.Fatalf("recorded call mutated via returned copy: %+v", again[0])
	}
}

func TestFakeNilHandlerSucceeds(t *testing.T) {
	fake := &Fake{}
	if _, err := fake.Run(context.Background(), Spec{Name: "anything"}); err != nil {
		t.Fatalf("nil-handler fake should succeed, got %v", err)
	}
	if got := fake.Calls(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}

// TestHelperProcess is not a real test: when CMDRUN_HELPER_PROCESS=1 it is
// re-executed by helperSpec as a scripted stand-in for an external command,
// then exits before the test framework prints its own output to the captured
// streams. During the normal test run the gate is unset and it does nothing.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	args := helperArgs()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "cmdrun helper: missing command")
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "stdout":
		fmt.Fprint(os.Stdout, strings.Join(rest, " "))
	case "streams":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "cmdrun helper: streams needs <stdout> <stderr>")
			os.Exit(2)
		}
		fmt.Fprint(os.Stdout, rest[0])
		fmt.Fprint(os.Stderr, rest[1])
	case "fail":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "cmdrun helper: fail needs <code> <stderr>")
			os.Exit(2)
		}
		code, err := strconv.Atoi(rest[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "cmdrun helper: bad exit code %q\n", rest[0])
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, rest[1])
		os.Exit(code)
	case "sleep":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "cmdrun helper: sleep needs <duration>")
			os.Exit(2)
		}
		d, err := time.ParseDuration(rest[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "cmdrun helper: bad duration %q\n", rest[0])
			os.Exit(2)
		}
		time.Sleep(d)
	case "echoenv":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "cmdrun helper: echoenv needs <var>")
			os.Exit(2)
		}
		fmt.Fprint(os.Stdout, os.Getenv(rest[0]))
	case "readfile":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "cmdrun helper: readfile needs <name>")
			os.Exit(2)
		}
		b, err := os.ReadFile(rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Stdout.Write(b)
	default:
		fmt.Fprintf(os.Stderr, "cmdrun helper: unknown command %q\n", cmd)
		os.Exit(2)
	}
	os.Exit(0)
}

// helperArgs returns the scripted command arguments passed after "--".
func helperArgs() []string {
	for i, a := range os.Args {
		if a == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}
