// Command env-diff compares two .env files by key and reports configuration
// drift without printing any value.
package main

import (
	"os"

	"github.com/blackhorseya/env-diff/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.Options{
		Version: version,
		Color:   colorEnabled(os.Stdout),
	}))
}

// colorEnabled reports whether f is an interactive terminal and the user
// has not opted out through NO_COLOR or TERM=dumb.
func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
