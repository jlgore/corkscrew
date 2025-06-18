# Corkscrew MCP-to-gRPC Bridge Implementation Plan

## Overview

This revised implementation creates an MCP server that acts as a bridge to your existing gRPC API (`corkscrew serve`). This approach leverages your existing infrastructure and provides a clean separation of concerns.

## Architecture Design

### 1. MCP-to-gRPC Bridge Architecture

```
┌─────────────┐      MCP Protocol      ┌──────────────────┐      gRPC      ┌─────────────────┐
│     LLM     │ ◄──────────────────► │   MCP Bridge     │ ◄────────────► │ Corkscrew Serve │
│   (Claude)  │    (stdio/HTTP)      │  (Translator)    │               │   (gRPC API)    │
└─────────────┘                      └──────────────────┘               └─────────────────┘
                                             │
                                             ▼
                                     Tool Registry &
                                     Request Mapping
```

### 2. Revised Directory Structure

```
cmd/mcp-bridge/
├── main.go                    # MCP bridge entry point
├── client/
│   └── grpc_client.go        # gRPC client to connect to corkscrew serve
├── handlers/
│   ├── bridge.go             # Core MCP-to-gRPC translation logic
│   ├── tool_registry.go      # MCP tool definitions and mapping
│   └── stream_adapter.go     # Streaming response adapter
├── transport/
│   ├── stdio.go              # Stdio transport for local LLMs
│   └── http.go               # HTTP transport for remote access
└── config/
    └── tools.yaml            # Tool definitions and gRPC mappings
```

### 3. Core Components

#### 3.1 MCP Bridge Main (`cmd/mcp-bridge/main.go`)

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    
    "github.com/jlgore/corkscrew/cmd/mcp-bridge/client"
    "github.com/jlgore/corkscrew/cmd/mcp-bridge/handlers"
    "github.com/jlgore/corkscrew/cmd/mcp-bridge/transport"
    pb "github.com/jlgore/corkscrew/internal/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {
    var (
        transportType = flag.String("transport", "stdio", "Transport type (stdio, http)")
        grpcAddr     = flag.String("grpc-addr", "localhost:50051", "Corkscrew gRPC server address")
        httpPort     = flag.Int("port", 8080, "HTTP port for MCP server")
        configPath   = flag.String("config", "", "Path to tools configuration")
        verbose      = flag.Bool("verbose", false, "Verbose logging")
    )
    
    flag.Parse()
    
    // Connect to Corkscrew gRPC server
    conn, err := grpc.Dial(*grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("Failed to connect to Corkscrew gRPC server: %v", err)
    }
    defer conn.Close()
    
    // Create gRPC client
    grpcClient := pb.NewCloudProviderClient(conn)
    
    // Initialize bridge handler
    bridge := handlers.NewBridge(grpcClient, *verbose)
    
    // Load tool registry
    registry, err := handlers.LoadToolRegistry(*configPath)
    if err != nil {
        log.Fatalf("Failed to load tool registry: %v", err)
    }
    bridge.SetRegistry(registry)
    
    // Start MCP server with selected transport
    switch *transportType {
    case "stdio":
        server := transport.NewStdioServer(bridge, *verbose)
        if err := server.Start(); err != nil {
            log.Fatalf("Failed to start stdio server: %v", err)
        }
    case "http":
        server := transport.NewHTTPServer(bridge, *httpPort, *verbose)
        if err := server.Start(); err != nil {
            log.Fatalf("Failed to start HTTP server: %v", err)
        }
    default:
        log.Fatalf("Unknown transport type: %s", *transportType)
    }
}
```

#### 3.2 gRPC Client Wrapper (`client/grpc_client.go`)

```go
package client

import (
    "context"
    "fmt"
    "time"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
    "google.golang.org/grpc"
)

type CorkscrewClient struct {
    client pb.CloudProviderClient
    timeout time.Duration
}

func NewCorkscrewClient(client pb.CloudProviderClient) *CorkscrewClient {
    return &CorkscrewClient{
        client: client,
        timeout: 5 * time.Minute,
    }
}

// Wrapper methods that add context, timeout, and error handling

func (c *CorkscrewClient) ScanResources(ctx context.Context, provider string, services []string, region string, opts ScanOptions) (*ScanResult, error) {
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()
    
    // Initialize provider if needed
    initReq := &pb.InitializeRequest{
        Provider: provider,
        Config: map[string]string{
            "region": region,
        },
    }
    
    initResp, err := c.client.Initialize(ctx, initReq)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize provider: %w", err)
    }
    
    if !initResp.Success {
        return nil, fmt.Errorf("provider initialization failed: %s", initResp.Error)
    }
    
    // Perform batch scan
    scanReq := &pb.BatchScanRequest{
        Services:             services,
        Region:               region,
        IncludeRelationships: opts.IncludeRelationships,
        Filters:              opts.Filters,
    }
    
    scanResp, err := c.client.BatchScan(ctx, scanReq)
    if err != nil {
        return nil, fmt.Errorf("scan failed: %w", err)
    }
    
    return &ScanResult{
        Resources: scanResp.Resources,
        Stats:     scanResp.Stats,
        Errors:    scanResp.Errors,
    }, nil
}

