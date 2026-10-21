# Tie Kubernetes watch mode to the client stream

Status: complete
Phase: P3
Depends on: 01

`plugins/kubernetes-provider/informer_cache.go:78-103`:
- Informers run on `ic.stopCh`, not on the stream's `ctx`, so they outlive the client.
- `AddEventHandler` adds a handler on every call and never removes it.
- `WaitForCacheSync` has no timeout and ignores `ctx`. If RBAC forbids `list`, it blocks forever
  while holding `ic.mu.RLock`, which also deadlocks `Stop()`.
- The initial ADDED events go into a 1000-slot buffer before the reader starts
  (`kubernetes_provider.go:818`), and `handleEvent` (`:126`) silently drops anything beyond that.
- `WatchResource` (`:298`) doesn't check `informerFactory != nil`.

Change: give each stream its own stop channel, closed when `ctx` is done. Keep the handler
registration and remove the handler on exit. Start reading before the sync. Bound
`WaitForCacheSync` with a timeout. Make the send block and select on `ctx.Done()` instead of
dropping events.

Acceptance: a test with a fake clientset and 5,000 objects receives all 5,000 ADDED events. After
the stream's ctx is cancelled, the informer goroutines exit (checked with goleak).

## Comments

2026-10-21: Implemented in `plugins/kubernetes-provider/informer_cache.go`. Cited lines had moved
(issue 10 changed `SetupInformers`); the defects were all still present.

- Each stream builds its own informer factory (`SetupInformers` returns an `informerSet`) and
  `StreamInformers` starts it on a context derived from the stream's. Judgment call: a per-stream
  stop channel alone is not enough with the old shared factory, because a factory starts an
  informer at most once, so informers stopped with one client could never serve the next.
  `Stop()` cancels every running stream through `ic.stopCh`; the stream then returns
  `errInformerCacheStopped`.
- Handler registrations are kept and removed on exit. On exit the stream cancels, calls
  `factory.Shutdown()` (which waits for the informer goroutines) and joins its own goroutines.
- The reader runs while `waitForSync` waits on the handler registrations' `HasSynced` (initial
  events delivered, not just cached), bounded by `cacheSyncTimeout` (2 min) and the stream ctx.
  The timeout is returned as a stream error. No lock is held across the wait, so `Stop()` no
  longer deadlocks.
- `handleEvent` blocks on a 1000-slot buffer and selects on `ctx.Done()`. client-go queues each
  handler's notifications separately, so blocking slows only this stream.
- `WatchResource` builds its own informer per call (a shared one would end a second watch
  immediately, and the `namespace` argument was ignored) and returns an error when the cache has
  no watch client. The old `informerFactory` field is gone, so that is the nil guard.
- Tests (`informer_cache_test.go`, dynamic fake client): 5,000 pods produce 5,000 distinct ADDED
  events with the reader stalled, so the buffer overflows. A blocked `list` reactor hits the sync
  timeout. `Stop()` returns during a blocked sync and ends the stream. goleak is only in `go.sum`
  (transitively), not `go.mod`, so the leak check compares `runtime.NumGoroutine()` with the
  baseline (+2 tolerance, bounded 30 s wait). If you skip `Shutdown` and keep the stream ctx off
  the informers, the check fails.
