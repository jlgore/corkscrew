# AWS: surface dropped types, accounts, describes and discovery failures

Status: ready-for-agent
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
