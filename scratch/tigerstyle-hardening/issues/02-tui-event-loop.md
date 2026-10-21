# Stop the TUI resize loop and cap the router history

Status: complete
Phase: P0

## Problem

- `internal/tui/app.go:155`: the `tea.WindowSizeMsg` case appends
  `func() tea.Msg { return msg }`, which sends the same message straight back to `Update`, forever.
  The router already receives `msg` through `app.router.Update(msg)`.
- `internal/tui/router.go:57`: `history` grows on every `SwitchView`, and only `GoBack` pops it.
- `internal/tui/app.go:477`: the quick scan uses `context.Background()`, so quitting doesn't cancel
  the scan goroutines or the plugin processes.
- `internal/tui/app.go:484`: progress divides by `len(quickScanProviders)`, and a duplicate
  completion message can push progress past 1.0.

## Change

- Delete the re-emit.
- Cap `history` at 32 entries and drop the oldest. Assert that `currentView` exists in `r.views`.
- Give `CorkscrewApp` a cancellable context and cancel it on quit.
- Assert `len(quickScanProviders) > 0` and `quickScanCompleted <= len(quickScanProviders)`.

## Acceptance

- Test: feed one `WindowSizeMsg` into `Update`, run the returned `tea.Cmd`s, and check that none of
  them produces another `WindowSizeMsg`.
- Test: 100 `SwitchView` calls leave `len(history) <= 32`.

## Comments

2026-10-21: Done. The resize re-emit is gone, history is capped at 32 (oldest dropped), and quit
cancels the app context that quick scans now use. An unknown view type returns an ErrorMsg and leaves
the router where it was; a missing current view panics because it is a router bug. Duplicate or stale
provider completions are ignored rather than asserted, since they are message-delivery conditions.
