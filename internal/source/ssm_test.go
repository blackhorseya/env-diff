package source

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestResolveSSM(t *testing.T) {
	tests := []struct {
		arg     string
		path    string
		wantErr string
	}{
		{arg: "ssm:///app/prod", path: "/app/prod"},
		{arg: "ssm://app/prod", path: "/app/prod"},
		{arg: "ssm:///app/prod/", path: "/app/prod"},
		{arg: "ssm://app/prod/", path: "/app/prod"},
		{arg: "SSM:///app", path: "/app"},
		{arg: "ssm://", path: "/"},
		{arg: "ssm:///", path: "/"},
		{arg: "ssm://app//prod", wantErr: "ssm://app//prod: want ssm://<path> such as ssm:///app/prod"},
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
			got, ok := src.(ssmSource)
			if !ok {
				t.Fatalf("resolved to %T", src)
			}
			if got.Name() != tt.arg || got.path != tt.path {
				t.Errorf("name = %q, path = %q, want path %q", got.Name(), got.path, tt.path)
			}
		})
	}
}

// TestSSMKey pins the parameter-name-to-key rule: strip the path, take
// the rest as it is, refuse anything that is not a direct child.
func TestSSMKey(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr string
	}{
		{"/app/prod/DB_HOST", "/app/prod", "DB_HOST", ""},
		{"/app/prod/db_host", "/app/prod", "db_host", ""},
		{"/DB_HOST", "/", "DB_HOST", ""},
		{"/app/prod/db-host", "/app/prod", "db-host", ""},
		{"/app/prod/db.host", "/app/prod", "db.host", ""},
		{"/app/prod", "/app/prod", "", "parameter /app/prod is not under /app/prod"},
		{"/app/prod/", "/app/prod", "", "parameter /app/prod/ is not under /app/prod"},
		{"/other/DB_HOST", "/app/prod", "", "parameter /other/DB_HOST is not under /app/prod"},
		{"/app/production/X", "/app/prod", "", "parameter /app/production/X is not under /app/prod"},
		{"/app/prod/nested/X", "/app/prod", "", "parameter /app/prod/nested/X is nested under /app/prod; only direct children are compared"},
		{"/app/X", "/", "", "parameter /app/X is nested under /; only direct children are compared"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" under "+tt.path, func(t *testing.T) {
			got, err := ssmKey(tt.name, tt.path)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("key = %q, err = %v, want %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("key = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSSMLoad(t *testing.T) {
	const want = "aws ssm get-parameters-by-path --path /app/prod --with-decryption --output json"
	r := Resolver{Run: fakeCLI(t, want,
		`{"Parameters":[`+
			`{"Name":"/app/prod/TOKEN","Type":"SecureString","Value":"`+secret+`","Version":3,"ARN":"arn:aws:ssm:us-east-1:123456789012:parameter/app/prod/TOKEN"},`+
			`{"Name":"/app/prod/LOG_LEVEL","Type":"String","Value":"info","Version":1}`+
			`]}`, "")}
	src, err := r.Resolve("ssm:///app/prod")
	if err != nil {
		t.Fatal(err)
	}
	vars, err := src.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := vars.Keys(); !slices.Equal(got, []string{"LOG_LEVEL", "TOKEN"}) {
		t.Errorf("keys = %v", got)
	}
	if v, _ := vars.Lookup("TOKEN"); v != secret {
		t.Error("decrypted value was not taken as is")
	}

	r = Resolver{Run: fakeCLI(t, "aws ssm get-parameters-by-path --path / --with-decryption --output json", `{"Parameters":[]}`, "")}
	src, _ = r.Resolve("ssm://")
	vars, err = src.Load(t.Context())
	if err != nil || len(vars.Keys()) != 0 {
		t.Errorf("empty root: keys = %v, err = %v", vars.Keys(), err)
	}
}

func TestSSMLoadErrors(t *testing.T) {
	cmdErr := &CommandError{Command: "aws ssm get-parameters-by-path --path /app/prod --with-decryption --output json", Code: 254}
	tests := []struct {
		name   string
		stdout string
		err    error
		want   string
	}{
		{"aws failed", "", cmdErr,
			"ssm:///app/prod: aws exited with status 254 (ran: aws ssm get-parameters-by-path --path /app/prod --with-decryption --output json)"},
		{"aws missing", "", &NotFoundError{Program: "aws"}, "ssm:///app/prod: aws not found in PATH"},
		{"not json", "Traceback: " + secret, nil, "ssm:///app/prod: aws output is not valid JSON"},
		{"duplicate parameter", `{"Parameters":[{"Name":"/app/prod/A","Value":"` + secret + `"},{"Name":"/app/prod/A","Value":"` + secret + `"}]}`, nil,
			"ssm:///app/prod: parameters /app/prod/A and /app/prod/A both map to key A"},
		{"outside the path", `{"Parameters":[{"Name":"/other/A","Value":"` + secret + `"}]}`, nil,
			"ssm:///app/prod: parameter /other/A is not under /app/prod"},
		{"nested", `{"Parameters":[{"Name":"/app/prod/db/HOST","Value":"` + secret + `"}]}`, nil,
			"ssm:///app/prod: parameter /app/prod/db/HOST is nested under /app/prod; only direct children are compared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Run: func(context.Context, io.Writer, string, ...string) ([]byte, error) {
				return []byte(tt.stdout), tt.err
			}}
			src, err := r.Resolve("ssm:///app/prod")
			if err != nil {
				t.Fatal(err)
			}
			vars, err := src.Load(t.Context())
			if vars != nil {
				t.Errorf("vars = %v, want nil", vars)
			}
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("err = %v, want prefix %q", err, tt.want)
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
