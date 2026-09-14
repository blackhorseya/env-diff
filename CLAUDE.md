# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`env-diff <source> <target>` — a small Go CLI that compares two `.env` files by key and reports drift (missing / extra / different) without ever printing a value. Exit codes 0 no drift, 1 drift, 2 error, so it works as a CI gate. v0.1 is two local files only: no network, no config file, no persistent state, no fixing or syncing, no secret scanning, no Kubernetes/AWS/Vault adapters. When scope is unclear, pick the smaller behaviour. Future remote sources (v0.3) must stay optional adapters outside the compare engine.

## Commands

Dev commands use [Task](https://taskfile.dev) (`Taskfile.yml`); binaries go to `./bin/`.

```
task build      # ./bin/env-diff, version stamped from git describe
task test       # go test -race ./...
task lint       # go vet + golangci-lint (v2 config in .golangci.yml)
task fmt        # gofmt + goimports via golangci-lint fmt
task snapshot   # goreleaser release --snapshot --clean → ./dist
```

Single test: `go test ./internal/envfile -run 'TestParse/inline_comment' -v` (subtest names replace spaces with underscores).
`go test -short ./...` skips `TestBinaryExitCodes` in `cmd/env-diff`, which runs `go build` and executes the real binary.
Validate release config: `goreleaser check`. Releases are cut by pushing a `v*` tag (`.github/workflows/release.yml`).

## Architecture

A straight pipeline of four packages under `internal/`, each depending only on the ones before it:

```
envfile.ParseFile ×2  → Env       values unexported; String/GoString print only the key count
diff.Compare          → Result    Same / Missing / Extra / Different, sorted, never nil
presenter.Terminal | presenter.JSON   key names only
cli.Run               → exit code cobra command; drift is app state, every error is exit 2
```

`cmd/env-diff/main.go` only sets `version` (ldflags `-X main.version`), detects color (stdout is a char device, `NO_COLOR` unset, `TERM` not `dumb`) and calls `cli.Run`, which returns the exit code instead of calling `os.Exit` so tests run it in-process.

### Invariants

- **No value ever reaches output.** Not stdout, not stderr, not an error string, not a test failure message. `envfile.Error` carries `Line` and `Msg` only; duplicate-key errors name the key (keys are printed everywhere anyway) but nothing else from the line. `Env` implements `String` and `GoString` so `%v`, `%+v` and `%#v` cannot leak. `TestNeverPrintsValues` in `internal/cli/cli_test.go` sweeps every output path with a sentinel value **and** asserts the expected key or line number is present, so the check cannot pass vacuously. A new flag, format or error path must be added there.
- **Parser rules are a contract.** They are pinned by table tests in `envfile_test.go` and documented in README "Supported `.env` syntax"; change both together. The rules most likely to be questioned: `#` starts a comment only at line start, after whitespace, or right after a closing quote (`PASSWORD=abc#123` stays whole); double quotes unescape only `\"` and `\\`; single quotes are literal; duplicate keys, unterminated quotes, text after a closing quote and keys outside `[A-Za-z_][A-Za-z0-9_]*` are errors; a quoted value may span lines (line endings inside it become LF, errors report the assignment's first line); an empty or comment-only file is a valid empty environment; `${VAR}` is literal text. Prefer an explicit error over guessing.
- **`diff` does not import `envfile`.** `diff.Vars` is a two-method interface declared on the consumer side; `envfile.Env` satisfies it and `diff_test.go` uses a plain map. Keep the comparator free of file and CLI concerns so v0.3 adapters can feed it directly.
- **Exit codes 0/1/2** are documented in README and pinned by `TestBinaryExitCodes` (real process) plus the cli tests (in-process). Usage errors (bad args, unknown flag, unknown `--format`) are `*usageError`, which adds the `Run 'env-diff --help'` hint; file and parse errors do not get the hint. `--quiet` silences stdout only; errors still reach stderr.
- **Output layout is golden-tested** in `presenter_test.go` and `cli_test.go`, and README samples are pasted from real output. Color must never change layout: `TestTerminalColor` strips the ANSI codes and expects byte equality with plain output. JSON lists are always arrays (`orEmpty`), never `null`. `examples/` holds the files behind every README sample; `TestExamples` in `cli_test.go` pins their exact output, so README, `examples/` and that test change together.
- **Dependencies**: cobra only, chosen to match `git-why`. Don't add a color, isatty or dotenv library.

## Testing notes

- Fixtures put a `secret` sentinel constant into every value so leak checks read the same in every package.
- `TestFileAndParseErrors` includes a permission-denied case via `os.Chmod(…, 0o000)`, skipped when running as root.
- `TestColorEnabled` in `cmd/env-diff` runs with stdout as a pipe, so only the "regular file is not a tty" case exercises real logic; the `NO_COLOR` and `TERM=dumb` assertions pass vacuously. Restructure `colorEnabled` before relying on them.
- `cli.Run` normalises a nil `args` to an empty slice; cobra otherwise falls back to `os.Args`, which breaks in-process tests that pass no arguments.

## Code conventions

- Go 1.26: `errors.AsType[T]`, `strings.Lines`, `slices.Sorted(maps.Keys(...))`, `for i := range n`.
- Receivers: `x` for value and error types (`Env`, `Result`, `*usageError`), `a` for the cli `*app`, `p` for `painter`.
- Layout, tooling and the `cli.Run(args, stdout, stderr, opts) int` shape mirror `~/workspace/personal/git-why`; commit messages follow Conventional Commits.
