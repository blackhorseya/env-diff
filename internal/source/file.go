package source

import (
	"context"

	"github.com/blackhorseya/env-diff/internal/diff"
	"github.com/blackhorseya/env-diff/internal/envfile"
)

// fileSource is a local .env file.
type fileSource struct {
	path string
}

func (x fileSource) Name() string {
	return x.path
}

func (x fileSource) Load(context.Context) (diff.Vars, error) {
	env, err := envfile.ParseFile(x.path)
	if err != nil {
		return nil, err
	}
	return env, nil
}
