# GCP Provider Plugin for Corkscrew

The GCP provider plugin enables comprehensive scanning and analysis of Google Cloud Platform resources using Cloud Asset Inventory for efficient discovery.

## Features

- **Cloud Asset Inventory Integration**: High-performance resource discovery across projects, folders, and organizations
- **Multi-Project Support**: Scan resources across multiple GCP projects simultaneously
- **Dynamic Service Discovery**: Automatically discover enabled GCP services
- **Relationship Extraction**: Discover relationships between GCP resources
- **Comprehensive Coverage**: Supports 20+ GCP services including Compute, Storage, BigQuery, GKE, and more
- **Graceful Fallback**: Falls back to standard API scanning when Asset Inventory is unavailable

## Supported Services

- **Compute Engine**: Instances, Disks, Networks, Subnetworks, Firewalls, Snapshots, Images
- **Cloud Storage**: Buckets and Objects  
- **Google Kubernetes Engine**: Clusters and Node Pools
- **BigQuery**: Datasets, Tables, Views, Models
- **Cloud SQL**: Instances, Databases, Backups
- **Pub/Sub**: Topics, Subscriptions, Schemas
- **Cloud Run**: Services, Revisions, Configurations
- **Cloud Functions**: Functions
- **App Engine**: Applications, Services, Versions
- **IAM**: Service Accounts, Roles, Policies
- **Cloud Logging**: Log Entries, Sinks, Metrics
- **Cloud Monitoring**: Alert Policies, Notification Channels, Dashboards
- And many more...

## Prerequisites

### 1. GCP Authentication

The provider uses Application Default Credentials (ADC). Set up authentication using one of these methods:

#### Local Development
```bash
# Install gcloud CLI and authenticate
gcloud auth application-default login
```

#### Service Account (Recommended for Production)
```bash
# Create and download service account key
gcloud iam service-accounts create corkscrew-scanner \
  --description="Service account for Corkscrew GCP scanning" \
  --display-name="Corkscrew Scanner"

# Grant necessary permissions (see IAM Permissions section)
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
  --member="serviceAccount:corkscrew-scanner@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/cloudasset.viewer"

# Download key and set environment variable
gcloud iam service-accounts keys create corkscrew-key.json \
  --iam-account=corkscrew-scanner@YOUR_PROJECT_ID.iam.gserviceaccount.com

export GOOGLE_APPLICATION_CREDENTIALS="/path/to/corkscrew-key.json"
```

#### Google Cloud Shell / GKE Workload Identity
Authentication is automatic when running in these environments.

### 2. Enable Required APIs

```bash
# Enable Cloud Asset Inventory API (recommended for best performance)
gcloud services enable cloudasset.googleapis.com

# Enable other required APIs
gcloud services enable compute.googleapis.com
gcloud services enable storage-api.googleapis.com
gcloud services enable container.googleapis.com
gcloud services enable serviceusage.googleapis.com
```

### 3. IAM Permissions

For optimal functionality, grant these permissions:

#### Minimum Required Permissions
```json
{
  "permissions": [
    "cloudasset.assets.listAssets",
    "cloudasset.assets.searchAllResources",
    "serviceusage.services.list",
    "resourcemanager.projects.list"
  ]
}
```

#### Recommended Permissions (for full functionality)
```json
{
  "permissions": [
    "cloudasset.assets.listAssets",
    "cloudasset.assets.searchAllResources", 
    "cloudasset.assets.analyzeIamPolicy",
    "serviceusage.services.list",
    "resourcemanager.projects.list",
    "resourcemanager.folders.list",
    "resourcemanager.organizations.search",
    "compute.instances.list",
    "compute.disks.list",
    "storage.buckets.list",
    "container.clusters.list",
    "bigquery.datasets.list",
    "cloudsql.instances.list"
  ]
}
```

#### Predefined Roles
You can use these predefined roles instead of custom permissions:

- `roles/cloudasset.viewer` - For Cloud Asset Inventory access
- `roles/browser` - For basic resource viewing across services
- `roles/viewer` - For comprehensive read access (recommended)

## Configuration

### Basic Configuration

```yaml
# Configure for single project
providers:
  gcp:
    project_ids: "my-gcp-project"
    scope: "projects"
```

### Multi-Project Configuration

```yaml
# Configure for multiple projects
providers:
  gcp:
    project_ids: "project-1,project-2,project-3"
    scope: "projects"
```

### Organization/Folder Scope

```yaml
# Configure for entire organization
providers:
  gcp:
    org_id: "123456789"
    scope: "organizations"

# Configure for specific folder
providers:
  gcp:
    folder_id: "987654321"
    scope: "folders"
```

### Auto-Discovery

