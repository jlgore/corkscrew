# ADR 0018: Cloud credentials stay out of the operator process

Status: Accepted

The long-lived Corkscrew operator reconciles Kubernetes resources and never receives AWS, Azure, or GCP credentials. Control-plane scanner Jobs use read-only provider credentials, while snapshot preparation and cleanup Jobs use separate narrowly scoped mutation permissions. Workload identity is preferred and static Secrets are a preview fallback.

Run-owned preparation results carry the identifiers needed for subsequent Kubernetes volume and scanner reconciliation. A run finalizer coordinates cleanup without expanding the operator's standing cloud privilege.
