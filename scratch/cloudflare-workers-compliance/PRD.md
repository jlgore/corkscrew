# Cloudflare Workers compliance: bindings, least privilege, and secrets

Status: needs-triage

## Problem

The Cloudflare provider inventories Workers from the script *list* endpoint only
(`plugins/cloudflare-provider/workers_scanner.go`). That response carries no bindings, no secret
names, and no workers.dev exposure. So Corkscrew can't answer the questions that matter about a
Worker:

- What can it reach? (R2, KV, D1, Queues, Durable Objects, other Workers, Secrets Store, outbound
  email, mTLS certs, VPC services)
- Is it reachable from places that skip zone security? (workers.dev and Preview URLs skip the
  zone's WAF, Access, and rate limiting)
- Are credentials stored as secrets, or pasted into `plain_text` vars and JSON blobs?

Test coverage is thin. The provider tests cover resource shaping, paging caps, and client timeouts.
No test runs a scanner against a fake Cloudflare API, and no compliance pack has ever run against
Cloudflare data. The one embedded pack (`cfi/ccc-storage`) targets AWS, and its executor test uses
a fake query engine, so no pack's SQL runs in a test.

## Outcome

1. The Workers scan records every binding of every script as a `worker_binding` resource, with a
   relationship edge to the resource it targets. It also records each script's workers.dev and
   Preview URL exposure and its observability settings.
2. Corkscrew never persists a `plain_text` or `json` binding value. The provider computes derived
   signals (`looks_like_credential`, `credential_reason`, `value_length`) and drops the value
   before the resource leaves the plugin. It stores no hash of the value either: an unsalted hash
   of a low-entropy secret lets anyone confirm guesses.
3. Corkscrew ships three embedded official packs (`cloudflare/workers-baseline`,
   `cloudflare/workers-bindings`, `cloudflare/workers-secrets`). Every control has at least one
   PASS and one FAIL fixture.
4. A hermetic test path runs a Forge-generated fake Cloudflare API → provider scan → golden snapshot → DuckDB → real
   pack SQL, and asserts the outcome of each control for each resource. An opt-in live smoke test
   runs the same assertions against a throwaway account seeded with wrangler.

## Decisions (2026-10-22)

| Question | Decision |
|---|---|
| Binding data model | One `worker_binding` resource per binding, plus a relationship from the worker to the bound target |
| `plain_text`/`json` values | Store derived signals only. No value, no hash |
| Where the packs live | Embedded official packs under `pkg/query/compliance/packs/cloudflare/` |
| Testing | Hermetic fixtures as the gate, plus an opt-in, env-gated live smoke test |
| Fake API | Generated from Cloudflare's `openapi.forge.json` by a Forge plugin. The generated Go is committed, fixtures are validated against the spec when seeded, and we keep up with Forge's churn |

## Data model

### `worker_script` (enriched)

New attributes, from `Scripts.ScriptAndVersionSettings.Get` and `Scripts.Subdomain.Get`:

| Attribute | Source |
|---|---|
| `workers_dev_enabled`, `previews_enabled` | subdomain |
| `logpush`, `observability_enabled`, `observability_logs_enabled` | settings (settings win over list) |
| `tail_consumers` | settings, comma-joined service names |
| `placement_mode` | settings |
| `binding_count` | settings |
| `has_route`, `has_custom_domain` | derived in-scan from routes and domains already listed |

### `worker_binding` (new, service `workers`)

- ID: `<account_id>/workers/<script>/bindings/<binding_name>`. Parent: the script.
- Attributes: `script`, `binding_name`, `kind` (the SDK `type`: `r2_bucket`, `kv_namespace`, `d1`,
  `queue`, `durable_object_namespace`, `service`, `secrets_store_secret`, `secret_text`,
  `plain_text`, `json`, `send_email`, `dispatch_namespace`, `mtls_certificate`, `hyperdrive`,
  `vpc_service`, `browser`, `ai`, …), `target_id` (resolved Corkscrew ID or empty),
  `target_ref` (the raw reference from the API: bucket name, queue name, `store_id/secret_name`,
  `script_name#class_name`), `target_resolved` (`true`/`false`).
- Kind-specific attributes: `send_email` → `destination_restricted`, `jurisdiction` for R2,
  `environment`/`entrypoint` for service bindings.
- Value-bearing kinds (`plain_text`, `json`) → `looks_like_credential`, `credential_reason`
  (e.g. `prefix:aws_access_key`, `entropy`), `value_length`. For `json`, the classifier walks each
  string leaf. A hit sets `credential_reason` to `json:<path>:<reason>`. The JSON path is safe to
  store; the value isn't.
- `raw_data` is the binding with `text`, `json`, `key_jwk`, and `simple` removed.

### Relationships

`worker_script --binds--> target` for every resolved binding, with properties `binding_name` and
`kind`. Target resolution:

| Kind | Binding gives | Corkscrew ID |
|---|---|---|
| `r2_bucket` | `bucket_name` | `<account>/r2/<name>` (deterministic) |
| `kv_namespace` | `namespace_id` | direct |
| `d1` | `database_id` | direct |
| `queue` | `queue_name` | per-account name→ID index from the Queues list |
| `durable_object_namespace` | `class_name` + `script_name` | per-account index from the DO namespace list |
| `secrets_store_secret` | `store_id` + `secret_name` | per-store index from the Secrets Store list |
| `service` | `service` (script name) | worker script ID |

The indexes come from the same list calls the storage and data scanners make, through the
existing list cache. When a lookup fails, the binding is still emitted with `target_resolved=false`.
A failed lookup is the dangling-binding signal, not a scan error.

## Packs

All controls emit one row per evaluated resource with `status` PASS or FAIL. All thresholds and
allowlists are pack parameters with safe defaults.

### `cloudflare/workers-baseline`

| ID | Control | Sev |
|---|---|---|
| CF.WRK.01 | A Worker with a route or custom domain also has workers.dev enabled, which skips zone WAF, Access, and rate limiting. Param: `workers_dev_allowlist` | HIGH |
| CF.WRK.02 | A routed Worker has Preview URLs enabled | MEDIUM |
| CF.WRK.03 | A Worker has no observability logs, no Logpush, and no tail consumer | MEDIUM |
| CF.WRK.04 | `compatibility_date` is older than `max_compat_age_days` (default 365) | LOW |
| CF.WRK.05 | A route points to a script that doesn't exist in the account | LOW |

### `cloudflare/workers-bindings` (least privilege)

Workers bindings have no read-only mode: an R2, KV, or D1 binding is full read-write. So
least-privilege review looks at *which* Workers hold a binding and *how many* do.

| ID | Control | Sev |
|---|---|---|
| CF.BND.01 | A binding of a privileged kind (`dispatch_namespace`, `mtls_certificate`, `vpc_service`, `vpc_network`, `hyperdrive`, `browser`, `send_email`) isn't in `privileged_binding_allowlist` (`script:kind`) | MEDIUM |
| CF.BND.02 | A `send_email` binding has no `destination_address` and no `allowed_destination_addresses` | MEDIUM |
| CF.BND.03 | A non-production Worker binds a production resource. Production is matched by name regex `prod_name_pattern` | HIGH |
| CF.BND.04 | More than `max_binding_fan_in` Workers (default 3) bind one storage resource (R2, KV, D1, Queue), or more than `max_secret_fan_in` Workers (default 2) bind one Secrets Store secret | LOW (MEDIUM for secrets) |
| CF.BND.05 | A Worker reached only through service bindings (no route, no domain) is exposed on workers.dev | HIGH |
| CF.BND.06 | A dangling binding: the target isn't in the inventory. Evaluated only when the target's service was scanned | MEDIUM |

### `cloudflare/workers-secrets`

| ID | Control | Sev |
|---|---|---|
| CF.SEC.01 | A `plain_text` or `json` binding value looks like a credential | CRITICAL |
| CF.SEC.02 | A `plain_text` binding name looks like a secret (`secret_name_pattern`, default `(?i)(token|secret|passw|api_?key|private_?key|credential)`) | HIGH |
| CF.SEC.03 | A Secrets Store secret wasn't modified within `max_secret_age_days` (default 90) | MEDIUM |
| CF.SEC.04 | A Secrets Store secret scoped to `workers` that no Worker binds | LOW |
| CF.SEC.05 | One `secret_text` name is duplicated across at least `min_duplicate_scripts` Workers (default 3), so it's a candidate for Secrets Store. Per-script secrets have no modified date, so nobody can audit their rotation | LOW |
| CF.SEC.06 | A Secrets Store secret has a scope outside `allowed_secret_scopes` (default `workers`). Corkscrew only sees Worker bindings, so it can't tell whether a non-Workers scope is in use, and the allowlist stands in for that | MEDIUM |
| CF.SEC.07 | A Worker binds a Secrets Store secret whose `status` isn't `active` | HIGH |

## Non-goals

- Analyzing Worker source code (`Scripts.Content`) or checking whether a binding is used.
- API token and member permission audits. These belong in a separate account-IAM pack.
- Workers for Platforms user Workers inside dispatch namespaces.
- Zero Trust, WAF, and zone-settings packs.
- External pack distribution. These packs are embedded, and moving them to a pack repo later is a
  separate decision (see the `compliance-pack-distribution` direction).

## Open questions

1. **Worker script identity (decided 2026-10-22: account-scoped).** `worker_script` uses the bare script name as its ID, and
   `cloudflare_resources.id` is the primary key. Two accounts with a Worker of the same name
   collide. Recommendation: switch to `<account_id>/workers/<script>`, the same shape as R2. This
   changes the identity of existing worker rows, so a drift comparison across the change shows
   every script as removed and re-added. See issue 02.
2. **Does settings return `secret_text` bindings?** The assumption is that settings lists secret
   bindings by name without a value, which makes `Scripts.Secrets.List` redundant. The live smoke
   test (issue 10) confirms it. If not, issue 04 adds a `Secrets.List` call for each script.
3. **Request budget.** Each script adds two requests (settings and subdomain). Cloudflare's
   account limit is 1,200 requests per 5 minutes. Use a bounded worker pool (default 4) and rely
   on the existing SDK retry and backoff for 429s.

## Issues

| # | Issue | Depends on | Status |
|---|---|---|---|
| 01 | [Forge-generated fake Cloudflare API](issues/01-fake-cloudflare-api-harness.md) | — | complete |
| 02 | [Account-scoped worker script IDs](issues/02-account-scoped-worker-ids.md) | — | complete |
| 03 | [Script settings and exposure enrichment](issues/03-script-settings-enrichment.md) | 01 | complete |
| 04 | [worker_binding resources and credential classifier](issues/04-worker-binding-resources.md) | 01, 02, 03 | ready-for-agent |
| 05 | [Binding relationships and target resolution](issues/05-binding-relationships.md) | 04 | ready-for-agent |
| 06 | [Golden scan and real-SQL pack test harness](issues/06-golden-scan-pack-harness.md) | 05 | ready-for-agent |
| 07 | [Pack: cloudflare/workers-baseline](issues/07-pack-workers-baseline.md) | 06 | ready-for-agent |
| 08 | [Pack: cloudflare/workers-bindings](issues/08-pack-workers-bindings.md) | 06 | ready-for-agent |
| 09 | [Pack: cloudflare/workers-secrets](issues/09-pack-workers-secrets.md) | 06 | ready-for-agent |
| 10 | [Live smoke test and wrangler seed](issues/10-live-smoke-test.md) | 07, 08, 09 | ready-for-human |
| 11 | [Docs](issues/11-docs.md) | 07, 08, 09 | ready-for-agent |
| 12 | [Derive token scopes from the Forge spec](issues/12-token-scopes-from-forge-spec.md) | 01 | needs-triage |
