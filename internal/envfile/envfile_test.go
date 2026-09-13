package envfile

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// secret is placed in every error-path fixture; it must never surface.
const secret = "s3cr3t-must-not-leak"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"basic", "DATABASE_URL=postgres://localhost/app\nLOG_LEVEL=debug\nFEATURE_X=true\n",
			map[string]string{"DATABASE_URL": "postgres://localhost/app", "LOG_LEVEL": "debug", "FEATURE_X": "true"}},
		{"no trailing newline", "A=1", map[string]string{"A": "1"}},
		{"empty value", "A=\n", map[string]string{"A": ""}},
		{"empty value with trailing spaces", "A=   \n", map[string]string{"A": ""}},
		{"comments and blank lines", "# top\n\n  \nA=1\n   # indented\n\nB=2\n", map[string]string{"A": "1", "B": "2"}},
		{"export prefix", "export A=1\nexport\tB=2\n", map[string]string{"A": "1", "B": "2"}},
		{"export only as a prefix word", "exportA=1\nexport=2\n", map[string]string{"exportA": "1", "export": "2"}},
		{"whitespace around key and equals", "  A = 1  \nB\t=\t2\n", map[string]string{"A": "1", "B": "2"}},
		{"double quoted", `A="hello world"`, map[string]string{"A": "hello world"}},
		{"double quoted keeps inner whitespace", `A="  x  "`, map[string]string{"A": "  x  "}},
		{"double quoted unescapes quote and backslash", `A="say \"hi\" \\ done"`, map[string]string{"A": `say "hi" \ done`}},
		{"double quoted keeps other escapes literally", `A="line\nbreak"`, map[string]string{"A": `line\nbreak`}},
		{"double quoted json", `A="{\"type\": \"service_account\"}"`, map[string]string{"A": `{"type": "service_account"}`}},
		{"double quoted hash", `A="a # b"`, map[string]string{"A": "a # b"}},
		{"single quoted is literal", `A='it"s \"raw\" # x'`, map[string]string{"A": `it"s \"raw\" # x`}},
		{"quoted followed by comment", "A=\"x\" # c\nB='y'\t#c\nC=\"z\"#c\n", map[string]string{"A": "x", "B": "y", "C": "z"}},
		{"quoted empty", "A=\"\"\nB=''\n", map[string]string{"A": "", "B": ""}},
		{"inline comment needs leading whitespace", "A=abc#123\nB=abc #123\nC=abc\t# 123\n", map[string]string{"A": "abc#123", "B": "abc", "C": "abc"}},
		{"hash right after equals is a value", "A=#not-a-comment\n", map[string]string{"A": "#not-a-comment"}},
		{"hash after whitespace is a comment", "A= # comment\n", map[string]string{"A": ""}},
		{"url fragment survives", "A=https://x.test/p#frag\n", map[string]string{"A": "https://x.test/p#frag"}},
		{"crlf", "A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
		{"bom", "\uFEFFA=1\n", map[string]string{"A": "1"}},
		{"variable expansion is literal", "A=${HOME}/x\n", map[string]string{"A": "${HOME}/x"}},
		{"equals inside value", "A=b=c=d\n", map[string]string{"A": "b=c=d"}},
		{"empty file", "", map[string]string{}},
		{"comment only", "# just a comment\n\n", map[string]string{}},
		{"keys are case sensitive", "a=1\nA=2\n", map[string]string{"a": "1", "A": "2"}},
		{"key charset", "_A1=1\nb_2=2\n", map[string]string{"_A1": "1", "b_2": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			got := map[string]string{}
			for _, k := range env.Keys() {
				got[k], _ = env.Lookup(k)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("Parse() = %v, want %v", got, tt.want)
			}
			if env.Len() != len(tt.want) {
				t.Errorf("Len() = %d, want %d", env.Len(), len(tt.want))
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		line  int
		msg   string
	}{
		{"no equals", "A=1\n" + secret + "\n", 2, "expected KEY=VALUE"},
		{"missing key", "=" + secret, 1, "missing variable name"},
		{"invalid key chars", "MY-KEY=" + secret, 1, "invalid variable name"},
		{"key starting with digit", "1A=" + secret, 1, "invalid variable name"},
		{"key with space", "MY KEY=" + secret, 1, "invalid variable name"},
		{"duplicate", "A=1\nB=2\nA=" + secret + "\n", 3, `duplicate key "A" (already defined at line 1)`},
		{"unterminated double", `A="` + secret, 1, "unterminated double-quoted value"},
		{"unterminated single", `A='` + secret, 1, "unterminated single-quoted value"},
		{"multi-line quoted", "A=\"" + secret + "\nmore\"\n", 1, "unterminated"},
		{"escaped closing quote", `A="` + secret + `\"`, 1, "unterminated"},
		{"text after double quote", `A="x"` + secret, 1, "unexpected text after closing quote"},
		{"text after single quote", `A='x'` + secret, 1, "unexpected text after closing quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			perr, ok := errors.AsType[*Error](err)
			if !ok {
				t.Fatalf("Parse() error = %v, want *Error", err)
			}
			if perr.Line != tt.line {
				t.Errorf("Line = %d, want %d", perr.Line, tt.line)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("Error() = %q, want it to contain %q", err, tt.msg)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("Error() = %q leaks the line content", err)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.env")
	bad := filepath.Join(dir, "bad.env")
	if err := os.WriteFile(good, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("A=1\nA="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := ParseFile(good)
	if err != nil {
		t.Fatalf("ParseFile(good) error = %v", err)
	}
	if v, ok := env.Lookup("A"); !ok || v != "1" {
		t.Errorf("Lookup(A) = %q, %v", v, ok)
	}

	_, err = ParseFile(bad)
	if err == nil || !strings.Contains(err.Error(), bad+": line 2:") {
		t.Errorf("ParseFile(bad) error = %v, want path and line prefix", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("ParseFile(bad) error leaks the value: %v", err)
	}
	if _, ok := errors.AsType[*Error](err); !ok {
		t.Errorf("ParseFile(bad) error = %T, want to unwrap to *Error", err)
	}

	_, err = ParseFile(filepath.Join(dir, "missing.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ParseFile(missing) error = %v, want ErrNotExist", err)
	}

	_, err = ParseFile(dir)
	if err == nil {
		t.Error("ParseFile(dir) error = nil, want an error")
	}
}

func TestEnvFormattingHidesValues(t *testing.T) {
	env, err := Parse(strings.NewReader("TOKEN=" + secret + "\nOTHER=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	renderings := map[string]string{
		"Sprint":      fmt.Sprint(env),
		"%v":          fmt.Sprintf("%v", env),
		"%+v":         fmt.Sprintf("%+v", env),
		"%#v":         fmt.Sprintf("%#v", env),
		"%s":          fmt.Sprintf("[%s]", env),
		"%v pointer":  fmt.Sprintf("%v", &env),
		"%#v pointer": fmt.Sprintf("%#v", &env),
		"String":      env.String(),
	}
	for name, s := range renderings {
		if strings.Contains(s, secret) {
			t.Errorf("%s = %q leaks a value", name, s)
		}
		if !strings.Contains(s, "2 keys") {
			t.Errorf("%s = %q, want the key count", name, s)
		}
	}
}

func TestEnvKeysSorted(t *testing.T) {
	env, err := Parse(strings.NewReader("Z=1\nA=2\nM=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := env.Keys(), []string{"A", "M", "Z"}; !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
	if _, ok := env.Lookup("missing"); ok {
		t.Error("Lookup(missing) = true, want false")
	}
}

func TestZeroEnv(t *testing.T) {
	var env Env
	if env.Len() != 0 || len(env.Keys()) != 0 {
		t.Errorf("zero Env has keys: %v", env.Keys())
	}
	if _, ok := env.Lookup("A"); ok {
		t.Error("zero Env Lookup = true")
	}
}
