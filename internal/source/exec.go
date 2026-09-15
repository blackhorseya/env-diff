package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Runner executes an external program, returns its stdout and streams its
// stderr to stderr as it is produced. It is a function type so tests can
// substitute a fake without starting a process.
type Runner func(c context.Context, stderr io.Writer, name string, args ...string) ([]byte, error)

// ErrInterrupted marks a program that was stopped because the context was
// cancelled.
var ErrInterrupted = errors.New("interrupted")

// ErrTimedOut marks a program that was stopped because the context's
// deadline passed.
var ErrTimedOut = errors.New("timed out")

// CommandError reports a program that ran and exited non-zero. Command is
// the exact command line so the user can rerun it; the program's own
// stderr was already forwarded.
type CommandError struct {
	Command string
	Code    int
}

func (x *CommandError) Error() string {
	program, _, _ := strings.Cut(x.Command, " ")
	return fmt.Sprintf("%s exited with status %d (ran: %s)", program, x.Code, x.Command)
}

// NotFoundError reports a program that is not on PATH.
type NotFoundError struct {
	Program string
}

func (x *NotFoundError) Error() string {
	return x.Program + " not found in PATH"
}

// execRunner is the Runner used outside tests. The program is looked up
// on PATH, gets no stdin, and is killed when c is cancelled.
func execRunner(c context.Context, stderr io.Writer, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(c, name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	// A grandchild holding the stderr pipe must not keep Wait blocked
	// after the program itself was killed.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	switch {
	case err == nil:
		return stdout.Bytes(), nil
	case errors.Is(err, exec.ErrNotFound):
		return nil, &NotFoundError{Program: name}
	case errors.Is(c.Err(), context.DeadlineExceeded):
		return nil, fmt.Errorf("%s %w", name, ErrTimedOut)
	case c.Err() != nil:
		return nil, fmt.Errorf("%s %w", name, ErrInterrupted)
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return nil, &CommandError{Command: commandLine(name, args), Code: exitErr.ExitCode()}
	}
	return nil, fmt.Errorf("run %s: %w", name, err)
}

// commandLine renders a command for error messages. Arguments are names
// the user typed (namespaces, resource names, paths), never values.
func commandLine(name string, args []string) string {
	return strings.Join(slices.Concat([]string{name}, args), " ")
}

func (x Resolver) runner() Runner {
	if x.Run != nil {
		return x.Run
	}
	return execRunner
}

func (x Resolver) stderr() io.Writer {
	if x.Stderr != nil {
		return x.Stderr
	}
	return io.Discard
}
