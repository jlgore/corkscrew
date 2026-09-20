# ADR 0017: Operator resources remain in one control namespace

Status: Accepted

`CorkscrewScanPolicy`, `CorkscrewScanRun`, and `CorkscrewIngestSource` are namespaced resources watched in one installation control namespace. Generated scanner Jobs and ingestion credentials also remain there, even when a policy selects cloud accounts, clusters, or Kubernetes namespaces outside it. A Trivy ingest controller may read reports across configured namespaces while retaining its declaration and credentials in the control namespace.

This avoids cross-namespace Secret copying and gives cleanup, quotas, and administration one boundary. Multiple installations use separate control namespaces and non-overlapping watch scopes.
