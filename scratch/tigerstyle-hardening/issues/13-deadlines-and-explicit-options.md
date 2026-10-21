# Give every external call a deadline and explicit client options

Status: complete
Phase: P3

- CLI: create one `signal.NotifyContext` in `runCLI` and pass it down. Replace `context.Background()`
  in `cmd/corkscrew/pack.go:137,308,410,586,670,762,916`, `main.go:296-871` and `quack.go:80`.
  Registry downloads and remote Quack queries currently can hang forever.
- HTTP clients with no `Timeout`:
  - `internal/providers/githubauth/githubauth.go:162-175` (`http.DefaultClient`, no ctx,
    unbounded `io.ReadAll` with its error discarded, and `code` not escaped in the URL)
  - `internal/secrets/vault.go:75`
  - `internal/providers/cloudflareauth/validate.go:35`
  - `plugins/github-provider/provider.go:417,422,446`
- GitHub `transport.go:62`: a retry can wait up to 15 minutes, ×3, which is longer than the
  5-minute scope deadline. Cap the wait below the scope deadline. `transport.go:115`: drain the body
  with `io.CopyN`.
- Cloudflare `client.go:18`: set `option.WithRequestTimeout` and `WithMaxRetries` explicitly.
- AWS: set `RetryMaxAttempts` and a per-call timeout for `GetResource`
  (`cloudcontrol_scanner.go:363`) explicitly.
- Kubernetes: `rest.Config.Timeout` (`kubernetes_provider.go:158-161`).
- gRPC: replace go-plugin's `MaxCallRecvMsgSize(math.MaxInt32)` with a named limit
  (`scanexec/executor.go:152`). Cap `ReadOnly` result rows (`data/session.go:231`) and `query
  -stdin` input (`main.go:812`).
- `quack.go:73`: the minimum token is 4 characters. Require at least 32 random bytes when
  `allowOther` is set. `fatalf` calls `os.Exit` and skips the deferred cleanup, so return errors
  instead.

## Comments

2026-10-21: Plugin part done for aws, github and cloudflare (core and kubernetes are separate).
GitHub: every `http.Client` comes from `newHTTPClient` with `httpClientTimeout` = 4m, which also
covers the rate-limit transport's retries. One rate-limit wait is capped at `retryWaitMax` = 60s
(3 retries, 3m worst case, under the 5-minute scope deadline). A reset or `Retry-After` beyond
the cap fails at once with `errRateLimitWaitTooLong`; it is not a GitHub 403 response, so
`appendAPIError` records it as an error, not an access-denied notice, and the scan is partial.
Retry bodies drain at most `drainBytesMax` = 64 KiB; waits already selected on `ctx.Done()`.
Cloudflare: `option.WithRequestTimeout(60s)` and `option.WithMaxRetries(3)` (SDK default was no
timeout and 10 retries); the SDK does not retry a timed-out attempt, so a failing call ends in
about 75s. AWS: `loadAWSConfig` sets `retry.NewStandard` with 5 attempts and a 5s backoff cap
(worst backoff 16s), overriding `AWS_MAX_ATTEMPTS`/`AWS_RETRY_MODE`; `GetResource` gets a 45s
per-call deadline, and a timed-out describe keeps its ListResources properties. Tests cover a
far reset failing promptly (also through go-github into `appendAPIError`), a cancelled wait,
client timeouts, the Cloudflare timeout against a hung server and its retry count, the AWS
retryer bounds, and a GetResource that blocks until its ctx is done.

2026-10-21: Kubernetes item done (with issue 14). `initializePrimaryCluster` and `addCluster` in
`plugins/kubernetes-provider/kubernetes_provider.go` set `rest.Config.Timeout = apiRequestTimeout`
(60 s). client-go makes this the `http.Client` timeout, which also covers reading the response
body, so it would cut every informer watch short. The primary cluster therefore builds a second
dynamic client (`watchClient`) from a `rest.CopyConfig` taken before the timeout is set, and only
the informers use it. Those watches are bounded by the reflector's server-side watch timeout and
by `cacheSyncTimeout`. Additional clusters are never watched, so they get only the bounded config.
The other items in this issue are still open.

### 2026-10-21 — core part done (plugins and Kubernetes tracked separately)

- CLI: `runCLI` creates one `signal.NotifyContext` (SIGINT, SIGTERM) after the TUI branch and
  passes it through `runCommand` to every command that does I/O (pack, discover/list/describe/
  info/schemas, query and compliance, scan, scans/drift, findings, graph, plugin build, init,
  aws-org, github, cloudflare auth, serve, quack). `init` reuses it instead of its own.
  `context.AfterFunc(ctx, stop)` restores default handling after the first signal, so a second
  Ctrl-C still kills a command that ignores ctx. `serve` and `quack serve` wait on the root
  context. `graphquery.Runner` gained `RunContext`. Cloudflare auth no longer passes a nil ctx.
- HTTP: GitHub manifest conversion uses `NewRequestWithContext`, a 30 s client, a 1 MiB
  `LimitReader` with its error handled and oversize rejected, and `url.PathEscape(code)`.
  Vault and Cloudflare token validation default to 30 s clients; Vault rejects bodies over
  1 MiB instead of truncating them.
- gRPC: `internal/provider/launcher.go` passes `GRPCDialOptions` with
  `MaxPluginMessageBytes = 256 MiB` for receive and send (go-plugin's `MaxInt32` is overridden
  because call options apply in order). An interceptor explains `ResourceExhausted`.
- Caps: `data.MaxReadOnlyRows = 1,000,000`; over it `ReadOnly` returns
  `ErrRowLimitExceeded` (rejects, never truncates). `query -stdin` rejects input over 4 MiB.
- Quack: a server reachable from other hosts requires a token of at least 32 characters, or
  generates 32 random bytes (64 hex chars) when none is given; loopback keeps the 4-character
  minimum and Quack's own generated token. `fatalf`/`os.Exit` are gone from `runQuackServe`.
- Still open in this issue: `plugins/github-provider` HTTP clients and transport retry cap,
  Cloudflare `client.go` options, AWS retry/timeout, Kubernetes `rest.Config.Timeout`.

2026-10-21: All three parts (core, AWS/GitHub/Cloudflare plugins, Kubernetes) are merged into
`hardening/tigerstyle`; issue complete.
