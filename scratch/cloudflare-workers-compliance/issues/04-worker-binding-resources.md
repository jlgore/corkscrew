# worker_binding resources and credential classifier

Status: complete
Depends on: 01, 02, 03

Emit one `worker_binding` resource for each binding in the settings response, following the PRD's
data model. Add `worker_binding` to `supportedResourcesForService("workers")` and to the
describe path.

## Credential classifier

`classifyBindingValue(value string) (looksLikeCredential bool, reason string)`, a pure function:

- Known prefixes and shapes: AWS access keys (`AKIA`/`ASIA` + 16), GitHub (`ghp_`, `gho_`,
  `ghs_`, `github_pat_`), Stripe (`sk_live_`, `rk_live_`), Slack (`xox[abprs]-`), GitLab
  (`glpat-`), Google API keys (`AIza`), PEM private keys (`-----BEGIN … PRIVATE KEY-----`), JWTs
  (three base64url segments starting `eyJ`), and URLs with userinfo (`scheme://user:pass@`).
- Entropy fallback: at least 20 characters, no whitespace, Shannon entropy ≥ 4.0 bits/char.
- `json` bindings: walk the string leaves (bounded depth and leaf count). The first hit sets
  `json:<path>:<reason>`.

## Invariant: no value leaves the plugin

The value-bearing fields (`text`, `json`, `key_jwk`, `simple`) are removed from `raw_data` before
`p.resource` is called. Add a test that builds a binding with sentinel values, scans it through
the fake API, serializes every emitted resource (attributes and raw_data), and asserts that no
sentinel appears anywhere. A table test for the classifier covers each prefix, the entropy
boundary, and the false positives that must stay negative (UUIDs, hostnames, `true`, semver
strings, short hex).

Open question 2 in the PRD: if the live test shows settings omit `secret_text` bindings, merge in
`Scripts.Secrets.List` here.

## Comments

2026-10-22: Confirmed against the pinned OpenAPI spec through Prism. `GET .../scripts/{name}/settings`
(`ScriptAndVersionSettings`) returns `bindings`, and a `plain_text` binding comes back with its value
in `text` (spec example: `{"name":"MY_ENV_VAR","text":"my_data","type":"plain_text"}`). So the
scrub is required. `.../script-settings` (`ScriptSettings`) has no bindings.

2026-10-22: Implemented (`workers_bindings.go`, `credential_classifier.go`).

- Bindings are built from each binding's raw JSON (`binding.JSON.RawJSON()`), not the SDK's
  typed union. The Forge spec already has kinds this SDK version doesn't (`artifacts`, `k2`,
  `messaging`), and they come through with `kind` and `binding_name`.
- **`raw_data` uses an allowlist, not a denylist** (`bindingReferenceKeys`): only names, IDs, and
  reference fields are stored. The spec makes `text` *required* on `secret_text` (marked
  `x-sensitive`), so we can't rely on the API leaving secret values out of reads. That settles
  PRD open question 2 for this design: the provider drops values whether or not the API returns
  them, so no `Secrets.List` call is needed.
- `target_ref` comes from a per-kind table (`bindingTargetRefs`). A DO namespace or workflow
  without `script_name` belongs to the binding's own script: `api-gateway#RateLimiter`.
  `target_id` and `target_resolved` are left to issue 05.
- The classifier checks prefixes in a fixed order, then entropy. Entropy applies only to
  token-alphabet strings of at least 20 characters with upper case, lower case, and digits, at
  4.0 bits per character or more. Hex IDs and hashes stay under 4.0. JSON walks are bounded to
  depth 16 and 1,024 leaves. A JSON key that itself looks like a credential is written as
  `[redacted]` in the path.
- `TestBindingValuesNeverLeaveThePlugin` puts a sentinel in every value field (plain_text,
  secret_text, a json leaf *and* a json key, secret_key `key_base64` and `key_jwk`), then
  searches the whole protojson-encoded scan response. Mutation check: allowing `text` through
  fails the test.

Baseline fixtures now carry 19 bindings across the three scripts, covering every kind the packs
check. They include `AKIAIOSFODNN7EXAMPLE` (AWS's documented fake key, safe for push
protection), a URL with credentials inside a json binding, a high-entropy token, an unrestricted
and a restricted `send_email`, a KV namespace bound by two scripts, and `STAGING_CACHE`,
pointing at a KV namespace that doesn't exist (for the dangling-binding control).
