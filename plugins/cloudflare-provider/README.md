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
