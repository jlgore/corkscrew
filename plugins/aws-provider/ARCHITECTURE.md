# AWS Provider Architecture

## Goal

Discover every AWS resource the calling principal can see, capture the full
configuration of each one, and store it in DuckDB so it can be queried with SQL.
Optionally do this across every account in an AWS Organization in one run.

## Top-level shape

```
corkscrew CLI                                AWS APIs
─────────────                                ────────
                                             ┌───────────────────────┐
scan ─► gRPC ─► AWSProvider ─► CC scanner ──►│ cloudformation.       │
                                             │   ListTypes (once)    │
                                             ├───────────────────────┤
                                             │ resourceexplorer2.    │
                                             │   Search (if a view   │
                                             │   exists)             │
                                             ├───────────────────────┤
                                             │ cloudcontrol.         │
                                             │   ListResources       │
                                             │   GetResource         │
                                             ├───────────────────────┤
                                             │ organizations.        │
                                             │   ListAccounts        │
                                             │ sts.AssumeRole         │
                                             │   (org mode)          │
                                             └───────────────────────┘
                                                       │
                                                       ▼
                              ┌──────────────────────────────────┐
                              │ DuckDB (~/.corkscrew/db/...)     │
                              │   aws_resources                  │
                              │   change_events                  │
                              └──────────────────────────────────┘
```

## Why Cloud Control

Cloud Control API exposes a single uniform list/get/update/delete interface for
**every CloudFormation-registered resource type** — about 1,100 types covering
~250 services. From corkscrew's perspective:

- **One API for everything**: no per-service typed clients, no SDK reflection,
  no operation classification, no codegen. `ListResources(TypeName)` works the
  same for `AWS::S3::Bucket`, `AWS::EC2::Instance`, `AWS::Lambda::Function`,
  whatever.
- **Full configuration, not metadata**: `GetResource` returns the resource's
  complete JSON state — the same shape CloudFormation reads when it manages
  the resource. That's exactly what we want to persist.
- **New resource types come for free**: when AWS publishes a new CFN type,
  `cloudformation.ListTypes` picks it up next run; nothing else changes.

This replaced ~25,000 lines of reflection scanner, codegen pipeline, parameter
analyzer, and unified registry with ~1,500 LOC.

## The scanner stack

### `pkg/scanner/cloudcontrol_scanner.go`

The single-account scanner. Holds a `cloudcontrol.Client`, a
`cloudformation.Client`, and (optionally) a `ResourceExplorer`. Three core methods:

- **`SupportedServices() []string`** — returns the services it knows how to
  enumerate. Triggers type discovery on first call.
- **`ScanService(ctx, serviceName) []*ResourceRef`** — for each CFN type under
  the service, calls `cloudcontrol.ListResources` and pages through. Dedupes by
  Id within the scan, since types like `AWS::S3::Bucket` and
  `AWS::S3::BucketPolicy` share identifiers.
- **`DescribeResource(ctx, ref) *Resource`** — calls `cloudcontrol.GetResource`
  and stuffs the full property JSON onto `raw_data`. Falls back to the
  properties already stashed on the ref if GetResource fails.

#### Type discovery

`cloudformation.ListTypes(Visibility=PUBLIC, Type=RESOURCE,
ProvisioningType=FULLY_MUTABLE | IMMUTABLE)` enumerates ~1,400 types. They get
parsed (`AWS::Service::Resource`) and grouped into a `service → []typeName` map,
plus a reverse `re-format → CFN-type` map for Resource Explorer lookups.

This runs **once per process** via package-level `sync.Once`. Multi-region or
multi-account scans share the cached map (otherwise CFN's throttle gets hit
when multiple scanner instances run discovery concurrently).

If discovery fails (no network, missing IAM, empty cfn client), the scanner
falls back to a small curated map covering the common services.

#### Resource Explorer mode

