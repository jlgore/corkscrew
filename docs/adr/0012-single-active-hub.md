# ADR 0012: The initial Hub is single-active infrastructure

Status: Accepted

The initial Corkscrew Hub runs one ingestion API and Quack query endpoint against one PVC-backed DuckDB database. It is deployed as a single StatefulSet replica, uses checkpoints and external volume snapshots for recovery, and makes no high-availability claim. Scanner uploads tolerate transient downtime through idempotent retries, but restart, upgrade, node loss, and restore may interrupt service.

This is an explicit maturity boundary rather than a hidden production guarantee. Corkscrew will not add an untested second writer or application-level replication layer; a future production-grade design requires a storage system and failover model built for that purpose.
