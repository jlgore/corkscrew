# Cloudflare Workers packs

Three embedded packs check Cloudflare Workers, their bindings, and their secrets:

| Pack | Controls | About |
|---|---|---|
| `cloudflare/workers-baseline` | CF.WRK.01–05 | Exposure, logging, and hygiene |
| `cloudflare/workers-bindings` | CF.BND.01–06 | Least privilege: what each Worker can reach, and who shares it |
| `cloudflare/workers-secrets` | CF.SEC.01–07 | Plaintext credentials, Secrets Store rotation, scope, and sprawl |

They read the inventory the Cloudflare provider records. The binding model and the guarantee
that binding values are never stored are described in the
[provider README](../../plugins/cloudflare-provider/README.md#workers-bindings-and-secrets).

## Running them

Scan the services the packs read. `workers` is required. `storage` and `data` let bindings
resolve to their targets, and let the secrets controls see Secrets Store:

```bash
corkscrew scan --provider cloudflare --services workers,storage,data
```

Then evaluate a pack against the completed scan. This opens, updates, and resolves findings:

```bash
corkscrew findings evaluate --scan <scan-id> --pack cloudflare/workers-bindings
```

Or run a pack against the latest inventory without recording findings:

```bash
corkscrew query --pack cloudflare/workers-secrets
```

Every parameter has a default. Override one with `--param name=value`. A comma makes a list
(`--param allowed_secret_scopes=workers,ai_gateway`), and a single value works where a list is
expected.

**Unknown isn't failure.** When a request failed or was denied (settings, subdomain, or a target
listing), the provider leaves the attribute out, and the control skips that resource instead of
guessing. A token without storage or data scopes still scans Workers. Its bindings just don't
resolve, and the scan reports coverage notices.

## cloudflare/workers-baseline

| ID | Severity | Fails when |
|---|---|---|
| CF.WRK.01 | HIGH | A Worker served through a route or custom domain is also on workers.dev. Requests to workers.dev skip the zone's WAF, Access policies, and rate limiting. |
| CF.WRK.02 | MEDIUM | A routed Worker has Preview URLs on, so every uploaded version is public on workers.dev. |
| CF.WRK.03 | MEDIUM | A Worker has Workers Logs, Logpush, and tail consumers all off. |
| CF.WRK.04 | LOW | `compatibility_date` is older than `max_compat_age_days`. |
| CF.WRK.05 | LOW | A route names a script its account doesn't have. Whoever deploys a Worker by that name takes over the route. |

| Parameter | Default | |
|---|---|---|
| `workers_dev_allowlist` | `[]` | Workers allowed on workers.dev alongside a route, by script name or `<account>/workers/<script>` |
| `max_compat_age_days` | `365` | |
| `as_of_date` | today | `YYYY-MM-DD` that ages are measured to |

## cloudflare/workers-bindings

Worker bindings have no read-only mode: an R2, KV, D1, or Queue binding grants full access to
the resource. So least privilege for bindings means asking which Workers hold one, and how many.

| ID | Severity | Fails when |
|---|---|---|
| CF.BND.01 | MEDIUM | A Worker holds a privileged binding (dispatch namespace, mTLS certificate, VPC, Hyperdrive, browser, send_email) that isn't on the allowlist. |
| CF.BND.02 | MEDIUM | A send_email binding has no `destination_address` and no `allowed_destination_addresses`, so it can mail any verified address. |
| CF.BND.03 | HIGH | A Worker named as non-production binds a resource named as production. A Secrets Store secret is named `<store>/<secret>`, so anything in a store called "production" counts as production. |
| CF.BND.04 | LOW, MEDIUM for secrets | More Workers bind one R2 bucket, KV namespace, D1 database, or queue than `max_binding_fan_in` allows, or more bind one Secrets Store secret than `max_secret_fan_in` allows. Each row lists the Workers. |
| CF.BND.05 | HIGH | An internal Worker (no route or domain, called through service bindings) is on workers.dev, so anyone can call it directly. |
| CF.BND.06 | MEDIUM | A binding names a resource that isn't in the inventory. Only bindings whose target listing succeeded are judged. |

| Parameter | Default | |
|---|---|---|
| `privileged_binding_kinds` | `dispatch_namespace, mtls_certificate, vpc_service, vpc_network, hyperdrive, browser, send_email` | |
| `privileged_binding_allowlist` | `[]` | `<script>:<kind>`, by script name or ID |
| `prod_name_pattern` | `(?i)(^\|[^a-z])prod(uction)?([^a-z]\|$)` | Matched against target names |
| `nonprod_name_pattern` | `(?i)(^\|[^a-z])(dev\|development\|staging\|stage\|test\|testing\|qa\|preview\|sandbox)([^a-z]\|$)` | Matched against Worker names |
| `max_binding_fan_in` | `3` | |
| `max_secret_fan_in` | `2` | |

## cloudflare/workers-secrets

| ID | Severity | Fails when |
|---|---|---|
| CF.SEC.01 | CRITICAL | A plain_text or json binding's value looks like a credential: a known key format (AWS, GitHub, Stripe, Slack, GitLab, Google, a private key, a JWT, a URL with a password) or a long random token. The finding names the pattern, or the JSON path for json bindings, never the value. |
| CF.SEC.02 | HIGH | A plain_text binding is named like a secret. |
| CF.SEC.03 | MEDIUM | A Secrets Store secret hasn't been modified in `max_secret_age_days`. |
| CF.SEC.04 | LOW | A Secrets Store secret scoped to Workers isn't bound by any Worker. Only evaluated for accounts whose Workers were scanned. |
| CF.SEC.05 | LOW | A secret_text of the same name is set in at least `min_duplicate_scripts` Workers. Per-Worker secrets have no modification date, so their rotation can't be audited, and copies multiply the work. |
| CF.SEC.06 | MEDIUM | A secret is scoped to a service outside `allowed_secret_scopes`. Corkscrew sees only Worker bindings, so the allowlist stands in for checking whether other scopes are used. |
| CF.SEC.07 | HIGH | A Worker binds a secret whose status is pending or deleted. |

| Parameter | Default | |
|---|---|---|
| `secret_name_pattern` | `(?i)(token\|secret\|passw\|api_?key\|access_?key\|private_?key\|credential)` | |
| `max_secret_age_days` | `90` | |
| `as_of_date` | today | |
| `min_duplicate_scripts` | `3` | |
| `allowed_secret_scopes` | `[workers]` | |

## How the packs are tested

Each control has at least one passing and one failing resource in a fixture account. The
Cloudflare provider scans that account from a fake API generated from Cloudflare's spec. The
result is saved as a golden scan and loaded into a real DuckDB, where the packs' SQL runs through
the same executor finding evaluation uses (`internal/packtest`). To change a pack, add the case
to `plugins/cloudflare-provider/testdata/fakeapi/baseline.json`, regenerate the golden scan
(`go test -run TestGoldenScan -update` in the provider), and add the expectation to the pack's
test in `internal/packtest`.