When a Resource Explorer default view is available in the calling region,
`ScanService` prefers RE for discovery — one indexed search across regions per
service vs. one `ListResources` per type. The reverse map built during type
discovery translates RE's `service:resource` format to the canonical
`AWS::Service::Resource` so `GetResource` can be called for enrichment.

#### Global services

S3, IAM, Route53, CloudFront, Organizations, etc. are returned by every
region's `ListResources` call. The scanner stamps `Region=""` on those refs;
the multi-region consumer collapses on `(account, type, id)` so they show up
once.

#### Unsupported types

Some types fail in expected ways:
- The CFN handler isn't registered (`TypeNotFoundException`).
- A parent resource doesn't exist (`AWS::S3::AccessGrant` returns
  "Access Grants Instance does not exist" if the account never set one up).
- AWS-side handler bugs (`HandlerInternalFailureException`).
- IAM gaps (`AccessDenied`).

`isUnsupportedTypeErr` classifies these as gaps rather than failures — they're
recorded in the scanner's `unsupportedTypes` set and surfaced in the scan
response with an `unsupported_type:` prefix. The scan continues.

### `orgscan/orgscan.go`

The org-mode scanner. Wraps N CloudControlScanners — one per account — behind
the same `SupportedServices`/`ScanService`/`DescribeResource`/`UnsupportedTypes`
surface. Implementation:

1. `organizations.ListAccounts` to enumerate active accounts.
2. Filter by include/exclude lists.
3. For each account, `stscreds.NewAssumeRoleProvider` to build assumed creds.
4. Construct a `CloudControlScanner` with that account's `aws.Config`.
5. Run scans in a bounded worker pool (default 5 concurrent accounts).
6. Stamp `AccountId` on every returned ref.

Resource Explorer is **not** wired into per-account scanners by default. RE
indexes are per-account and the management-account RE only sees its own
resources. A delegated-admin RE aggregator view is the longer-term answer for
cross-account discovery; for now, member accounts use per-type
`ListResources`.

`DescribeResource` re-assumes into the source account (creds are cached by the
SDK's `aws.NewCredentialsCache`, so subsequent calls don't re-issue STS
requests for the same account).

### `aws_provider.go`

The gRPC server. Implements `pb.CloudProvider`. `Initialize` decides whether
to construct a single-account scanner or `OrgScanner` based on env vars
(see README). The `activeScanner` interface lets the rest of the file work
without knowing which mode is active.

Methods that backed code-gen / analysis pipelines (`GenerateServiceScanners`,
`AnalyzeDiscoveredData`, `GenerateFromAnalysis`, `ConfigureDiscovery`) are
compliant stubs — the gRPC interface is shared with other providers and
removing them would be a cross-provider refactor.

## What persists

Two DuckDB tables:

- **`aws_resources`** (in `~/.corkscrew/db/corkscrew.duckdb`) — one row per
  unique resource. `raw_data` holds the full Cloud Control JSON; `attributes`
  is a flattened string-only subset for quick filtering; tags and ARN are
  promoted to first-class columns. Globally-namespaced resources have
  `region=""`.
- **`change_events`** (in `aws_scans.db` next to where the plugin runs) — one
  row per resource per scan, used for drift/change tracking. Created/updated/
  deleted classifications come from comparing scans over time.

## What's not built

- **Multi-region scan optimization for globals.** Right now each region's
  `ListResources` call still fires for global types; the consumer just
  dedupes the result. Saves no API calls. A "primary region" flag in the
  scanner would skip global types in non-primary regions.
- **RE aggregator view.** A delegated-admin Resource Explorer can produce one
  cross-account, cross-region search result. Would replace the per-account
  per-region `ListResources` calls in org mode with a single search.
- **AWS Config aggregator path.** AWS Config covers fewer types (~150) but
  returns full configuration without needing per-account credentials. Useful
  for orgs where Config is the system of record.

These are documented as follow-ups in the commit history if you want to
revisit them.
