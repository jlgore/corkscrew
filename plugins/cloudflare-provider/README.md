# Cloudflare Provider for Corkscrew

Scans Cloudflare accounts for core edge inventory, DNS, Workers, storage, and the MVP data-plane services. The current provider is read-only and built on `cloudflare-go/v6`.

## Current MVP Coverage

| Service | Resource Types |
|---|---|
| `accounts` | `account` |
| `zones` | `zone` |
| `dns` | `dns_record` |
| `workers` | `worker_script`, `worker_binding`, `worker_route`, `worker_domain` |
| `storage` | `r2_bucket`, `kv_namespace`, `queue` |
| `data` | `d1_database`, `durable_object_namespace`, `durable_object`, `secret_store`, `secret_store_secret` |

Resource IDs that are only unique inside an account carry the account: an R2 bucket is
`<account_id>/r2/<bucket>` and a worker script is `<account_id>/workers/<script>`. The
`name` (and the `script_name` attribute) stays the bare script name.

> **Identity change:** worker scripts used the bare script name as their ID before this. The first
> scan after upgrading shows every worker script as removed and re-added, and findings keyed on
> the old IDs resolve.

## Workers, Bindings, and Secrets

For every script, the workers scan fetches its settings and its workers.dev subdomain, so each
script costs two requests beyond the list. Four scripts are fetched at a time, and the SDK backs
off on 429s. Cloudflare allows 1,200 requests per five minutes per account.

`worker_script` adds these attributes:

- `workers_dev_enabled`, `previews_enabled`
- `logpush`, `observability_enabled`, `observability_logs_enabled`: logs default to on with
  observability when settings have no `logs` object
- `tail_consumers`, `placement_mode`, `binding_count`
- `has_route`, `has_custom_domain`

An attribute whose request failed is **left out**, not set to `false`. The same applies to
`has_route` and `has_custom_domain` when any route or domain listing failed.

Each binding is a `worker_binding` (`<account>/workers/<script>/bindings/<name>`, parent: the
script) with:

- `kind`: the binding type, such as `kv_namespace`, `r2_bucket`, `secrets_store_secret`,
  `service`, `plain_text`, or `send_email`.
- `target_ref`: what the binding names, such as a bucket name, namespace ID, `<store>/<secret>`,
  or `<script>#<class>`.
- `target_type`, `target_service`, and `target_resolved`, for the kinds Corkscrew inventories
  (R2, KV, D1, Queues, DO namespaces, Secrets Store secrets, other Workers):
  - `"true"` comes with `target_id`.
  - `"false"` means the listing succeeded and the target isn't in it: a dangling binding.
  - No attribute means the target couldn't be checked: its listing was denied or failed, or
    it's outside what the account listing shows.
- `send_email`: `destination_restricted`.
- `plain_text` and `json`: `looks_like_credential`, `credential_reason`, `value_length`.

When the scan requests relationships, a worker script carries a `binds` edge to each resolved
target. Resolving targets uses the R2, KV, Queues, D1, DO-namespace, and Secrets Store listings.
A scan shares one listing of each between binding resolution and the storage and data scans. A
token without those scopes still scans Workers: the targets stay unknown, and each denied listing
is a coverage notice.

### Binding values are never stored

Corkscrew never persists a binding's value: not plain_text or secret_text `text`, not `json`,
and not secret_key material. `raw_data` for a binding keeps only an allowlist of reference fields
(names, IDs, bucket and queue names, and so on). Any field a new binding kind adds is dropped by
default. For `plain_text` and `json`, the provider records only derived signals:

- `looks_like_credential`
- `credential_reason`: a pattern name such as `prefix:aws_access_key` or `entropy`, or for JSON
  a path such as `json:$.primary.url:prefix:url_credentials`. Never the value.
- `value_length`

A test puts a sentinel in every value field and fails if it appears anywhere in the scan
response.

The compliance packs that use all of this are documented in
[docs/packs/cloudflare-workers.md](../../docs/packs/cloudflare-workers.md).

## Auth

Supported methods:

- `CLOUDFLARE_API_TOKEN` — recommended; use `corkscrew cloudflare login` for guidance
- `CLOUDFLARE_API_KEY` + `CLOUDFLARE_EMAIL` — legacy Global API Key
- Vault KV secrets — read Cloudflare API token, API key/email, or OAuth token material from Vault
- OAuth profiles — stored in `~/.corkscrew/providers/cloudflare/profiles/<profile>.json`

