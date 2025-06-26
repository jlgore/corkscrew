# AWS Scanning Process Documentation

## Overview
This document provides a complete walkthrough of the AWS scanning process using Corkscrew from start to finish, including authentication setup, plugin building, scanning operations, and results analysis.

## Prerequisites
- AWS CLI configured with appropriate credentials
- Go installed for building plugins
- Corkscrew binary built and available

## Step 1: AWS Authentication Setup

### Configure AWS Profile
```bash
export AWS_PROFILE=sandbox
```

### Verify Authentication
```bash
aws sts get-caller-identity
```

**Expected Output:**
```json
{
    "UserId": "AROAYS2NVT7T75RDHKX4A:jg@gore.cc",
    "Account": "590184030183", 
    "Arn": "arn:aws:sts::590184030183:assumed-role/AWSReservedSSO_AdministratorAccess_1c6721131e98e773/jg@gore.cc"
}
```

## Step 2: Build AWS Provider Plugin

### Navigate to AWS Provider Directory
```bash
cd /home/jg/git/corkscrew/plugins/aws-provider
```

### Build the Plugin
```bash
go build -o aws-provider .
```

**Expected Result:**
- Binary created at `/home/jg/git/corkscrew/plugins/aws-provider/aws-provider` (233MB)

## Step 3: Discover Available Services

### Run Service Discovery
```bash
cd /home/jg/git/corkscrew
export AWS_PROFILE=sandbox
./corkscrew discover --provider aws
```

**Available Services:**
- cloudformation
- cloudwatch  
- dynamodb
- ec2
- ecs
- eks
- elasticache
- glue
- iam
- kinesis
- lambda
- rds
- redshift
- route53
- s3
- sns
- sqs

## Step 4: Full Region Scan (us-west-2)

### Execute Complete Scan
```bash
./corkscrew scan --provider aws --services s3,ec2,dynamodb,lambda,rds,iam --region us-west-2
```

**Scan Results:**
- **Total Resources:** 1,513
- **Duration:** 27.1 seconds
- **Services Scanned:** 6
- **Failed Resources:** 0

**Key Resource Types Found:**
- S3 Buckets: 18
- EC2 Instances (various types): 100+
- IAM Roles: 30
- IAM Users: 8
- IAM Policies: 100
- VPCs: 12
- Subnets: 9
- Security Groups: 10
- Lambda Functions: 5
- RDS Snapshots: 8
- And many more...

## Step 5: Targeted Scan (us-east-1)

### Scan Specific Services
```bash
./corkscrew scan --provider aws --services s3,dynamodb --region us-east-1
```

**Scan Results:**
- **Total Resources:** 19
- **Duration:** 479ms
- **Services Scanned:** 2
- **Failed Resources:** 0

**Resources Found:**
- S3 Buckets: 18
- DynamoDB Endpoint: 1
- DynamoDB Tables: 0

### Detailed S3 Bucket List (us-east-1)
```bash
./corkscrew list --provider aws --service s3 --region us-east-1
```

**S3 Buckets Found:**
1. innovarainsights-board-meetings-00c5f9
2. innovarainsights-board-meetings-00c5f9-logs-1045
3. innovarainsights-customer-database-00ed4e
4. innovarainsights-customer-database-00ed4e-logs-137a
5. innovarainsights-dev-credentials-00d372
6. innovarainsights-dev-credentials-00d372-logs-10b6
7. innovarainsights-employee-salaries-00ef70
8. innovarainsights-employee-salaries-00ef70-logs-1321
9. innovarainsights-product-roadmap-00d3ef
10. innovarainsights-product-roadmap-00d3ef-logs-11aa
11. innovarainsights-source-code-009fea
12. innovarainsights-source-code-009fea-logs-df66
13. innovatechlabs-security-logs-001717
14. lambda-demo-jg-mybucketbucket-cxxamxsx
15. sst-asset-doorfasrnrrw
16. sst-state-doorfasrnrrw
17. test-bucket-02my5of0
18. test-bucket-02my5of0-logs

