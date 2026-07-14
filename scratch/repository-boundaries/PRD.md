# Executable repository boundaries

Status: ready-for-human

## Outcome

Make command handlers thin adapters, enforce storage-schema ownership in tests, and preserve a path for both Corkscrew-supported and user-supplied providers.

## Acceptance criteria

- Core persistent DDL has explicit owners and violations fail the Go suite.
- Command, server, TUI, and scan presentation code cannot own persistent DDL.
- Scan option normalization and execution delegation live outside `cmd/corkscrew`.
- Service groups have one reusable owner and stable display ordering.
- Application workflows do not reject providers merely because they are absent from the official catalog.
- Official-versus-custom provider loading is tracked as a separate design and implementation slice.
