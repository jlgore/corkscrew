# ADR 0005: Cross-cloud correlation tables are a graph-extension contract

Status: Accepted

Date: 2026-07-15

## Context

A boundary review flagged the cross-cloud correlation tables in `internal/db`
(VPN, peering, direct-connect, load-balancer topology, security, identity
federation, security role, certificate, shared secret, and policy similarity)
as dead schema because no Go code inserts into or selects from them. Removing
them was proposed as a "prune dead DDL" cleanup.

Investigation showed the packaged graph extension reads these tables directly:
its correlation table functions `SELECT` from `cross_cloud_vpn_connections`,
`cross_cloud_network_peering`, `cross_cloud_direct_connections`,
`cross_cloud_loadbalancer_topology`, `cross_cloud_security_correlations`,
`identity_federation_relationships`, `security_role_relationships`,
`certificate_correlations`, `shared_secrets_correlation`, and
`policy_similarity_analysis` to answer `corkscrew graph correlate`. The tables
are empty until a future writer populates them, but they are the schema contract
between core and the extension. Dropping any of them breaks the corresponding
correlation.

## Decision

- The cross-cloud correlation tables are retained by the schema lifecycle even
  though no Go writer currently populates them. They are the read-side contract
  for the packaged graph extension, not dead schema.
- "No Go writer" is not sufficient evidence to remove a storage object. Schema
  removal must also account for the graph extension and any external reader.
- Five tables have no reader anywhere — in Go, in the extension, or in tests:
  `privilege_escalation_paths`, `security_risk_assessments`,
  `cross_cloud_enhanced_dns`, `compliance_mappings`, and
  `cross_cloud_visualization_metadata`. They were staged for a security and
  compliance analysis feature whose Go reader (`pkg/security`) has since been
  removed. They are left in place as harmless empty tables; a future migration
  may drop them once that feature's direction is settled.

## Consequences

Architecture reviews should treat the correlation tables as live contract, not
dead DDL. A future change that populates them belongs in a schema-lifecycle
migration plus a core writer; a future change that removes the abandoned
security/compliance tables belongs in its own migration version with the graph
extension confirmed not to depend on them.
