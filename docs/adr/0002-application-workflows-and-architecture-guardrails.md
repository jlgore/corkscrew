# ADR 0002: Application workflows and executable architecture guardrails

Status: Accepted

Date: 2026-07-14

## Context

Command handlers accumulated option precedence, service expansion, persistence routing, and direct calls into implementation packages. Architectural conventions were documented but could regress without failing a build. Provider terminology also conflated providers distributed by Corkscrew with every plugin a user might install.

## Decision

- CLI, TUI, and API code are adapters. They own flags, transport syntax, and presentation.
- Reusable orchestration lives under `internal/app`. The scan workflow owns configuration precedence, provider lifecycle, multi-scope execution, and atomic persistence.
- Core persistent table, view, and index DDL in `internal/db` has an explicit allowlist of schema-lifecycle owners enforced by tests.
- Command and presentation packages are forbidden from embedding persistent DDL.
- Provider plugin discovery-schema strings are outside that DDL rule because `GetSchemas` is metadata. Provider plugins may not import a database implementation, open storage, or execute persistent DDL themselves.
- Concurrent scan results are aggregated in requested scope order so first-wins resource deduplication is deterministic.
- The application-owned `Outcome` is the sole scan rendering contract. Machine-readable stdout contains only JSON or CSV; human notices use stderr.
- The official-provider catalog describes Corkscrew-supported providers; it must not become an execution allowlist. Custom provider names pass through application workflows to runtime configuration and plugin resolution.

## Consequences

New commands should depend on an application workflow rather than database, plugin-client, or scanner implementations directly. Architecture violations fail `go test ./...`. The remaining provider redesign must define runtime plugin registration and custom-provider persistence without weakening the official catalog's product metadata role.
