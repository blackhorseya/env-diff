package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secret is written into every fixture value; no output path may show it.
const secret = "s3cr3t-must-not-leak"

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, Options{Version: "v1.2.3"})
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtures returns a staging file and a production file that drift by one
// missing, one extra and one different key.
func fixtures(t *testing.T) (staging, production string) {
	t.Helper()
	staging = write(t, ".env.staging",
		"# staging\n"+
			"DATABASE_URL=postgres://user:"+secret+"@db/app\n"+
			"REDIS_URL=redis://cache\n"+
			"STRIPE_API_KEY=\"sk_"+secret+"\"\n"+
			"LOG_LEVEL=debug\n")
	production = write(t, ".env.production",
		"LOG_LEVEL=info # tuned\n"+
			"\n"+
			"REDIS_URL=redis://cache\n"+
			"export DATABASE_URL='postgres://user:"+secret+"@db/app'\n"+
			"OLD_FEATURE_FLAG="+secret+"\n")
	return staging, production
}

func TestIdenticalIgnoresOrderAndComments(t *testing.T) {
	a := write(t, "a.env", "# first\nA=1\nB=\"two\"\n\nC=\n")
	b := write(t, "b.env", "C=\n# other comment\nB=two\nA=1 # trailing\n")
	res := run(t, a, b)
	if res.code != exitOK {
		t.Fatalf("exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	if want := "Environment Drift\n\nNo differences found\n"; res.stdout != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", res.stdout, want)
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
}

func TestDrift(t *testing.T) {
	staging, production := fixtures(t)
	res := run(t, staging, production)
	if res.code != exitDrift {
		t.Fatalf("exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want := "Environment Drift\n" +
		"\n" +
		"Missing in target\n" +
		"  STRIPE_API_KEY\n" +
		"\n" +
		"Extra in target\n" +
		"  OLD_FEATURE_FLAG\n" +
		"\n" +
		"Different values\n" +
		"  LOG_LEVEL\n" +
		"\n" +
		"3 differences found\n"
	if res.stdout != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", res.stdout, want)
	}
}

func TestKeysOnly(t *testing.T) {
	a := write(t, "a.env", "A=1\nB=2\n")
	b := write(t, "b.env", "A=9\nB=8\n")
	if res := run(t, "--keys-only", a, b); res.code != exitOK {
		t.Errorf("value-only drift with --keys-only: exit = %d\n%s%s", res.code, res.stdout, res.stderr)
	}

	staging, production := fixtures(t)
	res := run(t, staging, production, "--keys-only")
	if res.code != exitDrift {
		t.Fatalf("exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	if !strings.HasPrefix(res.stdout, "Environment Drift (keys only)\n") {
		t.Errorf("stdout lacks keys-only title:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "LOG_LEVEL") || !strings.Contains(res.stdout, "2 differences found") {
		t.Errorf("stdout should ignore value differences:\n%s", res.stdout)
	}
}

func TestJSONFormat(t *testing.T) {
	staging, production := fixtures(t)
	for _, args := range [][]string{
		{"--format=json", staging, production},
		{staging, production, "--format", "json"},
	} {
		res := run(t, args...)
		if res.code != exitDrift {
			t.Fatalf("%v: exit = %d, stderr:\n%s", args, res.code, res.stderr)
		}
		var got struct {
			Drift     bool     `json:"drift"`
			KeysOnly  bool     `json:"keys_only"`
			Missing   []string `json:"missing"`
			Extra     []string `json:"extra"`
			Different []string `json:"different"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
			t.Fatalf("%v: stdout is not JSON: %v\n%s", args, err, res.stdout)
		}
		if !got.Drift || got.KeysOnly {
			t.Errorf("%v: drift/keys_only = %v/%v", args, got.Drift, got.KeysOnly)
		}
		if strings.Join(got.Missing, ",") != "STRIPE_API_KEY" ||
			strings.Join(got.Extra, ",") != "OLD_FEATURE_FLAG" ||
			strings.Join(got.Different, ",") != "LOG_LEVEL" {
			t.Errorf("%v: lists = %v/%v/%v", args, got.Missing, got.Extra, got.Different)
		}
	}

	a := write(t, "a.env", "A=1\n")
	res := run(t, "--format", "json", a, a)
	if res.code != exitOK || !strings.Contains(res.stdout, `"drift": false`) {
		t.Errorf("identical json: exit = %d\n%s", res.code, res.stdout)
	}
}

func TestQuiet(t *testing.T) {
	staging, production := fixtures(t)
	res := run(t, "--quiet", staging, production)
	if res.code != exitDrift || res.stdout != "" || res.stderr != "" {
		t.Errorf("quiet drift: exit = %d, stdout = %q, stderr = %q", res.code, res.stdout, res.stderr)
	}
	res = run(t, "--quiet", staging, staging)
	if res.code != exitOK || res.stdout != "" {
		t.Errorf("quiet identical: exit = %d, stdout = %q", res.code, res.stdout)
	}
	res = run(t, "--quiet", staging, filepath.Join(t.TempDir(), "missing.env"))
	if res.code != exitError || res.stderr == "" {
		t.Errorf("quiet must still report errors: exit = %d, stderr = %q", res.code, res.stderr)
	}
}

func TestArgumentForms(t *testing.T) {
	a := write(t, "a.env", "A=1\n")
	b := write(t, "b.env", "A=2\n")
	for _, args := range [][]string{
		{a, b},
		{"--", a, b},
		{a, b, "--keys-only"},
		{"--keys-only", a, b},
		{a, "--keys-only", b},
	} {
		if res := run(t, args...); res.code == exitError {
			t.Errorf("%v: exit = %d, stderr:\n%s", args, res.code, res.stderr)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		res := run(t, flag)
		if res.code != exitOK || res.stderr != "" {
			t.Errorf("%s: exit = %d, stderr = %q", flag, res.code, res.stderr)
		}
		for _, want := range []string{"Usage:", "--keys-only", "--format", "--quiet", "Exit codes:"} {
			if !strings.Contains(res.stdout, want) {
				t.Errorf("%s: stdout lacks %q:\n%s", flag, want, res.stdout)
			}
		}
	}
	res := run(t, "--version")
	if res.code != exitOK || res.stdout != "env-diff v1.2.3\n" {
		t.Errorf("--version: exit = %d, stdout = %q", res.code, res.stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	a := write(t, "a.env", "A=1\n")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no args", []string{}, "got 0"},
		{"one arg", []string{a}, "got 1"},
		{"three args", []string{a, a, a}, "got 3"},
		{"unknown flag", []string{"--nope", a, a}, "unknown flag"},
		{"bad format", []string{"--format", "yaml", a, a}, `unknown format "yaml"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.args...)
			if res.code != exitError {
				t.Errorf("exit = %d, want %d", res.code, exitError)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty", res.stdout)
			}
			if !strings.HasPrefix(res.stderr, "env-diff: ") || !strings.Contains(res.stderr, tt.want) {
				t.Errorf("stderr = %q, want prefix and %q", res.stderr, tt.want)
			}
			if !strings.Contains(res.stderr, "--help") {
				t.Errorf("stderr = %q, want a help hint", res.stderr)
			}
		})
	}
}

func TestFileAndParseErrors(t *testing.T) {
	good := write(t, "good.env", "A=1\n")
	missing := filepath.Join(t.TempDir(), "missing.env")
	dup := write(t, "dup.env", "A=1\nA="+secret+"\n")
	broken := write(t, "broken.env", "A=1\n"+secret+"\n")
	unreadable := write(t, "unreadable.env", "A="+secret+"\n")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing source", []string{missing, good}, missing},
		{"missing target", []string{good, missing}, missing},
		{"directory", []string{good, t.TempDir()}, "is a directory"},
		{"duplicate key", []string{good, dup}, `line 2: duplicate key "A" (already defined at line 1)`},
		{"invalid syntax", []string{broken, good}, "line 2: expected KEY=VALUE"},
		{"permission denied", []string{good, unreadable}, "permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "permission denied" && os.Geteuid() == 0 {
				t.Skip("root ignores file modes")
			}
			res := run(t, tt.args...)
			if res.code != exitError {
				t.Errorf("exit = %d, want %d", res.code, exitError)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty", res.stdout)
			}
			if !strings.HasPrefix(res.stderr, "env-diff: ") || !strings.Contains(res.stderr, tt.want) {
				t.Errorf("stderr = %q, want prefix and %q", res.stderr, tt.want)
			}
			if strings.Contains(res.stderr, secret) {
				t.Errorf("stderr = %q leaks a value", res.stderr)
			}
			if strings.Contains(res.stderr, "--help") {
				t.Errorf("stderr = %q, help hint is for usage errors only", res.stderr)
			}
		})
	}
}