### DynamoDB Resources (us-east-1)
```bash
./corkscrew list --provider aws --service dynamodb --region us-east-1
```

**DynamoDB Resources:**
- 1 Endpoint (no tables found)

## Command Reference

### Basic Commands
```bash
# Discover services
./corkscrew discover --provider aws

# Full scan with multiple services
./corkscrew scan --provider aws --services [service1,service2] --region [region]

# List specific service resources
./corkscrew list --provider aws --service [service] --region [region]

# Get provider information
./corkscrew info --provider aws
```

### Available Commands
- `scan` - Full resource scanning
- `discover` - Discover available services
- `list` - List resources
- `describe` - Describe specific resources
- `info` - Show provider information
- `schemas` - Get database schemas for resources

## Performance Notes

### Scan Performance
- **us-west-2 (6 services):** 27.1 seconds for 1,513 resources
- **us-east-1 (2 services):** 479ms for 19 resources

### Plugin Architecture
- Uses HashiCorp go-plugin for gRPC communication
- Automatic service discovery through SDK reflection
- Rate limiting and retry mechanisms built-in
- No Resource Explorer views found (uses SDK scanning fallback)

## Troubleshooting

### Common Issues
1. **Missing required fields:** Some AWS API calls require specific parameters that cannot be auto-discovered
2. **Authentication errors:** Ensure AWS_PROFILE is set and credentials are valid
3. **Plugin not found:** Verify plugin binary exists and is executable

### Debug Mode
Add verbose logging by checking plugin debug output in the scan results.

## Step 6: Database Storage (DuckDB)

### Database Location and Structure
Corkscrew automatically creates a DuckDB database at `~/.corkscrew/db/corkscrew.duckdb` to store scan results, metadata, and relationships.

**Database Schema:**
- `aws_resources` - AWS resource data with metadata
- `azure_resources` - Azure resource data  
- `cloud_relationships` - Cross-cloud resource relationships
- `scan_metadata` - Scan execution metadata and performance metrics
- `api_action_metadata` - API call tracking and debugging info

### Initializing and Testing Database
```bash
# Create a test program to verify database functionality
cd /home/jg/git/corkscrew
go run test_database_save.go
```

**Expected Output:**
```
🔧 Initializing DuckDB database...
✅ Database initialized at: /home/jg/.corkscrew/db/corkscrew.duckdb
💾 Saving 2 sample resources to database...
✅ Saved resource: test-bucket-12345 (Bucket)
✅ Saved resource: test-instance (Instance)
✅ Saved scan metadata: scan_1748724512
```

### Querying Scan Results

#### Using DuckDB CLI
```bash
# Connect to database
duckdb ~/.corkscrew/db/corkscrew.duckdb

# Basic resource queries
SELECT service, type, COUNT(*) as count 
FROM aws_resources 
GROUP BY service, type 
ORDER BY count DESC;

# Resources by region
SELECT region, COUNT(*) as count
FROM aws_resources
GROUP BY region;

# Recent scans
SELECT id, provider, total_resources, scan_start_time, status
FROM scan_metadata
ORDER BY scan_start_time DESC
LIMIT 5;

# Search by tags
SELECT name, region, tags
FROM aws_resources
WHERE JSON_EXTRACT(tags, '$.Environment') = 'production';
```

#### Using Unified Views
```sql
-- Cross-cloud resource view
SELECT provider, COUNT(*) as count
FROM all_cloud_resources
GROUP BY provider;

-- Resource counts by provider
SELECT * FROM resource_counts_by_provider;
```

### Advanced Database Operations

#### Export Data
```bash
# Export to CSV
duckdb ~/.corkscrew/db/corkscrew.duckdb -c "COPY aws_resources TO 'aws_resources.csv' (FORMAT CSV, HEADER);"

# Export to JSON
duckdb ~/.corkscrew/db/corkscrew.duckdb -c "COPY (SELECT * FROM aws_resources) TO 'aws_resources.json' (FORMAT JSON);"

# Export to Parquet
duckdb ~/.corkscrew/db/corkscrew.duckdb -c "COPY aws_resources TO 'aws_resources.parquet' (FORMAT PARQUET);"
```

