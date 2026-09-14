package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	code := Run(t.Context(), args, &stdout, &stderr, Options{Version: "v1.2.3"})
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

// fakeCLIs installs shell scripts as the only programs on PATH, so a remote
// source runs them instead of the real kubectl or aws, and points every
// configuration variable those CLIs read at an empty file, so a test can
// never reach a real cluster or account with the developer's credentials.
// The scripts must use only shell builtins: PATH holds nothing else.
func fakeCLIs(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("KUBECONFIG", empty)
	for _, v := range []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(v, "")
	}
	t.Setenv("AWS_CONFIG_FILE", empty)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty)
}

func fakeCLI(t *testing.T, name, script string) {
	t.Helper()
	fakeCLIs(t, map[string]string{name: script})
}

// cliScript answers command lines from a table of arguments to stdout
// bodies and fails any other command line.
func cliScript(name string, replies map[string]string) string {
	var b strings.Builder
	fmt.Fprintln(&b, `case "$*" in`)
	for args, body := range replies {
		fmt.Fprintf(&b, "  %q) printf '%%s' '%s' ;;\n", args, body)
	}
	fmt.Fprintf(&b, "  *) echo \"fake %s: unexpected command line: $*\" >&2; exit 9 ;;\n", name)
	fmt.Fprintln(&b, "esac")
	return b.String()
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
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
		for _, want := range []string{"Usage:", "--keys-only", "--allow-extra", "--ignore", "--format", "--quiet", "Exit codes:", "k8s://<namespace>/configmap/<name>", "k8s://<namespace>/secret/<name>", "lambda://<function>"} {
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
		{"one arg", []string{a}, "at least two environments (<source> <target>...), got 1"},
		{"unknown flag", []string{"--nope", a, a}, "unknown flag"},
		{"bad format", []string{"--format", "yaml", a, a}, `unknown format "yaml"`},
		{"unknown scheme", []string{a, "foo://x"}, `foo://x: unknown source scheme "foo"`},
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

// TestResolveBeforeLoad checks that every argument is validated before any
// is read, so a bad scheme is reported even when an earlier file is missing.
func TestResolveBeforeLoad(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	res := run(t, missing, "foo://x")
	if res.code != exitError {
		t.Fatalf("exit = %d, want %d", res.code, exitError)
	}
	if !strings.Contains(res.stderr, `unknown source scheme "foo"`) || strings.Contains(res.stderr, missing) {
		t.Errorf("stderr = %q, want the scheme error and not the missing file", res.stderr)
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
	qa := write(t, ".env.qa", "DATABASE_URL="+secret+"\nLOG_LEVEL=info\n")
	dup := write(t, "dup.env", "TOKEN="+secret+"\nTOKEN="+secret+"\n")
	unterminated := write(t, "unterminated.env", "TOKEN=\""+secret+"\n")
	broken := write(t, "broken.env", secret+"\n")
	fakeCLIs(t, map[string]string{
		"kubectl": cliScript("kubectl", map[string]string{
			"get configmap app -n prod -o json":   `{"data":{"DATABASE_URL":"` + secret + `","LOG_LEVEL":"info","TOKEN":"` + secret + `"}}`,
			"get secret app -n prod -o json":      `{"data":{"DATABASE_URL":"` + b64(secret) + `","TOKEN":"` + b64(secret) + `"}}`,
			"get configmap noisy -n prod -o json": `error: ` + secret,
			"get secret bad -n prod -o json":      `{"data":{"TOKEN":"` + secret + `!"}}`,
		}),
		"aws": cliScript("aws", map[string]string{
			"lambda get-function-configuration --function-name fn --output json":     `{"Environment":{"Variables":{"DATABASE_URL":"` + secret + `","TOKEN":"` + secret + `"}}}`,
			"lambda get-function-configuration --function-name locked --output json": `{"Environment":{"Variables":{},"Error":{"ErrorCode":"AccessDeniedException","Message":"` + secret + `"}}}`,
			"lambda get-function-configuration --function-name noisy --output json":  `Traceback: ` + secret,
		}),
	})
	tests := []struct {
		name    string
		args    []string
		wantKey string
	}{
		{"configmap", []string{staging, "k8s://prod/configmap/app"}, "TOKEN"},
		{"secret json", []string{"--format", "json", "k8s://prod/secret/app", staging}, "TOKEN"},
		{"configmap matrix", []string{staging, "k8s://prod/configmap/app", "k8s://prod/secret/app"}, "TOKEN"},
		{"kubectl output not json", []string{staging, "k8s://prod/configmap/noisy"}, "not valid JSON"},
		{"secret not base64", []string{staging, "k8s://prod/secret/bad"}, "TOKEN"},
		{"lambda", []string{staging, "lambda://fn"}, "TOKEN"},
		{"lambda json", []string{"--format", "json", "lambda://fn", "k8s://prod/secret/app"}, "TOKEN"},
		{"lambda decrypt error", []string{staging, "lambda://locked"}, "AccessDeniedException"},
		{"aws output not json", []string{staging, "lambda://noisy"}, "not valid JSON"},
		{"terminal", []string{staging, production}, "STRIPE_API_KEY"},
		{"json", []string{"--format", "json", staging, production}, "STRIPE_API_KEY"},
		{"keys only", []string{"--keys-only", staging, production}, "STRIPE_API_KEY"},
		{"quiet", []string{"--quiet", staging, production}, ""},
		{"matrix", []string{staging, production, qa}, "STRIPE_API_KEY"},
		{"matrix json", []string{"--format", "json", staging, production, qa}, "STRIPE_API_KEY"},
		{"ignore", []string{"--ignore", "STRIPE_API_KEY,LOG_LEVEL", staging, production}, "OLD_FEATURE_FLAG"},
		{"allow extra", []string{"--allow-extra", staging, production}, "STRIPE_API_KEY"},
		{"duplicate", []string{staging, dup}, "TOKEN"},
		{"unterminated quote", []string{unterminated, staging}, "line 1"},
		{"invalid syntax", []string{staging, broken}, "line 1"},
		{"bad format", []string{"--format", "yaml", staging, production}, "unknown format"},
		{"usage", []string{staging}, "got 1"},
		{"unknown scheme", []string{staging, "foo://x"}, `unknown source scheme "foo"`},
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

	res = run(t, ".env.example", ".env.production", "--keys-only", "--allow-extra")
	if res.code != exitDrift {
		t.Fatalf("validate: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = "Environment Drift (keys only)\n\nMissing in target\n  STRIPE_API_KEY\n\n1 difference found\n1 extra key allowed\n"
	if res.stdout != want {
		t.Errorf("validate:\n%s\nwant:\n%s", res.stdout, want)
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
  "envs": [
    ".env.staging",
    ".env.production"
  ],
  "keys_only": false,
  "allow_extra": false,
  "drift": true,
  "summary": {
    "same": 2,
    "missing": 1,
    "extra": 1,
    "different": 1,
    "ignored": 0
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
  ],
  "ignored": [],
  "matrix": {
    "DATABASE_URL": [
      0,
      0
    ],
    "LOG_LEVEL": [
      0,
      1
    ],
    "OLD_FEATURE_FLAG": [
      null,
      0
    ],
    "REDIS_URL": [
      0,
      0
    ],
    "STRIPE_API_KEY": [
      0,
      null
    ]
  }
}
`
	if res.stdout != want {
		t.Errorf("json:\n%s\nwant:\n%s", res.stdout, want)
	}

	res = run(t, ".env.staging", ".env.production", "--ignore", "LOG_LEVEL,STRIPE_API_KEY")
	if res.code != exitDrift {
		t.Fatalf("ignore: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = "Environment Drift\n\nExtra in target\n  OLD_FEATURE_FLAG\n\n1 difference found\n2 keys ignored\n"
	if res.stdout != want {
		t.Errorf("ignore:\n%s\nwant:\n%s", res.stdout, want)
	}

	sp := func(n int) string { return strings.Repeat(" ", n) }
	res = run(t, ".env.staging", ".env.production", ".env.qa")
	if res.code != exitDrift {
		t.Fatalf("three files: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = "Environment Drift\n" +
		"\n" +
		"KEY" + sp(15) + ".env.staging  .env.production  .env.qa\n" +
		"STRIPE_API_KEY" + sp(4) + "+" + sp(13) + "-" + sp(16) + "+" + sp(8) + "missing in .env.production\n" +
		"OLD_FEATURE_FLAG" + sp(2) + "-" + sp(13) + "+" + sp(16) + "-" + sp(8) + "extra in .env.production\n" +
		"LOG_LEVEL" + sp(9) + "a" + sp(13) + "b" + sp(16) + "b" + sp(8) + "different\n" +
		"\n" +
		"3 differences found\n"
	if res.stdout != want {
		t.Errorf("three files:\n%s\nwant:\n%s", res.stdout, want)
	}

	res = run(t, ".env.staging", ".env.production", ".env.qa", "--format", "json")
	if res.code != exitDrift {
		t.Fatalf("three files json: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want = `{
  "source": ".env.staging",
  "envs": [
    ".env.staging",
    ".env.production",
    ".env.qa"
  ],
  "keys_only": false,
  "allow_extra": false,
  "drift": true,
  "summary": {
    "same": 2,
    "missing": 1,
    "extra": 1,
    "different": 1,
    "ignored": 0
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
  ],
  "ignored": [],
  "matrix": {
    "DATABASE_URL": [
      0,
      0,
      0
    ],
    "LOG_LEVEL": [
      0,
      1,
      1
    ],
    "OLD_FEATURE_FLAG": [
      null,
      0,
      null
    ],
    "REDIS_URL": [
      0,
      0,
      0
    ],
    "STRIPE_API_KEY": [
      0,
      null,
      0
    ]
  }
}
`
	if res.stdout != want {
		t.Errorf("three files json:\n%s\nwant:\n%s", res.stdout, want)
	}
}

func TestKubernetesSources(t *testing.T) {
	staging, _ := fixtures(t)
	fakeCLI(t, "kubectl", cliScript("kubectl", map[string]string{
		"get configmap app -n prod -o json": `{"kind":"ConfigMap","data":{"DATABASE_URL":"postgres://user:` + secret + `@db/app","LOG_LEVEL":"info","REDIS_URL":"redis://cache"}}`,
		"get secret app -n prod -o json":    `{"kind":"Secret","data":{"DATABASE_URL":"` + b64("postgres://user:"+secret+"@db/app") + `","LOG_LEVEL":"` + b64("debug") + `","REDIS_URL":"` + b64("redis://cache") + `","STRIPE_API_KEY":"` + b64("sk_"+secret) + `"}}`,
	}))

	res := run(t, staging, "k8s://prod/configmap/app")
	if res.code != exitDrift {
		t.Fatalf("configmap: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want := "Environment Drift\n\nMissing in target\n  STRIPE_API_KEY\n\nDifferent values\n  LOG_LEVEL\n\n2 differences found\n"
	if res.stdout != want {
		t.Errorf("configmap:\n%s\nwant:\n%s", res.stdout, want)
	}
	if res.stderr != "" {
		t.Errorf("configmap: stderr = %q, want empty", res.stderr)
	}

	// The secret holds the same values as the staging file once decoded.
	res = run(t, staging, "k8s://prod/secret/app")
	if res.code != exitOK || res.stdout != "Environment Drift\n\nNo differences found\n" {
		t.Errorf("secret: exit = %d\n%s%s", res.code, res.stdout, res.stderr)
	}

	res = run(t, "--format", "json", "k8s://prod/configmap/app", "k8s://prod/secret/app", staging)
	if res.code != exitDrift {
		t.Fatalf("json: exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	var got struct {
		Source string   `json:"source"`
		Envs   []string `json:"envs"`
		Extra  []string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, res.stdout)
	}
	if got.Source != "k8s://prod/configmap/app" || !slices.Equal(got.Envs, []string{"k8s://prod/configmap/app", "k8s://prod/secret/app", staging}) {
		t.Errorf("json names = %q %v", got.Source, got.Envs)
	}
	if !slices.Equal(got.Extra, []string{"STRIPE_API_KEY"}) {
		t.Errorf("json extra = %v", got.Extra)
	}
}

func TestLambdaSources(t *testing.T) {
	staging, _ := fixtures(t)
	empty := write(t, "empty.env", "")
	fakeCLI(t, "aws", cliScript("aws", map[string]string{
		"lambda get-function-configuration --function-name my-function:prod --output json": `{"FunctionName":"my-function","Environment":{"Variables":{"DATABASE_URL":"postgres://user:` + secret + `@db/app","LOG_LEVEL":"info","REDIS_URL":"redis://cache"}}}`,
		"lambda get-function-configuration --function-name bare --output json":             `{"FunctionName":"bare","Runtime":"provided.al2023"}`,
	}))

	res := run(t, staging, "lambda://my-function:prod")
	if res.code != exitDrift {
		t.Fatalf("exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	want := "Environment Drift\n\nMissing in target\n  STRIPE_API_KEY\n\nDifferent values\n  LOG_LEVEL\n\n2 differences found\n"
	if res.stdout != want || res.stderr != "" {
		t.Errorf("stdout:\n%s\nwant:\n%s\nstderr: %q", res.stdout, want, res.stderr)
	}

	// A function without environment variables is an empty environment.
	res = run(t, empty, "lambda://bare")
	if res.code != exitOK || res.stdout != "Environment Drift\n\nNo differences found\n" {
		t.Errorf("no environment: exit = %d\n%s%s", res.code, res.stdout, res.stderr)
	}

	t.Run("aws fails", func(t *testing.T) {
		fakeCLI(t, "aws", `echo 'An error occurred (ResourceNotFoundException) when calling the GetFunctionConfiguration operation: Function not found' >&2; exit 254`)
		res := run(t, staging, "lambda://missing")
		want := "An error occurred (ResourceNotFoundException) when calling the GetFunctionConfiguration operation: Function not found\n" +
			"env-diff: lambda://missing: aws exited with status 254 (ran: aws lambda get-function-configuration --function-name missing --output json)\n"
		if res.code != exitError || res.stdout != "" || res.stderr != want {
			t.Errorf("exit = %d, stdout = %q, stderr =\n%s\nwant:\n%s", res.code, res.stdout, res.stderr, want)
		}
	})
	t.Run("aws missing", func(t *testing.T) {
		fakeCLI(t, "not-aws", "exit 0")
		res := run(t, staging, "lambda://my-function")
		want := "env-diff: lambda://my-function: aws not found in PATH\n"
		if res.code != exitError || res.stderr != want {
			t.Errorf("exit = %d, stderr = %q, want %q", res.code, res.stderr, want)
		}
	})
	t.Run("bad shape is a usage error", func(t *testing.T) {
		fakeCLI(t, "aws", `echo "must not run" >&2; exit 9`)
		res := run(t, staging, "lambda://")
		want := "env-diff: lambda://: want lambda://<function>[:<qualifier>]\nRun 'env-diff --help' for usage.\n"
		if res.code != exitError || res.stderr != want {
			t.Errorf("exit = %d, stderr = %q, want %q", res.code, res.stderr, want)
		}
	})
}

func TestRemoteSourceErrors(t *testing.T) {
	good := write(t, "good.env", "A=1\n")
	t.Run("kubectl fails", func(t *testing.T) {
		fakeCLI(t, "kubectl", `echo 'Error from server (NotFound): configmaps "app" not found' >&2; exit 1`)
		res := run(t, good, "k8s://prod/configmap/app")
		if res.code != exitError || res.stdout != "" {
			t.Errorf("exit = %d, stdout = %q", res.code, res.stdout)
		}
		want := "Error from server (NotFound): configmaps \"app\" not found\n" +
			"env-diff: k8s://prod/configmap/app: kubectl exited with status 1 (ran: kubectl get configmap app -n prod -o json)\n"
		if res.stderr != want {
			t.Errorf("stderr =\n%s\nwant:\n%s", res.stderr, want)
		}
	})
	t.Run("kubectl stderr is forwarded even with --quiet", func(t *testing.T) {
		fakeCLI(t, "kubectl", `echo 'Warning: kubeconfig is deprecated' >&2; printf '{"data":{"A":"1"}}'`)
		res := run(t, "--quiet", good, "k8s://prod/configmap/app")
		if res.code != exitOK || res.stdout != "" || res.stderr != "Warning: kubeconfig is deprecated\n" {
			t.Errorf("exit = %d, stdout = %q, stderr = %q", res.code, res.stdout, res.stderr)
		}
	})
	t.Run("kubectl missing", func(t *testing.T) {
		fakeCLI(t, "not-kubectl", "exit 0")
		res := run(t, good, "k8s://prod/configmap/app")
		want := "env-diff: k8s://prod/configmap/app: kubectl not found in PATH\n"
		if res.code != exitError || res.stderr != want {
			t.Errorf("exit = %d, stderr = %q, want %q", res.code, res.stderr, want)
		}
	})
	t.Run("bad shape is a usage error", func(t *testing.T) {
		fakeCLI(t, "kubectl", `echo "must not run" >&2; exit 9`)
		res := run(t, good, "k8s://prod/app")
		want := "env-diff: k8s://prod/app: want k8s://<namespace>/<configmap|secret>/<name>\nRun 'env-diff --help' for usage.\n"
		if res.code != exitError || res.stderr != want {
			t.Errorf("exit = %d, stderr = %q, want %q", res.code, res.stderr, want)
		}
	})
	t.Run("bad shape is reported before any source is loaded", func(t *testing.T) {
		fakeCLI(t, "kubectl", `echo "must not run" >&2; exit 9`)
		res := run(t, "k8s://prod/configmap/app", "k8s://prod/deployment/app")
		if res.code != exitError || strings.Contains(res.stderr, "must not run") {
			t.Errorf("exit = %d, stderr = %q", res.code, res.stderr)
		}
	})
}

func TestIgnore(t *testing.T) {
	staging, production := fixtures(t)
	for _, args := range [][]string{
		{"--ignore", "STRIPE_API_KEY,LOG_LEVEL,OLD_FEATURE_FLAG", staging, production},
		{"--ignore", "STRIPE_API_KEY", "--ignore", "LOG_LEVEL", "--ignore=OLD_FEATURE_FLAG", staging, production},
		{staging, production, "--ignore", "LOG_LEVEL", "--ignore", "STRIPE_API_KEY,OLD_FEATURE_FLAG"},
	} {
		res := run(t, args...)
		if res.code != exitOK {
			t.Errorf("%v: exit = %d\n%s%s", args, res.code, res.stdout, res.stderr)
		}
		if want := "Environment Drift\n\nNo differences found\n3 keys ignored\n"; res.stdout != want {
			t.Errorf("%v: stdout =\n%s\nwant:\n%s", args, res.stdout, want)
		}
	}

	res := run(t, "--ignore", "LOG_LEVEL", staging, production)
	if res.code != exitDrift || !strings.HasSuffix(res.stdout, "2 differences found\n1 key ignored\n") {
		t.Errorf("partial ignore: exit = %d\n%s", res.code, res.stdout)
	}
	if strings.Contains(res.stdout, "LOG_LEVEL") {
		t.Errorf("ignored key listed:\n%s", res.stdout)
	}

	res = run(t, "--ignore", "NOT_A_KEY", staging, staging)
	if res.code != exitOK || !strings.HasSuffix(res.stdout, "0 keys ignored\n") {
		t.Errorf("unmatched ignore: exit = %d\n%s", res.code, res.stdout)
	}

	res = run(t, "--ignore", "LOG_LEVEL", "--format", "json", staging, production)
	var got struct {
		Ignored   []string `json:"ignored"`
		Different []string `json:"different"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Ignored, []string{"LOG_LEVEL"}) || len(got.Different) != 0 {
		t.Errorf("json ignored = %v, different = %v", got.Ignored, got.Different)
	}
}

func TestAllowExtra(t *testing.T) {
	staging, production := fixtures(t)
	res := run(t, "--allow-extra", staging, production)
	if res.code != exitDrift {
		t.Fatalf("exit = %d, stderr:\n%s", res.code, res.stderr)
	}
	if strings.Contains(res.stdout, "OLD_FEATURE_FLAG") || !strings.HasSuffix(res.stdout, "2 differences found\n1 extra key allowed\n") {
		t.Errorf("stdout:\n%s", res.stdout)
	}

	base := write(t, "base.env", "A=1\n")
	more := write(t, "more.env", "A=1\nB=2\n")
	if res := run(t, base, more); res.code != exitDrift {
		t.Errorf("without flag: exit = %d, want %d", res.code, exitDrift)
	}
	res = run(t, "--allow-extra", base, more)
	if res.code != exitOK || res.stdout != "Environment Drift\n\nNo differences found\n1 extra key allowed\n" {
		t.Errorf("with flag: exit = %d\n%s", res.code, res.stdout)
	}
	if res := run(t, "--allow-extra", more, base); res.code != exitDrift {
		t.Errorf("missing keys must still fail: exit = %d", res.code)
	}
}
