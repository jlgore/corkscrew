# Corkscrew User Guide

## Table of Contents

1. [Getting Started](#getting-started)
   - [Introduction](#introduction)
   - [Prerequisites](#prerequisites)
2. [Installation & Setup](#installation--setup)
   - [Quick Start](#quick-start)
   - [Detailed Installation](#detailed-installation)
   - [Configuration Basics](#configuration-basics)
3. [Configuration Guide](#configuration-guide)
   - [Configuration File Structure](#configuration-file-structure)
   - [Provider Configuration](#provider-configuration)
   - [Advanced Settings](#advanced-settings)
4. [Cloud Provider Guides](#cloud-provider-guides)
   - [AWS](#aws)
   - [Azure](#azure)
   - [GCP](#gcp)
   - [Kubernetes](#kubernetes)
5. [Scanning Operations](#scanning-operations)
   - [Basic Scanning](#basic-scanning)
   - [Advanced Scanning](#advanced-scanning)
   - [Cross-Cloud Scanning](#cross-cloud-scanning)
6. [Querying and Analysis](#querying-and-analysis)
   - [SQL Query Basics](#sql-query-basics)
   - [Advanced Queries](#advanced-queries)
   - [Compliance Queries](#compliance-queries)
7. [Practical Workflows](#practical-workflows)
   - [Security Auditing](#security-auditing)
   - [Cost Optimization](#cost-optimization)
   - [Network Analysis](#network-analysis)
8. [Troubleshooting](#troubleshooting)
9. [Command Reference](#command-reference)

---

## Getting Started

### Introduction

**Corkscrew** is a powerful, modular multi-cloud configuration scanner that helps you discover, analyze, and manage cloud resources across AWS, Azure, GCP, and Kubernetes. Built on a plugin architecture with SQL-based analysis capabilities, Corkscrew provides:

- **Unified Multi-Cloud Discovery**: Scan resources across all major cloud providers
- **SQL-Based Analysis**: Query your cloud infrastructure using familiar SQL syntax
- **Relationship Mapping**: Automatically discover dependencies between resources
- **Compliance Checking**: Built-in and custom compliance rules
- **Cross-Cloud Correlation**: Find relationships across different cloud providers
- **Performance Optimized**: Concurrent scanning with intelligent caching

### Prerequisites

Before installing Corkscrew, ensure you have:

#### System Requirements
- **Operating System**: Linux, macOS, or Windows
- **Go**: Version 1.21 or higher
- **Git**: For cloning the repository
- **Make**: For building the project
- **Memory**: Minimum 4GB RAM (8GB recommended for large-scale scans)
- **Disk Space**: At least 2GB free space

#### Cloud Provider Access
You'll need appropriate credentials and permissions for each cloud provider you want to scan:

**AWS Requirements:**
- AWS CLI configured with credentials
- IAM permissions for read access to services you want to scan
- Recommended: `ReadOnlyAccess` policy or custom policy with specific service read permissions

**Azure Requirements:**
- Azure CLI installed and authenticated
- Reader role on subscriptions/resource groups you want to scan
- For enterprise features: Global Administrator or Application Administrator role

**GCP Requirements:**
- `gcloud` CLI installed and authenticated
- Viewer role on projects you want to scan
- For organization-wide scanning: Organization Viewer role

**Kubernetes Requirements:**
- `kubectl` configured with cluster access
- RBAC permissions to list/get resources in target namespaces

---

## Installation & Setup

### Quick Start

The fastest way to get started with Corkscrew:

```bash
# Clone the repository
git clone https://github.com/your-org/corkscrew.git
cd corkscrew

# Create a default configuration file
./corkscrew config init

# Initialize Corkscrew (downloads dependencies and builds plugins)
./corkscrew init

# Add to PATH
export PATH="$HOME/.corkscrew/bin:$PATH"

# Run your first scan
corkscrew scan --provider aws
```

### Detailed Installation

#### Step 1: Clone and Navigate to Repository

```bash
git clone https://github.com/your-org/corkscrew.git
cd corkscrew
```

#### Step 2: Create Configuration File

Create a `corkscrew.yaml` file in the project root. Here's a comprehensive example:

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
      - lambda
      - iam
      - rds
      - dynamodb
  
  azure:
    enabled: true
    regions:
      - eastus
      - westus2
    services:
      - storage
      - compute
      - keyvault
      - sql
  
  gcp:
    enabled: false
    regions:
      - us-central1-a
      - us-west1-a
    services:
      - storage
      - compute
      - bigquery
  
  kubernetes:
    enabled: false
    regions:
      - default
    services:
      - pods
      - services

dependencies:
  protoc:
    version: "25.3"
    auto_download: true
  duckdb:
    version: "1.3.0"
    auto_download: true

database:
  path: "~/.corkscrew/db/corkscrew.duckdb"
  auto_create: true

query:
  timeout: "5m"
  streaming_threshold: 10000
  max_memory: "4GB"

logging:
  level: "info"
  format: "json"

output:
  default_format: "table"
  colors: true
  progress_bars: true
```

#### Step 3: Initialize Corkscrew

Run the initialization command:

```bash
./corkscrew init
```

This command will:
1. Create the `~/.corkscrew` directory structure
2. Download required dependencies (protoc and DuckDB)
3. Generate scanner code for enabled providers
4. Build provider plugins
5. Set up the database

Expected output:
```
🚀 Initializing Corkscrew v2.0.0...

📁 Creating directory structure...
  ✓ Created ~/.corkscrew directories

📦 Downloading dependencies...
  ✓ protoc v25.3 already installed
  ✓ duckdb v1.30.1 already installed

🔍 Reading configuration from ./corkscrew.yaml...
  ✓ Configuration file found and parsed
  ✓ AWS provider: enabled (6 services)
  ✓ AZURE provider: enabled (4 services)
  ✗ GCP provider: disabled
  ✗ KUBERNETES provider: disabled

⚙️  Generating scanner code for enabled providers...
  ⚙️  Generating aws-provider code... ✓ (6 services)
  ⚙️  Generating azure-provider code... ✓ (4 services)

🔨 Building enabled plugins...
  🔨 Building aws-provider... ✓
  🔨 Building azure-provider... ✓

🎉 Corkscrew initialized successfully!

Add to your PATH: export PATH="/home/user/.corkscrew/bin:$PATH"
Or run directly: /home/user/.corkscrew/bin/corkscrew scan --provider aws --services s3
```

#### Step 4: Configure Your Shell

Add Corkscrew to your PATH:

```bash
# For bash
echo 'export PATH="$HOME/.corkscrew/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc

# For zsh
echo 'export PATH="$HOME/.corkscrew/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

#### Step 5: Verify Installation

```bash
# Check version
corkscrew version

# List available commands
corkscrew --help

# Verify provider plugins
ls ~/.corkscrew/plugins/
```

### Configuration Basics

After initialization, you can manage your configuration:

```bash
# Initialize a new configuration file
corkscrew config init

# Validate your configuration
corkscrew config validate

# Show current configuration
corkscrew config show
```

---

## Configuration Guide

### Configuration File Structure

Corkscrew uses a single config schema. The most important keys are `providers`, `database`, and `output`.

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
      - lambda
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
  path: "~/.corkscrew/db/corkscrew.duckdb"

output:
  default_format: "table"
  colors: true
  progress_bars: true
  hide_empty_regions: true
  hide_empty_services: true
```

### Provider Configuration

Each provider block follows the same pattern:

- `enabled`: `true`/`false`
- `regions`: list of regions, zones, or contexts
- `services`: list of services to scan

Notes:

- Use `regions: [all]` for full region discovery where supported.
- Kubernetes can use context names in `regions`.

### Config Commands

```bash
# Create default config (fails if one already exists)
corkscrew config init

# Print current config and resolved summary
corkscrew config show

# Validate provider names and basic list integrity
corkscrew config validate
```

### Config Resolution

Configuration path resolution order:

1. `CORKSCREW_CONFIG_FILE`
2. `corkscrew.yaml`
3. `corkscrew.yml`
4. `.corkscrew.yaml`
5. `.corkscrew.yml`
6. `~/.corkscrew/config.yaml`

For scan commands, you can override directly with `--config`:

```bash
corkscrew scan --provider aws --config ./corkscrew.yaml
```

### Database Path Behavior

Canonical default database path:

- `~/.corkscrew/db/corkscrew.duckdb`

Behavior by command:

- `scan`: `--database` overrides, otherwise `database.path` from config, otherwise canonical default.
- `query`: `--db` overrides, otherwise canonical default.
- API server and TUI use the same canonical default.

Examples:

```bash
# Scan into custom DB
corkscrew scan --provider aws --database /tmp/cs.duckdb

# Query the same DB
corkscrew query --db /tmp/cs.duckdb "SELECT COUNT(*) FROM aws_resources"
```

### Important Compatibility Note

Old keys like `default_region`, `discovery_mode`, and nested `services.include/exclude` are legacy examples and are not the active config model for current scan/config commands.

For the authoritative schema, see `docs/CONFIGURATION_GUIDE.md`.

### Security Configuration

```yaml
# optional examples
security:
  credentials:
    aws:
      use_instance_profile: true
      profile: "production"
    azure:
      use_managed_identity: true
    gcp:
      use_workload_identity: true
  
  # Encryption
  database:
    encrypt_at_rest: true
    encryption_key: "path/to/key"
  
  # Audit
  audit:
    enabled: true
    log_api_calls: true
    log_queries: true
```

---

## Cloud Provider Guides

### AWS

#### Authentication Setup

Corkscrew uses the standard AWS SDK credential chain:

1. **Environment Variables**
   ```bash
   export AWS_ACCESS_KEY_ID="your-access-key"
   export AWS_SECRET_ACCESS_KEY="your-secret-key"
   export AWS_SESSION_TOKEN="optional-session-token"
   export AWS_REGION="us-east-1"
   ```

2. **AWS Credentials File**
   ```ini
   # ~/.aws/credentials
   [default]
   aws_access_key_id = your-access-key
   aws_secret_access_key = your-secret-key
   
   [production]
   aws_access_key_id = prod-access-key
   aws_secret_access_key = prod-secret-key
   ```

3. **IAM Instance Profile** (EC2)
   - Automatically used when running on EC2
   - No configuration needed

4. **IAM Roles** (Cross-Account)
   ```yaml
   # In corkscrew.yaml
   providers:
     aws:
       assume_role:
         role_arn: "arn:aws:iam::123456789012:role/CorkscrewScanner"
   ```

#### Required Permissions

Minimum IAM policy for scanning:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:Describe*",
        "s3:ListAllMyBuckets",
        "s3:GetBucket*",
        "s3:ListBucket",
        "lambda:List*",
        "lambda:Get*",
        "iam:List*",
        "iam:Get*",
        "rds:Describe*",
        "dynamodb:List*",
        "dynamodb:Describe*",
        "cloudformation:Describe*",
        "cloudformation:List*",
        "tag:GetResources",
        "tag:GetTagKeys",
        "tag:GetTagValues"
      ],
      "Resource": "*"
    }
  ]
}
```

#### Scanning Examples

1. **Basic Scan**
   ```bash
   # Scan default services in default region
   corkscrew scan --provider aws
   
   # Scan specific services
   corkscrew scan --provider aws --services s3,ec2,lambda
   
   # Scan specific region
   corkscrew scan --provider aws --region us-west-2
   ```

2. **Multi-Region Scan**
   ```bash
   # Scan multiple regions
   corkscrew scan --provider aws --region us-east-1,us-west-2,eu-west-1
   
   # Scan all regions
   corkscrew scan --provider aws --region all
   ```

3. **Service Groups**
   ```bash
   # Scan common services (s3, ec2, lambda, rds, iam)
   corkscrew scan --provider aws --services common
   
   # Scan compute services
   corkscrew scan --provider aws --services compute
   
   # Combine service groups
   corkscrew scan --provider aws --services compute,storage,database
   ```

4. **Advanced Options**
   ```bash
   # High-performance scan
   corkscrew scan --provider aws --region all --concurrency 10
   
   # Save results
   corkscrew scan --provider aws --save
   
   # JSON output
   corkscrew scan --provider aws --output json
   
   # Include empty services
   corkscrew scan --provider aws --show-empty
   ```

#### Service Discovery

AWS provider supports 200+ services automatically:

```bash
# Discover available services
corkscrew discover --provider aws

# Output includes:
# - Service name
# - API version
# - Supported operations
# - Resource types
```

#### Performance Tips

1. **Use Service Groups**: Scan related services together
2. **Increase Concurrency**: For large environments, use `--concurrency 10`
3. **Cache Results**: Enable caching in configuration
4. **Exclude Unused Services**: Configure service exclusions

### Azure

#### Authentication Setup

1. **Azure CLI Authentication**
   ```bash
   # Login with Azure CLI
   az login
   
   # Select subscription
   az account set --subscription "subscription-name"
   ```

2. **Service Principal**
   ```bash
   # Create service principal
   az ad sp create-for-rbac --name "corkscrew-scanner" --role Reader
   
   # Set environment variables
   export AZURE_CLIENT_ID="app-id"
   export AZURE_CLIENT_SECRET="password"
   export AZURE_TENANT_ID="tenant-id"
   export AZURE_SUBSCRIPTION_ID="subscription-id"
   ```

3. **Managed Identity** (Azure VMs)
   - Automatically used when running on Azure
   - Assign Reader role to the VM's identity

#### Enterprise App Setup

For organization-wide scanning:

```bash
# Deploy enterprise app (requires Global Admin)
corkscrew azure deploy-enterprise-app

# This will:
# 1. Create an Entra ID application
# 2. Assign required permissions
# 3. Create service principal
# 4. Configure certificate authentication
```

#### Scanning Examples

1. **Basic Scan**
   ```bash
   # Scan current subscription
   corkscrew scan --provider azure
   
   # Scan specific services
   corkscrew scan --provider azure --services storage,compute,keyvault
   
   # Scan specific region
   corkscrew scan --provider azure --region eastus
   ```

2. **Resource Graph Queries**
   ```bash
   # Use Resource Graph for fast scanning
   corkscrew scan --provider azure --use-resource-graph
   
   # Scan management group
   corkscrew scan --provider azure --management-group "mg-production"
   ```

3. **Multi-Subscription**
   ```bash
   # Configure in corkscrew.yaml
   providers:
     azure:
       subscription_filter:
         - "00000000-0000-0000-0000-000000000000"
         - "11111111-1111-1111-1111-111111111111"
   ```

#### Resource Types

Azure provider discovers 305+ resource types:

- **Compute**: Virtual Machines, VMSS, App Services
- **Storage**: Storage Accounts, Disks, File Shares
- **Networking**: VNets, Load Balancers, NSGs
- **Databases**: SQL, Cosmos DB, PostgreSQL
- **Security**: Key Vaults, Managed Identities

### GCP

#### Authentication Setup

1. **Application Default Credentials**
   ```bash
   # Login with gcloud
   gcloud auth application-default login
   
   # Set default project
   gcloud config set project PROJECT_ID
   ```

2. **Service Account Key**
   ```bash
   # Create service account
   gcloud iam service-accounts create corkscrew-scanner
   
   # Grant permissions
   gcloud projects add-iam-policy-binding PROJECT_ID \
     --member="serviceAccount:corkscrew-scanner@PROJECT_ID.iam.gserviceaccount.com" \
     --role="roles/viewer"
   
   # Create and download key
   gcloud iam service-accounts keys create key.json \
     --iam-account=corkscrew-scanner@PROJECT_ID.iam.gserviceaccount.com
   
   # Set environment variable
   export GOOGLE_APPLICATION_CREDENTIALS="path/to/key.json"
   ```

3. **Workload Identity** (GKE)
   - Configure workload identity binding
   - No key management required

#### Organization-Wide Scanning

```bash
# Enable Cloud Asset Inventory API
gcloud services enable cloudasset.googleapis.com

# Grant organization-level permissions
gcloud organizations add-iam-policy-binding ORG_ID \
  --member="serviceAccount:corkscrew-scanner@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/cloudasset.viewer"

# Scan entire organization
corkscrew scan --provider gcp --organization ORG_ID
```

#### Scanning Examples

1. **Basic Scan**
   ```bash
   # Scan current project
   corkscrew scan --provider gcp
   
   # Scan specific services
   corkscrew scan --provider gcp --services compute,storage
   
   # Scan specific region
   corkscrew scan --provider gcp --region us-central1
   ```

2. **Cloud Asset Inventory** (10x faster)
   ```bash
   # Use Asset Inventory for bulk scanning
   corkscrew scan --provider gcp --use-asset-inventory
   
   # Scan multiple projects
   corkscrew scan --provider gcp --projects "prod-*,dev-*"
   ```

3. **Advanced Filtering**
   ```yaml
   # In corkscrew.yaml
   providers:
     gcp:
       asset_inventory:
         asset_types:
           - "compute.googleapis.com/Instance"
           - "storage.googleapis.com/Bucket"
   ```

### Kubernetes

#### Authentication Setup

1. **Kubeconfig File**
   ```bash
   # Default location
   export KUBECONFIG=~/.kube/config
   
   # Custom location
   export KUBECONFIG=/path/to/kubeconfig
   ```

2. **In-Cluster** (Pod)
   - Automatically detected
   - Uses service account token

3. **Multiple Clusters**
   ```bash
   # List contexts
   kubectl config get-contexts
   
   # Scan specific context
   corkscrew scan --provider kubernetes --context production
   ```

#### RBAC Requirements

Minimum ClusterRole for scanning:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: corkscrew-scanner
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["nodes", "namespaces"]
  verbs: ["get", "list"]
```

#### Scanning Examples

1. **Basic Scan**
   ```bash
   # Scan current context
   corkscrew scan --provider kubernetes
   
   # Scan specific namespaces
   corkscrew scan --provider kubernetes --namespace default,kube-system
   
   # Scan all namespaces
   corkscrew scan --provider kubernetes --namespace "*"
   ```

2. **Resource Filtering**
   ```bash
   # Scan specific resource types
   corkscrew scan --provider kubernetes --resources pods,services,deployments
   
   # Scan with label selector
   corkscrew scan --provider kubernetes --selector "app=frontend"
   ```

3. **CRD Support**
   ```bash
   # Automatically discovers CRDs
   corkscrew scan --provider kubernetes --include-crds
   
   # Scan specific CRD
   corkscrew scan --provider kubernetes --resources "apps.example.com/v1/MyApp"
   ```

---

## Scanning Operations

### Basic Scanning

#### Single Provider Scan

```bash
# Minimal scan - uses defaults from config
corkscrew scan --provider aws

# Specify services
corkscrew scan --provider aws --services s3,ec2

# Specify region
corkscrew scan --provider aws --region us-west-2

# Combine options
corkscrew scan --provider aws --services s3,ec2,lambda --region us-east-1,us-west-2
```

#### Output Formats

```bash
# Table format (default)
corkscrew scan --provider aws --output table

# JSON format for processing
corkscrew scan --provider aws --output json

# CSV format for spreadsheets
corkscrew scan --provider aws --output csv > resources.csv

# Save to timestamped file
corkscrew scan --provider aws --save
# Creates: scan_results_aws_20240115_143022.json
```

#### Filtering Results

```bash
# Show only non-empty services
corkscrew scan --provider aws --hide-empty

# Show empty services too
corkscrew scan --provider aws --show-empty

# Filter by tags (AWS)
corkscrew scan --provider aws --tag-filter "Environment=production"

# Multiple tag filters
corkscrew scan --provider aws --tag-filter "Environment=production,Owner=team-a"
```

### Advanced Scanning

#### Multi-Region Scanning

```bash
# Scan specific regions
corkscrew scan --provider aws --region us-east-1,us-west-2,eu-west-1

# Scan all regions
corkscrew scan --provider aws --region all

# Control concurrency
corkscrew scan --provider aws --region all --concurrency 10

# With progress tracking
corkscrew scan --provider aws --region all --progress
```

#### Service Groups

Service groups make it easy to scan related services:

```bash
# Common services (s3, ec2, lambda, rds, iam)
corkscrew scan --provider aws --services common

# All compute services
corkscrew scan --provider aws --services compute

# All storage services
corkscrew scan --provider aws --services storage

# Multiple groups
corkscrew scan --provider aws --services compute,storage,database

# Mix groups and individual services
corkscrew scan --provider aws --services common,sns,sqs
```

Available service groups:
- **common**: Most frequently used services
- **compute**: EC2, Lambda, ECS, EKS, Batch
- **storage**: S3, EBS, EFS, FSx, Backup
- **database**: RDS, DynamoDB, ElastiCache, Redshift
- **network**: VPC, ELB, Route53, CloudFront
- **security**: IAM, KMS, Secrets Manager, ACM
- **monitoring**: CloudWatch, X-Ray, SNS, SQS

#### Performance Optimization

```bash
# Increase concurrent operations
corkscrew scan --provider aws --concurrency 20

# Use caching for repeated scans
corkscrew scan --provider aws --use-cache

# Set cache TTL
corkscrew scan --provider aws --cache-ttl 1h

# Disable progress bars for scripts
corkscrew scan --provider aws --no-progress

# Streaming mode for large results
corkscrew scan --provider aws --stream
```

### Cross-Cloud Scanning

#### Basic Cross-Cloud Scan

```bash
# Scan multiple providers
corkscrew crosscloud scan --providers aws,azure

# Specify regions for each provider
corkscrew crosscloud scan --providers aws,azure \
  --aws-regions us-east-1,us-west-2 \
  --azure-regions eastus,westus

# Include correlation analysis
corkscrew crosscloud scan --providers aws,azure --correlate
```

#### Correlation Analysis

```bash
# Find correlated resources across clouds
corkscrew crosscloud correlate --providers aws,azure

# Set correlation confidence threshold
corkscrew crosscloud correlate --confidence 0.8

# Specific correlation types
corkscrew crosscloud correlate --types ip,dns,certificate

# Export correlation graph
corkscrew crosscloud correlate --output graph > correlations.dot
```

#### Network Topology Discovery

```bash
# Discover cross-cloud network connections
corkscrew crosscloud network --providers aws,azure,gcp

# Find VPN connections
corkscrew crosscloud network --connection-type vpn

# Find peering connections
corkscrew crosscloud network --connection-type peering

# Generate network diagram
corkscrew crosscloud network --output diagram
```

---

## Querying and Analysis

### SQL Query Basics

Corkscrew stores all scan results in a DuckDB database, enabling powerful SQL queries.

#### Running Queries

```bash
# Basic query
corkscrew query "SELECT COUNT(*) FROM aws_resources"

# Query with formatted output
corkscrew query "SELECT service, COUNT(*) as count FROM aws_resources GROUP BY service" --output table

# Query from file
corkscrew query --file analysis.sql

# Query from stdin
echo "SELECT * FROM azure_resources WHERE type = 'VirtualMachine'" | corkscrew query --stdin
```

#### Understanding the Schema

Key tables in the database:

1. **Provider Resource Tables**
   - `aws_resources`: All AWS resources
   - `azure_resources`: All Azure resources
   - `gcp_resources`: All GCP resources
   - `kubernetes_resources`: All Kubernetes resources

2. **Relationship Tables**
   - `cloud_relationships`: Resource dependencies
   - `cross_cloud_correlations`: Cross-cloud relationships

3. **Metadata Tables**
   - `scan_metadata`: Scan execution history
   - `api_action_metadata`: API call audit log

#### Basic Query Examples

```sql
-- Count resources by type
SELECT type, COUNT(*) as count 
FROM aws_resources 
GROUP BY type 
ORDER BY count DESC;

-- Find resources by name pattern
SELECT id, name, type, region 
FROM aws_resources 
WHERE name LIKE '%prod%';

-- Resources created in last 7 days
SELECT id, name, type, created_at 
FROM aws_resources 
WHERE created_at > CURRENT_DATE - INTERVAL 7 DAY;

-- Resources by tag
SELECT id, name, type, tags 
FROM aws_resources 
WHERE json_extract_string(tags, '$.Environment') = 'production';

-- Cross-cloud resource count
SELECT 
  'AWS' as provider, COUNT(*) as count FROM aws_resources
UNION ALL
SELECT 
  'Azure' as provider, COUNT(*) as count FROM azure_resources
UNION ALL
SELECT 
  'GCP' as provider, COUNT(*) as count FROM gcp_resources;
```

### Advanced Queries

#### JSON Field Queries

DuckDB's JSON functions enable querying nested data:

```sql
-- Extract specific JSON fields
SELECT 
  id,
  name,
  json_extract_string(attributes, '$.State') as state,
  json_extract_string(attributes, '$.InstanceType') as instance_type
FROM aws_resources
WHERE type = 'AWS::EC2::Instance';

-- Query JSON arrays
SELECT 
  id,
  name,
  json_array_length(json_extract(attributes, '$.SecurityGroups')) as sg_count
FROM aws_resources
WHERE type = 'AWS::EC2::Instance'
  AND json_array_length(json_extract(attributes, '$.SecurityGroups')) > 5;

-- Complex JSON queries
SELECT 
  b.name as bucket_name,
  json_extract_string(b.attributes, '$.BucketEncryption.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm') as encryption
FROM aws_resources b
WHERE b.type = 'AWS::S3::Bucket'
  AND json_extract_string(b.attributes, '$.BucketEncryption') IS NOT NULL;
```

#### Relationship Queries

```sql
-- Find all resources connected to a specific resource
WITH RECURSIVE dependencies AS (
  -- Direct dependencies
  SELECT 
    r.id,
    r.name,
    r.type,
    rel.relationship_type,
    1 as depth
  FROM cloud_relationships rel
  JOIN aws_resources r ON rel.to_id = r.id
  WHERE rel.from_id = 'vpc-12345'
  
  UNION ALL
  
  -- Recursive dependencies
  SELECT 
    r.id,
    r.name,
    r.type,
    rel.relationship_type,
    d.depth + 1
  FROM cloud_relationships rel
  JOIN aws_resources r ON rel.to_id = r.id
  JOIN dependencies d ON rel.from_id = d.id
  WHERE d.depth < 5
)
SELECT DISTINCT * FROM dependencies ORDER BY depth, type;

-- Find orphaned resources (no relationships)
SELECT id, name, type, region
FROM aws_resources r
WHERE NOT EXISTS (
  SELECT 1 FROM cloud_relationships 
  WHERE from_id = r.id OR to_id = r.id
);

-- Resource dependency count
SELECT 
  r.id,
  r.name,
  r.type,
  COUNT(DISTINCT rel_out.to_id) as dependencies,
  COUNT(DISTINCT rel_in.from_id) as dependents
FROM aws_resources r
LEFT JOIN cloud_relationships rel_out ON rel_out.from_id = r.id
LEFT JOIN cloud_relationships rel_in ON rel_in.to_id = r.id
GROUP BY r.id, r.name, r.type
HAVING dependencies > 0 OR dependents > 0
ORDER BY dependencies DESC, dependents DESC;
```

#### Cross-Cloud Queries

```sql
-- Find resources with same IP across clouds
SELECT 
  a.provider as cloud_a,
  a.resource_name,
  a.resource_type,
  b.provider as cloud_b,
  b.resource_name as matched_resource,
  b.resource_type as matched_type,
  a.ip_address
FROM cross_cloud_ip_addresses a
JOIN cross_cloud_ip_addresses b 
  ON a.ip_address = b.ip_address 
  AND a.provider != b.provider
ORDER BY a.ip_address;

-- Cross-cloud VPN connections
SELECT 
  source_provider,
  source_vpn_name,
  target_provider,
  target_vpn_name,
  connection_status,
  bandwidth_mbps
FROM cross_cloud_vpn_connections
WHERE connection_status = 'active';

-- Security correlation summary
SELECT 
  source_provider,
  target_provider,
  correlation_type,
  COUNT(*) as correlation_count,
  AVG(confidence_score) as avg_confidence
FROM cross_cloud_correlations
GROUP BY source_provider, target_provider, correlation_type
ORDER BY correlation_count DESC;
```

### Compliance Queries

#### Running Compliance Packs

```bash
# List available compliance packs
corkscrew query --list-packs

# Run a specific control
corkscrew query --control jlgore/cfi-ccc/CCC.C01

# Run an entire compliance pack
corkscrew query --pack jlgore/cfi-ccc/s3-security

# Run with parameters
corkscrew query --control aws/s3/encryption \
  --param trusted_kms_keys="arn:aws:kms:us-east-1:123456789012:key/*"

# Export compliance results
corkscrew query --pack aws-cis --output csv > compliance_report.csv
```

#### Writing Custom Compliance Queries

Compliance queries must return specific columns:

```sql
-- Example: Check for unencrypted S3 buckets
SELECT 
  CASE 
    WHEN json_extract_string(attributes, '$.BucketEncryption') IS NULL 
    THEN 'FAIL' 
    ELSE 'PASS' 
  END as status,
  id as resource_id,
  name as resource_name,
  'AWS::S3::Bucket' as resource_type,
  'S3.1' as control_id,
  'S3 Bucket Encryption' as control_name,
  'HIGH' as severity,
  CASE 
    WHEN json_extract_string(attributes, '$.BucketEncryption') IS NULL 
    THEN 'S3 bucket is not encrypted' 
    ELSE 'S3 bucket is properly encrypted' 
  END as details,
  CASE 
    WHEN json_extract_string(attributes, '$.BucketEncryption') IS NULL 
    THEN 'Enable default encryption on the S3 bucket' 
    ELSE NULL 
  END as remediation
FROM aws_resources
WHERE type = 'AWS::S3::Bucket';

-- Example: Check for public RDS instances
SELECT 
  CASE 
    WHEN json_extract_string(attributes, '$.PubliclyAccessible') = 'true' 
    THEN 'FAIL' 
    ELSE 'PASS' 
  END as status,
  id as resource_id,
  name as resource_name,
  'AWS::RDS::DBInstance' as resource_type,
  'RDS.2' as control_id,
  'RDS Public Access' as control_name,
  'CRITICAL' as severity,
  CASE 
    WHEN json_extract_string(attributes, '$.PubliclyAccessible') = 'true' 
    THEN 'RDS instance is publicly accessible' 
    ELSE 'RDS instance is not publicly accessible' 
  END as details,
  CASE 
    WHEN json_extract_string(attributes, '$.PubliclyAccessible') = 'true' 
    THEN 'Modify the RDS instance to disable public accessibility' 
    ELSE NULL 
  END as remediation
FROM aws_resources
WHERE type = 'AWS::RDS::DBInstance';
```

#### Compliance Reporting

```bash
# Generate compliance summary
corkscrew query "
  SELECT 
    control_id,
    control_name,
    severity,
    COUNT(*) as resource_count,
    SUM(CASE WHEN status = 'FAIL' THEN 1 ELSE 0 END) as failures,
    ROUND(100.0 * SUM(CASE WHEN status = 'PASS' THEN 1 ELSE 0 END) / COUNT(*), 2) as compliance_rate
  FROM compliance_results
  GROUP BY control_id, control_name, severity
  ORDER BY severity, compliance_rate
" --output table

# Failed resources by severity
corkscrew query "
  SELECT 
    severity,
    COUNT(DISTINCT resource_id) as failed_resources
  FROM compliance_results
  WHERE status = 'FAIL'
  GROUP BY severity
  ORDER BY 
    CASE severity 
      WHEN 'CRITICAL' THEN 1 
      WHEN 'HIGH' THEN 2 
      WHEN 'MEDIUM' THEN 3 
      WHEN 'LOW' THEN 4 
      ELSE 5 
    END
" --output table
```

---

## Practical Workflows

### Security Auditing

#### Finding Exposed Resources

```bash
# Find publicly accessible S3 buckets
corkscrew query "
  SELECT 
    id,
    name,
    region,
    json_extract_string(attributes, '$.PublicAccessBlockConfiguration') as public_access_block
  FROM aws_resources
  WHERE type = 'AWS::S3::Bucket'
    AND (
      json_extract_string(attributes, '$.PublicAccessBlockConfiguration.BlockPublicAcls') != 'true'
      OR json_extract_string(attributes, '$.PublicAccessBlockConfiguration') IS NULL
    )
"

# Find internet-facing load balancers
corkscrew query "
  SELECT 
    id,
    name,
    region,
    json_extract_string(attributes, '$.Scheme') as scheme,
    json_extract_string(attributes, '$.DNSName') as dns_name
  FROM aws_resources
  WHERE type = 'AWS::ElasticLoadBalancing::LoadBalancer'
    AND json_extract_string(attributes, '$.Scheme') = 'internet-facing'
"

# Find publicly accessible databases
corkscrew query "
  SELECT 
    'AWS' as cloud,
    id,
    name,
    type,
    json_extract_string(attributes, '$.PubliclyAccessible') as public_access
  FROM aws_resources
  WHERE type IN ('AWS::RDS::DBInstance', 'AWS::Redshift::Cluster')
    AND json_extract_string(attributes, '$.PubliclyAccessible') = 'true'
  UNION ALL
  SELECT 
    'Azure' as cloud,
    id,
    name,
    type,
    json_extract_string(properties, '$.publicNetworkAccess') as public_access
  FROM azure_resources
  WHERE type LIKE '%database%'
    AND json_extract_string(properties, '$.publicNetworkAccess') = 'Enabled'
"
```

#### Permission Analysis

```bash
# Find overly permissive IAM policies
corkscrew query "
  SELECT 
    id,
    name,
    json_extract_string(attributes, '$.PolicyDocument') as policy_document
  FROM aws_resources
  WHERE type = 'AWS::IAM::Policy'
    AND json_extract_string(attributes, '$.PolicyDocument') LIKE '%\"*\"%'
    AND json_extract_string(attributes, '$.PolicyDocument') LIKE '%\"*\"%'
"

# Cross-account role trusts
corkscrew query "
  SELECT 
    r.name as role_name,
    r.id as role_id,
    json_extract_string(r.attributes, '$.AssumeRolePolicyDocument') as trust_policy,
    rel.target_account_id as trusted_account,
    rel.risk_score
  FROM aws_resources r
  JOIN security_role_relationships rel ON r.id = rel.source_role_id
  WHERE r.type = 'AWS::IAM::Role'
    AND rel.relationship_type = 'cross_account_trust'
  ORDER BY rel.risk_score DESC
"

# Find service accounts with high privileges
corkscrew query "
  SELECT 
    namespace,
    name,
    json_array_length(json_extract(attributes, '$.secrets')) as secret_count,
    json_extract_string(attributes, '$.automountServiceAccountToken') as automount_token
  FROM kubernetes_resources
  WHERE type = 'ServiceAccount'
    AND namespace NOT IN ('kube-system', 'kube-public')
    AND json_extract_string(attributes, '$.automountServiceAccountToken') = 'true'
"
```

#### Certificate Tracking

```bash
# Find expiring certificates
corkscrew query "
  SELECT 
    provider,
    id,
    name,
    json_extract_string(attributes, '$.NotAfter') as expiry_date,
    DATEDIFF('day', CURRENT_DATE, json_extract_string(attributes, '$.NotAfter')::DATE) as days_until_expiry
  FROM (
    SELECT 'AWS' as provider, id, name, attributes FROM aws_resources WHERE type = 'AWS::ACM::Certificate'
    UNION ALL
    SELECT 'Azure' as provider, id, name, properties as attributes FROM azure_resources WHERE type = 'Microsoft.Web/certificates'
  )
  WHERE DATEDIFF('day', CURRENT_DATE, json_extract_string(attributes, '$.NotAfter')::DATE) < 30
  ORDER BY days_until_expiry
"

# Certificate correlation across clouds
corkscrew query "
  SELECT 
    c1.source_provider,
    c1.source_cert_name,
    c1.target_provider,
    c1.target_cert_name,
    c1.correlation_type,
    c1.confidence_score
  FROM certificate_correlations c1
  WHERE c1.confidence_score > 0.8
    AND c1.correlation_type = 'same_domain'
  ORDER BY c1.confidence_score DESC
"
```

### Cost Optimization

#### Finding Unused Resources

```bash
# Unattached EBS volumes
corkscrew query "
  SELECT 
    id,
    name,
    region,
    json_extract_string(attributes, '$.Size') as size_gb,
    json_extract_string(attributes, '$.VolumeType') as volume_type,
    json_extract_string(tags, '$.Environment') as environment,
    ROUND(CAST(json_extract_string(attributes, '$.Size') AS FLOAT) * 0.10, 2) as estimated_monthly_cost
  FROM aws_resources
  WHERE type = 'AWS::EC2::Volume'
    AND json_extract_string(attributes, '$.State') = 'available'
  ORDER BY size_gb DESC
"

# Idle load balancers
corkscrew query "
  SELECT 
    id,
    name,
    region,
    type,
    json_array_length(json_extract(attributes, '$.Instances')) as instance_count,
    json_extract_string(tags, '$.Environment') as environment
  FROM aws_resources
  WHERE type IN ('AWS::ElasticLoadBalancing::LoadBalancer', 'AWS::ElasticLoadBalancingV2::LoadBalancer')
    AND (
      json_array_length(json_extract(attributes, '$.Instances')) = 0
      OR json_extract(attributes, '$.Instances') IS NULL
    )
"

# Oversized instances
corkscrew query "
  WITH instance_metrics AS (
    SELECT 
      id,
      name,
      json_extract_string(attributes, '$.InstanceType') as instance_type,
      region,
      -- Simulated metrics (in real scenario, would join with CloudWatch data)
      json_extract_string(attributes, '$.InstanceType') as size_category
    FROM aws_resources
    WHERE type = 'AWS::EC2::Instance'
      AND json_extract_string(attributes, '$.State.Name') = 'running'
  )
  SELECT 
    id,
    name,
    instance_type,
    region,
    CASE 
      WHEN instance_type LIKE '%xlarge%' THEN 'Consider downsizing'
      WHEN instance_type LIKE '%large%' THEN 'Review utilization'
      ELSE 'Appropriately sized'
    END as recommendation
  FROM instance_metrics
  WHERE instance_type LIKE '%xlarge%'
"
```

#### Resource Utilization Analysis

```bash
# Storage utilization summary
corkscrew query "
  SELECT 
    'S3' as storage_type,
    COUNT(*) as bucket_count,
    COUNT(DISTINCT region) as regions_used,
    COUNT(DISTINCT json_extract_string(tags, '$.Environment')) as environments
  FROM aws_resources
  WHERE type = 'AWS::S3::Bucket'
  UNION ALL
  SELECT 
    'EBS' as storage_type,
    COUNT(*) as volume_count,
    COUNT(DISTINCT region) as regions_used,
    COUNT(DISTINCT json_extract_string(tags, '$.Environment')) as environments
  FROM aws_resources
  WHERE type = 'AWS::EC2::Volume'
  UNION ALL
  SELECT 
    'Storage Account' as storage_type,
    COUNT(*) as account_count,
    COUNT(DISTINCT location) as regions_used,
    COUNT(DISTINCT json_extract_string(tags, '$.Environment')) as environments
  FROM azure_resources
  WHERE type = 'Microsoft.Storage/storageAccounts'
"

# Database resource allocation
corkscrew query "
  SELECT 
    type,
    json_extract_string(attributes, '$.DBInstanceClass') as instance_class,
    COUNT(*) as count,
    SUM(CAST(json_extract_string(attributes, '$.AllocatedStorage') AS INT)) as total_storage_gb
  FROM aws_resources
  WHERE type = 'AWS::RDS::DBInstance'
  GROUP BY type, instance_class
  ORDER BY count DESC
"
```

#### Cross-Region Duplication

```bash
# Find duplicate resources across regions
corkscrew query "
  WITH resource_fingerprints AS (
    SELECT 
      name,
      type,
      region,
      json_extract_string(tags, '$.Environment') as environment,
      json_extract_string(tags, '$.Application') as application,
      MD5(name || type || COALESCE(environment, '') || COALESCE(application, '')) as fingerprint
    FROM aws_resources
    WHERE type IN ('AWS::EC2::Instance', 'AWS::RDS::DBInstance', 'AWS::Lambda::Function')
  )
  SELECT 
    name,
    type,
    COUNT(DISTINCT region) as region_count,
    STRING_AGG(DISTINCT region, ', ') as regions,
    environment,
    application
  FROM resource_fingerprints
  GROUP BY name, type, environment, application, fingerprint
  HAVING COUNT(DISTINCT region) > 1
  ORDER BY region_count DESC
"
```

### Network Analysis

#### VPC/VNet Mapping

```bash
# VPC overview with resource counts
corkscrew query "
  WITH vpc_resources AS (
    SELECT 
      vpc.id as vpc_id,
      vpc.name as vpc_name,
      vpc.region,
      json_extract_string(vpc.attributes, '$.CidrBlock') as cidr_block,
      r.type as resource_type,
      COUNT(r.id) as resource_count
    FROM aws_resources vpc
    LEFT JOIN cloud_relationships rel ON vpc.id = rel.from_id
    LEFT JOIN aws_resources r ON rel.to_id = r.id
    WHERE vpc.type = 'AWS::EC2::VPC'
    GROUP BY vpc.id, vpc.name, vpc.region, cidr_block, r.type
  )
  SELECT 
    vpc_id,
    vpc_name,
    region,
    cidr_block,
    COUNT(DISTINCT resource_type) as resource_types,
    SUM(resource_count) as total_resources
  FROM vpc_resources
  GROUP BY vpc_id, vpc_name, region, cidr_block
  ORDER BY total_resources DESC
"

# Cross-region VPC peering
corkscrew query "
  SELECT 
    pc.id as peering_id,
    pc.name as peering_name,
    json_extract_string(pc.attributes, '$.Status.Code') as status,
    vpc1.name as requester_vpc,
    vpc1.region as requester_region,
    vpc2.name as accepter_vpc,
    vpc2.region as accepter_region
  FROM aws_resources pc
  JOIN cloud_relationships rel1 ON pc.id = rel1.from_id AND rel1.relationship_type = 'requester'
  JOIN aws_resources vpc1 ON rel1.to_id = vpc1.id
  JOIN cloud_relationships rel2 ON pc.id = rel2.from_id AND rel2.relationship_type = 'accepter'
  JOIN aws_resources vpc2 ON rel2.to_id = vpc2.id
  WHERE pc.type = 'AWS::EC2::VPCPeeringConnection'
    AND vpc1.region != vpc2.region
"
```

#### Cross-Cloud Connectivity

```bash
# VPN connections between clouds
corkscrew query "
  SELECT 
    source_provider,
    source_vpn_name,
    source_region,
    target_provider,
    target_vpn_name,
    target_region,
    connection_status,
    bandwidth_mbps,
    monthly_cost_estimate
  FROM cross_cloud_vpn_connections
  WHERE connection_status = 'active'
  ORDER BY source_provider, target_provider
"

# Network overlap detection
corkscrew query "
  WITH all_networks AS (
    SELECT 
      'AWS' as provider,
      id,
      name,
      region,
      json_extract_string(attributes, '$.CidrBlock') as cidr
    FROM aws_resources
    WHERE type = 'AWS::EC2::VPC'
    UNION ALL
    SELECT 
      'Azure' as provider,
      id,
      name,
      location as region,
      json_extract_string(properties, '$.addressSpace.addressPrefixes[0]') as cidr
    FROM azure_resources
    WHERE type = 'Microsoft.Network/virtualNetworks'
  )
  SELECT 
    n1.provider as provider1,
    n1.name as network1,
    n1.cidr as cidr1,
    n2.provider as provider2,
    n2.name as network2,
    n2.cidr as cidr2
  FROM all_networks n1
  JOIN all_networks n2 
    ON n1.provider != n2.provider
    AND n1.cidr = n2.cidr
"
```

#### Load Balancer Analysis

```bash
# Load balancer distribution
corkscrew query "
  SELECT 
    lb.region,
    lb.type,
    COUNT(*) as lb_count,
    SUM(json_array_length(json_extract(lb.attributes, '$.Instances'))) as total_instances,
    AVG(json_array_length(json_extract(lb.attributes, '$.Instances'))) as avg_instances_per_lb
  FROM aws_resources lb
  WHERE lb.type IN ('AWS::ElasticLoadBalancing::LoadBalancer', 'AWS::ElasticLoadBalancingV2::LoadBalancer')
  GROUP BY lb.region, lb.type
  ORDER BY lb_count DESC
"

# Cross-zone load balancing analysis
corkscrew query "
  SELECT 
    id,
    name,
    region,
    json_extract_string(attributes, '$.CrossZoneLoadBalancing.Enabled') as cross_zone_enabled,
    json_array_length(json_extract(attributes, '$.AvailabilityZones')) as az_count
  FROM aws_resources
  WHERE type = 'AWS::ElasticLoadBalancing::LoadBalancer'
    AND json_extract_string(attributes, '$.CrossZoneLoadBalancing.Enabled') != 'true'
    AND json_array_length(json_extract(attributes, '$.AvailabilityZones')) > 1
"
```

#### DNS Correlation

```bash
# DNS records pointing to multiple clouds
corkscrew query "
  SELECT 
    dns_name,
    record_type,
    COUNT(DISTINCT provider) as provider_count,
    STRING_AGG(DISTINCT provider, ', ') as providers,
    STRING_AGG(target_resource, ', ') as targets
  FROM cross_cloud_dns_records
  GROUP BY dns_name, record_type
  HAVING COUNT(DISTINCT provider) > 1
  ORDER BY provider_count DESC
"

# Find resources by DNS name
corkscrew query "
  WITH dns_resources AS (
    SELECT 
      'AWS' as provider,
      id,
      name,
      type,
      json_extract_string(attributes, '$.DNSName') as dns_name
    FROM aws_resources
    WHERE json_extract_string(attributes, '$.DNSName') IS NOT NULL
    UNION ALL
    SELECT 
      'Azure' as provider,
      id,
      name,
      type,
      json_extract_string(properties, '$.dnsSettings.fqdn') as dns_name
    FROM azure_resources
    WHERE json_extract_string(properties, '$.dnsSettings.fqdn') IS NOT NULL
  )
  SELECT * FROM dns_resources
  WHERE dns_name LIKE '%example.com'
  ORDER BY provider, type
"
```

---

## Troubleshooting

### Common Issues

#### Authentication Problems

**AWS Authentication Errors**
```bash
# Error: Unable to locate credentials
# Solution: Configure AWS credentials
aws configure

# Error: ExpiredToken
# Solution: Refresh temporary credentials
aws sts get-session-token

# Error: AccessDenied
# Solution: Check IAM permissions
aws sts get-caller-identity
```

**Azure Authentication Errors**
```bash
# Error: No subscriptions found
# Solution: Login to Azure
az login

# Error: AuthorizationFailed
# Solution: Check role assignments
az role assignment list --assignee $(az account show --query user.name -o tsv)
```

**GCP Authentication Errors**
```bash
# Error: Could not load the default credentials
# Solution: Set up application default credentials
gcloud auth application-default login

# Error: Permission denied
# Solution: Check IAM roles
gcloud projects get-iam-policy PROJECT_ID
```

#### Permission Errors

**Insufficient Permissions**
```bash
# AWS: Add required permissions
aws iam attach-user-policy --user-name YOUR_USER --policy-arn arn:aws:iam::aws:policy/ReadOnlyAccess

# Azure: Assign Reader role
az role assignment create --assignee YOUR_USER --role Reader --scope /subscriptions/SUBSCRIPTION_ID

# GCP: Grant viewer role
gcloud projects add-iam-policy-binding PROJECT_ID --member=user:YOUR_EMAIL --role=roles/viewer
```

#### Plugin Build Failures

**Go Module Errors**
```bash
# Error: go.mod file not found
# Solution: Initialize go modules
cd plugins/aws-provider
go mod init
go mod tidy

# Error: Build constraints exclude all Go files
# Solution: Check Go version
go version  # Should be 1.21+
```

**Protobuf Compilation Errors**
```bash
# Error: protoc not found
# Solution: Re-run init
corkscrew init --upgrade

# Error: Plugin failed to compile
# Solution: Check protoc version
~/.corkscrew/bin/protoc --version
```

#### Performance Issues

**Slow Scanning**
```bash
# Increase concurrency
corkscrew scan --provider aws --concurrency 10

# Enable caching
cat >> corkscrew.yaml << EOF
providers:
  aws:
    analysis:
      cache_enabled: true
      cache_ttl: 24h
EOF

# Use service groups instead of individual services
corkscrew scan --provider aws --services common  # Faster than listing each service
```

**Memory Issues**
```bash
# Reduce batch size
cat >> corkscrew.yaml << EOF
performance:
  max_concurrent_regions: 3
  max_concurrent_services: 5
EOF

# Use streaming for large results
corkscrew scan --provider aws --stream

# Increase DuckDB memory limit
cat >> corkscrew.yaml << EOF
database:
  connection_options:
    memory_limit: "8GB"
EOF
```

### Debug Options

#### Verbose Logging

```bash
# Enable debug logging
export CORKSCREW_LOG_LEVEL=debug
corkscrew scan --provider aws --verbose

# Log to file
corkscrew scan --provider aws --log-file scan.log

# Trace API calls
corkscrew scan --provider aws --trace
```

#### API Call Tracking

```bash
# View API calls made during scan
corkscrew query "
  SELECT 
    provider,
    service,
    operation,
    region,
    status_code,
    duration_ms,
    timestamp
  FROM api_action_metadata
  WHERE timestamp > CURRENT_TIMESTAMP - INTERVAL 1 HOUR
  ORDER BY timestamp DESC
  LIMIT 100
"

# Find failed API calls
corkscrew query "
  SELECT 
    provider,
    service,
    operation,
    error_message,
    COUNT(*) as error_count
  FROM api_action_metadata
  WHERE status_code >= 400
  GROUP BY provider, service, operation, error_message
  ORDER BY error_count DESC
"
```

#### Query Debugging

```bash
# Explain query plan
corkscrew query --explain "SELECT * FROM aws_resources WHERE type = 'AWS::S3::Bucket'"

# Profile query execution
corkscrew query --profile "SELECT COUNT(*) FROM aws_resources GROUP BY type"

# Test query syntax
corkscrew query --dry-run "SELECT * FROM aws_resources"
```

---

## Command Reference

### Core Commands

#### `corkscrew init`
Initialize Corkscrew with dependencies and plugins.

```bash
corkscrew init [flags]

Flags:
  --dry-run    Show what would be done without making changes
  --upgrade    Force upgrade dependencies even if they exist
  --help       Show help for init command
```

#### `corkscrew scan`
Scan cloud resources from a specific provider.

```bash
corkscrew scan --provider PROVIDER [flags]

Required Flags:
  --provider string    Cloud provider (aws, azure, gcp, kubernetes)

Optional Flags:
  --region string      Comma-separated regions or 'all'
  --services string    Comma-separated services or service groups
  --output string      Output format: table, json, csv (default "table")
  --save              Save results to timestamped file
  --concurrency int    Number of concurrent operations (default 3)
  --show-empty        Show services with no resources
  --use-cache         Use cached results if available
  --cache-ttl string  Cache time-to-live (e.g., "1h", "24h")
  --stream            Stream results for large datasets
  --no-progress       Disable progress bars
  --config string     Path to configuration file
  --verbose           Enable verbose output
  --help              Show help for scan command

Service Groups (AWS):
  common      s3, ec2, lambda, rds, iam
  compute     ec2, lambda, ecs, eks, batch
  storage     s3, ebs, efs, fsx, backup
  database    rds, dynamodb, elasticache, redshift
  network     vpc, elb, route53, cloudfront
  security    iam, kms, secretsmanager, acm
  monitoring  cloudwatch, logs, xray, sns, sqs
```

#### `corkscrew query`
Execute SQL queries against scan results.

```bash
corkscrew query [SQL_QUERY] [flags]

Flags:
  --file string        Read query from file
  --stdin             Read query from stdin
  --output string     Output format: table, json, csv (default "table")
  --db string         Database file path
  --param key=value   Set query parameters
  --control string    Run compliance control
  --pack string       Run compliance pack
  --list-packs        List available compliance packs
  --explain           Show query execution plan
  --profile           Profile query execution
  --no-header         Omit header in output
  --verbose           Enable verbose output
  --help              Show help for query command
```

#### `corkscrew crosscloud`
Cross-cloud operations and analysis.

```bash
corkscrew crosscloud SUBCOMMAND [flags]

Subcommands:
  scan        Scan multiple cloud providers
  correlate   Find correlations between clouds
  network     Analyze cross-cloud network connections
  topology    Generate network topology

Common Flags:
  --providers string    Comma-separated list of providers
  --confidence float    Correlation confidence threshold (0-1)
  --output string       Output format
  --help                Show help
```

#### `corkscrew discover`
Discover available services for a provider.

```bash
corkscrew discover --provider PROVIDER [flags]

Flags:
  --provider string    Cloud provider
  --detailed          Show detailed information
  --output string     Output format (default "table")
  --help              Show help
```

#### `corkscrew config`
Manage Corkscrew configuration.

```bash
corkscrew config SUBCOMMAND [flags]

Subcommands:
  init        Create default configuration file
  validate    Validate configuration file
  show        Display current configuration

Flags:
  --config string    Path to configuration file
  --help             Show help
```

#### `corkscrew list`
List resources from previous scans (from database).

```bash
corkscrew list [flags]

Flags:
  --provider string    Filter by provider
  --type string        Filter by resource type
  --region string      Filter by region
  --service string     Filter by service
  --name string        Filter by name pattern
  --output string      Output format (default "table")
  --limit int          Limit number of results
  --help               Show help
```

#### `corkscrew describe`
Show detailed information about a specific resource.

```bash
corkscrew describe --resource-id ID [flags]

Flags:
  --resource-id string    Resource ID to describe
  --provider string       Cloud provider
  --show-relationships    Include relationships
  --show-raw             Show raw JSON data
  --output string         Output format (default "table")
  --help                  Show help
```

### Environment Variables

```bash
# Configuration
CORKSCREW_CONFIG_FILE      # Path to configuration file
CORKSCREW_LOG_LEVEL        # Log level (debug, info, warn, error)
CORKSCREW_NO_COLOR         # Disable colored output

# AWS
AWS_PROFILE                # AWS profile to use
AWS_REGION                 # Default AWS region
CORKSCREW_AWS_SERVICES     # Override AWS services list

# Azure  
AZURE_SUBSCRIPTION_ID      # Default Azure subscription
AZURE_TENANT_ID           # Azure tenant ID

# GCP
GOOGLE_APPLICATION_CREDENTIALS  # Path to GCP service account key
GCLOUD_PROJECT                 # Default GCP project

# Kubernetes
KUBECONFIG                # Path to kubeconfig file

# Performance
CORKSCREW_MAX_CONCURRENCY  # Maximum concurrent operations
CORKSCREW_CACHE_DIR        # Cache directory path
```

### Exit Codes

- `0`: Success
- `1`: General error
- `2`: Configuration error
- `3`: Authentication error
- `4`: Permission error
- `5`: Network error
- `10`: Plugin error
- `11`: Database error
- `12`: Query error

---

## Appendix

### Glossary

- **Provider**: A cloud platform (AWS, Azure, GCP, Kubernetes)
- **Plugin**: Provider-specific scanner implementation
- **Service**: A cloud service within a provider (e.g., S3, EC2)
- **Resource**: An individual cloud resource instance
- **Service Group**: Predefined collection of related services
- **Correlation**: Relationship between resources across clouds
- **Compliance Pack**: Collection of compliance rules
- **Control**: Individual compliance check

### Version History

- **v2.0.0**: Current version with unified scanner architecture
- **v1.x**: Legacy version (deprecated)

### Support

- GitHub Issues: https://github.com/your-org/corkscrew/issues
- Documentation: https://docs.corkscrew.io
- Community Slack: https://corkscrew.slack.com

---

*Last updated: January 2024*
