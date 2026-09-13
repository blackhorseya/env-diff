# env-diff

Detect environment configuration drift without exposing secrets.

```
$ env-diff .env.staging .env.production

Environment Drift

Missing in target
  STRIPE_API_KEY

Extra in target
  OLD_FEATURE_FLAG

Different values
  LOG_LEVEL

3 differences found
```

`env-diff` compares two `.env` files **by key**, not by line. Variable order,
comments and blank lines are ignored, and **values are never printed**: the
output only says whether a key is the same, missing, extra, or different.
The exit code tells CI whether the environments drifted.

## Installation

With Go:

```
go install github.com/blackhorseya/env-diff/cmd/env-diff@latest
```

Or download a binary for macOS or Linux from the
[releases page](https://github.com/blackhorseya/env-diff/releases).

## Usage

```
env-diff <source> <target> [flags]
```

Every key is classified relative to the **source**:

| Status    | Meaning                                        |
|-----------|------------------------------------------------|
| same      | present in both files with the same value      |
| missing   | present in source, absent from target          |
| extra     | absent from source, present in target          |
| different | present in both files with a different value   |

### Exit codes

| Code | Meaning                                                 |
|------|---------------------------------------------------------|
| 0    | no differences                                          |
| 1    | differences found                                       |
| 2    | error: file not found, invalid syntax, bad arguments    |

### `--keys-only`

Compare which keys exist and ignore value differences. Useful for checking a
deployment file against `.env.example`:

```
$ env-diff .env.example .env.production --keys-only

Environment Drift (keys only)

Missing in target
  STRIPE_API_KEY

1 difference found
```

### `--format json`

Structured output for automation. Lists are always arrays, never `null`.

```
$ env-diff .env.staging .env.production --format json
{
  "source": ".env.staging",
  "target": ".env.production",
  "keys_only": false,
  "drift": true,
  "summary": {
    "same": 2,
    "missing": 1,
    "extra": 1,
    "different": 1
  },
  "missing": [
    "STRIPE_API_KEY"
  ],
  "extra": [
    "OLD_FEATURE_FLAG"
  ],
  "different": [
    "LOG_LEVEL"
  ],
  "same": [
    "DATABASE_URL",
    "REDIS_URL"
  ]
}
```

### `--quiet`

Print nothing and rely on the exit code. Errors are still reported on stderr.

### CI example

Fail a pipeline when the production environment is missing a key that the
example file requires:

```yaml
# GitHub Actions
- name: Check environment keys
  run: |
    go install github.com/blackhorseya/env-diff/cmd/env-diff@latest
    env-diff .env.example .env.production --keys-only
```

The same command works in GitLab CI, Makefile targets, or a pre-deploy hook.
Flags may appear before or after the file arguments.

## Security

Environment files hold secrets, so safe output is the default rather than an
option:

- Values are never printed, in terminal or JSON output.
- Error messages carry a line number and, for duplicates, the key name, but
  never the content of the line.
- The tool reads two local files and nothing else: no network, no telemetry,
  no state on disk.

## Supported `.env` syntax

The parser is strict and predictable rather than shell compatible.

```
DATABASE_URL=postgres://localhost/app   # unquoted; surrounding whitespace trimmed
LOG_LEVEL="debug"                       # double quotes; \" and \\ are unescaped
PASSWORD='p@ss"word'                    # single quotes; taken literally
EMPTY=                                  # empty value
export FEATURE_X=true                   # optional export prefix
# full-line comment
API_HOST=api.example.com # inline comment
```

Rules worth knowing:

- Keys must match `[A-Za-z_][A-Za-z0-9_]*` and are case-sensitive.
- A `#` starts a comment only at the beginning of a line, when preceded by
  whitespace, or immediately after a closing quote, so `PASSWORD=abc#123`
  keeps the whole value.
- Comparison is exact: `A=true` and `A="true"` are the same, `A=x` and `A=x `
  are the same (trailing whitespace is trimmed), `A=X` and `A=x` differ.
- **Duplicate keys are an error** (exit 2) that names the key and both line
  numbers. Ambiguous configuration is never silently resolved.
- An empty file, or one with only comments, is a valid empty environment.
- A UTF-8 byte order mark and CRLF line endings are tolerated.
- Not supported: multi-line quoted values (error), escape sequences other
  than `\"` and `\\` (kept literally), and variable expansion (`${VAR}` is
  kept as literal text).

## Development

Requires Go 1.26+ and [Task](https://taskfile.dev).

```
task build      # compile ./bin/env-diff
task test       # go test -race ./...
task lint       # go vet + golangci-lint
task snapshot   # goreleaser snapshot build into ./dist
```

## Non-goals

`env-diff` detects drift. It does not fix it, sync files, scan for secrets,
or talk to Kubernetes, AWS, Vault, or any remote source.

## License

Apache-2.0. See [LICENSE](LICENSE).