#### Complex Analysis Queries
```sql
-- Find resources created in the last 7 days
SELECT name, type, service, region, created_at
FROM aws_resources
WHERE created_at >= current_date - interval '7 days';

-- Top 10 services by resource count
SELECT service, COUNT(*) as resource_count
FROM aws_resources
GROUP BY service
ORDER BY resource_count DESC
LIMIT 10;

-- Resources without proper tagging
SELECT name, type, service, region
FROM aws_resources
WHERE tags IS NULL OR JSON_EXTRACT(tags, '$.Environment') IS NULL;
```

### Database Schema Details

#### AWS Resources Table
```sql
-- Primary resource storage
CREATE TABLE aws_resources (
    id VARCHAR PRIMARY KEY,           -- AWS Resource ID/ARN
    arn VARCHAR UNIQUE,               -- AWS ARN
    name VARCHAR NOT NULL,            -- Resource name
    type VARCHAR NOT NULL,            -- Resource type
    service VARCHAR,                  -- AWS service
    region VARCHAR,                   -- AWS region
    account_id VARCHAR,               -- AWS account
    parent_id VARCHAR,                -- Parent resource
    tags JSON,                        -- Resource tags
    attributes JSON,                  -- AWS-specific attributes
    raw_data JSON,                    -- Complete raw data
    state VARCHAR,                    -- Resource state
    created_at TIMESTAMP,             -- Resource creation
    modified_at TIMESTAMP,            -- Last modification
    scanned_at TIMESTAMP              -- Discovery time
);
```

#### Scan Metadata Table
```sql
-- Scan execution tracking
CREATE TABLE scan_metadata (
    id VARCHAR PRIMARY KEY,           -- Unique scan ID
    provider VARCHAR NOT NULL,        -- Cloud provider
    scan_type VARCHAR NOT NULL,       -- Scan type
    services JSON,                    -- Services scanned
    regions JSON,                     -- Regions scanned
    total_resources INTEGER,          -- Total resources found
    scan_start_time TIMESTAMP,        -- Start time
    scan_end_time TIMESTAMP,          -- End time
    duration_ms BIGINT,               -- Duration
    status VARCHAR                    -- Scan status
);
```

### Interactive Diagram Viewer

Corkscrew includes an interactive diagram viewer powered by the DuckDB data:

```bash
# Launch interactive relationship viewer
./corkscrew diagram

# View specific resource relationships
./corkscrew diagram -resource vpc-123456789 -type relationships

# Export network topology
./corkscrew diagram -type network -region us-east-1 -export network.md
```

**Diagram Features:**
- Real-time resource relationships
- Network topology visualization
- Service dependency mapping
- Interactive filtering and navigation
- Export to Mermaid or ASCII formats

## Summary

The Corkscrew AWS scanning process successfully:
1. **Authentication** - Configured AWS_PROFILE=sandbox and verified access
2. **Plugin Build** - Built the AWS provider plugin (233MB binary)
3. **Service Discovery** - Found 17 available AWS services
4. **Regional Scanning** - Scanned us-west-2 (1,513 resources in 27s) and us-east-1 (19 resources in 479ms)
5. **Database Storage** - Automatically stored results in DuckDB with full metadata
6. **Data Analysis** - Provided rich querying capabilities and relationship mapping

### Key Benefits:
- **Persistent Storage** - All scan results saved to DuckDB for historical analysis
- **Rich Metadata** - Complete resource details, tags, relationships, and scan metrics
- **SQL Querying** - Full SQL support for complex analysis and reporting
- **Cross-Cloud Views** - Unified queries across AWS, Azure, and other providers
- **Performance Tracking** - Detailed API call metrics and scan performance data
- **Export Capabilities** - Data export to CSV, JSON, Parquet formats
- **Visualization** - Interactive diagram viewer for resource relationships

The system demonstrates enterprise-grade cloud resource discovery with comprehensive data persistence and analysis capabilities.