# Lint

Lint guards new code without a big-bang cleanup. Go code is checked only on lines changed since
`origin/main`, so existing long functions and long lines never block a PR. Rust is checked in
full. Lint failures block PRs (`.github/workflows/lint.yml`).

## Go: golangci-lint

The config is `.golangci.yml` at the repo root. It applies to the root module and to every
`plugins/*-provider` module: golangci-lint finds it by walking up from the working directory, and
each module is linted from its own directory.

Linters: golangci-lint's `standard` set (`errcheck`, `govet`, `ineffassign`, `staticcheck`,
`unused`) plus:

| Linter | Setting |
|---|---|
| `funlen` | 70 lines per function, no statement limit |
| `lll` | 100 columns, tab width 4 |
| `errcheck` | a deferred `x.Close()` is not reported (see below) |
| `contextcheck` | |
| `noctx` | |
| `bodyclose` | |

**Close errors.** errcheck matches callees by name, so it cannot tell an error-return path from a
happy path. The rule it can express: a `defer x.Close()` is cleanup that runs on the error paths,
so it is not reported. Any other `Close` must be checked. On an error-return path where the
original error matters more, drop the Close error explicitly with `_ = x.Close()`.

**Excluded paths.** Code-generator templates are not linted: `scanner_generator.go`,
`cmd/*-generator/`, `cmd/analyze-*/`, and protoc output (`*.pb.go`, plus any file with a
`Code generated ... DO NOT EDIT.` header).

### Run it locally

Use the pinned version, `v2.14.0` (the same one CI uses). Install the release binary into a
directory of your choice, for example:

```sh
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
  | sh -s -- -b "$HOME/.local/bin" v2.14.0
```

Then, from the repo root:

```sh
make lint-go                    # root + every plugin module, new code since origin/main
make lint-go LINT_BASE=main     # compare against your local main instead
make lint-go GOLANGCI_LINT=/path/to/golangci-lint
```

`make lint-go` refuses to run with any other golangci-lint version, since linter behavior changes
between releases. To lint one module by hand:

```sh
cd plugins/aws-provider
golangci-lint run --new-from-rev=origin/main ./...
```

`new-from-rev` counts uncommitted changes too, so you can lint before you commit. Run
`git fetch origin main` first if your `origin/main` is stale.

## Rust: rustfmt and Clippy

Both graph crates must be `cargo fmt` clean (`make fmt` formats them; CI runs
`cargo fmt --check`) and are checked with:

```sh
cargo clippy --all-targets -- -D warnings -W clippy::undocumented_unsafe_blocks -W clippy::unwrap_used
```

`make lint-rust` runs the format check and Clippy in `extensions/corkscrew-graph` and `extensions/corkscrew-graph-tests`.
`make lint` runs both the Go and Rust checks. CI pins the Rust toolchain (`RUST_TOOLCHAIN` in the
workflow) so a new Clippy release cannot fail unrelated PRs.

Rules:

- Every `unsafe` block has a `// SAFETY:` comment saying why it is sound.
- No `unwrap()` in extension code. Return an error, or use `expect` with the invariant as its
  message when the value is guaranteed by construction.
- `#[allow(...)]` only with a reason: `#[allow(lint, reason = "...")]`. Test and benchmark crates
  allow `clippy::unwrap_used` at crate level, because a panic is how a test reports failure.
