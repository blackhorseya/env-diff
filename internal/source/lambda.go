package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blackhorseya/env-diff/internal/diff"
	"github.com/blackhorseya/env-diff/internal/envfile"
)

// lambdaSource is the environment of an AWS Lambda function, addressed as
// lambda://<function>[:<qualifier>] where <function> is a name or an ARN,
// read with the aws CLI. The account, region and credentials come from the
// CLI's own configuration (AWS_PROFILE, AWS_REGION, the credential chain).
type lambdaSource struct {
	arg      string
	function string
	resolver Resolver
}

func parseLambda(x Resolver, arg, rest string) (Source, error) {
	if rest == "" || strings.Contains(rest, "/") {
		return nil, fmt.Errorf("%s: want lambda://<function>[:<qualifier>]", arg)
	}
	return lambdaSource{arg: arg, function: rest, resolver: x}, nil
}

func (x lambdaSource) Name() string {
	return x.arg
}

// Load reads Environment.Variables from get-function-configuration. A
// function with no variables has no Environment at all and is an empty
// environment; an Environment.Error means AWS could not decrypt the
// variables, which is reported by its error code only.
func (x lambdaSource) Load(c context.Context) (diff.Vars, error) {
	out, err := x.resolver.runner()(c, x.resolver.stderr(),
		"aws", "lambda", "get-function-configuration", "--function-name", x.function, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", x.arg, err)
	}
	var cfg struct {
		Environment *struct {
			Variables map[string]string `json:"Variables"`
			Error     *struct {
				ErrorCode string `json:"ErrorCode"`
			} `json:"Error"`
		} `json:"Environment"`
	}
	// The decode error is not wrapped: it could quote the output.
	if err := json.Unmarshal(out, &cfg); err != nil {
		return nil, fmt.Errorf("%s: aws output is not valid JSON", x.arg)
	}
	if cfg.Environment == nil {
		return envfile.FromMap(nil), nil
	}
	if cfg.Environment.Error != nil {
		return nil, fmt.Errorf("%s: environment could not be decrypted (%s)", x.arg, cfg.Environment.Error.ErrorCode)
	}
	return envfile.FromMap(cfg.Environment.Variables), nil
}
