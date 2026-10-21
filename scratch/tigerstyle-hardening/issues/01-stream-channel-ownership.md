# Fix stream channel ownership in the GCP, Azure and Kubernetes plugins

Status: ready-for-agent
Phase: P0

## Problem

`StreamScanResources` closes `resourceChan` with `defer close(...)`, and the goroutine in each
caller closes the same channel again. So every stream scan that finishes normally panics with
"close of closed channel", and the plugin process dies.

- `plugins/gcp-provider/gcp_provider.go:585` + `plugins/gcp-provider/scanner.go:128`
- `plugins/azure-provider/azure_provider.go:533` + `plugins/azure-provider/scanner.go:339`
- `plugins/kubernetes-provider/kubernetes_provider.go:777`, `:1034` + `plugins/kubernetes-provider/scanner.go:255`

Two related bugs live in the same loops:

- `kubernetes_provider.go:1047`: the `break` inside `select` only leaves the `select`. Once the
  channel is closed, the loop spins at 100% CPU.
- All of these loops select on both a closed `resourceChan` and a filled `errChan`. Go picks at
  random, so about half the time the call returns `nil` and drops the error.

## Change

- Only the producer closes the channel. Remove the callers' `defer close`.
- Replace the goroutine + `errChan` pair with `errgroup.Group`. Receive until the channel closes,
  then return `g.Wait()`.
- Pull the receive loop out into a function that `return`s, so there is no `break` inside `select`.

## Acceptance

- New test for each provider: a fake scanner emits N resources and then returns nil. StreamScan
  sends N resources and returns nil, with no panic.
- New test: a fake scanner emits resources and then returns an error. StreamScan returns that error
  in 100 out of 100 runs (`-count=100`).
- `go test -race ./...` passes in all three plugin modules.
