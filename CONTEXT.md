# Corkscrew Domain Context

Corkscrew discovers resources through provider plugins, persists normalized resource and relationship data, and exposes that data to SQL and graph consumers.

## Ubiquitous language

### Provider discovery schema

A plugin-owned description returned by `GetSchemas` that explains what a provider can discover. It is metadata for discovery and must not create or alter Corkscrew's persistent database objects.

### Official provider

A provider distributed and supported by Corkscrew. Official-provider metadata and canonical storage mappings live in `pkg/providers`. The official catalog is not an allowlist for plugin execution.

### Custom provider

A user-supplied plugin resolved at runtime by name or installation metadata. Custom providers use the same application workflows as official providers and must not require a core-code edit merely to load. Persistent storage for a custom provider requires an explicit registered mapping or validated table override.

### Storage schema

The core-owned DuckDB/Quack tables and views used for persisted resources, relationships, scan metadata, correlations, and graph queries.

### Schema lifecycle

The versioned, transactional process in `internal/db` that creates and upgrades the storage schema. All database entry points call the lifecycle before persistence or graph loading begins.

### Provider resource table

The canonical `<provider>_resources` table for a provider shipped with Corkscrew. Every provider resource table exposes the common graph columns while retaining provider-specific columns.

### Canonical relationship table

`cloud_relationships`, the only writable relationship store. `<provider>_relationships` objects are filtered compatibility views over this table.

### Legacy archive

An old table preserved as `<name>_legacy_v0` during migration. Rows are copied into the canonical schema before the migration commits.

### Graph-extension correlation tables

The cross-cloud correlation tables created by the schema lifecycle (VPN, peering, direct-connect, load-balancer topology, security, identity-federation, security-role, certificate, shared-secret, and policy-similarity) are the input contract for the packaged graph extension. The correlation materialization workflow writes provider-emitted evidence into them; the extension's correlation table functions read them to answer `corkscrew graph correlate`. They are **not** dead schema — removing one breaks the corresponding correlation. See [ADR 0005](docs/adr/0005-graph-extension-correlation-tables.md).

### Resource observation

The immutable normalized state of one resource in one scan. Completed observations are the authoritative input for comparable-scan drift; provider resource tables remain latest-known, upsert-based inventory.

### Comparable scan

A completed scan with the same provider and canonical service/scope key as another scan. Automatic drift baselines and finding resolution only use comparable scans.

### Correlation evidence

A versioned provider-emitted envelope in resource attributes. Core validates and materializes this evidence into graph-extension correlation tables without parsing provider-specific raw configuration.

### Finding

The durable, producer-agnostic lifecycle record for an actionable security issue. A finding has stable identity across scans and a current state of open or resolved, with an active suppression presented as suppressed.

### Finding occurrence

Immutable evidence that a scanner observed a finding during one scan. Occurrences retain producer-specific details without changing the finding's stable identity or lifecycle.

### Finding target

An asset affected by a finding. A finding may have multiple targets, and targets remain distinct from the evidence that produced the finding.

### Asset

Any stable subject represented in the security graph, including a cloud resource, workload, container image, host, snapshot, repository, or dataset. Every asset has a deterministic identity within its source namespace.

### Cloud resource

An asset discovered from a provider control plane. Provider resource tables retain specialized inventory detail while participating in the broader asset graph.

### Snapshot scan

A scan of a temporary, read-only volume restored from a point-in-time cloud snapshot. The restored volume and source snapshot create cleanup obligations owned by the scan run.

### Filesystem scanner

A scanner that reads a mounted asset filesystem without receiving authority over the source workload, cluster, or cloud account. Its access to guest files is broader than its execution and network authority.

### Scanner

A component that observes a target and produces a scan envelope. A scanner does not mutate Corkscrew storage directly.

### Scan envelope

