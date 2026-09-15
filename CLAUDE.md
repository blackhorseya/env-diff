# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`env-diff <source> <target> [<target>...]` — a small Go CLI that compares two or more environments by key and reports drift (missing / extra / different) without ever printing a value. An environment is a `.env` file or a remote store addressed by URI (`k8s://`, `lambda://`, `ssm://`), read by shelling out to `kubectl` or `aws`. Exit codes 0 no drift, 1 drift, 2 error, so it works as a CI gate. Out of scope: config files, persistent state, fixing or syncing, writing to any store, secret scanning, network access from env-diff itself, and stores beyond the four adapters (Vault and the like go through process substitution). When scope is unclear, pick the smaller behaviour.

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

Five packages under `internal/`. `envfile` and `diff` import nothing internal, `source` imports both, `presenter` imports `diff`, `cli` imports everything:

```
source.Resolve ×N     → Source    classifies each arg with no I/O: bare = file, scheme:// = remote store
Source.Load ×N        → diff.Vars files via envfile.ParseFile; remotes run kubectl/aws, then envfile.FromMap
diff.Compare          → Result    one Row per key with a Cell per env (present + value group), plus
                                  Same / Missing / Extra / Different lists; env 0 is the reference
presenter.Terminal | presenter.JSON   key names only; sections for 2 envs, a matrix for 3+
cli.Run               → exit code cobra command; drift is app state, every error is exit 2
```

`cmd/env-diff/main.go` only sets `version` (ldflags `-X main.version`), detects color (stdout is a char device, `NO_COLOR` unset, `TERM` not `dumb`), cancels a context on SIGINT/SIGTERM (`signal.NotifyContext`) and calls `cli.Run(c, args, stdout, stderr, opts)`, which returns the exit code instead of calling `os.Exit` so tests run it in-process.

### Remote sources (`internal/source`)

