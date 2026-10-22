# Account-scoped worker script IDs

Status: complete
Depends on: —

`workerScriptResource` uses `script.ID`, the script name, as the resource ID.
`cloudflare_resources.id` is the primary key, so same-named Workers in two accounts overwrite
each other. Binding IDs and service-binding edges (issues 04 and 05) need a stable,
account-unique script ID.

Proposal: `<account_id>/workers/<script>`, the same shape as R2's `<account_id>/r2/<name>`. Keep
`name` as the script name and add a `script_name` attribute. `DescribeResource` for
`worker_script` has to accept the new form.

Cost: on the first scan after the change, drift shows every worker as removed and re-added, and
findings keyed on the old ID resolve. Decide whether that's acceptable before the 1.0 storage
contract, or whether the schema lifecycle needs a migration that rewrites the old IDs.

## Comments

2026-10-22: Decided: account-scoped, `<account_id>/workers/<script>`. No migration that rewrites
old IDs. Drift across the change shows Workers as re-added, and findings keyed on the old IDs
resolve. Call this out in the release notes.

2026-10-22: Implemented on `feat/cloudflare-forge-fake`. `workerScriptID(accountID, name)` in
`workers_scanner.go` builds the ID. `name` and a new `script_name` attribute keep the bare name.
`describe` resolves the script from the `script_name` attribute, the name part of an
account-scoped ID, or a bare name, so refs saved before the change still describe. The identity
change is noted in the provider README, since there's no changelog. Tests:
`TestWorkerScriptIDsAreAccountScoped` (the same name in two accounts gives two IDs) and
`TestDescribeWorkerScriptByIDForms`.
