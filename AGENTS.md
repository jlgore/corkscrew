## Agent skills

### Issue tracker

Issues and PRDs are tracked as Markdown under `./scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the five default triage labels. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repository. See `docs/agents/domain.md`.

## Contributor notes

### Assertions and errors

Assertions (`internal/assert` in Go, `assert!` in the graph extension) are for programmer errors
and are never disabled. Operating errors (bad input, cloud or plugin responses, network,
permissions) are returned or recorded in a scan's `Errors`. See
`docs/contributing/assertions.md`.
