# ADR 0010: Assets are the universal security graph nodes

Status: Accepted

Cloud resources are only one class of security subject; workload scanning and DSPM also produce images, hosts, snapshots, repositories, packages, and datasets. Corkscrew therefore models each stable subject as an `Asset` with deterministic identity in a source namespace. Findings target assets and relationships connect assets instead of embedding foreign keys to provider-specific inventory tables.

Provider resource tables remain specialized latest-known inventory projections and continue to expose their existing graph columns. The normalized graph evolves toward an `all_assets` node source so new scanner types can participate without adding provider-table branches to finding or traversal logic.