func TestEmptyFiles(t *testing.T) {
	empty := write(t, "empty.env", "")
	comments := write(t, "comments.env", "# nothing here\n\n")
	full := write(t, "full.env", "A=1\nB=2\n")

	if res := run(t, empty, comments); res.code != exitOK {
		t.Errorf("empty vs comments-only: exit = %d\n%s", res.code, res.stderr)
	}
	res := run(t, empty, full)
	if res.code != exitDrift || !strings.Contains(res.stdout, "Extra in target\n  A\n  B\n") {
		t.Errorf("empty vs full: exit = %d\n%s", res.code, res.stdout)
	}
	res = run(t, full, empty)
	if res.code != exitDrift || !strings.Contains(res.stdout, "Missing in target\n  A\n  B\n") {
		t.Errorf("full vs empty: exit = %d\n%s", res.code, res.stdout)
	}
}

// TestNeverPrintsValues sweeps every output path. Each case must show the
// expected key name (so a silent, empty output cannot pass) and must not
// show the secret placed in every value.
func TestNeverPrintsValues(t *testing.T) {
	staging, production := fixtures(t)
	dup := write(t, "dup.env", "TOKEN="+secret+"\nTOKEN="+secret+"\n")
	unterminated := write(t, "unterminated.env", "TOKEN=\""+secret+"\n")
	broken := write(t, "broken.env", secret+"\n")
	tests := []struct {
		name    string
		args    []string
		wantKey string
	}{
		{"terminal", []string{staging, production}, "STRIPE_API_KEY"},
		{"json", []string{"--format", "json", staging, production}, "STRIPE_API_KEY"},
		{"keys only", []string{"--keys-only", staging, production}, "STRIPE_API_KEY"},
		{"quiet", []string{"--quiet", staging, production}, ""},
		{"duplicate", []string{staging, dup}, "TOKEN"},
		{"unterminated quote", []string{unterminated, staging}, "line 1"},
		{"invalid syntax", []string{staging, broken}, "line 1"},
		{"bad format", []string{"--format", "yaml", staging, production}, "unknown format"},
		{"usage", []string{staging}, "got 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.args...)
			out := res.stdout + res.stderr
			if strings.Contains(out, secret) {
				t.Errorf("output leaks a value:\n%s", out)
			}
			if tt.wantKey != "" && !strings.Contains(out, tt.wantKey) {
				t.Errorf("output lacks %q:\n%s", tt.wantKey, out)
			}
		})
	}
}

