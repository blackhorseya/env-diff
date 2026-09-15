package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blackhorseya/env-diff/internal/diff"
	"github.com/blackhorseya/env-diff/internal/envfile"
)

// ssmSource is one level of the AWS SSM Parameter Store hierarchy,
// addressed as ssm://<path>, read with the aws CLI. Only the direct
// children of the path are read (no --recursive). SecureString values are
// decrypted (--with-decryption, which needs kms:Decrypt on their key) so
// they compare as the application sees them. The aws CLI pages through
// the result itself.
type ssmSource struct {
	arg      string
	path     string // normalised: leading "/", no trailing "/", or exactly "/"
	resolver Resolver
}

// parseSSM accepts ssm:///app/prod, ssm://app/prod and ssm:///app/prod/
// as the same path; ssm:// alone is the root.
func parseSSM(x Resolver, arg, rest string) (Source, error) {
	path := "/" + strings.Trim(rest, "/")
	if strings.Contains(path, "//") {
		return nil, fmt.Errorf("%s: want ssm://<path> such as ssm:///app/prod", arg)
	}
	return ssmSource{arg: arg, path: path, resolver: x}, nil
}

func (x ssmSource) Name() string {
	return x.arg
}

func (x ssmSource) Load(c context.Context) (diff.Vars, error) {
	out, err := x.resolver.runner()(c, x.resolver.stderr(),
		"aws", "ssm", "get-parameters-by-path", "--path", x.path, "--with-decryption", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", x.arg, err)
	}
	var res struct {
		Parameters []struct {
			Name  string `json:"Name"`
			Value string `json:"Value"`
		} `json:"Parameters"`
	}
	// The decode error is not wrapped: it could quote the output.
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("%s: aws output is not valid JSON", x.arg)
	}

	vars := make(map[string]string, len(res.Parameters))
	names := make(map[string]string, len(res.Parameters)) // key → parameter it came from
	for _, p := range res.Parameters {
		key, err := ssmKey(p.Name, x.path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.arg, err)
		}
		if other, dup := names[key]; dup {
			return nil, fmt.Errorf("%s: parameters %s and %s both map to key %s", x.arg, other, p.Name, key)
		}
		names[key] = p.Name
		vars[key] = p.Value
	}
	return envfile.FromMap(vars), nil
}

// ssmKey turns a parameter name into the variable name used for the
// comparison: the name with the path prefix removed, taken as it is.
//
// path is normalised: a leading "/", no trailing "/", or exactly "/" for
// the root. A parameter that is not directly under path is an error even
// though a non-recursive query should never return one; an explicit error
// beats silently comparing the wrong thing. Characters that .env keys
// cannot hold ("-", ".") pass through unchanged, like ConfigMap keys do,
// so such a parameter shows up as missing or extra under its real name
// and can be left out with --ignore.
func ssmKey(name, path string) (string, error) {
	prefix := path + "/"
	if path == "/" {
		prefix = "/"
	}
	key, ok := strings.CutPrefix(name, prefix)
	if !ok || key == "" {
		return "", fmt.Errorf("parameter %s is not under %s", name, path)
	}
	if strings.Contains(key, "/") {
		return "", fmt.Errorf("parameter %s is nested under %s; only direct children are compared", name, path)
	}
	return key, nil
}
