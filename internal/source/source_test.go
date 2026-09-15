package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// secret is placed in every fixture value; no error or output may show it.
const secret = "s3cr3t-must-not-leak"

func TestResolveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.env")
	if err := os.WriteFile(path, []byte("A="+secret+"\nB=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		path,
		"a.env",
		"./a.env",
		"../dir/a.env",
		"dir:with:colons/a.env",
		`C:\config\a.env`,
		"k8s:/one-slash",
		"://no-scheme",
	} {
		src, err := Resolver{}.Resolve(arg)
		if err != nil {
			t.Errorf("%q: %v", arg, err)
			continue
		}
		if src.Name() != arg {
			t.Errorf("%q: name = %q", arg, src.Name())
		}
		if _, ok := src.(fileSource); !ok {
			t.Errorf("%q: resolved to %T, want fileSource", arg, src)
		}
	}

	src, err := Resolver{}.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	vars, err := src.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := vars.Keys(); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("keys = %v", got)
	}
	if s := fmt.Sprintf("%v %+v %#v", vars, vars, vars); strings.Contains(s, secret) {
		t.Errorf("formatting leaks a value: %s", s)
	}

	missing := filepath.Join(dir, "missing.env")
	src, _ = Resolver{}.Resolve(missing)
	if _, err := src.Load(t.Context()); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file: err = %v, want the path", err)
	}
}

func TestResolveUnknownScheme(t *testing.T) {
	tests := []struct {
		arg    string
		scheme string
	}{
		{"foo://x", "foo"},
		{"HTTPS://example.com/.env", "HTTPS"},
		{"a+b.c-d://x", "a+b.c-d"},
		{"foo://", "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			src, err := Resolver{}.Resolve(tt.arg)
			if src != nil {
				t.Errorf("source = %v, want nil", src)
			}
			unknown, ok := errors.AsType[*UnknownSchemeError](err)
			if !ok {
				t.Fatalf("err = %T %v, want *UnknownSchemeError", err, err)
			}
			if unknown.Scheme != tt.scheme || unknown.Arg != tt.arg {
				t.Errorf("err = %+v", unknown)
			}
			want := tt.arg + `: unknown source scheme "` + tt.scheme + `" (want a file path`
			if !strings.HasPrefix(err.Error(), want) {
				t.Errorf("message = %q, want prefix %q", err.Error(), want)
			}
		})
	}
}
