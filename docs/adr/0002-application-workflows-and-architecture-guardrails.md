# ADR 0002: Application workflows and executable architecture guardrails

Status: Accepted

Date: 2026-07-14

## Context

Command handlers accumulated option precedence, service expansion, persistence routing, and direct calls into implementation packages. Architectural conventions were documented but could regress without failing a build. Provider terminology also conflated providers distributed by Corkscrew with every plugin a user might install.

## Decision

- CLI, TUI, and API code are adapters. They own flags, transport syntax, and presentation.
- Reusable orchestration lives under `internal/app`. The scan workflow owns service-group expansion, list normalization, Quack environment precedence, and mapping into smart-scan options.
- Core persistent table, view, and index DDL in `internal/db` has an explicit allowlist of schema-lifecycle owners enforced by tests.
- Command and presentation packages are forbidden from embedding persistent DDL.
- Provider plugin discovery-schema strings are outside that DDL rule because `GetSchemas` is metadata. Legacy plugins that execute their own storage DDL are separate cleanup work.
- The official-provider catalog describes Corkscrew-supported providers; it must not become an execution allowlist. Custom provider names pass through application workflows to runtime configuration and plugin resolution.

## Consequences

New commands should depend on an application workflow rather than database, plugin-client, or scanner implementations directly. Architecture violations fail `go test ./...`. The remaining provider redesign must define runtime plugin registration and custom-provider persistence without weakening the official catalog's product metadata role.
