# Scanning an AWS Organization

This walks through the end-to-end org-wide scan: provision a per-account role
across the org via CloudFormation StackSets, then run corkscrew with org mode
enabled.

## Architecture in one paragraph

A CloudFormation **StackSet** rooted in your management (or delegated-admin)
account deploys an IAM role to every member account. The role trusts a
principal you specify (or the management account root) and grants
`ReadOnlyAccess`. When corkscrew runs in org-scan mode, it enumerates accounts
via `organizations.ListAccounts`, assumes that role in each one with `sts`,
and runs the same Cloud Control scanner it would in single-account mode —
just N times in parallel.

## Prerequisites

1. **Organizations trusted access for StackSets.** One-time, idempotent:

   ```bash
   aws organizations enable-aws-service-access \
     --service-principal stacksets.cloudformation.amazonaws.com
   ```

   Without this, `bootstrap-deploy` fails on stack creation. The error message
   from corkscrew points at this.

2. **Run from the management account or a delegated admin.** The bootstrap
   stack needs `cloudformation:CreateStack`, `iam:CreateRole`, and
   `organizations:ListAccounts`. Member accounts only need to be reachable
   via service-managed StackSets (no per-account setup).

## Step 1 — provision the role

Two ways to do this. Pick one.

### Option A: bootstrap-deploy (corkscrew creates the stack for you)

```bash
corkscrew aws-org bootstrap-deploy --ous root
```

`--ous root` resolves the org root ID (`r-xxxx`) automatically and deploys to
every account in the org. Other useful flags:

| Flag | Purpose |
|---|---|
| `--ous ou-aaaa-1234,ou-bbbb-5678` | Deploy to specific OUs only. |
| `--role-name CorkscrewScanRole` | Override the IAM role name (default shown). Must match `CORKSCREW_AWS_ORG_ROLE` at scan time. |
| `--trusted-principal arn:aws:iam::111:role/scan-runner` | Trust a specific role/user instead of the management account root. Tighter security; recommended. |
| `--external-id $RANDOM` | Require `sts:ExternalId` on AssumeRole. |
| `--auto-deploy=false` | Don't auto-add the role to new accounts as they join the org. |
| `--stack-name corkscrew-org-bootstrap` | CFN stack name in the management account. Default shown. |

The command returns a stack ARN. CloudFormation then asynchronously creates
the StackSet and rolls it out.

### Option B: bootstrap-template (you apply the YAML yourself)

```bash
corkscrew aws-org bootstrap-template --ous root > corkscrew-stackset.yaml
# review, version-control, apply via your preferred CFN tool
aws cloudformation create-stack \
  --stack-name corkscrew-org-bootstrap \
  --template-body file://corkscrew-stackset.yaml \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameters ParameterKey=ManagementAccountId,ParameterValue=$(aws sts get-caller-identity --query Account --output text)
```

Useful for environments where corkscrew can't be granted CFN write access
directly, or where you want the template under version control.

## Step 2 — wait for the rollout

```bash
aws cloudformation list-stack-instances \
  --stack-set-name corkscrew-scan-role \
  --query 'Summaries[*].[Account,Status,StatusReason]' \
  --output table
```

Statuses you'll see:

- `OUTDATED / User initiated operation` — the StackSet is processing this
  account right now. Wait.
- `CURRENT` — role landed cleanly. Done.
- `INOPERABLE` / `FAILED` — surface the StatusReason; usually means the
  account is suspended, or a previous stack with the same name exists.

For large orgs this takes minutes. CFN will keep working in the background;
you can move on once at least one account is `CURRENT` to test.

### Verify the role is real

Pick a `CURRENT` account and try assuming directly:

```bash
aws sts assume-role \
  --role-arn arn:aws:iam::<account-id>:role/CorkscrewScanRole \
  --role-session-name verify \
  --query 'AssumedRoleUser.Arn' --output text
```

Should return the assumed-role ARN. If it returns `AccessDenied`, the
`--trusted-principal` you set during bootstrap doesn't match the calling
identity — re-run bootstrap with the right principal.

## Step 3 — run the scan

```bash
CORKSCREW_AWS_ORG_SCAN=true \
  corkscrew scan --provider aws --services s3,ec2,iam
```

The plugin enumerates accounts, assumes the role in each, and scans them in
parallel (default 5 at a time). Every resource in the result carries the
source `account_id`.

### Useful env vars

```bash
# Limit to a subset
CORKSCREW_AWS_ORG_INCLUDE_ACCOUNTS=111111111111,222222222222 ...

# Skip a few
CORKSCREW_AWS_ORG_EXCLUDE_ACCOUNTS=999999999999 ...

# Bigger orgs: more parallelism
CORKSCREW_AWS_ORG_MAX_CONCURRENCY=20 ...

# If you set --external-id during bootstrap
CORKSCREW_AWS_ORG_EXTERNAL_ID=$RANDOM ...

# If you used a non-default role name
CORKSCREW_AWS_ORG_ROLE=MyCorkscrewRole ...
```

### Verify what landed

```bash
duckdb ~/.corkscrew/db/corkscrew.duckdb \
  "SELECT account_id, COUNT(*) FROM aws_resources GROUP BY account_id"
```

One row per account, with the count of resources discovered.

## Removing the role

When you're done (or want to redeploy with different parameters):

```bash
# Tear down the per-account stack instances first
aws cloudformation delete-stack-instances \
  --stack-set-name corkscrew-scan-role \
  --deployment-targets OrganizationalUnitIds=r-xxxx \
  --regions us-east-1 \
  --no-retain-stacks

# Wait for deletion, then drop the StackSet itself
aws cloudformation delete-stack-set \
  --stack-set-name corkscrew-scan-role

# And the bootstrap stack in the management account
aws cloudformation delete-stack \
  --stack-name corkscrew-org-bootstrap
```

## Things to know

- **Per-account RE doesn't help here.** Resource Explorer indexes are
  per-account; the management-account RE only sees the management account's
  resources. Org-mode scanners don't get an RE handoff and use per-type
  `ListResources` instead. A delegated-admin RE aggregator view is the
  longer-term answer; not yet wired up.
- **AccessDenied per type is normal.** Some resource types fail with
  AccessDenied in some accounts — typically because the account has SCP
  guardrails that exceed `ReadOnlyAccess`. The plugin classifies these as
  `unsupported_type:` and the scan continues.
- **STS rate limits.** If you bump concurrency very high, STS can throttle
  AssumeRole calls. Default 5 has been comfortable in testing; raise
  cautiously.
- **Auto-deployment.** With `--auto-deploy=true` (the default), accounts added
  to your targeted OUs after the StackSet is created automatically receive
  the role. Removed accounts have their stack instance retained or removed
  per `RetainStacksOnAccountRemoval` (currently `false`).
