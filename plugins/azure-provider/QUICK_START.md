# Azure Provider Quick Start Guide

## 🚀 5-Minute Setup

### Step 1: Prerequisites
```bash
# Ensure Azure CLI is installed and logged in
az login
az account show  # Verify you're logged in
```

### Step 2: Deploy Enterprise App
```bash
cd plugins/azure-provider
./deploy-corkscrew-enterprise-app.sh
```

**Save the output credentials:**
```bash
export AZURE_CLIENT_ID="your-app-id"
export AZURE_CLIENT_SECRET="your-client-secret"
export AZURE_TENANT_ID="your-tenant-id"
```

### Step 3: Test the Provider
```bash
# Build the provider
go build -o azure-provider .

# Test basic functionality
go test -run TestAzureProviderImplementation -v

# Test management groups
./test-management-groups.sh
```

### Step 4: Initialize with Management Group Scope
```bash
# Find your root management group
ROOT_MG=$(az account management-group list --query "[?properties.details.parent==null].name" -o tsv | head -1)

# Initialize Corkscrew with tenant-wide scope
corkscrew azure init \
  --client-id $AZURE_CLIENT_ID \
  --client-secret $AZURE_CLIENT_SECRET \
  --tenant-id $AZURE_TENANT_ID \
  --scope-type management_group \
  --scope-id $ROOT_MG
```

### Step 5: Start Discovering Resources
```bash
# Discover all services (auto-discovery via Resource Graph)
corkscrew azure discover

# List all resources across your tenant
corkscrew azure scan --all

# Query specific resource types
corkscrew azure query "Resources | where type == 'microsoft.compute/virtualmachines'"
```

## 🎯 Common Use Cases

### Enterprise Deployment
```bash
# For large organizations with multiple management groups
./deploy-corkscrew-enterprise-app.sh
# ✅ Deploys with tenant-wide Reader permissions
# ✅ Configures proper RBAC at management group level
# ✅ Sets up audit logging and compliance tracking
```

### Development/Testing
```bash
# For single subscription testing
export AZURE_SUBSCRIPTION_ID="your-dev-subscription"
corkscrew azure init --scope-type subscription --scope-id $AZURE_SUBSCRIPTION_ID
```

### Multi-Subscription Management
```bash
# Automatically discovers all accessible subscriptions
corkscrew azure discover-management-groups
# ✅ Maps entire management group hierarchy
# ✅ Identifies all subscriptions in scope
# ✅ Optimizes Resource Graph queries across subscriptions
```

## 🔧 Configuration Examples

### Basic Configuration
```yaml
# ~/.corkscrew/config.yaml
providers:
  azure:
    scope_type: "management_group"
    scope_id: "tenant-root-group"
    enable_resource_graph: true
    max_concurrency: 10
```

### Advanced Configuration
```yaml
# ~/.corkscrew/config.yaml
providers:
  azure:
    # Scope settings
    scope_type: "management_group"
    scope_id: "mg-production"
    
    # Performance settings
    max_concurrency: 20
    enable_streaming: true
    cache_ttl: "12h"
    
    # Resource Graph settings
    enable_resource_graph: true
    query_timeout: "5m"
    batch_size: 1000
    
    # Database settings
    enable_database: true
    database_path: "~/.corkscrew/db/azure.duckdb"
    
    # Filtering
    include_services: ["compute", "storage", "network"]
    exclude_resource_types: ["microsoft.insights/components"]
```

## 📊 Key Commands

### Discovery Commands
```bash
# Auto-discover all services and resource types
corkscrew azure discover

# Discover management group hierarchy
corkscrew azure discover-management-groups

# Test Resource Graph connectivity
corkscrew azure test-resource-graph
```

### Scanning Commands
```bash
# Scan all resources in current scope
corkscrew azure scan --all

# Scan specific services
corkscrew azure scan --services compute,storage,network

# Scan with filters
corkscrew azure scan --location eastus --resource-group production
```

### Query Commands
```bash
# Execute custom KQL queries
corkscrew azure query "Resources | where location == 'eastus' | count"

# Query with output formatting
corkscrew azure query "Resources | project name, type, location" --output table

# Save query results
corkscrew azure query "Resources | where type contains 'microsoft.compute'" --output json > vms.json
```

### Management Commands
```bash
# Change scope to different management group
corkscrew azure set-scope --type management_group --id mg-development

# Change scope to specific subscription
corkscrew azure set-scope --type subscription --id sub-12345

# View current scope and permissions
corkscrew azure status
```

## 🔍 Troubleshooting

### Common Issues

**1. "Management group not found"**
```bash
# Check available management groups
az account management-group list

# Verify permissions
az role assignment list --assignee $(az ad signed-in-user show --query id -o tsv)
```

**2. "Resource Graph query failed"**
```bash
# Test Resource Graph access
az graph query -q "Resources | count"

# Check subscription access
az account list --query "[].{Name:name, State:state}"
```

**3. "Enterprise app deployment failed"**
```bash
# Check permissions for app registration
az ad app permission list --id $(az ad signed-in-user show --query id -o tsv)

# Verify you have Application Administrator role
az ad directory-role member list --role "Application Administrator"
```

### Debug Mode
```bash
# Enable debug logging
export CORKSCREW_DEBUG=true
export AZURE_PROVIDER_LOG_LEVEL=debug

# Run with verbose output
corkscrew azure discover --debug --verbose
```

## 🎓 Best Practices

### Security
- ✅ Use managed identity when running on Azure
- ✅ Rotate client secrets regularly (every 6 months)
- ✅ Use least privilege principle for role assignments
- ✅ Monitor enterprise app sign-ins and usage

### Performance
- ✅ Use management group scoping for better performance
- ✅ Enable Resource Graph for bulk operations
- ✅ Adjust concurrency based on your tenant size
- ✅ Use caching for frequently accessed data

### Operations
- ✅ Set up monitoring for the enterprise app
- ✅ Configure alerts for permission changes
- ✅ Regular backup of configuration and schemas
- ✅ Document your management group structure

## 📈 Monitoring

### Key Metrics to Track
```bash
# Resource discovery performance
corkscrew azure metrics --metric discovery_time

# Resource Graph query performance
corkscrew azure metrics --metric query_performance

# Coverage statistics
corkscrew azure metrics --metric resource_coverage
```

### Health Checks
```bash
# Verify provider health
corkscrew azure health-check

# Test all components
corkscrew azure test --comprehensive

# Validate permissions
corkscrew azure validate-permissions
```

## 🆘 Getting Help

### Self-Service
1. Check this Quick Start guide
2. Review the full [README.md](README.md)
3. Run diagnostic commands: `corkscrew azure diagnose`
4. Check logs: `~/.corkscrew/logs/azure-provider.log`

### Community Support
- GitHub Issues: Report bugs and feature requests
- Documentation: Comprehensive guides and API reference
- Examples: Sample configurations and use cases

### Enterprise Support
- Priority support for enterprise customers
- Custom deployment assistance
- Performance optimization consulting
- Compliance and security reviews

---

**🎉 Congratulations! You now have enterprise-grade, tenant-wide Azure resource discovery running in under 5 minutes!**

*Next steps: Explore advanced features in the [full README](README.md) or start building custom queries and dashboards.*