The resolver supports graceful fallback between methods. For example, if you configure `oauth` but the profile is missing or expired, the resolver can fall back to an API token or API key when available (enabled in the provider by default).

### CLI Helpers

```bash
# Guided token setup with scope planning
corkscrew cloudflare login --services zones,dns,workers

# Inspect required scopes
corkscrew cloudflare auth plan --services zones,dns,workers,storage,data
corkscrew cloudflare auth plan --bundle full_readonly

# Check current auth material and validate against the Cloudflare API
corkscrew cloudflare auth status
corkscrew cloudflare auth validate
corkscrew cloudflare auth verify --services zones,dns,workers,storage,data

# Remove a stored OAuth profile
corkscrew cloudflare logout
```

### Provider Resolution

During initialization the provider resolves credentials in this order based on `auth.method`:

1. **OAuth** — load profile from disk; if expired and `UseRefreshToken` is true, attempt refresh via the configured `TokenRefresher`; on failure optionally fall back.
2. **API Token** — read from env `CLOUDFLARE_API_TOKEN` or override config.
3. **API Key** — read from env `CLOUDFLARE_API_KEY` + `CLOUDFLARE_EMAIL`.
4. **Auto** — when no method is specified, try OAuth profile, then API token, then API key.

Validation against the Cloudflare API happens before the provider starts scanning when `Validate` is enabled (default in the plugin).

### Vault Secrets

Vault support uses Corkscrew's shared secrets adapter, which reads secret engine material into generic credential fields that any provider can consume. The first supported engine is KV v2 by default; KV v1/generic reads are available with `auth.secret.engine=kv-v1`.

```yaml
providers:
  cloudflare:
    enabled: true
    regions: [global]
    services: [zones, dns, workers, storage]
    config:
      auth.method: api_token
      auth.secret.provider: vault
      auth.secret.address: https://vault.example.com
      auth.secret.token_env: VAULT_TOKEN
      auth.secret.engine: kv-v2
      auth.secret.mount: secret
      auth.secret.path: cloudflare/prod
      auth.secret.token_field: api_token
```

For KV v2, Corkscrew reads `/v1/<mount>/data/<path>`. For KV v1/generic, it reads `/v1/<mount>/<path>`. `auth.secret.namespace` sets `X-Vault-Namespace`, and `auth.secret.version` selects a KV v2 version.

Supported Cloudflare secret fields:

- API token: `api_token` by default; override with `auth.secret.token_field`
- API key: `api_key` and `email` by default; override with `auth.secret.api_key_field` and `auth.secret.email_field`
- OAuth: token from `api_token`, `token`, or `access_token`; scopes from `scopes`
- Credential kind: optional `kind`, `method`, or `auth_method` field in Vault, or set `auth.secret.kind`/`auth.secret.method`

When a Vault secret is configured, a failed Vault read fails initialization by default. Set `auth.secret.allow_fallback=true` to fall back to environment/config credentials.

## Scope Controls

The provider honors these config keys when present:

- `account_ids`
- `zone_ids`
- `include_zones`
- `exclude_zones`

This lets you keep scans narrow without changing auth credentials.

## Build

```bash
corkscrew plugin build cloudflare
```

## Tests

```bash
go test ./...
```

Scanners run end to end against **`internal/fakecf`**, a fake Cloudflare API generated from
Cloudflare's Forge spec (`openapi.forge.json`). Tests seed it with fixtures, and every fixture
and every response is checked against the spec. A request no operation matches, an unknown query
parameter, or a write fails the test. It can inject 403, 429, 5xx, delays, and repeated pages.

- `testdata/fakeapi/baseline.json` is the fixture account: Workers that are routed, internal, on
  and off workers.dev, staging, and cron, with bindings of most kinds, plus the storage and data
  resources they reach. Its good and bad cases feed the compliance packs' tests.
- `TestGoldenScan` scans the fixture account and compares the result with
  `internal/packtest/testdata/cloudflare/golden-scan.json`. The core module loads that file into
  DuckDB to test the packs. After a deliberate change to a scanner or fixture, run
  `go test -run TestGoldenScan -update` and review the diff.
- When the provider starts calling a new endpoint, add its operation to
  `tools/forge-fakecf/operations.ts` and run `make gen-cloudflare-fake`. See
  `tools/forge-fakecf/README.md` for the pins and how to keep up with Forge.
