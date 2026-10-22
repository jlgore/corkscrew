# Assertions and errors

Corkscrew separates two kinds of failure, and handles each one differently.

| | Programmer error | Operating error |
|---|---|---|
| Meaning | The code is wrong | The world is not what we hoped |
| Examples | A scope with no result, a counter past its bound, a relative path that a caller should have resolved, a cache bigger than its cap | Bad user input or config, a malformed cloud or plugin response, a network failure, a missing permission, a hostile archive |
| Handling | An assertion that crashes | A returned error, or an entry in the scan's `Errors` |

## Rules

1. **Assertions are for programmer errors only.** An assertion states something the code
   guarantees by construction. If it fails, continuing would turn a bug into bad data. For
   example, a scan could claim completeness for a region it never saw.
2. **Assertions are never disabled.** They stay on in release builds. No build tag, flag or
   environment variable turns them off.
3. **Operating errors are never assertions.** If anything outside the code can make a condition
   false, return an error. This includes a user, a config file, a pack manifest, a cloud API, a
   plugin process or a downloaded file. Inside a scan, the error goes in `Errors`, so the scan
   is reported `partial` (ADR 0023). An assertion must never fire on bad input.
4. **Pair the check with the bound.** When external data could push a value past a limit,
   check it and return an error first. Then assert the invariant that the check guarantees.
   The pack loader works this way: a dependency chain longer than `packDepthMax` is an error,
   and a load path longer than `packDepthMax` is an assertion.
5. **Write the message as the property that should hold**, for example
   `"every scope has a result"`, not as the failure.

## Go

Use `internal/assert`. Plugins can import it too, because they replace the core module with
`../..`.

```go
assert.That(len(results) == len(refs), "aws: every ref has a result")
assert.Thatf(page <= pagesMax, "paging: page %d is within pagesMax %d", page, pagesMax)
```

A failed assertion panics with an `assert.Failure`, and its message starts with
`assertion failed: `. Don't write a bare `panic(...)` for an invariant. In tests, use
`internal/assert/asserttest` to require that a violation trips the assertion:

```go
asserttest.FailsWith(t, "every scope has a result", func() {
	collectScopeResults(results, scopes)
})
```

`asserttest.Fails` fails the test if the function panics with anything other than an
`assert.Failure`. That way, an unrelated nil dereference can't pass for the assertion.

## Rust (`extensions/corkscrew-graph`)

Use `assert!`, not `debug_assert!`, so the check also runs in release builds. Compile-time
relationships between constants use `const _: () = assert!(...)`.

Every table-function callback (`bind`, `init`, `func`) runs inside `params::guard`, which
catches the unwind and returns it as a DuckDB query error. A failed assertion therefore fails
the query and never aborts the host process. The crate refuses to build with
`panic = "abort"`, because `guard` could not contain an assertion failure then. Any new
callback must go through `guard` too.
