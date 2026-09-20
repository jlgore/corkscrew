# ADR 0014: Hub ingestion uses retryable unary gRPC operations

Status: Accepted

Scanner ingestion uses a dedicated protobuf `HubIngest` service with unary `BeginScan`, `AppendBatch`, `CompleteScan`, and `FailScan` operations. Numbered batch calls are independently idempotent and retryable, which matches the accepted at-least-once delivery model more directly than a long-lived client-streaming RPC. The service is separate from the legacy `CorkscrewAPI`, while Quack remains a distinct SQL query endpoint.

The Hub exposes standard gRPC health reporting and authenticates ingestion through a server interceptor. External scanner formats are normally translated by Corkscrew ingestion clients rather than posted as unversioned REST payloads.
