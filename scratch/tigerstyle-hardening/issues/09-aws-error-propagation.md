# AWS: surface dropped types, accounts, describes and discovery failures

Status: complete
Phase: P2
Depends on: 08

- `plugins/aws-provider/pkg/scanner/cloudcontrol_scanner.go:215-224`: a ListResources failure that
  isn't classed as "unsupported" (for example throttling after retries) is logged with `continue`.
  The partial refs are thrown away and the call returns nil. `isUnsupportedTypeErr` also treats
  `AccessDenied` and "does not exist" as unsupported. Report access errors separately, as
  `access_denied:<type>`, and decide whether they make the scope partial.
- `plugins/aws-provider/orgscan/orgscan.go:130-134`: when AssumeRole or the scan fails for an
  account, the account is dropped. Return an `account:<id>: …` error alongside the partial results.
- `plugins/aws-provider/aws_provider.go:399-401, 485-487`: the stream paths drop resources whose
  describe fails. Use `unenrichedResource`, as BatchScan does.
- `plugins/aws-provider/aws_provider.go:268-273`: ListResources over all services swallows the
  per-service errors.
- `cloudcontrol_scanner.go:427-437`: `sync.Once` keeps a failed discovery for the life of the
  process. Run discovery with a detached context that has an explicit timeout, cache only success
  (a mutex plus a flag), and report `discovery: …` in Errors whenever the curated fallback is used.
- `cloudcontrol_scanner.go:579-581`: when STS fails, every ref gets `AccountId=""`. Fail the scope
  instead.
- Assert `TotalResources == Σ ResourceCounts` at `aws_provider.go:355`.

Follow-up already known (not part of this issue): `GetResource` is sent the ARN as `Identifier`,
which most CFN types reject (from the #22 review).

## Comments

2026-10-21: Done in `plugins/aws-provider` only.
- Shape: `activeScanner.ScanService` keeps its signature but now returns the refs it observed
  together with an `errors.Join` of one error per failed type (CloudControlScanner) or per
  failed account (`account:<id>: …`, OrgScanner). `scanner.FlattenErrors` splits the join, and
  BatchScan emits one `shared.ServiceScanError(svc, e)` per leaf while keeping the partial refs.
- Access errors are `access_denied:<type>: …` error entries and make the scope partial.
  `isUnsupportedTypeErr` now matches only TypeNotFound, UnsupportedAction, "not currently
  supported" and "does not exist" (parent resource absent, nothing to list). Handler internal
  failures are errors too: resources of the type may exist.
- Discovery: mutex plus success flag; runs on `context.WithoutCancel(ctx)` with
  `discoveryTimeout` (30s); a failure is retried after `discoveryRetryAfter` (5m) rather than on
  every service. `DiscoveryErr()` is on `activeScanner`; BatchScan, ScanService, ListResources and
  the stream paths report `service:discovery: …` while the curated map is in use.
- STS failure fails ScanService (`resolve account id: …`) instead of emitting `AccountId=""`.
  RE-path refs with no account get the scanner's account.
- BatchScan panics if `TotalResources` disagrees with the sum of `ServiceCounts` or
  `ResourceCounts`; `ServiceCounts` now uses `+=` so a repeated service cannot trip it.
- Stream paths send unenriched resources on describe failure, send partial refs, and end with
  an error when anything failed (no Errors field on a stream). ScanService RPC fills `Errors`;
  ListResources (no Errors field) sets `metadata["complete"]="false"` and `metadata["errors"]`.