func (c *CorkscrewClient) QueryDatabase(ctx context.Context, query string) (*QueryResult, error) {
    // This would call a gRPC method for database queries
    // You might need to add this to your proto definition
    return nil, fmt.Errorf("database query via gRPC not yet implemented")
}

// Add more wrapper methods for other operations...
```

#### 3.3 MCP-to-gRPC Bridge Handler (`handlers/bridge.go`)

```go
package handlers

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    
    "github.com/jlgore/corkscrew/cmd/mcp-bridge/client"
    pb "github.com/jlgore/corkscrew/internal/proto"
)

type Bridge struct {
    grpcClient pb.CloudProviderClient
    wrapper    *client.CorkscrewClient
    registry   *ToolRegistry
    verbose    bool
}

func NewBridge(grpcClient pb.CloudProviderClient, verbose bool) *Bridge {
    return &Bridge{
        grpcClient: grpcClient,
        wrapper:    client.NewCorkscrewClient(grpcClient),
        verbose:    verbose,
    }
}

func (b *Bridge) SetRegistry(registry *ToolRegistry) {
    b.registry = registry
}

// HandleToolCall is the main entry point for MCP tool calls
func (b *Bridge) HandleToolCall(ctx context.Context, toolName string, params json.RawMessage) (interface{}, error) {
    if b.verbose {
        log.Printf("Handling MCP tool call: %s", toolName)
    }
    
    tool, exists := b.registry.GetTool(toolName)
    if !exists {
        return nil, fmt.Errorf("unknown tool: %s", toolName)
    }
    
    // Route to appropriate handler based on tool type
    switch tool.Type {
    case "scan":
        return b.handleScanTool(ctx, tool, params)
    case "query":
        return b.handleQueryTool(ctx, tool, params)
    case "list":
        return b.handleListTool(ctx, tool, params)
    case "describe":
        return b.handleDescribeTool(ctx, tool, params)
    case "plugin":
        return b.handlePluginTool(ctx, tool, params)
    default:
        return nil, fmt.Errorf("unknown tool type: %s", tool.Type)
    }
}

func (b *Bridge) handleScanTool(ctx context.Context, tool *Tool, params json.RawMessage) (interface{}, error) {
    var req ScanRequest
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, fmt.Errorf("invalid scan parameters: %w", err)
    }
    
    // Translate MCP request to gRPC call
    scanReq := &pb.BatchScanRequest{
        Services:             req.Services,
        Region:               req.Region,
        IncludeRelationships: req.IncludeRelationships,
        Filters:              req.Filters,
    }
    
    // Call gRPC service
    resp, err := b.grpcClient.BatchScan(ctx, scanReq)
    if err != nil {
        return nil, fmt.Errorf("gRPC scan failed: %w", err)
    }
    
    // Transform response for MCP
    return map[string]interface{}{
        "success": true,
        "resources_found": len(resp.Resources),
        "stats": resp.Stats,
        "errors": resp.Errors,
    }, nil
}

// Implement other handler methods...

// GetToolList returns available tools for MCP discovery
func (b *Bridge) GetToolList() []ToolInfo {
    return b.registry.GetAllTools()
}
```

#### 3.4 Tool Registry Configuration (`config/tools.yaml`)

```yaml
tools:
  # Scanning Tools
  - name: scan_resources
    type: scan
    description: "Scan cloud resources and save to database"
    grpc_method: BatchScan
    parameters:
      provider:
        type: string
        required: true
        description: "Cloud provider (aws, azure, gcp)"
      services:
        type: array
        required: true
        description: "Services to scan"
      region:
        type: string
        required: true
        description: "Target region"
      include_relationships:
        type: boolean
        description: "Include resource relationships"
      filters:
        type: object
        description: "Resource filters"

  # Query Tools  
  - name: query_database
    type: query
    description: "Execute SQL query on resource database"
    grpc_method: ExecuteQuery  # You may need to add this to your gRPC service
    parameters:
      query:
        type: string
        required: true
        description: "SQL query to execute"
      format:
        type: string
        enum: ["json", "table", "csv"]
        default: "json"
        description: "Output format"

  # List Tools
  - name: list_resources
    type: list
    description: "List resources of a specific type"
    grpc_method: ListResources
    parameters:
      provider:
        type: string
        required: true
      service:
        type: string
        required: true
      resource_type:
        type: string
      region:
        type: string
        required: true

  # Service Discovery
  - name: discover_services
    type: discover
    description: "Discover available services for a provider"
    grpc_method: DiscoverServices
    parameters:
      provider:
        type: string
        required: true
      force_refresh:
        type: boolean
        default: false
