# ADR 0015: Hub ingestion and Quack queries use separate credentials

Status: Accepted

Scanner Jobs receive a shared Hub ingestion token that is valid only for the gRPC ingestion service. They never receive the separate Quack query token used by analysts and trusted administrative clients. The initial operator stores both credentials in Kubernetes Secrets and may require workload restart for rotation; per-agent credentials, PKI, and fine-grained authorization are outside the preview scope.

An envelope's `agent_id` is provenance metadata, not proof of identity. This separation limits a compromised scanner to validated ingestion operations instead of granting general SQL authority over the security graph.
