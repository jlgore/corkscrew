# Azure SDK Analyzer

A comprehensive tool that automatically discovers and analyzes Azure resources by parsing the Azure SDK for Go using AST (Abstract Syntax Tree) analysis.

## Features

- **Automatic SDK Management**: Clones or updates the Azure SDK for Go repository
- **Comprehensive Analysis**: Parses all services under `sdk/resourcemanager/*`
- **Operation Detection**: Identifies List, Get, Create, Update, Delete patterns
- **Resource Extraction**: Discovers resource types with full ARM paths
- **Property Analysis**: Extracts property structures from response types
- **Metadata Extraction**: Service names, namespaces, API versions, parameters
- **Flexible Filtering**: Analyze specific services or all available services

## Installation

```bash
# Build the analyzer
go build -o analyze-azure-sdk ./cmd/analyze-azure-sdk

# Or use the test script
./scripts/test-azure-sdk-analyzer.sh
```

## Usage

### Basic Usage

```bash
# Analyze all services (downloads SDK automatically)
./analyze-azure-sdk -update -verbose

# Analyze specific services
./analyze-azure-sdk -services "compute,storage,network" -verbose

# Use existing SDK clone
./analyze-azure-sdk -sdk-path ./my-azure-sdk -output my-analysis.json
```

### Command Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-sdk-path` | Path to Azure SDK for Go repository | `./azure-sdk-for-go` |
| `-output` | Output file for analysis results | `azure-sdk-analysis.json` |
| `-update` | Clone or update Azure SDK repository | `false` |
| `-verbose` | Enable verbose logging | `false` |
| `-services` | Comma-separated list of services to analyze | `""` (all) |

### Examples

```bash
# Quick analysis of compute service only
./analyze-azure-sdk -services compute -update -verbose

# Analyze storage and network services
./analyze-azure-sdk -services "storage,network" -output storage-network.json

# Full analysis with custom SDK path
./analyze-azure-sdk -sdk-path /path/to/azure-sdk-for-go -output full-analysis.json -verbose
```

## Detected Patterns

The analyzer identifies these Azure SDK patterns:

### List Operations
```go
func (client *VirtualMachinesClient) NewListPager(...) *runtime.Pager[...]
func (client *VirtualMachinesClient) NewListByResourceGroupPager(...) *runtime.Pager[...]
```

### Get Operations  
```go
func (client *VirtualMachinesClient) Get(ctx context.Context, ...) (...Response, error)
```

### Create/Update Operations
```go
func (client *VirtualMachinesClient) BeginCreateOrUpdate(...) (*runtime.Poller[...], error)
```

### Delete Operations
```go
func (client *VirtualMachinesClient) BeginDelete(...) (*runtime.Poller[...], error)
```

## Output Format

