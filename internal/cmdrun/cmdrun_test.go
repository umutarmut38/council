package cmdrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutputSuccess(t *testing.T) {
	out, err := OS{}.Output(context.Background(), Spec{Name: "sh", Args: []string{"-c", "printf hello"}})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("Output = %q, want %q", out, "hello")
	}
}

func TestCombinedOutputInterleavesStreams(t *testing.T) {
	out, err := OS{}.CombinedOutput(context.Background(), Spec{Name: "sh", Args: []string{"-c", "echo out; echo err 1>&2"}})
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Fatalf("CombinedOutput = %q, want it to contain both streams", got)
	}
}

func TestRunCapturesStreamsSeparately(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Spec{Name: "sh", Args: []string{"-c", "echo to-out; echo to-err 1>&2"}})
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
	out, err := OS{}.Output(context.Background(), Spec{Name: "sh", Args: []string{"-c", "echo boom 1>&2; exit 7"}})
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
	if cmdErr.Name != "sh" {
		t.Fatalf("Name = %q, want sh", cmdErr.Name)
	}
	if !strings.Contains(string(cmdErr.Output), "boom") {
		t.Fatalf("captured Output = %q, want it to contain stderr", cmdErr.Output)
	}
	if !strings.Contains(cmdErr.Error(), "boom") || !strings.Contains(cmdErr.Error(), "sh") {
		t.Fatalf("Error() = %q, want it to mention the command and output", cmdErr.Error())
	}

	// The structured error still unwraps to *exec.ExitError.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error does not unwrap to *exec.ExitError: %v", err)
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
	_, err := OS{}.CombinedOutput(ctx, Spec{Name: "sleep", Args: []string{"10"}})
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
	out, err := OS{}.Output(context.Background(), Spec{
		Name:      "sh",
		Args:      []string{"-c", "printf aaaaaaaaaa"}, // 10 bytes
		MaxOutput: 4,
	})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if want := "aaaa" + truncationMarker; string(out) != want {
		t.Fatalf("capped output = %q, want %q", out, want)
	}
}

func TestEnvOverride(t *testing.T) {
	out, err := OS{}.Output(context.Background(), Spec{
		Name: "sh",
		Args: []string{"-c", "printf %s \"$CMDRUN_TEST_VAR\""},
		Env:  map[string]string{"CMDRUN_TEST_VAR": "injected"},
	})
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
	out, err := OS{}.Output(context.Background(), Spec{Name: "cat", Args: []string{"marker.txt"}, Dir: dir})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "in-workdir" {
		t.Fatalf("workdir read = %q, want in-workdir", out)
	}
}

func TestPackageHelpersUseOSRunner(t *testing.T) {
	out, err := Output(context.Background(), Spec{Name: "sh", Args: []string{"-c", "printf ok"}})
	if err != nil || string(out) != "ok" {
		t.Fatalf("Output helper = %q, %v", out, err)
	}
	if _, err := Run(context.Background(), Spec{Name: "true"}); err != nil {
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

func TestFakeNilHandlerSucceeds(t *testing.T) {
	fake := &Fake{}
	if _, err := fake.Run(context.Background(), Spec{Name: "anything"}); err != nil {
		t.Fatalf("nil-handler fake should succeed, got %v", err)
	}
	if got := fake.Calls(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}
