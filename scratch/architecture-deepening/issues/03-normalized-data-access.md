# Implement normalized inventory and graph data access

Status: complete

- [x] Add a shared session contract for local DuckDB and Quack targets.
- [x] Add normalized inventory and relationship repositories.
- [x] Enforce single-statement transactional read-only query execution with rollback.
- [x] Load the packaged graph extension in-process.
- [x] Migrate TUI, diagrams, graph, compliance, and server consumers.
- [x] Remove provider-table enumeration and subprocess query paths from adapters.

## Comments

Completed on 2026-07-14. DuckDB 1.5.4 is now the shared embedded/extension build version, and architecture tests guard the normalized boundary.
