package source

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// The runner tests use sh, which every supported platform has, so no
// real kubectl or aws is ever started.

func TestExecRunnerForwardsStderr(t *testing.T) {
	var stderr bytes.Buffer
	out, err := execRunner(t.Context(), &stderr, "sh", "-c", "printf out; printf err >&2")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "out" {
		t.Errorf("stdout = %q, want %q", out, "out")
	}
	if stderr.String() != "err" {
		t.Errorf("stderr = %q, want %q", stderr.String(), "err")
	}
}

func TestExecRunnerExitStatus(t *testing.T) {
	var stderr bytes.Buffer
	_, err := execRunner(t.Context(), &stderr, "sh", "-c", "echo boom >&2; exit 3")
	cmdErr, ok := errors.AsType[*CommandError](err)
	if !ok {
		t.Fatalf("err = %T %v, want *CommandError", err, err)
	}
	if cmdErr.Code != 3 || cmdErr.Command != "sh -c echo boom >&2; exit 3" {
		t.Errorf("err = %+v", cmdErr)
	}
	if want := "sh exited with status 3 (ran: sh -c echo boom >&2; exit 3)"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if stderr.String() != "boom\n" {
		t.Errorf("stderr = %q, want the program's own message", stderr.String())
	}
}

func TestExecRunnerNotFound(t *testing.T) {
	_, err := execRunner(t.Context(), io.Discard, "env-diff-no-such-program-for-tests")
	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Fatalf("err = %T %v, want *NotFoundError", err, err)
	}
	if want := "env-diff-no-such-program-for-tests not found in PATH"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	c, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := execRunner(c, io.Discard, "sh", "-c", "exec sleep 30")
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("err = %v, want ErrTimedOut", err)
	}
	if want := "sh timed out"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v, the program was not killed", took)
	}
}

func TestExecRunnerCancelled(t *testing.T) {
	c, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := execRunner(c, io.Discard, "sh", "-c", "exec sleep 30")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v, want ErrInterrupted", err)
	}
	if !strings.HasPrefix(err.Error(), "sh interrupted") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestResolverDefaults(t *testing.T) {
	var r Resolver
	if r.runner() == nil || r.stderr() != io.Discard {
		t.Error("zero Resolver must run from PATH and discard stderr")
	}
	var buf bytes.Buffer
	called := false
	r = Resolver{
		Run:    func(context.Context, io.Writer, string, ...string) ([]byte, error) { called = true; return nil, nil },
		Stderr: &buf,
	}
	if _, _ = r.runner()(t.Context(), nil, "x"); !called || r.stderr() != &buf {
		t.Error("Resolver fields must be used when set")
	}
}
