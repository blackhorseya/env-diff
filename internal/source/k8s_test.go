package source

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestResolveK8s(t *testing.T) {
	tests := []struct {
		arg     string
		want    k8sSource
		wantErr string
	}{
		{arg: "k8s://prod/configmap/app", want: k8sSource{namespace: "prod", kind: "configmap", name: "app"}},
		{arg: "k8s://prod/secret/app-secrets", want: k8sSource{namespace: "prod", kind: "secret", name: "app-secrets"}},
		{arg: "K8S://prod/configmap/app", want: k8sSource{namespace: "prod", kind: "configmap", name: "app"}},
		{arg: "k8s://prod/app", wantErr: "k8s://prod/app: want k8s://<namespace>/<configmap|secret>/<name>"},
		{arg: "k8s://prod/configmap/app/extra", wantErr: "want k8s://<namespace>/<configmap|secret>/<name>"},
		{arg: "k8s:///configmap/app", wantErr: "want k8s://<namespace>/<configmap|secret>/<name>"},
		{arg: "k8s://prod/configmap/", wantErr: "want k8s://<namespace>/<configmap|secret>/<name>"},
		{arg: "k8s://prod/deployment/app", wantErr: `k8s://prod/deployment/app: unknown kind "deployment" (want configmap or secret)`},
		{arg: "k8s://prod/ConfigMap/app", wantErr: `unknown kind "ConfigMap"`},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			src, err := Resolver{}.Resolve(tt.arg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, ok := src.(k8sSource)
			if !ok {
				t.Fatalf("resolved to %T", src)
			}
			if got.Name() != tt.arg {
				t.Errorf("name = %q, want the argument as typed", got.Name())
			}
			parsed := [3]string{got.namespace, got.kind, got.name}
			if want := [3]string{tt.want.namespace, tt.want.kind, tt.want.name}; parsed != want {
				t.Errorf("parsed = %v, want %v", parsed, want)
			}
		})
	}
}

// fakeKubectl returns a Runner that checks the exact command line and
// replies with stdout, writing note to the forwarded stderr.
func fakeKubectl(t *testing.T, wantArgs, stdout, note string) Runner {
	t.Helper()
	return func(_ context.Context, stderr io.Writer, name string, args ...string) ([]byte, error) {
		if got := commandLine(name, args); got != wantArgs {
			t.Errorf("ran %q, want %q", got, wantArgs)
		}
		_, _ = io.WriteString(stderr, note)
		return []byte(stdout), nil
	}
}

func TestK8sLoadConfigMap(t *testing.T) {
	var stderr bytes.Buffer
	r := Resolver{
		Run: fakeKubectl(t, "kubectl get configmap app -n prod -o json",
			`{"apiVersion":"v1","kind":"ConfigMap","data":{"LOG_LEVEL":"debug","TOKEN":"`+secret+`"},"binaryData":{"BLOB":"`+b64("bin-"+secret)+`"}}`,
			"kubectl says hello\n"),
		Stderr: &stderr,
	}
	src, err := r.Resolve("k8s://prod/configmap/app")
	if err != nil {
		t.Fatal(err)
	}
	vars, err := src.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := vars.Keys(); !slices.Equal(got, []string{"BLOB", "LOG_LEVEL", "TOKEN"}) {
		t.Errorf("keys = %v", got)
	}
	if v, _ := vars.Lookup("TOKEN"); v != secret {
		t.Error("data value was not taken as is")
	}
	if v, _ := vars.Lookup("BLOB"); v != "bin-"+secret {
		t.Error("binaryData value was not base64-decoded")
	}
	if stderr.String() != "kubectl says hello\n" {
		t.Errorf("forwarded stderr = %q", stderr.String())
	}
	if s := fmt.Sprintf("%v %+v %#v", vars, vars, vars); strings.Contains(s, secret) {
		t.Errorf("formatting leaks a value: %s", s)
	}
}

func TestK8sLoadSecret(t *testing.T) {
	r := Resolver{Run: fakeKubectl(t, "kubectl get secret app -n prod -o json",
		`{"kind":"Secret","type":"Opaque","data":{"API_KEY":"`+b64(secret)+`","EMPTY":""}}`, "")}
	src, err := r.Resolve("k8s://prod/secret/app")
	if err != nil {
		t.Fatal(err)
	}
	vars, err := src.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := vars.Keys(); !slices.Equal(got, []string{"API_KEY", "EMPTY"}) {
		t.Errorf("keys = %v", got)
	}
	if v, _ := vars.Lookup("API_KEY"); v != secret {
		t.Error("secret value was not base64-decoded")
	}
	if v, ok := vars.Lookup("EMPTY"); !ok || v != "" {
		t.Errorf("empty value = %q, %v", v, ok)
	}
}

func TestK8sLoadEmptyObject(t *testing.T) {
	for _, stdout := range []string{`{}`, `{"data":null}`, `{"kind":"ConfigMap","metadata":{"name":"app"}}`} {
		r := Resolver{Run: fakeKubectl(t, "kubectl get configmap app -n prod -o json", stdout, "")}
		src, _ := r.Resolve("k8s://prod/configmap/app")
		vars, err := src.Load(t.Context())
		if err != nil {
			t.Errorf("%s: %v", stdout, err)
			continue
		}
		if len(vars.Keys()) != 0 {
			t.Errorf("%s: keys = %v, want none", stdout, vars.Keys())
		}
	}
}

func TestK8sLoadErrors(t *testing.T) {
	cmdErr := &CommandError{Command: "kubectl get configmap app -n prod -o json", Code: 1}
	tests := []struct {
		name   string
		arg    string
		stdout string
		err    error
		want   string
	}{
		{"kubectl failed", "k8s://prod/configmap/app", "", cmdErr,
			"k8s://prod/configmap/app: kubectl exited with status 1 (ran: kubectl get configmap app -n prod -o json)"},
		{"kubectl missing", "k8s://prod/configmap/app", "", &NotFoundError{Program: "kubectl"},
			"k8s://prod/configmap/app: kubectl not found in PATH"},
		{"not json", "k8s://prod/configmap/app", "error: " + secret, nil,
			"k8s://prod/configmap/app: kubectl output is not valid JSON"},
		{"bad base64", "k8s://prod/secret/app", `{"data":{"API_KEY":"` + secret + `!"}}`, nil,
			"k8s://prod/secret/app: key API_KEY is not valid base64"},
		{"bad binary base64", "k8s://prod/configmap/app", `{"binaryData":{"BLOB":"` + secret + `!"}}`, nil,
			"k8s://prod/configmap/app: key BLOB is not valid base64"},
		{"key in both", "k8s://prod/configmap/app", `{"data":{"A":"` + secret + `"},"binaryData":{"A":"` + b64(secret) + `"}}`, nil,
			"k8s://prod/configmap/app: key A is in both data and binaryData"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Run: func(context.Context, io.Writer, string, ...string) ([]byte, error) {
				return []byte(tt.stdout), tt.err
			}}
			src, err := r.Resolve(tt.arg)
			if err != nil {
				t.Fatal(err)
			}
			vars, err := src.Load(t.Context())
			if vars != nil {
				t.Errorf("vars = %v, want nil", vars)
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error leaks a value: %v", err)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Errorf("runner error not wrapped: %v", err)
			}
		})
	}
}