- **Resolve before Load.** `app.run` resolves every argument before loading any, so a malformed address fails before a file is read or a command runs. Unknown schemes are `*source.UnknownSchemeError`, bad shapes plain errors; `cli` wraps both in `*usageError`. A new scheme goes in the switch in `Resolve` and in `supportedSchemes`.
- **One read command per source.** Each adapter parses its address, runs one read-only command with JSON output, unmarshals into an anonymous struct holding only the fields it needs, and returns `envfile.FromMap` (a copy; keys are not validated, so ConfigMap or SSM names with `-` or `.` pass through). Never add a command that writes.
- **`Runner` is the seam.** `execRunner` in production: `exec.CommandContext`, no stdin, stdout captured, stderr streamed to env-diff's stderr, `WaitDelay` so a grandchild holding the pipe cannot hang Wait. Failures map to `*NotFoundError`, `*CommandError{Command, Code}` (message carries the exact command line so it can be rerun), `ErrInterrupted`, `ErrTimedOut`. Every `Load` error is prefixed with the argument as typed.
- **Values.** Secret `data` and ConfigMap `binaryData` are base64-decoded; SSM always passes `--with-decryption`; a Lambda `Environment.Error` reports its error code, never its message. Decoded values are compared like file values (decision of 2026-09-14: compare, don't limit secrets to keys).
- **Configuration** (kube context, AWS profile and region) comes only from the CLI's own environment variables and config files. env-diff has no flags for it.
- **SSM keys**: the name minus `path + "/"` (`"/"` at the root), taken as is. A parameter that is not a direct child of the path, or two that map to one key, is an error.

### Invariants

- **No value ever reaches output.** Not stdout, not stderr, not an error string, not a test failure message. `envfile.Error` carries `Line` and `Msg` only; duplicate-key errors name the key (keys are printed everywhere anyway) but nothing else from the line. `Env` implements `String` and `GoString` so `%v`, `%+v` and `%#v` cannot leak. JSON decode errors from adapters are deliberately not wrapped, since `encoding/json` can quote the input and the input is the values; base64 errors name the key only. `TestNeverPrintsValues` in `internal/cli/cli_test.go` sweeps every output path with a sentinel value **and** asserts the expected key or line number is present, so the check cannot pass vacuously. A new flag, format, scheme or error path must be added there. The one sanctioned exception is the external CLI's stderr, which is forwarded verbatim (also under `--quiet`) so kubectl and aws diagnostics reach the user; sweep fixtures therefore put the sentinel in fetched values, never in a fake CLI's stderr, and never in arguments, which are echoed as typed like file paths.
- **Parser rules are a contract.** They are pinned by table tests in `envfile_test.go` and documented in README "Supported `.env` syntax"; change both together. The rules most likely to be questioned: `#` starts a comment only at line start, after whitespace, or right after a closing quote (`PASSWORD=abc#123` stays whole); double quotes unescape only `\"` and `\\`; single quotes are literal; duplicate keys, unterminated quotes, text after a closing quote and keys outside `[A-Za-z_][A-Za-z0-9_]*` are errors; a quoted value may span lines (line endings inside it become LF, errors report the assignment's first line); an empty or comment-only file is a valid empty environment; `${VAR}` is literal text. Prefer an explicit error over guessing.
- **`diff` does not import `envfile` or `source`.** `diff.Vars` is a two-method interface declared on the consumer side; `envfile.Env` satisfies it and `diff_test.go` uses a plain map. `Compare` takes `[]diff.Environment` and judges presence before values: a key absent from any env is Missing or Extra even if the present copies disagree; the row's cells still carry the value groups (`TestCompareRows`). Group numbers are assigned in env order among present cells, so group 0 is the reference's value only when the reference has the key. Keep the comparator free of file, CLI and store concerns.
- **Exit codes 0/1/2** are documented in README and pinned by `TestBinaryExitCodes` (real process) plus the cli tests (in-process). Usage errors (bad args, unknown flag, unknown `--format`, unknown scheme, malformed address) are `*usageError`, which adds the `Run 'env-diff --help'` hint; file, parse and remote errors do not get the hint. `--quiet` silences stdout only; errors and forwarded CLI stderr still reach stderr. `--ignore` drops keys before classification (`Result.Ignored` lists the ones that existed); the terminal prints `N keys ignored` whenever the flag is given, including `0 keys ignored`, so a mistyped name is visible rather than silently inert. `--allow-extra` keeps Extra keys in the lists and in JSON but removes them from `Count`/`HasDrift`, the sections and the matrix rows; the terminal prints `N extra keys allowed`. `--keys-only --allow-extra` is the `.env.example` validation mode the README and the action document.
- **Output layout is golden-tested** in `presenter_test.go` and `cli_test.go`, and README samples are pasted from real output. Color must never change layout: `TestTerminalColor` strips the ANSI codes and expects byte equality with plain output for both the sectioned and the matrix form (matrix cells are padded before painting). JSON key lists are always arrays (`orEmpty`), never `null`; inside `matrix`, `null` means the key is absent from that env, `envs` holds the arguments as typed (the scheme is the kind), and `target` is emitted only for exactly two envs. `examples/` (`.env.staging`, `.env.production`, `.env.qa`, `.env.example`) holds the files behind every README sample; `TestExamples` in `cli_test.go` pins their exact two-file and three-file output, so README, `examples/` and that test change together. The `--help` text lists every scheme and `TestHelpAndVersion` checks it.
- **Dependencies**: cobra only, chosen to match `git-why`. Remote stores are reached by shelling out to `kubectl` / `aws`, never through an SDK (v0.3 decision: no new dependencies, the user's existing auth just works, the binary stays small). Don't add a color, isatty, dotenv, client-go or AWS SDK library.

### GitHub Action (`action.yml`)

A composite action at the repo root, used as `blackhorseya/env-diff@<tag>`. The install step downloads the release archive matching `RUNNER_OS`/`RUNNER_ARCH`, verifies it against `checksums.txt` and prepends it to `PATH`; the run step maps inputs to flags and writes `exit-code` and `drift` outputs. `files` accepts source URIs too; kubectl or aws must be configured by an earlier step. Two things are easy to break: the version must resolve to a `v*` tag (`inputs.version`, else `github.action_ref`, which is empty for `uses: ./`), and `fail-on-drift: "false"` must forgive exit 1 only, never exit 2. CI covers both paths: `action-install` downloads `v0.1.0` on ubuntu and macos, `action-source` builds from the checkout and runs with `install: "false"`. A tag must contain `action.yml` before it is usable as an action ref.

## Testing notes

- Fixtures put a `secret` sentinel constant into every value so leak checks read the same in every package.
- No test may reach a real cluster or AWS account. `fakeCLIs` in `cli_test.go` writes shell scripts into a temp dir that becomes the whole `PATH`, points `KUBECONFIG`, `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE` at an empty file and clears `AWS_PROFILE` and the key variables; the scripts may use only shell builtins. `cliScript` answers exact command lines and exits 9 on any other, which also pins the argv each adapter builds. Unit tests in `internal/source` inject a `Runner` func (`fakeCLI` there) that asserts the command line.
- `exec_test.go` drives `execRunner` through `sh`; the timeout and cancel tests use `exec sleep 30` so the kill reaches the sleeping process.
- `TestFileAndParseErrors` includes a permission-denied case via `os.Chmod(…, 0o000)`, skipped when running as root.
- `TestColorEnabled` in `cmd/env-diff` runs with stdout as a pipe, so only the "regular file is not a tty" case exercises real logic; the `NO_COLOR` and `TERM=dumb` assertions pass vacuously. Restructure `colorEnabled` before relying on them.
- `cli.Run` normalises a nil `args` to an empty slice; cobra otherwise falls back to `os.Args`, which breaks in-process tests that pass no arguments.

## Code conventions

- Go 1.26: `errors.AsType[T]`, `strings.Lines`, `slices.Sorted(maps.Keys(...))`, `for i := range n`.
- Receivers: `x` for value and error types (`Env`, `Result`, `*usageError`, sources, `Resolver`), `a` for the cli `*app`, `p` for `painter`.
- Layout, tooling and the `cli.Run(c, args, stdout, stderr, opts) int` shape mirror `~/workspace/personal/git-why`; commit messages follow Conventional Commits.
