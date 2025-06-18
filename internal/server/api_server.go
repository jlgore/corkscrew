package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jlgore/corkscrew/internal/client"
	pb "github.com/jlgore/corkscrew/internal/proto"
	idmsdiscovery "github.com/jlgore/corkscrew/pkg/idmsdiscovery"
	"github.com/jlgore/corkscrew/pkg/query"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type APIServer struct {
	pb.UnimplementedCorkscrewAPIServer
	startTime      time.Time
	requestCount   int64
	errorCount     int64
	dbPath         string
	queryEngine    query.QueryEngine
	idmsDiscovery  *idmsdiscovery.IDMSDiscovery
}

func NewAPIServer() *APIServer {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".corkscrew", "corkscrew.db")
	
	// Initialize query engine
	engine, err := query.NewEngine(dbPath)
	if err != nil {
		log.Printf("Warning: Failed to initialize query engine: %v", err)
		engine = nil
	}
	
	// Initialize IDMS discovery
	idmsDiscovery := idmsdiscovery.NewIDMSDiscovery()
	
	return &APIServer{
		startTime:     time.Now(),
		dbPath:        dbPath,
		queryEngine:   engine,
		idmsDiscovery: idmsDiscovery,
	}
}

func (s *APIServer) ListProviders(ctx context.Context, req *pb.APIListProvidersRequest) (*pb.APIListProvidersResponse, error) {
	s.requestCount++
	
	providers := []string{"aws", "azure", "gcp", "kubernetes"}
	var providerInfos []*pb.APIProviderInfo
	
	for _, providerName := range providers {
		info := &pb.APIProviderInfo{
			Name:        providerName,
			Description: fmt.Sprintf("%s provider for Corkscrew", providerName),
		}
		
		if req.IncludeStatus {
			status := s.getProviderStatus(providerName)
			info.Status = status
			
			if status.Available && status.Initialized {
				// Get detailed info from provider
				if providerInfo, err := s.getDetailedProviderInfo(providerName); err == nil {
					info.Version = providerInfo.Version
					info.SupportedServices = providerInfo.SupportedServices
					info.Capabilities = providerInfo.Capabilities
				}
			}
		}
		
		providerInfos = append(providerInfos, info)
	}
	
	return &pb.APIListProvidersResponse{
		Providers: providerInfos,
	}, nil
}

func (s *APIServer) GetProviderInfo(ctx context.Context, req *pb.APIGetProviderInfoRequest) (*pb.APIProviderInfoResponse, error) {
	s.requestCount++
	
	if req.Provider == "" {
		s.errorCount++
		return nil, status.Error(codes.InvalidArgument, "provider name is required")
	}
	
	status := s.getProviderStatus(req.Provider)
	if !status.Available {
		return &pb.APIProviderInfoResponse{
			Error: fmt.Sprintf("Provider %s is not available: %s", req.Provider, status.Error),
		}, nil
	}
	
	providerInfo, err := s.getDetailedProviderInfo(req.Provider)
	if err != nil {
		s.errorCount++
		return &pb.APIProviderInfoResponse{
			Error: fmt.Sprintf("Failed to get provider info: %v", err),
		}, nil
	}
	
	info := &pb.APIProviderInfo{
		Name:              req.Provider,
		Version:           providerInfo.Version,
		Description:       providerInfo.Description,
		SupportedServices: providerInfo.SupportedServices,
		Capabilities:      providerInfo.Capabilities,
		Status:            status,
	}
	
	return &pb.APIProviderInfoResponse{
		ProviderInfo: info,
	}, nil
}

