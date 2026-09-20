# ADR 0009: Scan ingestion is chunked, idempotent, and explicitly completed

Status: Accepted

Snapshot, image, and DSPM scans can produce result sets too large for one request. A scanner therefore begins a scan envelope, appends numbered idempotent batches, and explicitly completes or fails it. Delivery is at least once: the Hub deduplicates retries by scan and batch identity, validates completion, and owns finding lifecycle updates and publication of the completed scan.

Incomplete or failed envelopes remain diagnostic execution history but never become comparable scans and never resolve previously open findings. The Hub may expire abandoned uploads after a retention window without exposing partial results as authoritative inventory or graph state.
