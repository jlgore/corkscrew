# ADR 0003: Manifest-driven provider runtime

Status: Accepted

Date: 2026-07-14

## Context

Provider metadata, binary discovery, process ownership, configuration validation, and capability knowledge were split across an official catalog, a release registry, two plugin managers, and command handlers. Custom providers could not participate consistently without core edits.

## Decision

- Every installed provider has one versioned JSON or YAML manifest; filename-only discovery is not supported.
- Managed installs normalize manifests to JSON and copy the executable into the provider directory.
- Official provider names are reserved. The official catalog describes supported providers but is not an execution allowlist.
- Unsigned local custom providers are allowed with a visible warning.
- Protocol v2 remains the wire contract. Manifests declare canonical capabilities used for routing before an RPC is invoked.
- One runtime owns provider resolution, initialization, process reuse, and shutdown.
- Custom providers default to the core-owned generic resource table and may opt into a validated existing specialized table.
- Managed official installations take precedence over same-named local build installations; local builds supplement official providers not present in managed storage.

## Consequences

Existing binary-only installations must be reinstalled with manifests. Official and custom providers use the same runtime seam, while provenance, installation, configuration, health, and capabilities remain distinct states.
