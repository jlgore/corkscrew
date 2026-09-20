# ADR 0019: Scan runs share an immutable lifecycle

Status: Accepted

Every `CorkscrewScanRun` uses the same coarse lifecycle: `Pending`, optional `Preparing`, `Running`, `Finalizing`, and a terminal `Succeeded`, `Failed`, or `Cancelled` phase. A scanner Job is not successful until the Hub accepts `CompleteScan`; finalization includes run-owned temporary-resource cleanup, with detailed progress and failures represented as Kubernetes conditions.

Run specifications and terminal results are immutable. A retry creates a new run linked to the prior attempt, preserving execution history and preventing a controller from changing the meaning of an existing scan identity.