```yaml
# Let the provider discover accessible resources
providers:
  gcp:
    # Will automatically discover organizations, then projects
```

## Building and Installation

### Build the Plugin

```bash
# Build the GCP provider plugin
./plugins/build-gcp-plugin.sh
```

### Test the Plugin

```bash
# Test basic functionality (no GCP credentials required)
./plugins/build/gcp-provider --test

# Test with real GCP credentials
./plugins/build/gcp-provider --test-gcp

# Test Cloud Asset Inventory setup
export GCP_PROJECT_ID=your-project-id
./plugins/build/gcp-provider --check-asset-inventory
```

### Install in Corkscrew

```bash
# Copy plugin to Corkscrew plugins directory
cp plugins/build/gcp-provider /path/to/corkscrew/plugins/

# Register the plugin with Corkscrew
./corkscrew plugin install gcp-provider
```

## Usage Examples

### Basic Resource Scanning

```bash
# Scan all GCP resources
corkscrew scan --provider gcp

# Scan specific services
corkscrew scan --provider gcp --services compute,storage,container

# Scan with streaming output
corkscrew scan --provider gcp --stream
```

### Advanced Queries

```sql
-- Find all compute instances
SELECT name, zone, machine_type, status 
FROM gcp_compute_instances 
WHERE status = 'RUNNING';

-- Find storage buckets with public access
SELECT name, location, public_access_prevention 
FROM gcp_storage_buckets 
WHERE public_access_prevention != 'enforced';

-- Find GKE clusters by region
SELECT name, location, current_node_count, status
FROM gcp_container_clusters 
WHERE location LIKE 'us-central%';

-- Cross-service relationships
SELECT 
  i.name as instance_name,
  i.zone,
  c.name as cluster_name
FROM gcp_compute_instances i
JOIN gcp_container_clusters c ON i.project_id = c.project_id
WHERE i.labels ? 'gke-cluster';
```

## Architecture

### High-Performance Discovery

The GCP provider uses a two-tier architecture for optimal performance:

1. **Cloud Asset Inventory** (Primary): Provides comprehensive, fast resource discovery across entire organizations
2. **Standard GCP APIs** (Fallback): Used when Asset Inventory is unavailable or for detailed resource information

### Multi-Level Caching

- **Service Cache**: 24-hour TTL for discovered services
- **Resource Cache**: 15-minute TTL for resource listings  
- **Operation Cache**: 5-minute TTL for API operations

### Concurrent Processing

- Configurable concurrency (default: 20 concurrent operations)
- Rate limiting (100 requests/second with burst of 200)
- Per-project parallel scanning

## Troubleshooting

### Common Issues

#### "Permission denied" errors
```bash
# Check your authentication
gcloud auth list
gcloud auth application-default print-access-token

# Verify required APIs are enabled
gcloud services list --enabled
```

#### "Cloud Asset Inventory not available"
```bash
# Enable the API
gcloud services enable cloudasset.googleapis.com

# Check permissions
gcloud projects get-iam-policy YOUR_PROJECT_ID
```

#### "No projects found"
```bash
# List accessible projects
gcloud projects list

# Check Resource Manager permissions
gcloud iam roles describe roles/browser
```

### Debug Mode

Run with debug output:
```bash
export DEBUG=true
./plugins/build/gcp-provider --test-gcp
```

### Performance Optimization

#### For Large Organizations
```yaml
providers:
  gcp:
    max_concurrency: 50  # Increase for larger environments
    rate_limit: 200      # Requests per second
```

#### For Rate Limit Issues
```yaml
providers:
  gcp:
    max_concurrency: 5   # Reduce concurrent operations
    rate_limit: 50       # Lower request rate
```

## Security Considerations

- Use service accounts with minimal required permissions
- Enable audit logging for resource scanning activities
- Regularly rotate service account keys
- Consider using Workload Identity in GKE environments
- Store credentials securely (avoid hardcoding in configuration)

## Performance Benchmarks

| Environment | Resources | Scan Time | Method |
|-------------|-----------|-----------|---------|
| Single Project (100 resources) | 100 | 15 seconds | Asset Inventory |
| Multi-Project (1,000 resources) | 1,000 | 45 seconds | Asset Inventory |
| Organization (10,000 resources) | 10,000 | 3 minutes | Asset Inventory |
| Fallback API Mode | 100 | 60 seconds | Standard APIs |

## Contributing

1. Follow the existing code patterns
2. Add tests for new functionality
3. Update documentation for new features
4. Ensure proper error handling and logging

## Support

For issues, questions, or contributions:
- Create an issue in the Corkscrew repository
- Follow the debugging steps above
- Include relevant logs and configuration when reporting issues