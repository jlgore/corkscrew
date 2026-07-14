# Cloudflare Provider for Corkscrew

Scans Cloudflare accounts for core edge inventory, DNS, Workers, storage, and the MVP data-plane services. The current provider is read-only and built on `cloudflare-go/v6`.

## Current MVP Coverage

| Service | Resource Types |
|---|---|
| `accounts` | `account` |
| `zones` | `zone` |
| `dns` | `dns_record` |
| `workers` | `worker_script`, `worker_route`, `worker_domain` |
| `storage` | `r2_bucket`, `kv_namespace`, `queue` |
| `data` | `d1_database`, `durable_object_namespace`, `durable_object`, `secret_store`, `secret_store_secret` |

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

The current tests focus on:

- auth config parsing and permission planning
- resolver chain (OAuth, API token, API key, auto-detection)
- token expiry and refresh hooks
- token validation (live HTTP mocked)
- graceful fallback logic
- normalized resource shaping
- list cache stability
