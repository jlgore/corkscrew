# Give every external call a deadline and explicit client options

Status: ready-for-agent
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
