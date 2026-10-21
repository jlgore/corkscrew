# ADR 0023: Scans that may have dropped data are partial

Status: Accepted

Findings are evaluated only against completed scans, and evaluation resolves open findings that are absent from the latest comparable scan. A scan that reports `complete` while a provider silently skipped a failed service, type, account, page, or describe would therefore resolve findings for resources that were never observed. Completeness must be an explicit claim by the provider, not the absence of a reported problem.

Every entry in `BatchScanResponse.Errors` is either a notice or an error. A notice (built with `shared.UnsupportedTypeNotice`, prefix `unsupported_type:`) is informational and leaves the scope complete. Every other entry, including unprefixed entries from providers that predate the helpers, is an error: the scope was not fully observed. Providers report each failure that may have dropped resources with `shared.ServiceScanError` and keep the resources they did observe.

The executor marks a scope with any error as failed and the scan `partial`, which is persisted but never comparable and never resolves findings, matching ADR 0009's rule for incomplete envelopes. A scan is `failed` only when no scope answered at all. A streamed scan is complete only when the provider ends the stream with `io.EOF`; any other receive error is returned.
