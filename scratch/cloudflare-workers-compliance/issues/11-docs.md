# Docs

Status: complete
Depends on: 07, 08, 09

- `plugins/cloudflare-provider/README.md`: add `worker_binding` to the coverage table. Document the
  binding model and the rule that Corkscrew never stores binding values, the per-script request
  cost, and how to run the live test.
- A pack reference page that lists each control, its parameters, and their defaults.
- Make sure `corkscrew cloudflare auth plan --services workers` still lists every scope the
  new calls need.

## Comments

2026-10-22: Done.

- `plugins/cloudflare-provider/README.md`: `worker_binding` added to the coverage table. A new
  section covers the script attributes, the binding model, the three states of
  `target_resolved`, request cost, and why binding values are never stored. The Tests section now
  covers the fake API, the golden scan, and regeneration. The live test isn't documented yet,
  because issue 10 doesn't exist yet.
- `docs/packs/cloudflare-workers.md`: every control, parameter, and default, how to run the
  packs (`scan`, `findings evaluate`, `query --pack`), and how they're tested. Linked from the
  user guide's compliance section.
- **Auth plan:** the `data` bundle lacked Secrets Store, though the data scan has listed it all
  along, so a token built from the plan always got 403s there. It now has `secrets_store:read`
  and `workers:read` (for DO namespaces), with a test. The workers description notes that binding
  targets resolve with the storage and data scopes. The scope names are still Corkscrew's
  shorthand: issue 12 is the real fix.
- **CLI parameters:** `--param` turns `30` into the string `"30"`, and a single list item into a
  plain string. `TestCloudflarePacksAcceptCLIParameterForms` checks that every threshold and
  allowlist works in those forms.