A versioned, idempotent logical submission containing one scan's identity, observations, finding occurrences, targets, and relationships. An envelope may arrive in ordered batches but becomes authoritative only when the Hub accepts its completion.

### Hub

The central authority that validates scan envelopes, owns persistent writes, and exposes persisted security data to query consumers.

### Trust domain

The set of cloud accounts, clusters, repositories, agents, and users allowed to share one Hub and security graph. Mutually untrusted organizations or teams belong to separate trust domains and separate Hubs.

### Control namespace

The Kubernetes namespace containing one operator installation's policies, runs, ingest sources, execution workloads, and credentials. It is the administrative boundary for that installation, even when scans observe targets elsewhere.

### Scan policy

The recurring intent to scan a class of targets under a schedule, selection, and concurrency policy. It describes desired coverage and does not represent execution history.

### Scan run

One immutable attempt to execute a scan policy or a manually requested scan. A run progresses through preparation, execution, and finalization to a terminal result, and owns its status and scan envelope independently of later runs.

### Ingest source

A long-lived declaration that imports observations from an external producer such as Trivy Operator. It is distinct from a scheduled scan policy.

### Cleanup obligation

The tracked responsibility to remove temporary cloud and Kubernetes resources created for a scan run. It remains visible independently of whether the Hub accepted the run's scan envelope.

## Ownership boundaries

- `pkg/providers` owns the catalog of providers shipped with Corkscrew.
- Provider plugins own discovery behavior and provider discovery schemas, but never open or mutate Corkscrew storage.
- `internal/db` schema lifecycle owns all persistent storage DDL and migrations.
- Normalized readers and graph stores consume the storage schema; they do not create it opportunistically.
- The correlation materialization workflow owns Corkscrew-generated rows in cross-cloud correlation tables; the packaged graph extension owns their read side.
- The Hub is the sole persistent writer. Scanners submit scan envelopes and never receive direct storage mutation authority.
- Scan policies create scan runs; scan runs own execution workloads and status. Ingest sources own continuous external integrations rather than scheduled runs.
- The operator schedules scan runs directly. It does not generate Kubernetes CronJobs, and only a scan run may own an execution workload.
- The Hub accepts scan envelopes with at-least-once delivery and idempotent batches. Only completed envelopes participate in comparison, finding resolution, or graph publication.
- Assets are the canonical security graph nodes. Findings and relationships refer to asset identity rather than directly coupling to provider-specific tables.
- One Hub serves one trust domain. Corkscrew does not present shared DuckDB storage as secure in-database multi-tenancy.
- A Hub is single-active infrastructure. Availability gaps during restart, upgrade, or recovery are expected until Corkscrew adopts and tests a replicated storage design.
- DuckDB and Quack are product constraints for the Hub, not replaceable storage adapters. The ingestion boundary exists to protect and validate the Hub, not to promise backend portability.
- Scanner credentials grant Hub ingestion authority only. Quack query and administration use separate credentials that scanners never receive.
- Hub selection belongs to an operator installation, not to individual scan policies, runs, or ingest sources. A separately administered Hub has a separately scoped operator.
- Corkscrew control resources and execution workloads remain in their installation's control namespace. Selecting a target does not move control resources or credentials into the target namespace.
- The operator holds Kubernetes reconciliation authority only. Provider credentials and cloud mutations belong to run-owned execution workloads with the minimum role needed for their step.
- Scan run specifications are immutable. Retrying an execution creates a new run rather than rewriting the identity or history of an existing run.
- Accepting a scan envelope and discharging cleanup obligations are separate outcomes. Cleanup is bounded during finalization and recoverable by an installation janitor.
- CLI and API handlers orchestrate reusable packages and should not contain storage or provider business rules.
- Application workflows under `internal/app` accept adapter requests, apply precedence and normalization, and invoke domain packages.
- CLI, TUI, and API adapters own transport syntax and rendering, not workflow policy. TUI Quick Scan invokes the same single-provider application workflow once per enabled provider.
