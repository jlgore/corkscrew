# worker_binding resources and credential classifier

Status: ready-for-agent
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
