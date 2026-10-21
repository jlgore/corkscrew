# GCP, Azure and Kubernetes: stop dropping errors

Status: complete
Phase: P2
Depends on: 08, 01

- GCP:
  - `plugins/gcp-provider/asset_inventory.go:98,166,203`: a failed page ends the loop with `break`.
  - `scanner.go:117,133`: errors are logged and skipped.
  - `gcp_provider.go:243`: an error whose text contains "permission" is turned into `nil, nil`.
- Azure:
  - `plugins/azure-provider/scanner.go:28`, `discovery.go:42`: `client, _ := armresources.New…`
    leaves a nil client that panics on first use. Return the error.
  - `azure_provider.go:232`, `:1203`: `subscriptions, _ =` drops the error.
  - `scanner.go:132,344`, `azure_provider.go:966`: a failed describe quietly downgrades the resource.
  - `management_group_client.go:200`: a failed child management group gives a partial hierarchy.
- Kubernetes: `scanner.go:271` and `informer_cache.go:55` log and drop errors.
- Data races (PLAUSIBLE): `p.resourceGraph` is written at `azure_provider.go:1205` without the lock.
  `p.initialized` is read without `p.mu` in all three providers. Use `atomic` or take the lock.

Every one of these becomes an entry in `BatchScanResponse.Errors` under the issue 08 contract.

## Comments

2026-10-21: Implemented, one commit per provider. Failures now come back with the resources that
were observed and become `shared.ServiceScanError` entries (or returned stream errors);
`ListResources`, which cannot express a partial answer, returns the error.

- GCP: Asset Inventory, per-project service scanners and `ScanAllServicesEnhanced` (the default
  BatchScan path, which dropped every error) keep partial results and report failures.
  `StreamScanService` no longer re-runs the all-projects query once per project. Organization
  discovery returns its error instead of `nil, nil`. Judgment call: falling back to project scope
  on `PermissionDenied` is the steady scope for project-level credentials, so it is not an error;
  any other failure narrows the scope and adds an error entry to every scan (`scopeScanErrors`).
  App Engine 404 stays an empty result.
- Azure: `NewAzureResourceScanner`/`NewAzureServiceDiscovery` return constructor errors and
  `Initialize` fails on them. The `subscriptions, _ =` sites drop a scope-type string, not an
  error, so nothing changed there. Describe failures are reported (summarized per service). A
  partial management group walk (non-403) is recorded at init and reported by every scan; a
  Resource Graph failure stays reported after the ARM fallback unless its scope was exactly the
  ARM subscription. The hierarchy walk has `managementGroupDepthMax` and a visited set; JSON walks
  stop at `maxJSONDepth = 32`.
- Kubernetes: failed types, namespaces, namespace listing (which fell back to `default`
  silently), selector scans and informer setup are reported; BatchScan keeps a failed cluster's
  resources.
- Races: `initialized` is `atomic.Bool` in all three; Azure `resourceGraph` is
  `atomic.Pointer`.

Tests (fakes, no network): per provider, one failing service/type keeps the other resources and
yields a non-notice entry; GCP permission error stays an error; Azure constructor failure fails
`Initialize`; Azure 100-deep JSON is depth-bounded; repeated-token fakes for GCP, Azure (ARM and
Resource Graph) and Kubernetes. Leftover: BigQuery `Datasets.List`/`Tables.List` read only the
first page, and the management group walk has no fake seam, so its visited set is untested.
