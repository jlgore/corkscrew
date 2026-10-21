# Fix fan-out cancellation and small CLI correctness bugs

Status: complete
Phase: P0

## Fan-out

- `plugins/github-provider/scanner.go:222`: in `parallelRepos`, the `ctx.Done` branch's `break`
  only leaves the `select`. The loop then still calls `wg.Add` and starts a goroutine without a
  semaphore slot, and that goroutine's `<-sem` can block forever. `wg.Wait()` then hangs, which
  happens whenever a large org hits the 5-minute scope deadline.
- `plugins/aws-provider/aws_provider.go:336` and `plugins/aws-provider/orgscan/orgscan.go:125`:
  `sem <- struct{}{}` doesn't select on `ctx.Done()`, so goroutines keep starting after a cancel.
- `plugins/aws-provider/aws_provider.go`, `scannerFor` / `BatchScan`:
  - `p.currentProgressTracker` is written without `p.mu` while BatchScans run concurrently, one
    per region.
  - `p.initialized` is read without the lock.
  - `scannerFor` writes to `p.regional` after `Cleanup` sets it to nil, which panics.

Fix: label the loop and `break` out of it before `wg.Add`. Acquire the semaphore with a `select`
that also watches `ctx.Done()`. Take `p.mu` for the tracker. Make `initialized` an `atomic.Bool`.
Return an error from `scannerFor` when the provider is not initialized.

## CLI

- `cmd/corkscrew/pack.go:311`: an operator-precedence bug sends `owner/name` refs, including the
  help example `jlgore/cfi-ccc`, to the `installFromGitHub` stub. `--force` is also never passed to
  the registry path. Route only explicit `github.com/` refs to the GitHub path, and pass `force`
  through.
- `cmd/corkscrew/cloudflare_auth.go:83`: the error from `requestedServices` is discarded, so a typo
  in `--bundle` silently logs in with no permission plan.
- `cmd/corkscrew/main.go:1287-1291`: `crosscloud scan --timeout/--parallel/--services` are parsed
  and ignored. Wire them through or delete them.
- `internal/app/findings/findings.go:151`: the existence-query error is discarded, so recurring
  findings get counted as opened. Lines 215 and 223-224 discard `json.Unmarshal` errors.
- `internal/app/correlation/materialize.go:275-285`: no `rows.Err()` check after the loop. Line 338
  discards the `insertRun` error.
- `internal/provider/install.go:93`: `_ = os.Rename(backup, destination)`. Put the restore error and
  the backup path in the returned error.
- tabwriter/CSV `Flush` and `Write` errors are discarded in `cmd/corkscrew/{history,findings,graph,plugin}.go`.
  Check `writer.Error()` after `Flush`.

## Acceptance

- Test: `parallelRepos` with a cancelled context and a full semaphore returns within 1s
  (`-race`).
- Test: `pack install jlgore/cfi-ccc --dry-run` goes to the registry path.
- Test: the findings evaluator returns an error when the existence query fails.

## Comments

**2026-10-21:** Implemented.
- Fan-out: `parallelRepos` breaks out of a labelled loop before `wg.Add`. AWS `BatchScan`
  (now in `describeAll`) and `orgscan.ScanService` acquire the semaphore with a `select` on
  `ctx.Done()` and report the cancellation as an error. `initialized` is an `atomic.Bool`,
  the progress tracker is set under `p.mu`, and `scannerFor` returns `errNotInitialized`
  after `Cleanup` (all call sites already return errors).
- `pack install`: `isGitHubPackRef` routes only explicit `github.com/` refs to GitHub. The
  registry path now refuses to overwrite an installed pack unless `--force` is set; the check
  lives in `cmd/corkscrew/pack.go`, since `registry.go` belongs to issue 05.
- `crosscloud scan`: `runNetworkScan` is a removed-command stub, so the three ignored flags
  were deleted rather than wired through.
- Findings existence-query test goes through the `findingExists` seam using a closed
  connection. Making only that query fail inside a full `Evaluate` wasn't practical.
