# Resource created and modified times are lost in storage

Status: needs-triage
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
