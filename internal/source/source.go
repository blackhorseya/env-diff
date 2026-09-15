// Package source turns a command-line argument into a named set of
// variables. A bare argument is a local .env file; an argument with a URI
// scheme names a remote store that is read through that store's own CLI.
// The comparison engine never learns where variables came from.
package source

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/blackhorseya/env-diff/internal/diff"
)

// Source is one environment to compare: resolved from an argument, not yet
// loaded, so every argument is checked before any file or network access.
type Source interface {
	// Name is the argument as the user typed it.
	Name() string
	// Load reads the variables. Errors name the source, never a value.
	Load(c context.Context) (diff.Vars, error)
}

// Resolver turns arguments into Sources. The zero value runs external
// programs from PATH and discards what they write to stderr.
type Resolver struct {
	// Run executes an external CLI such as kubectl. nil looks the program
	// up on PATH and runs it.
	Run Runner
	// Stderr receives whatever the external CLI writes to its stderr,
	// verbatim. nil discards it.
	Stderr io.Writer
}

// supportedSchemes is listed in the unknown-scheme error; remote adapters
// extend it as they are added.
const supportedSchemes = ", k8s://, lambda:// or ssm://"

var schemeRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*)://(.*)$`)

// Resolve classifies arg without touching it: a scheme prefix such as
// k8s:// selects a remote store, anything else is a file path. An error
// describes the form of the argument, so callers treat it as a usage error.
func (x Resolver) Resolve(arg string) (Source, error) {
	m := schemeRE.FindStringSubmatch(arg)
	if m == nil {
		return fileSource{path: arg}, nil
	}
	switch strings.ToLower(m[1]) {
	case "k8s":
		return parseK8s(x, arg, m[2])
	case "lambda":
		return parseLambda(x, arg, m[2])
	case "ssm":
		return parseSSM(x, arg, m[2])
	}
	return nil, &UnknownSchemeError{Arg: arg, Scheme: m[1]}
}

// UnknownSchemeError reports an argument whose scheme names no supported
// store.
type UnknownSchemeError struct {
	Arg    string
	Scheme string
}

func (x *UnknownSchemeError) Error() string {
	return fmt.Sprintf("%s: unknown source scheme %q (want a file path%s)", x.Arg, x.Scheme, supportedSchemes)
}