func (s *APIServer) ExecuteQuery(ctx context.Context, req *pb.APIExecuteQueryRequest) (*pb.APIExecuteQueryResponse, error) {
	s.requestCount++
	
	if req.Query == "" {
		s.errorCount++
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	
	if s.queryEngine == nil {
		s.errorCount++
		return &pb.APIExecuteQueryResponse{
			Error: "Query engine not available",
		}, nil
	}
	
	startTime := time.Now()
	
	// Execute the query
	rows, columns, err := query.ExecuteQuery(s.queryEngine, req.Query)
	if err != nil {
		s.errorCount++
		return &pb.APIExecuteQueryResponse{
			Error: fmt.Sprintf("Query execution failed: %v", err),
		}, nil
	}
	
	duration := time.Since(startTime)
	
	// Convert results
	var queryResults []*pb.APIQueryResult
	for _, row := range rows {
		values := make(map[string]string)
		for i, col := range columns {
			if i < len(row) && row[i] != nil {
				values[col] = fmt.Sprintf("%v", row[i])
			} else {
				values[col] = ""
			}
		}
		queryResults = append(queryResults, &pb.APIQueryResult{Values: values})
	}
	
	// Apply limit if specified
	if req.Limit > 0 && int(req.Limit) < len(queryResults) {
		queryResults = queryResults[:req.Limit]
	}
	
	return &pb.APIExecuteQueryResponse{
		Rows:            queryResults,
		Columns:         columns,
		RowCount:        int32(len(queryResults)),
		ExecutionTimeMs: duration.Milliseconds(),
	}, nil
}

func (s *APIServer) HealthCheck(ctx context.Context, req *pb.APIHealthCheckRequest) (*pb.APIHealthCheckResponse, error) {
	s.requestCount++
	
	return &pb.APIHealthCheckResponse{
		Status:    pb.APIHealthStatus_HEALTHY,
		Version:   "2.0.0",
		Timestamp: timestamppb.Now(),
		Details: map[string]string{
			"uptime": fmt.Sprintf("%.2f seconds", time.Since(s.startTime).Seconds()),
		},
	}, nil
}

func (s *APIServer) GetStatus(ctx context.Context, req *pb.APIGetStatusRequest) (*pb.APIGetStatusResponse, error) {
	s.requestCount++
	
	response := &pb.APIGetStatusResponse{
		OverallStatus: pb.APIHealthStatus_HEALTHY,
		Timestamp:     timestamppb.Now(),
	}
	
	// Get system stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	response.SystemStats = &pb.APISystemStats{
		UptimeSeconds:       int64(time.Since(s.startTime).Seconds()),
		MemoryUsageBytes:    int64(memStats.Alloc),
		ActiveConnections:   0, // TODO: Track active connections
		TotalRequests:       s.requestCount,
		TotalErrors:         s.errorCount,
	}
	
	// Check database status if requested
	if req.IncludeDatabase {
		dbStatus := &pb.APIDatabaseStatus{
			Path: s.dbPath,
		}
		
		if stat, err := os.Stat(s.dbPath); err == nil {
			dbStatus.Available = true
			dbStatus.SizeBytes = stat.Size()
		} else {
			dbStatus.Available = false
			dbStatus.Error = err.Error()
		}
		
		// TODO: Get table count and resource count from database
		dbStatus.TableCount = 0
		dbStatus.TotalResources = 0
		
		response.DatabaseStatus = dbStatus
	}
	
	// Check provider status if requested
	if req.IncludeProviders {
		providers := []string{"aws", "azure", "gcp", "kubernetes"}
		for _, provider := range providers {
			status := s.getProviderStatus(provider)
			response.ProviderStatus = append(response.ProviderStatus, status)
		}
	}
	
	return response, nil
}

func (s *APIServer) DiscoverIDMSServices(ctx context.Context, req *pb.APIDiscoverIDMSRequest) (*pb.APIDiscoverIDMSResponse, error) {
	s.requestCount++
	
	log.Printf("🔍 IDMS discovery requested via API")
	
	result, err := s.idmsDiscovery.DiscoverIDMSServices(ctx)
	if err != nil {
		s.errorCount++
		return &pb.APIDiscoverIDMSResponse{
			Success: false,
			Error:   fmt.Sprintf("IDMS discovery failed: %v", err),
		}, nil
	}
	
	// Convert discovery result to protobuf format
	var pbServices []*pb.APIIDMSService
	for _, service := range result.Services {
		pbService := &pb.APIIDMSService{
			Provider:     service.Provider,
			ServiceType:  service.ServiceType,
			Name:         service.Name,
			Region:       service.Region,
			Endpoint:     service.Endpoint,
			Status:       service.Status,
			DiscoveredAt: timestamppb.New(service.DiscoveredAt),
		}
		
		// Convert metadata to string map
		pbService.Metadata = make(map[string]string)
		for k, v := range service.Metadata {
			pbService.Metadata[k] = fmt.Sprintf("%v", v)
		}
		
		pbServices = append(pbServices, pbService)
	}
	
	var errors []string
	for _, errStr := range result.Errors {
		errors = append(errors, errStr)
	}
	
	return &pb.APIDiscoverIDMSResponse{
		Success:      true,
		Services:     pbServices,
		TotalFound:   int32(result.TotalFound),
		DurationMs:   result.Duration.Milliseconds(),
		Errors:       errors,
		DiscoveredAt: timestamppb.New(result.DiscoveredAt),
	}, nil
}