The analyzer outputs comprehensive JSON with this structure:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "sdkVersion": "v68.0.0",
  "services": [
    {
      "name": "compute",
      "namespace": "Microsoft.Compute",
      "package": "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5",
      "version": "v5",
      "resources": [
        {
          "type": "virtualMachines",
          "armType": "Microsoft.Compute/virtualMachines",
          "operations": {
            "list": {
              "method": "NewListPager",
              "supportsResourceGroup": true,
              "responseType": "VirtualMachine",
              "isPaginated": true,
              "parameters": [
                {
                  "name": "resourceGroupName",
                  "type": "string",
                  "required": true
                }
              ]
            },
            "get": {
              "method": "Get",
              "parameters": [
                "resourceGroupName",
                "vmName"
              ]
            },
            "create": {
              "method": "BeginCreateOrUpdate",
              "responseType": "*runtime.Poller[VirtualMachinesClientCreateOrUpdateResponse]"
            }
          },
          "properties": {
            "location": {
              "type": "string",
              "required": true
            },
            "properties.hardwareProfile.vmSize": {
              "type": "string",
              "description": "Specifies the size of the virtual machine"
            },
            "properties.storageProfile.osDisk.diskSizeGB": {
              "type": "int32",
              "description": "Specifies the size of the operating system disk in gigabytes"
            }
          },
          "metadata": {
            "clientType": "VirtualMachinesClient",
            "hasListOperation": true,
            "hasGetOperation": true,
            "hasCreateOperation": true
          }
        }
      ],
      "metadata": {
        "packagePath": "/sdk/resourcemanager/compute/armcompute",
        "totalResources": 15,
        "totalOperations": 45
      }
    }
  ],
  "summary": {
    "totalServices": 25,
    "totalResources": 150,
    "totalOperations": 750,
    "analysisTimeMs": 5420
  }
}
```

## Advanced Features

### Service Discovery
- Automatically discovers all services in `sdk/resourcemanager/*`
- Maps service names to Microsoft namespaces
- Extracts package information and API versions

### Operation Classification
- **List Operations**: `NewListPager`, `NewListByResourceGroupPager`
- **Get Operations**: `Get` methods (single resource)
- **Create Operations**: `BeginCreateOrUpdate`, `CreateOrUpdate`
- **Update Operations**: `BeginUpdate`, `Update`
- **Delete Operations**: `BeginDelete`, `Delete`

### Property Extraction
- Analyzes response types and struct definitions
- Extracts nested property paths (e.g., `properties.hardwareProfile.vmSize`)
- Identifies required vs optional properties
- Captures property descriptions from comments

### Resource Type Detection
- Converts operation names to resource types
- Handles special cases (VirtualMachine → virtualMachines)
- Generates ARM resource type paths
- Supports nested resource types

## Integration with Corkscrew

This analyzer integrates with the Corkscrew orchestrator to enable automatic Azure provider generation:

```go
// Use in orchestrator integration
func (p *AzureProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
    // Run Azure SDK analyzer
    analyzer := NewAzureSDKAnalyzer(sdkPath)
    result, err := analyzer.AnalyzeSDK(targetServices)
    
    // Convert to orchestrator format
    return convertToAnalysisResponse(result), nil
}
```

## Performance

- **Fast Analysis**: Processes 20+ services in under 10 seconds
- **Incremental Updates**: Only re-analyzes changed services when SDK is updated
- **Memory Efficient**: Streams large files and cleans up AST after analysis
- **Concurrent Processing**: Analyzes multiple services in parallel

## Error Handling

The analyzer gracefully handles:
- Missing or invalid SDK paths
- Malformed Go code in SDK
- Network issues during SDK cloning
- File system permission errors
- Invalid service names in filters

## Troubleshooting

### Common Issues

1. **SDK Clone Fails**
   ```bash
   # Ensure git is installed and network is available
   git --version
   ping github.com
   ```

2. **Permission Errors**
   ```bash
   # Ensure write permissions to SDK path
   chmod 755 ./azure-sdk-for-go
   ```

3. **No Services Found**
   ```bash
   # Verify SDK structure
   ls ./azure-sdk-for-go/sdk/resourcemanager/
   ```

4. **Analysis Timeout**
   ```bash
   # Analyze specific services instead of all
   ./analyze-azure-sdk -services "compute,storage" -verbose
   ```

### Debug Mode

Use `-verbose` flag for detailed logging:

```bash
./analyze-azure-sdk -services compute -verbose
```

Output includes:
- SDK cloning/updating progress
- Service discovery details
- AST parsing progress
- Operation detection results
- Property extraction details

## Contributing

To extend the analyzer:

1. **Add New Operation Patterns**: Update `classifyOperation()` function
2. **Improve Property Extraction**: Enhance `extractStructProperties()` 
3. **Add Service Mappings**: Update `generateNamespace()` function
4. **Support New Response Types**: Extend `analyzeResponseTypeProperties()`

## Testing

Run the comprehensive test suite:

```bash
# Run all tests
./scripts/test-azure-sdk-analyzer.sh

# Test specific service
./analyze-azure-sdk -services compute -verbose

# Validate output format
jq '.' azure-sdk-analysis.json
```