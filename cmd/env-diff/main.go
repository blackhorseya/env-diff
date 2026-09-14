// Command env-diff compares two .env files by key and reports configuration
// drift without printing any value.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/blackhorseya/env-diff/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Ctrl-C and SIGTERM cancel the context, which kills any external
	// program fetching a remote source instead of leaving it orphaned.
	c, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(c, os.Args[1:], os.Stdout, os.Stderr, cli.Options{
		Version: version,
		Color:   colorEnabled(os.Stdout),
	})
	stop()
	os.Exit(code)
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
