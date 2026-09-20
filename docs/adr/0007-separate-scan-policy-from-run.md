# ADR 0007: Separate scan policy from scan run

Status: Accepted

A recurring scan declaration and one execution have different identity, lifecycle, retry, history, and deletion semantics. Corkscrew therefore models `CorkscrewScanPolicy` as recurring desired coverage and `CorkscrewScanRun` as one immutable manual or policy-created attempt. Control-plane, snapshot, and DSPM scans are typed policy configurations rather than separate top-level policy kinds; continuous external integrations use `CorkscrewIngestSource` because they do not share the scheduled-run lifecycle.

This lets policy status summarize coverage while each run independently owns its Jobs, detailed phase, counts, errors, and cleanup state. New scan types can extend the policy and run contracts without multiplying nearly identical CRDs.
