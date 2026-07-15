# ADR 0004: Normalized inventory and graph data access

Status: Accepted

Date: 2026-07-14

## Context

TUI, diagram, graph, compliance, and server query paths opened databases independently and embedded provider-table or extension knowledge. Graph commands also launched a DuckDB subprocess and reparsed CSV.

## Decision

- Data consumers use one session abstraction for local DuckDB and remote Quack targets.
- The session expands `~` for local targets and applies the same lifecycle and transaction contract through a hermetic remote-adapter seam in tests.
- Inventory reads use `all_cloud_resources`; relationship reads use `cloud_relationships` and compatibility views.
- Read-only query execution accepts one query statement, rejects mutation statement classes, executes inside a transaction, and always rolls it back.
- The packaged graph extension loads in-process and reports availability as a session capability.
- Rendering remains an adapter responsibility.

## Consequences

Provider-table knowledge remains inside schema lifecycle and persistence implementations. TUI, diagrams, graph commands, compliance, and server adapters consume normalized results through the same seam.
