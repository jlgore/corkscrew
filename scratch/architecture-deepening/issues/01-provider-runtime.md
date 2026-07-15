# Implement the manifest-driven provider runtime

Status: complete

- [x] Parse and validate JSON/YAML manifests.
- [x] Package official providers with manifests.
- [x] Install and atomically replace custom providers in managed directories.
- [x] Resolve official and custom registrations with reserved-name enforcement.
- [x] Own protocol-v2 process initialization, capability gating, reuse, and shutdown.
- [x] Add generic custom-provider persistence.

## Comments

Implementation started on 2026-07-14.
Completed on 2026-07-14. Storage routing for installed providers now comes from manifests; the core catalog only reserves official names and descriptions.
