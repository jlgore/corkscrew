# Corkscrew Configuration Guide

## Overview

Corkscrew uses a single YAML configuration model for scanning and config commands.

Core commands:

```bash
corkscrew config init
corkscrew config show
corkscrew config validate
```

## Config File Location

Corkscrew resolves configuration in this order:

1. `CORKSCREW_CONFIG_FILE` (if set)
2. `corkscrew.yaml`
3. `corkscrew.yml`
4. `.corkscrew.yaml`
5. `.corkscrew.yml`
6. `~/.corkscrew/config.yaml`

You can also pass `--config <path>` to `corkscrew scan`.

## Schema

```yaml
version: "2.0"

providers:
  aws:
    enabled: true
    regions:
      - us-east-1
      - us-west-2
    services:
      - s3
      - ec2
      - iam

  azure:
    enabled: false
    regions:
      - eastus
    services:
      - storage
      - compute

  gcp:
    enabled: false
    regions:
      - us-central1-a
    services:
      - storage
      - compute

  kubernetes:
    enabled: false
    regions:
      - default
    services:
      - pods
      - services

database:
  path: ~/.corkscrew/db/corkscrew.duckdb

output:
  default_format: table
  colors: true
  progress_bars: true
  hide_empty_regions: true
  hide_empty_services: true
```

## Provider Fields

- `enabled`: enables/disables provider usage.
- `regions`: list of regions/zones/contexts. Use `all` for full region discovery where supported.
- `services`: list of provider service identifiers.

## Database Defaults

Canonical default database path:

- `~/.corkscrew/db/corkscrew.duckdb`

Behavior:

- `scan`: uses `--database` if set, otherwise `database.path` from config, otherwise canonical default.
- `query`: uses `--db` if set, otherwise canonical default.
- API server and TUI default to the same canonical path.

## Typical Workflow

```bash
# Create config once
corkscrew config init

# Review and edit providers/regions/services
corkscrew config show

# Validate structure/content
corkscrew config validate

# Run scan with config
corkscrew scan --provider aws

# Query results (same DB by default)
corkscrew query "SELECT COUNT(*) FROM aws_resources"
```

## Troubleshooting

- `no configuration file found`: run `corkscrew config init`.
- `provider X not found in configuration`: add that provider under `providers:`.
- query shows empty tables after scan: ensure scan and query point to the same DB (`--database` / `--db`).
