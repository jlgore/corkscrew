# ADR 0006: The Hub owns scanner ingestion and persistent writes

Status: Accepted

Direct Quack access gives a client general SQL authority, which is too broad for ephemeral scanners processing potentially hostile targets. Scanners therefore submit versioned, idempotent scan envelopes to an authenticated Hub ingestion boundary; the Hub validates them and performs transactional writes through the storage schema lifecycle. Quack remains the shared query and trusted-administration protocol, while the ingestion boundary writes directly to DuckDB without an intermediate queue, database, or ETL pipeline.

This preserves provider isolation and gives schema evolution, authorization, deduplication, and partial-failure handling one owner. Scanner credentials grant ingestion authority rather than arbitrary database mutation authority.
