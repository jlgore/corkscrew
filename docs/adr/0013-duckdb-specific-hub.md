# ADR 0013: The Hub is deliberately DuckDB- and Quack-specific

Status: Accepted

This operator is not being designed for a future PostgreSQL migration or large-scale multi-user backend. The Hub, schema lifecycle, graph extension, and query surface may use DuckDB and Quack capabilities directly; Corkscrew will not introduce speculative storage abstractions or constrain its SQL to a portable subset.

The scanner ingestion API remains necessary as an authorization, validation, idempotency, and lifecycle boundary, not as a promise that Hub storage can be swapped. A future system intended for materially different scale or concurrency requirements may choose a different architecture rather than preserving implementation compatibility with this one.
