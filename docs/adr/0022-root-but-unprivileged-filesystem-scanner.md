# ADR 0022: Filesystem scanners are root but unprivileged

Status: Accepted

A filesystem scanner may run as UID 0 so it can read protected files in a mounted guest root filesystem, but it is never a privileged container. It drops all Linux capabilities, uses the runtime-default seccomp profile, has read-only container and target filesystems, uses no host namespaces or host mounts, receives no cloud credentials or service-account token, and may reach only DNS and the Hub ingestion endpoint. Vulnerability databases are initially baked into versioned scanner images rather than fetched at runtime.

Snapshot preparation and cleanup Jobs hold narrowly scoped cloud permissions but never mount hostile target data. Namespaces that require the Kubernetes restricted Pod Security profile must explicitly accommodate this root-but-unprivileged scanner workload.
