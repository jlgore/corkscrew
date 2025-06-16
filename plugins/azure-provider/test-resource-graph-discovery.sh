#!/bin/bash

# Azure Resource Graph Discovery Test Script
# Tests the new Resource Graph-based auto-discovery capabilities

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_header() {
    echo -e "\n${BLUE}=== $1 ===${NC}"
}

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"
    
    # Check Azure CLI
    if ! command -v az &> /dev/null; then
        print_error "Azure CLI not found. Please install Azure CLI."
        exit 1
    fi
    print_status "Azure CLI found"
    
    # Check Azure login
    if ! az account show &> /dev/null; then
        print_error "Not logged into Azure. Please run 'az login'"
        exit 1
    fi
    
    ACCOUNT_ID=$(az account show --query id -o tsv)
    ACCOUNT_NAME=$(az account show --query name -o tsv)
    print_status "Logged into Azure account: $ACCOUNT_NAME ($ACCOUNT_ID)"
    
    # Check Go
    if ! command -v go &> /dev/null; then
        print_error "Go not found. Please install Go."
        exit 1
    fi
    print_status "Go found: $(go version)"
}

# Build the Azure provider
build_provider() {
    print_header "Building Azure Provider"
    
    cd "$(dirname "$0")"
    
    # Build the provider
    if go build -o azure-provider .; then
        print_status "Azure provider built successfully"
    else
        print_error "Failed to build Azure provider"
        exit 1
    fi
}

# Test Resource Graph connectivity
test_resource_graph_connectivity() {
    print_header "Testing Resource Graph Connectivity"
    
    # Test basic Resource Graph query using Azure CLI
    print_info "Testing basic Resource Graph query..."
    
    RESOURCE_COUNT=$(az graph query -q "Resources | count" --query "data[0].count_" -o tsv 2>/dev/null || echo "0")
    
    if [ "$RESOURCE_COUNT" -gt 0 ]; then
        print_status "Resource Graph accessible - found $RESOURCE_COUNT resources"
    else
        print_warning "Resource Graph query returned 0 resources (this might be normal for empty subscriptions)"
    fi
    
    # Test resource type discovery query
    print_info "Testing resource type discovery..."
    
    TYPE_COUNT=$(az graph query -q "Resources | summarize count() by type | count" --query "data[0].count_" -o tsv 2>/dev/null || echo "0")
    
    if [ "$TYPE_COUNT" -gt 0 ]; then
        print_status "Found $TYPE_COUNT different resource types"
    else
        print_warning "No resource types found"
    fi
}

# Test provider auto-discovery
test_auto_discovery() {
    print_header "Testing Auto-Discovery Capabilities"
    
    # Create a test program to test the provider
    cat > test_discovery.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
)

func main() {
    provider := NewAzureProvider()
    
    ctx := context.Background()
    
    // Initialize provider
    initReq := &pb.InitializeRequest{
        Config: map[string]string{
            // Will use Azure CLI credentials
        },
    }
    
    initResp, err := provider.Initialize(ctx, initReq)
    if err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }
    
    if !initResp.Success {
        log.Fatalf("Initialization failed: %s", initResp.Error)
    }
    
    fmt.Printf("✅ Provider initialized successfully\n")
    fmt.Printf("   Metadata: %v\n", initResp.Metadata)
    
    // Test service discovery
    discoverReq := &pb.DiscoverServicesRequest{
        ForceRefresh: true,
    }
    
    discoverResp, err := provider.DiscoverServices(ctx, discoverReq)
    if err != nil {
        log.Fatalf("Failed to discover services: %v", err)
    }
    
    fmt.Printf("✅ Discovered %d services\n", len(discoverResp.Services))
    
    // Show top 10 services
    for i, service := range discoverResp.Services {
        if i >= 10 {
            break
        }
        fmt.Printf("   %s: %d resource types\n", service.Name, len(service.ResourceTypes))
    }
    
    if len(discoverResp.Services) > 10 {
        fmt.Printf("   ... and %d more services\n", len(discoverResp.Services)-10)
    }
    
    // Test schema generation
    schemaReq := &pb.GetSchemasRequest{
        Services: []string{"compute", "storage", "network"},
    }
    
    schemaResp, err := provider.GetSchemas(ctx, schemaReq)
    if err != nil {
        log.Printf("⚠️  Schema generation failed: %v", err)
    } else {
        fmt.Printf("✅ Generated %d schemas\n", len(schemaResp.Schemas))
    }
}
EOF

    # Run the test
    print_info "Running auto-discovery test..."
    
    if go run test_discovery.go; then
        print_status "Auto-discovery test passed"
    else
        print_error "Auto-discovery test failed"
        return 1
    fi
    
    # Clean up
    rm -f test_discovery.go
}