// Helper methods
func (s *APIServer) getProviderStatus(providerName string) *pb.APIProviderStatus {
	status := &pb.APIProviderStatus{
		LastCheck: timestamppb.Now(),
	}
	
	pc, err := client.NewPluginClient(providerName)
	if err != nil {
		status.Available = false
		status.Error = err.Error()
		return status
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		status.Available = true
		status.Initialized = false
		status.Error = err.Error()
		return status
	}
	
	// Try to get provider info to verify it's working
	_, err = provider.GetProviderInfo(context.Background(), &pb.Empty{})
	if err != nil {
		status.Available = true
		status.Initialized = false
		status.Error = err.Error()
		return status
	}
	
	status.Available = true
	status.Initialized = true
	return status
}

func (s *APIServer) getDetailedProviderInfo(providerName string) (*pb.ProviderInfoResponse, error) {
	pc, err := client.NewPluginClient(providerName)
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		return nil, err
	}
	
	return provider.GetProviderInfo(context.Background(), &pb.Empty{})
}

// StartAPIServer starts the gRPC API server on the specified port
func StartAPIServer(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	
	s := grpc.NewServer()
	
	server := NewAPIServer()
	pb.RegisterCorkscrewAPIServer(s, server)
	
	// Enable reflection for tools like grpcurl
	reflection.Register(s)
	
	log.Printf("🚀 Corkscrew gRPC API server starting on port %d", port)
	log.Printf("📋 Available services:")
	log.Printf("   - CorkscrewAPI (provider management, queries, health)")
	log.Printf("   - ServerReflection (for grpcurl/grpc tools)")
	log.Printf("\n💡 Try these commands:")
	log.Printf("   grpcurl -plaintext localhost:%d list", port)
	log.Printf("   grpcurl -plaintext localhost:%d corkscrew.api.CorkscrewAPI.HealthCheck", port)
	log.Printf("   grpcurl -plaintext localhost:%d corkscrew.api.CorkscrewAPI.ListProviders", port)
	
	// Start IDMS discovery in background
	go func() {
		log.Printf("\n🔍 Starting background IDMS service discovery...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		
		result, err := server.idmsDiscovery.DiscoverIDMSServices(ctx)
		if err != nil {
			log.Printf("❌ IDMS discovery failed: %v", err)
		} else {
			log.Printf("✅ IDMS discovery completed successfully")
			log.Printf("📊 Discovered %d IDMS services across %d providers:", result.TotalFound, len(result.ByProvider))
			for provider, count := range result.ByProvider {
				log.Printf("   - %s: %d services", provider, count)
			}
		}
		
		// Schedule periodic rediscovery every 30 minutes
		ticker := time.NewTicker(30 * time.Minute)
		go func() {
			defer ticker.Stop()
			for range ticker.C {
				log.Printf("🔄 Starting periodic IDMS rediscovery...")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				result, err := server.idmsDiscovery.DiscoverIDMSServices(ctx)
				cancel()
				
				if err != nil {
					log.Printf("❌ Periodic IDMS discovery failed: %v", err)
				} else {
					log.Printf("✅ Periodic IDMS discovery completed - found %d services", result.TotalFound)
				}
			}
		}()
	}()
	
	return s.Serve(lis)
}