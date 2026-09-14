// Package cli implements the env-diff command: flags, orchestration of the
// parser, comparator and presenter, and exit codes.
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/blackhorseya/env-diff/internal/diff"
	"github.com/blackhorseya/env-diff/internal/envfile"
	"github.com/blackhorseya/env-diff/internal/presenter"
)

// Exit codes, as documented in the README.
const (
	exitOK    = 0 // no differences
	exitDrift = 1 // differences found
	exitError = 2 // file, parse, or usage error
)

// Options carries the process-level settings main resolves before Run.
type Options struct {
	// Version stamped at build time; "" or "dev" falls back to module info.
	Version string
	// Color enables ANSI colors in terminal output.
	Color bool
}

// Run executes env-diff with args (excluding the program name) and returns
// the process exit code. Normal output goes to stdout, errors to stderr.
func Run(args []string, stdout, stderr io.Writer, opts Options) int {
	if args == nil {
		args = []string{} // a nil slice makes cobra fall back to os.Args
	}
	a := &app{opts: opts}
	cmd := a.command()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		// Nothing useful can be done if stderr itself is broken.
		_, _ = fmt.Fprintf(stderr, "env-diff: %v\n", err)
		if _, ok := errors.AsType[*usageError](err); ok {
			_, _ = fmt.Fprintln(stderr, "Run 'env-diff --help' for usage.")
		}
		return exitError
	}
	if a.drift {
		return exitDrift
	}
	return exitOK
}

type app struct {
	opts     Options
	keysOnly bool
	ignore   []string
	format   string
	quiet    bool
	drift    bool // set by run once the comparison is done
}

func (a *app) command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env-diff <source> <target> [<target>...]",
		Short: "Detect environment configuration drift without exposing secrets",
		Long: "Compare .env files by key, ignoring order, comments and blank lines,\n" +
			"and report which keys are missing, extra, or have a different value\n" +
			"relative to the first file. Values are never printed.\n\n" +
			"Exit codes:\n" +
			"  0  no differences\n" +
			"  1  differences found\n" +
			"  2  error (file not found, invalid syntax, bad arguments)",
		Example: "  env-diff .env.staging .env.production\n" +
			"  env-diff .env.example .env.production --keys-only\n" +
			"  env-diff .env.staging .env.production --format json\n" +
			"  env-diff .env.dev .env.staging .env.production\n" +
			"  env-diff .env.staging .env.production --ignore DEBUG,LOCAL_PORT",
		Version:           resolveVersion(a.opts.Version),
		Args:              atLeastTwoFiles,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              a.run,
	}
	cmd.Flags().BoolVar(&a.keysOnly, "keys-only", false, "compare key presence only; ignore value differences")
	cmd.Flags().StringSliceVar(&a.ignore, "ignore", nil, "keys to leave out of the comparison (repeatable or comma-separated)")
	cmd.Flags().StringVar(&a.format, "format", "terminal", "output format: terminal or json")
	cmd.Flags().BoolVar(&a.quiet, "quiet", false, "print nothing; rely on the exit code")
	cmd.SetVersionTemplate("env-diff {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	return cmd
}

func atLeastTwoFiles(_ *cobra.Command, args []string) error {
	if len(args) >= 2 {
		return nil
	}
	return &usageError{err: fmt.Errorf("expected at least two files (<source> <target>...), got %d", len(args))}
}

func (a *app) run(cmd *cobra.Command, args []string) error {
	if a.format != "terminal" && a.format != "json" {
		return &usageError{err: fmt.Errorf("unknown format %q (want terminal or json)", a.format)}
	}
	envs := make([]diff.Environment, 0, len(args))
	for _, path := range args {
		env, err := envfile.ParseFile(path)
		if err != nil {
			return err
		}
		envs = append(envs, diff.Environment{Name: path, Vars: env})
	}

	result := diff.Compare(envs, diff.Options{KeysOnly: a.keysOnly, Ignore: a.ignore})
	a.drift = result.HasDrift()
	if a.quiet {
		return nil
	}

	w := cmd.OutOrStdout()
	var err error
	if a.format == "json" {
		err = presenter.JSON(w, result)
	} else {
		err = presenter.Terminal(w, result, a.opts.Color)
	}
	if err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// usageError marks bad arguments or flags so Run can add a help hint.
type usageError struct {
	err error
}

func (x *usageError) Error() string {
	return x.err.Error()
}

func (x *usageError) Unwrap() error {
	return x.err
}

// resolveVersion prefers the version stamped at build time and falls back
// to the module version recorded by go install.
func resolveVersion(stamped string) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