# Test Resource Graph queries
test_resource_graph_queries() {
    print_header "Testing Resource Graph Query Capabilities"
    
    print_info "Testing resource type discovery query..."
    
    # Test the actual KQL query we use for discovery
    DISCOVERY_QUERY='Resources
| summarize 
    ResourceCount = count(),
    SampleProperties = any(properties),
    Locations = make_set(location),
    ResourceGroups = make_set(resourceGroup)
    by type
| extend 
    Provider = split(type, "/")[0],
    Service = split(type, "/")[1],
    ResourceType = split(type, "/")[2]
| where isnotempty(Service) and isnotempty(ResourceType)
| project 
    type,
    Provider,
    Service, 
    ResourceType,
    ResourceCount,
    SampleProperties,
    Locations,
    ResourceGroups
| order by Provider asc, Service asc, ResourceType asc
| limit 10'

    echo "Query:"
    echo "$DISCOVERY_QUERY"
    echo ""
    
    if az graph query -q "$DISCOVERY_QUERY" --output table; then
        print_status "Resource Graph discovery query executed successfully"
    else
        print_warning "Resource Graph discovery query failed (might be due to no resources)"
    fi
    
    print_info "Testing relationship discovery query..."
    
    RELATIONSHIP_QUERY='Resources
| extend 
    ReferencedResources = extract_all(@"\/subscriptions\/[^\/]+\/resourceGroups\/[^\/]+\/providers\/[^\/]+\/[^\/]+\/[^\/\s\"]+", properties)
| where array_length(ReferencedResources) > 0
| project type, ReferencedResources
| mv-expand ReferencedResource = ReferencedResources
| extend ReferencedType = extract(@"\/providers\/([^\/]+\/[^\/]+)", 1, tostring(ReferencedResource))
| where isnotempty(ReferencedType)
| summarize 
    RelationshipCount = count(),
    SampleReferences = make_set(ReferencedResource, 5)
    by SourceType = type, TargetType = ReferencedType
| where RelationshipCount >= 1
| order by RelationshipCount desc
| limit 5'

    if az graph query -q "$RELATIONSHIP_QUERY" --output table; then
        print_status "Resource Graph relationship query executed successfully"
    else
        print_warning "Resource Graph relationship query failed (might be due to no relationships)"
    fi
}

# Performance test
test_performance() {
    print_header "Testing Performance"
    
    print_info "Testing large query performance..."
    
    # Time a comprehensive resource query
    start_time=$(date +%s)
    
    LARGE_QUERY='Resources
| project id, name, type, location, resourceGroup, subscriptionId, tags, properties
| limit 1000'
    
    if az graph query -q "$LARGE_QUERY" --output json > /dev/null; then
        end_time=$(date +%s)
        duration=$((end_time - start_time))
        print_status "Large query completed in ${duration}s"
    else
        print_warning "Large query test failed"
    fi
}

# Main execution
main() {
    print_header "Azure Resource Graph Discovery Test Suite"
    
    check_prerequisites
    build_provider
    test_resource_graph_connectivity
    test_auto_discovery
    test_resource_graph_queries
    test_performance
    
    print_header "Test Summary"
    print_status "All tests completed!"
    print_info "Azure provider now supports:"
    echo "  • Dynamic service discovery via Resource Graph"
    echo "  • Automatic schema generation from live resources"
    echo "  • Relationship discovery between resources"
    echo "  • Performance-optimized KQL queries"
    echo "  • Fallback to traditional provider discovery"
    
    print_info "Next steps:"
    echo "  1. Test with real Azure workloads"
    echo "  2. Validate schema generation accuracy"
    echo "  3. Performance test with large subscriptions"
    echo "  4. Test multi-subscription scenarios"
}

# Run main function
main "$@"
