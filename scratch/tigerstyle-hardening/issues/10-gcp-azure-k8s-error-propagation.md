# GCP, Azure and Kubernetes: stop dropping errors

Status: ready-for-agent
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
