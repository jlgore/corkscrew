# GitHub Provider for Corkscrew

Scans a GitHub organization for inventory, security alerts, CI/CD surfaces, access metadata, and synthesized posture findings. Supports both GitHub.com and GitHub Enterprise Server (REST only on Enterprise; GraphQL transparently falls back).

## Quick start

GitHub App auth (recommended):

```bash
corkscrew github bootstrap-app --org my-org
corkscrew scan --provider github --services repos,security,findings
```

Personal access token (development / scripted runs):

```bash
export GITHUB_ORG=my-org
export GITHUB_TOKEN=github_pat_...
corkscrew scan --provider github
```

## Configuration

Config is layered, highest precedence wins:

1. Per-call `req.Config` from the corkscrew client
2. `~/.corkscrew/providers/github/app.json` (override path with `CORKSCREW_GITHUB_CONFIG`)
3. Environment variables

| Key                | Env                                | Description |
|--------------------|------------------------------------|-------------|
| `org`              | `GITHUB_ORG`                       | **Required.** Organization login. |
| `token`            | `GITHUB_TOKEN`                     | PAT or fine-grained token. |
| `token_env`        | —                                  | Alternative env var name to read the token from (default `GITHUB_TOKEN`). |
| `app_id`           | `GITHUB_APP_ID`                    | GitHub App ID. |
| `installation_id`  | `GITHUB_APP_INSTALLATION_ID`       | App installation ID. Auto-resolved from the org if omitted. |
| `private_key_path` | `GITHUB_APP_PRIVATE_KEY_PATH`      | Path to the App's PEM private key. |
| `base_url`         | —                                  | Enterprise REST base URL (e.g. `https://github.example.com/api/v3/`). When set, GraphQL is disabled and REST handles everything. |
| `concurrency`      | `GITHUB_CONCURRENCY`               | Per-repo worker pool size for parallel scans. Default `8`. |

## Auth modes

- **GitHub App** — primary path. Requires `app_id` + `private_key_path`. The provider auto-resolves `installation_id` by listing org installations if you don't pin one.
- **Token** — `token` (any scope-bearing PAT or fine-grained token). Used when App credentials are absent.

The error you get when neither is configured tells you exactly which keys to set.

## Resource coverage

| Service     | Types |
|-------------|-------|
| `orgs`      | `organization`, `member`, `outside_collaborator`, `org_webhook` |
| `repos`     | `repository`, `branch_protection`, `ruleset`, `repo_webhook`, `deploy_key` |
| `security`  | `dependabot_alert`, `secret_scanning_alert`, `code_scanning_alert` |
| `actions`   | `workflow`, `org_runner`, `repo_runner` |
| `access`    | `team`, `collaborator` |
| `findings`  | `finding` (synthetic posture issues — public repo, default branch unprotected, archived repo, issues disabled, open alerts) |

Severities on alerts and findings are read from GitHub's structured fields (`security_advisory.severity`, `rule.security_severity_level`), not heuristically grepped from raw JSON.

## How it scans

- **GraphQL-first repo discovery.** A paginated org-repos query fetches metadata + the default branch's protection rule in one round trip per 100 repos. Branch protection state is then served from an in-memory cache, eliminating per-repo REST calls during subsequent scans. Falls back to REST on Enterprise or GraphQL errors.
- **Bounded concurrency.** Per-repo work runs through a semaphore-bounded worker pool (size = `concurrency`).
- **Rate-limit-aware transport.** Both REST and GraphQL traffic share a transport that honors `Retry-After` (secondary rate limit / 429), waits for `X-RateLimit-Reset` on primary rate limit exhaustion, and applies bounded exponential backoff on 5xx and transient network errors. A throttled stderr warning fires when remaining budget drops below 50.
- **Permission failures don't break scans.** A 403/404 on any individual endpoint becomes a "skipped" entry in `errors`; everything else continues.

## Permissions

A non-exhaustive list of scopes the scanner consumes (depending on which services you enable):

- `metadata:read`, `contents:read` — repos, branch protection, rulesets
- `administration:read` — webhooks, deploy keys
- `members:read` — members, outside collaborators, teams, collaborators
- `security_events:read` — Dependabot / secret scanning / code scanning alerts
- `actions:read` — workflows, runners

Missing scopes produce per-resource "skipped" notes rather than aborting the scan.

## Build

```bash
corkscrew plugin build github
```

## Tests

```bash
go test ./...
```

The test suite covers the pure helpers (severity parsers, ID parsers, cache key, config merge, permissions resolver) and the rate-limit transport (every retry/backoff branch with a scripted RoundTripper, no real wall time).
