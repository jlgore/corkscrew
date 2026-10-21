# Tie Kubernetes watch mode to the client stream

Status: ready-for-agent
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
