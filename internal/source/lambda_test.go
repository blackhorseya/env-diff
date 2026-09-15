package source

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestResolveLambda(t *testing.T) {
	tests := []struct {
		arg      string
		function string
		wantErr  string
	}{
		{arg: "lambda://my-function", function: "my-function"},
		{arg: "lambda://my-function:prod", function: "my-function:prod"},
		{arg: "lambda://arn:aws:lambda:us-east-1:123456789012:function:my-function:3", function: "arn:aws:lambda:us-east-1:123456789012:function:my-function:3"},
		{arg: "LAMBDA://my-function", function: "my-function"},
		{arg: "lambda://", wantErr: "lambda://: want lambda://<function>[:<qualifier>]"},
		{arg: "lambda://my-function/prod", wantErr: "want lambda://<function>[:<qualifier>]"},
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
			got, ok := src.(lambdaSource)
			if !ok {
				t.Fatalf("resolved to %T", src)
			}
			if got.Name() != tt.arg || got.function != tt.function {
				t.Errorf("name = %q, function = %q", got.Name(), got.function)
			}
		})
	}
}

func TestLambdaLoad(t *testing.T) {
	const want = "aws lambda get-function-configuration --function-name my-function:prod --output json"
	tests := []struct {
		name     string
		stdout   string
		wantKeys []string
	}{
		{"variables", `{"FunctionName":"my-function","Environment":{"Variables":{"TOKEN":"` + secret + `","LOG_LEVEL":"info"}}}`, []string{"LOG_LEVEL", "TOKEN"}},
		{"no environment", `{"FunctionName":"my-function","Runtime":"provided.al2023"}`, nil},
		{"empty variables", `{"Environment":{"Variables":{}}}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Run: fakeCLI(t, want, tt.stdout, "")}
			src, err := r.Resolve("lambda://my-function:prod")
			if err != nil {
				t.Fatal(err)
			}
			vars, err := src.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got := vars.Keys(); !slices.Equal(got, tt.wantKeys) {
				t.Errorf("keys = %v, want %v", got, tt.wantKeys)
			}
			if tt.name == "variables" {
				if v, _ := vars.Lookup("TOKEN"); v != secret {
					t.Error("value was not taken as is")
				}
			}
		})
	}
}

func TestLambdaLoadErrors(t *testing.T) {
	cmdErr := &CommandError{Command: "aws lambda get-function-configuration --function-name my-function --output json", Code: 254}
	tests := []struct {
		name   string
		stdout string
		err    error
		want   string
	}{
		{"aws failed", "", cmdErr,
			"lambda://my-function: aws exited with status 254 (ran: aws lambda get-function-configuration --function-name my-function --output json)"},
		{"aws missing", "", &NotFoundError{Program: "aws"}, "lambda://my-function: aws not found in PATH"},
		{"not json", "Traceback: " + secret, nil, "lambda://my-function: aws output is not valid JSON"},
		{"decrypt error", `{"Environment":{"Variables":{},"Error":{"ErrorCode":"AccessDeniedException","Message":"` + secret + `"}}}`, nil,
			"lambda://my-function: environment could not be decrypted (AccessDeniedException)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Run: func(context.Context, io.Writer, string, ...string) ([]byte, error) {
				return []byte(tt.stdout), tt.err
			}}
			src, err := r.Resolve("lambda://my-function")
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