// TestExamples pins the files in examples/ to the samples in README.md.
// The README shows these exact outputs, so a change to the example files,
// the output format, or the README must update the others.
func TestExamples(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "examples"))

	res := run(t, ".env.staging", ".env.production")
	if res.code != exitDrift {
		t.Fatalf("staging vs production: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want := "Environment Drift\n" +
		"\n" +
		"Missing in target\n" +
		"  STRIPE_API_KEY\n" +
		"\n" +
		"Extra in target\n" +
		"  OLD_FEATURE_FLAG\n" +
		"\n" +
		"Different values\n" +
		"  LOG_LEVEL\n" +
		"\n" +
		"3 differences found\n"
	if res.stdout != want {
		t.Errorf("staging vs production:\n%s\nwant:\n%s", res.stdout, want)
	}

	res = run(t, ".env.example", ".env.production", "--keys-only")
	if res.code != exitDrift {
		t.Fatalf("example vs production: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = "Environment Drift (keys only)\n" +
		"\n" +
		"Missing in target\n" +
		"  STRIPE_API_KEY\n" +
		"\n" +
		"Extra in target\n" +
		"  OLD_FEATURE_FLAG\n" +
		"\n" +
		"2 differences found\n"
	if res.stdout != want {
		t.Errorf("example vs production --keys-only:\n%s\nwant:\n%s", res.stdout, want)
	}

	if res = run(t, ".env.example", ".env.staging", "--keys-only"); res.code != exitOK {
		t.Errorf("example vs staging --keys-only: exit = %d\n%s%s", res.code, res.stdout, res.stderr)
	}

	res = run(t, ".env.staging", ".env.production", "--format", "json")
	if res.code != exitDrift {
		t.Fatalf("json: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = `{
  "source": ".env.staging",
  "target": ".env.production",
  "keys_only": false,
  "drift": true,
  "summary": {
    "same": 2,
    "missing": 1,
    "extra": 1,
    "different": 1
  },
  "missing": [
    "STRIPE_API_KEY"
  ],
  "extra": [
    "OLD_FEATURE_FLAG"
  ],
  "different": [
    "LOG_LEVEL"
  ],
  "same": [
    "DATABASE_URL",
    "REDIS_URL"
  ]
}
`
	if res.stdout != want {
		t.Errorf("json:\n%s\nwant:\n%s", res.stdout, want)
	}
}
