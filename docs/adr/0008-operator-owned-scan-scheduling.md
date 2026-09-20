# ADR 0008: The operator schedules scan runs directly

Status: Accepted

The Corkscrew operator computes due policy occurrences and creates deterministically named `CorkscrewScanRun` resources. It does not generate Kubernetes CronJobs; the run controller is the sole creator and owner of execution Jobs. Policy scheduling defines missed-run and `Allow`, `Forbid`, or `Replace` concurrency behavior, while leader election and deterministic run identity make reconciliation idempotent.

Standalone agent mode may reuse the schedule calculation, but it is a separate deployment adapter and never competes with an operator for the same policy. This keeps execution history, retries, cancellation, and snapshot cleanup attached to one run lifecycle.
