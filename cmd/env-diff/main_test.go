package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBinaryExitCodes builds the real binary and checks the process exit
// codes end to end, which the in-process cli tests cannot observe.
func TestBinaryExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "env-diff")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	same := write("same.env", "A=1\nB=2\n")
	drift := write("drift.env", "B=2\nA=3\n")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"identical", []string{same, same}, 0},
		{"drift", []string{same, drift}, 1},
		{"missing file", []string{same, filepath.Join(dir, "missing.env")}, 2},
		{"unknown scheme", []string{same, "foo://x"}, 2},
		{"usage", []string{same}, 2},
		{"version", []string{"--version"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(), "NO_COLOR=1")
			out, _ := cmd.CombinedOutput()
			if got := cmd.ProcessState.ExitCode(); got != tt.want {
				t.Errorf("exit = %d, want %d\n%s", got, tt.want, out)
			}
			if tt.name == "version" && !strings.HasPrefix(string(out), "env-diff ") {
				t.Errorf("version output = %q", out)
			}
		})
	}
}

func TestColorEnabled(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	if colorEnabled(f) {
		t.Error("regular file reported as a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(os.Stdout) {
		t.Error("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled(os.Stdout) {
		t.Error("TERM=dumb ignored")
	}
}
