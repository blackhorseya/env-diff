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

`env-diff` compares two or more `.env` files **by key**, not by line. Variable order,
comments and blank lines are ignored, and **values are never printed**: the
output only says whether a key is the same, missing, extra, or different.
The exit code tells CI whether the environments drifted.

The files behind every sample in this README live in [`examples/`](examples/);
run the commands from that directory to reproduce them.

## Installation

With Go:

```
go install github.com/blackhorseya/env-diff/cmd/env-diff@latest
```

Or download a binary for macOS or Linux from the
[releases page](https://github.com/blackhorseya/env-diff/releases).

## Usage

```
env-diff <source> <target> [<target>...] [flags]
```

Every key is classified relative to the **source**, the first file:

| Status    | Meaning                                            |
|-----------|----------------------------------------------------|
| same      | present in every file with the same value          |
| missing   | present in source, absent from a target            |
| extra     | absent from source, present in a target            |
| different | present in every file, values not all the same     |

Presence is judged before values: a key absent from one target is
*missing* even if the remaining copies also disagree.

### Exit codes

| Code | Meaning                                                 |
|------|---------------------------------------------------------|
| 0    | no differences                                          |
| 1    | differences found                                       |
| 2    | error: file not found, invalid syntax, bad arguments    |

### `--keys-only`

Compare which keys exist and ignore value differences:

```
$ env-diff .env.example .env.production --keys-only

Environment Drift (keys only)

Missing in target
  STRIPE_API_KEY

Extra in target
  OLD_FEATURE_FLAG

2 differences found
```

### `--allow-extra` and validating `.env.example`

Keys that only the targets have are listed but not counted as drift. With
`--keys-only` this checks that a deployment file defines every required key
without failing on keys the example does not know about:

```
$ env-diff .env.example .env.production --keys-only --allow-extra

Environment Drift (keys only)

Missing in target
  STRIPE_API_KEY

1 difference found
1 extra key allowed
```

### `--ignore`

Leave keys out of the comparison, repeatable or comma-separated. The report
says how many of them existed so a mistyped name is noticed:

```
$ env-diff .env.staging .env.production --ignore LOG_LEVEL,STRIPE_API_KEY

Environment Drift

Extra in target
  OLD_FEATURE_FLAG

1 difference found
2 keys ignored
```

### Compare more than two files

With three or more files the report becomes a matrix with one row per
drifted key and one column per file:

```
$ env-diff .env.staging .env.production .env.qa

Environment Drift

KEY               .env.staging  .env.production  .env.qa
STRIPE_API_KEY    +             -                +        missing in .env.production
OLD_FEATURE_FLAG  -             +                -        extra in .env.production
LOG_LEVEL         a             b                b        different

3 differences found
```

`+` means the key is present, `-` absent. When a row's values disagree the
present cells show a letter instead, and files sharing a letter share a
value, so `a b b` above says staging is the odd one out. No value is
printed, only which files agree.

### `--format json`

Structured output for automation. `envs` lists the files in order; `matrix`
maps every key to one entry per file: files with the same number share a
value, `null` marks the key as absent. `target` is present only when exactly
two files were compared. `ignored` lists the ignored keys that existed. With `--allow-extra`, `extra`
is still listed but `drift` ignores it. The key lists are always arrays,
never `null`.

```
$ env-diff .env.staging .env.production --format json
{
  "source": ".env.staging",
  "target": ".env.production",
  "envs": [
    ".env.staging",
    ".env.production"
  ],
  "keys_only": false,
  "allow_extra": false,
  "drift": true,
  "summary": {
    "same": 2,
    "missing": 1,
    "extra": 1,
    "different": 1,
    "ignored": 0
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
  ],
  "ignored": [],
  "matrix": {
    "DATABASE_URL": [
      0,
      0
    ],
    "LOG_LEVEL": [
      0,
      1
    ],
    "OLD_FEATURE_FLAG": [
      null,
      0
    ],
    "REDIS_URL": [
      0,
      0
    ],
    "STRIPE_API_KEY": [
      0,
      null
    ]
  }
}
```

### `--quiet`

Print nothing and rely on the exit code. Errors are still reported on stderr.

### CI

On GitHub, use the action (Linux and macOS runners):

```yaml
- uses: blackhorseya/env-diff@v0.2.0
  with:
    files: .env.example .env.production
    keys-only: "true"
    allow-extra: "true"
```

| Input | Default | Meaning |
|-------|---------|---------|
| `files` | required | whitespace-separated files; the first is the reference |
| `keys-only` | `false` | ignore value differences |
| `allow-extra` | `false` | keys only the targets have are not drift |
| `ignore` | | comma-separated keys to leave out |
| `format` | `terminal` | `terminal` or `json` |
| `version` | the action's ref | release to download, such as `v0.2.0` |
| `install` | `true` | `false` uses an `env-diff` already on `PATH` |
| `fail-on-drift` | `true` | `false` lets the step pass on drift; errors still fail it |

Outputs: `exit-code` and `drift` (`true` or `false`), for steps that want to
decide for themselves.

Anywhere else, install the binary and let the exit code gate the pipeline:

```
go install github.com/blackhorseya/env-diff/cmd/env-diff@latest
env-diff .env.example .env.production --keys-only --allow-extra
```

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
PRIVATE_KEY="-----BEGIN KEY-----
...
-----END KEY-----"                      # quoted values may span lines
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
- A quoted value may span several lines. Line endings inside it become LF,
  `#` and blank lines inside it are content, and an error in such a value
  reports the line where the assignment starts.
- Not supported: escape sequences other than `\"` and `\\` (kept literally)
  and variable expansion (`${VAR}` is kept as literal text).

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
