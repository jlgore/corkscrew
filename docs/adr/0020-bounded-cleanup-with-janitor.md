# ADR 0020: Cleanup is bounded and backed by a janitor

Status: Accepted

Hub acceptance and temporary-resource cleanup are independent outcomes. After `CompleteScan` is accepted, a run remains in `Finalizing` while cleanup retries within a bounded deadline; exhausted cleanup makes the run terminal `Failed` with reason `CleanupFailed`, while an `EnvelopeAccepted=True` condition preserves the authoritative scan result. The run finalizer is then removed so deletion cannot remain stuck indefinitely.

Every temporary cloud resource is tagged with installation identity, run UID, and expiry. A periodic janitor Job with the dedicated cleanup role removes expired resources, providing recovery when the original run or controller cannot finish synchronous cleanup.
