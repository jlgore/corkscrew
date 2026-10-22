# Resource created and modified times are lost in storage

Status: complete
Depends on: —

Found while writing CF.SEC.03 (issue 09). Providers set `pb.Resource.CreatedAt` and `ModifiedAt`
(Cloudflare does it for scripts, secrets, D1, and stores), but:

- `GraphStore.storeGenericScanResource` (`internal/db/graph_store.go`) writes `created_at` and
  `modified_at` as `discoveredAt`, the scan time, for every provider using the generic table.
- `hydrateSnapshot` (`internal/app/findings/findings.go`) rebuilds resources from observations
  without `CreatedAt` or `ModifiedAt`, so packs evaluated through findings never see them anyway.

So any pack that reads `created_at` or `modified_at` (age, rotation, staleness) silently gets the
scan time. The Cloudflare secrets pack works around it with `created_on` and `modified_on`
attributes.

Check whether the provider-specific writers (Azure, GCP) and resource observations keep the
times. Write them where they exist, carry them through hydration, and add a packtest that reads
the columns.

## Comments

2026-10-22: Fixed on `fix/resource-timestamps`.

| Path | Before | After |
|---|---|---|
| generic writer (aws, cloudflare, github, kubernetes) | scan time | the resource's own times, or NULL |
| GCP writer | scan time | the resource's own times, or NULL (`scanned_at` keeps the scan time) |
| Azure writer | ARM `createdTime`/`changedTime`, else NULL | the same, falling back to the resource's own times |
| custom provider writer | already correct | shares the new `resourceTimes` helper |
| `resource_observations` | no time columns | schema v7 adds `created_at` and `modified_at` |
| `Session.Observations` / `hydrateSnapshot` | dropped | carried into the evaluation snapshot |

Semantics: `created_at` and `modified_at` mean *the resource's own times*, and are NULL when
the provider didn't report them. Nothing in Go or pack SQL read the old scan-time values (checked
with grep), and `scanned_at` still holds the scan time. Observations recorded before v7 keep
NULL.

Tests: `TestResourceTimesPersist` and `TestResourceTimesSurviveSnapshotHydration` (packtest, through
`findings.Evaluate` with a test pack that reads `modified_at`). Both failed before the fix.
`TestStoreScanResourcesWritesResourceTimes` covers the generic, GCP, and Azure writers; a mutation
that put GCP back to the scan time fails it. `TestEnsureSchemaV7AddsObservationTimes` upgrades a v6
database.

The Cloudflare secrets pack still reads the `modified_on` attribute, which works with or without
this fix. Its comment no longer claims the column holds the scan time.

Also fixed: `TestLoadServiceConfig` read the developer's `~/.corkscrew/config.yaml`, which failed
once a current-format config existed there. It now points `HOME` at a temp directory.