```

### 4. Key Implementation Differences from REST Approach

#### 4.1 Direct gRPC Communication
- The MCP bridge connects directly to your `corkscrew serve` gRPC endpoint
- No need to reimplement business logic - just translate between protocols
- Leverages existing protobuf definitions

#### 4.2 Streaming Support
```go
// Stream adapter for gRPC streaming to MCP streaming
func (b *Bridge) handleStreamingScan(ctx context.Context, req *pb.StreamScanRequest, mcpStream MCPStream) error {
    stream, err := b.grpcClient.StreamScan(ctx, req)
    if err != nil {
        return err
    }
    
    for {
        resource, err := stream.Recv()
        if err == io.EOF {
            return mcpStream.Close()
        }
        if err != nil {
            return err
        }
        
        // Convert gRPC resource to MCP format
        mcpResource := convertResourceToMCP(resource)
        if err := mcpStream.Send(mcpResource); err != nil {
            return err
        }
    }
}
```

#### 4.3 Authentication Passthrough
```go
// Pass authentication from MCP to gRPC
func (b *Bridge) withAuth(ctx context.Context, mcpAuth string) context.Context {
    // Extract token from MCP auth
    token := extractToken(mcpAuth)
    
    // Add to gRPC metadata
    md := metadata.Pairs("authorization", "Bearer " + token)
    return metadata.NewOutgoingContext(ctx, md)
}
```

### 5. Implementation Phases

#### Phase 1: Basic Bridge (Week 1)
1. Create MCP bridge structure
2. Implement stdio transport
3. Create gRPC client wrapper
4. Implement core tool translations (scan, list, describe)
5. Test with local LLM

#### Phase 2: Enhanced Features (Week 2)
1. Add streaming support for large operations
2. Implement authentication passthrough
3. Add HTTP transport with SSE for streaming
4. Create comprehensive tool registry
5. Add request/response logging

#### Phase 3: Advanced Integration (Week 3)
1. Implement bidirectional streaming for real-time updates
2. Add caching layer for frequently accessed data
3. Create tool composition capabilities
4. Add metrics and monitoring
5. Implement circuit breakers for gRPC calls

### 6. Advantages of gRPC-Based Approach

1. **Protocol Efficiency**: Binary protocol is more efficient than REST
2. **Type Safety**: Protobuf ensures type safety across the bridge
3. **Streaming**: Native bidirectional streaming support
4. **Code Generation**: Can generate client code from proto files
5. **Existing Infrastructure**: Leverages your current gRPC implementation

### 7. Usage Examples

#### 7.1 Starting the MCP Bridge

```bash
# Start corkscrew gRPC server
corkscrew serve --port 50051

# In another terminal, start MCP bridge
corkscrew mcp-bridge --grpc-addr localhost:50051 --transport stdio

# Or with HTTP transport
corkscrew mcp-bridge --grpc-addr localhost:50051 --transport http --port 8080
```

#### 7.2 Configuration for Claude Desktop

```json
{
  "mcpServers": {
    "corkscrew": {
      "command": "corkscrew",
      "args": ["mcp-bridge", "--grpc-addr", "localhost:50051"],
      "env": {
        "CORKSCREW_MCP_MODE": "stdio"
      }
    }
  }
}
```

### 8. Extended gRPC Service Methods

You may need to add these methods to your gRPC service for full MCP support:

```proto
service CorkscrewService {
  // Existing methods from CloudProvider...
  
  // Database operations
  rpc ExecuteQuery(QueryRequest) returns (QueryResponse);
  rpc StreamQuery(QueryRequest) returns (stream QueryRow);
  
  // Plugin management
  rpc ListPlugins(Empty) returns (PluginListResponse);
  rpc BuildPlugin(BuildPluginRequest) returns (BuildPluginResponse);
  rpc GetPluginStatus(PluginStatusRequest) returns (PluginStatusResponse);
  
  // Analytics
  rpc GetScanHistory(ScanHistoryRequest) returns (ScanHistoryResponse);
  rpc GetResourceStats(ResourceStatsRequest) returns (ResourceStatsResponse);
  
  // Compliance
  rpc ExecuteCompliancePack(CompliancePackRequest) returns (CompliancePackResponse);
  rpc StreamComplianceResults(CompliancePackRequest) returns (stream ComplianceResult);
}
```

### 9. Error Handling Strategy

```go
// Unified error handling between gRPC and MCP
type ErrorTranslator struct {
    grpcToMCP map[codes.Code]MCPErrorCode
}

func (et *ErrorTranslator) TranslateError(err error) MCPError {
    if s, ok := status.FromError(err); ok {
        mcpCode := et.grpcToMCP[s.Code()]
        return MCPError{
            Code:    mcpCode,
            Message: s.Message(),
            Details: s.Details(),
        }
    }
    return MCPError{
        Code:    MCPErrorInternal,
        Message: err.Error(),
    }
}
```

### 10. Testing Strategy

1. **Unit Tests**: Mock gRPC client for bridge testing
2. **Integration Tests**: Test against real corkscrew serve
3. **End-to-End Tests**: Test with actual LLM connections
4. **Performance Tests**: Measure latency and throughput

This approach creates a clean, maintainable bridge that leverages your existing gRPC infrastructure while providing full MCP compatibility for LLM integration.
