# AWS Provider for Corkscrew

The AWS provider is a small (~1,500 LOC) plugin that scans AWS resources via the
**AWS Cloud Control API** and persists their full configuration to DuckDB. It
optionally fans out across an entire AWS Organization, assuming a per-account
role that can be provisioned via a CloudFormation StackSet.

This replaced an older reflection-based scanner with codegen pipelines, parameter
inference, and ~25,000 lines of supporting machinery. See `ARCHITECTURE.md` for
the why and how.

## Quick start

### Single-account scan

```bash
corkscrew scan --provider aws --services s3,ec2,iam
```

That's it. No configuration, no service registration, no codegen. Cloud Control
discovers what types exist via `cloudformation.ListTypes` (~1,400 types) and
enumerates them. Resources are stored to `~/.corkscrew/db/corkscrew.duckdb`.

### Org-wide scan

Three steps:

```bash
# 1. Provision the per-account scan role across the org via CloudFormation StackSet.
#    Run from the management or delegated-admin account.
corkscrew aws-org bootstrap-deploy --ous root

# 2. Wait for the rollout to finish (a few minutes).
aws cloudformation list-stack-instances \
  --stack-set-name corkscrew-scan-role \
  --query 'Summaries[*].[Account,Status]' --output table

# 3. Scan. CORKSCREW_AWS_ORG_SCAN=true switches the plugin into fan-out mode.
CORKSCREW_AWS_ORG_SCAN=true corkscrew scan --provider aws --services s3,ec2
```

Every resource in the result carries the source `account_id`. The Organization is
enumerated via `organizations.ListAccounts`; the role is assumed in each active
account in parallel.

For the full org-scan workflow, see `ORG_SCAN.md`.

## Configuration

Single-account mode needs no configuration. Org-scan mode and a few other knobs
are env-var-driven:

| Variable | Default | Purpose |
|---|---|---|
| `CORKSCREW_AWS_ORG_SCAN` | _unset_ | Set to `true` to enable org fan-out. |
| `CORKSCREW_AWS_ORG_ROLE` | `CorkscrewScanRole` | IAM role assumed in each member account. Matches the StackSet template default. |
| `CORKSCREW_AWS_ORG_EXTERNAL_ID` | _unset_ | If set, sent as `sts:ExternalId` during AssumeRole. Must match the role's trust policy. |
| `CORKSCREW_AWS_ORG_INCLUDE_ACCOUNTS` | _unset_ | CSV of account IDs to scan. Empty = all active accounts. |
| `CORKSCREW_AWS_ORG_EXCLUDE_ACCOUNTS` | _unset_ | CSV of account IDs to skip after the include filter. |
| `CORKSCREW_AWS_ORG_MAX_CONCURRENCY` | `5` | Bounded parallelism across accounts. |

### Vault Credentials

AWS can read credentials through Corkscrew's shared secrets adapter. Static keys use `aws_access_key_id`, `aws_secret_access_key`, and optional `aws_session_token`. Add `aws_role_arn` and optional `external_id` to assume a role after loading the base credentials.

```yaml
providers:
  aws:
    config:
      auth.secret.provider: vault
      auth.secret.address: https://vault.example.com
      auth.secret.token_env: VAULT_TOKEN
      auth.secret.engine: kv-v2
      auth.secret.mount: secret
      auth.secret.path: aws/prod
      auth.secret.kind: aws_static
```

For a role-only flow, set `auth.secret.kind: aws_role` and store `aws_role_arn`; the provider assumes the role using the default AWS credential chain unless static key material is also present.

## Diagnostics

Cloud Control covers ~1,100 resource types but not every AWS resource is wired
in yet, and some types fail in expected ways (e.g. `AWS::S3::AccessGrant` if the
account hasn't set up an Access Grants Instance). The provider reports those in
`BatchScanResponse.Errors` with the prefix `unsupported_type:` so they're
greppable:

```bash
corkscrew scan --provider aws --services s3 2>&1 | grep unsupported_type:
```

The matcher is in `pkg/scanner/cloudcontrol_scanner.go` (see `isUnsupportedTypeErr`).

## What it does, end to end

1. **Initialize**: load AWS config, optionally check for a Resource Explorer
   default view in the current region.
2. **Discover types**: `cloudformation.ListTypes` once per process (cached
   across regions/accounts). Builds a `service → []AWS::Service::Resource` map.
3. **Scan**: for each requested service, call `cloudcontrol.ListResources` for
   every CFN type under that service (or query Resource Explorer if available).
   Dedupe by Id within the scan (e.g., Bucket and BucketPolicy share IDs).
   Resource Explorer returns ARNs, but `GetResource` takes the type's
   CloudFormation primary identifier (bucket name, instance id, role name, ...),
   so each type's `primaryIdentifier` is read once per scanner with
   `cloudformation.DescribeType` and the identifier is derived from the ARN
   (`pkg/scanner/identifier.go`). A type whose identifier cannot be derived
   (composite identifiers, ambiguous ARNs, a failed `DescribeType`) is listed
   with `cloudcontrol.ListResources` instead, replacing its Resource Explorer
   results. A denied `DescribeType` is reported as an `access_denied:` notice.
   See [Resource Explorer and regions](#resource-explorer-and-regions) for which
   results each region's scan keeps.
4. **Enrich**: for each resource ref, call `cloudcontrol.GetResource` and store
   the full JSON config as `raw_data`. Extract Arn and Tags into typed columns.
   A resource whose `GetResource` fails is still stored, without its
   configuration, and the failures are reported once per service and type
   (`describe failed for N of M AWS::X::Y, first: ...`). That makes the scan
   partial, so it resolves no findings (ADR 0023). A denied `GetResource` is an
   `access_denied:` notice instead and leaves the scan complete.
5. **Persist**: write to DuckDB. Global resources (S3, IAM, Route53, etc.) are
   marked with `region=""` so they aren't double-counted across regions.

## Resource Explorer and regions

Each region is scanned by its own scanner, which uses that region's default
Resource Explorer view and that region's Cloud Control endpoint. When the
account has an aggregator index, a search returns resources from every indexed
region, but a regional scanner keeps only:

- the resources in its own region, and
- every resource of a global service (IAM, S3, Route 53, CloudFront,
  Organizations, ...; `globalServices` in `pkg/scanner/cloudcontrol_scanner.go`)
  and every resource Resource Explorer reports in region `global`, with
  `region=""`.

That is the same scope as listing with `cloudcontrol.ListResources` in the
region, so a scan sees the same resources with or without Resource Explorer.
Resources of other regions are left to those regions' scans: include their
regions in the scan to cover them. Each resource is described in the region
that owns it, and a scan of several regions emits each regional resource once.

## Permissions

The role assumed in each account needs `ReadOnlyAccess` (covers Cloud Control's
list/get + the underlying per-service Describe/Get/List that GetResource calls
internally). The StackSet template provisions this — see `ORG_SCAN.md`.
With a Resource Explorer view, the scanner also calls
`cloudformation:DescribeType` (included in `ReadOnlyAccess` via
`cloudformation:Describe*`) to resolve identifiers, plus
`resource-explorer-2:GetDefaultView` and `resource-explorer-2:Search`.

If you scan a single account directly, the calling principal needs the same.
