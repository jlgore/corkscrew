package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// UnifiedScannerProvider provides access to the unified scanner
type UnifiedScannerProvider interface {
	ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error)
	GetMetrics() interface{} // Optional metrics support
}


// ScannerMetadata contains metadata about a scanner
type ScannerMetadata struct {
	ServiceName          string
	ResourceTypes        []string
	SupportsPagination   bool
	SupportsParallelScan bool
	RequiredPermissions  []string
	RateLimit           rate.Limit
	BurstLimit          int
}

// ScannerRegistry manages the unified scanner and rate limiting
type ScannerRegistry struct {
	unifiedScanner UnifiedScannerProvider
	mu             sync.RWMutex
	
	// Rate limiting per service
	limiters map[string]*rate.Limiter
	metadata map[string]*ScannerMetadata
	
	// Configuration
	defaultRateLimit  rate.Limit
	defaultBurstLimit int
}

// NewScannerRegistry creates a new scanner registry
func NewScannerRegistry() *ScannerRegistry {
	log.Printf("DEBUG: NewScannerRegistry() called")
	return &ScannerRegistry{
		metadata:          make(map[string]*ScannerMetadata),
		limiters:          make(map[string]*rate.Limiter),
		defaultRateLimit:  rate.Limit(10), // 10 requests per second
		defaultBurstLimit: 20,
	}
}

// RegisterServiceMetadata registers metadata for a service (for rate limiting)
func (r *ScannerRegistry) RegisterServiceMetadata(serviceName string, metadata *ScannerMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.metadata[serviceName] = metadata
	
	// Create rate limiter for the service
	rateLimit := r.defaultRateLimit
	burstLimit := r.defaultBurstLimit
	
	if metadata != nil {
		if metadata.RateLimit > 0 {
			rateLimit = metadata.RateLimit
		}
		if metadata.BurstLimit > 0 {
			burstLimit = metadata.BurstLimit
		}
	}
	
	r.limiters[serviceName] = rate.NewLimiter(rateLimit, burstLimit)
}

// GetMetadata returns metadata for a service scanner
func (r *ScannerRegistry) GetMetadata(serviceName string) (*ScannerMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	metadata, exists := r.metadata[serviceName]
	return metadata, exists
}

// ListServices returns all services with registered metadata
func (r *ScannerRegistry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	services := make([]string, 0, len(r.metadata))
	for service := range r.metadata {
		services = append(services, service)
	}
	return services
}

// GetRateLimiter returns the rate limiter for a service
func (r *ScannerRegistry) GetRateLimiter(serviceName string) *rate.Limiter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if limiter, exists := r.limiters[serviceName]; exists {
		return limiter
	}
	
	// Return default limiter if service not found
	return rate.NewLimiter(r.defaultRateLimit, r.defaultBurstLimit)
}

// ScanServiceUnified scans a single service using UnifiedScanner exclusively
func (r *ScannerRegistry) ScanServiceUnified(ctx context.Context, serviceName string, cfg aws.Config, region string) ([]*pb.Resource, error) {
	// Apply rate limiting
	limiter := r.GetRateLimiter(serviceName)
	if err := limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}
	
	r.mu.RLock()
	scanner := r.unifiedScanner
	r.mu.RUnlock()
	
	if scanner == nil {
		return nil, fmt.Errorf("unified scanner not configured")
	}
	
	// Performance tracking
	scanStart := time.Now()
	
	// Scan and enrich in one pass
	refs, err := scanner.ScanService(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("scan failed for %s: %w", serviceName, err)
	}
	
	scanDuration := time.Since(scanStart)
	log.Printf("Performance: %s scan phase completed in %v, found %d resources", serviceName, scanDuration, len(refs))
	
	// Convert refs to full resources with configuration
	enrichStart := time.Now()
	resources := make([]*pb.Resource, 0, len(refs))
	
	for i, ref := range refs {
		// Set region on ref if not already set
		if ref.Region == "" {
			ref.Region = region
		}
		
		resource, err := scanner.DescribeResource(ctx, ref)
		if err != nil {
			log.Printf("Failed to enrich resource %s: %v", ref.Id, err)
			// Create basic resource on enrichment failure
			resource = &pb.Resource{
				Provider: "aws",
				Service:  ref.Service,
				Type:     ref.Type,
				Id:       ref.Id,
				Name:     ref.Name,
				Region:   ref.Region,
				Tags:     make(map[string]string),
			}
			
			// Copy ARN from BasicAttributes if available
			if ref.BasicAttributes != nil {
				if arn, exists := ref.BasicAttributes["arn"]; exists {
					resource.Arn = arn
				}
			}
		}
		
		resources = append(resources, resource)
		
		// Log performance for every 10th resource
		if (i+1)%10 == 0 {
			avgTime := time.Since(enrichStart) / time.Duration(i+1)
			log.Printf("Performance: Enriched %d/%d resources, avg %v per resource", i+1, len(refs), avgTime)
		}
	}
	
	enrichDuration := time.Since(enrichStart)
	totalDuration := time.Since(scanStart)
	
	log.Printf("Performance: %s completed - Scan: %v, Enrich: %v, Total: %v for %d resources", 
		serviceName, scanDuration, enrichDuration, totalDuration, len(resources))
	
	return resources, nil
}

// ScanService is deprecated - use ScanServiceUnified
func (r *ScannerRegistry) ScanService(ctx context.Context, serviceName string, cfg aws.Config, region string) ([]*pb.Resource, error) {
	return r.ScanServiceUnified(ctx, serviceName, cfg, region)
}

// ScanMultipleServices scans multiple services concurrently
func (r *ScannerRegistry) ScanMultipleServices(ctx context.Context, services []string, cfg aws.Config, region string, concurrency int) (map[string][]*pb.Resource, map[string]error) {
	results := make(map[string][]*pb.Resource)
	errors := make(map[string]error)
	
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	
	startTime := time.Now()
	
	for _, service := range services {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			// Scan service
			resources, err := r.ScanServiceUnified(ctx, svc, cfg, region)
			
			// Store results
			mu.Lock()
			if err != nil {
				errors[svc] = err
			} else {
				results[svc] = resources
			}
			mu.Unlock()
		}(service)
	}
	
	wg.Wait()
	
	totalResources := 0
	for _, resources := range results {
		totalResources += len(resources)
	}
	
	log.Printf("Performance: Scanned %d services in %v, found %d total resources (%d errors)", 
		len(services), time.Since(startTime), totalResources, len(errors))
	
	return results, errors
}


// BatchScanner provides batch scanning capabilities
type BatchScanner struct {
	registry    *ScannerRegistry
	concurrency int
	timeout     time.Duration
}

// NewBatchScanner creates a new batch scanner
func NewBatchScanner(registry *ScannerRegistry, concurrency int) *BatchScanner {
	return &BatchScanner{
		registry:    registry,
		concurrency: concurrency,
		timeout:     5 * time.Minute,
	}
}

// ScanAll scans all registered services
func (b *BatchScanner) ScanAll(ctx context.Context, cfg aws.Config, region string) (*BatchScanResult, error) {
	services := b.registry.ListServices()
	return b.ScanServices(ctx, services, cfg, region)
}

// ScanServices scans specified services
func (b *BatchScanner) ScanServices(ctx context.Context, services []string, cfg aws.Config, region string) (*BatchScanResult, error) {
	// Create context with timeout
	scanCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	
	startTime := time.Now()
	
	// Scan services
	results, errors := b.registry.ScanMultipleServices(scanCtx, services, cfg, region, b.concurrency)
	
	// Compile results
	batchResult := &BatchScanResult{
		StartTime:      startTime,
		EndTime:        time.Now(),
		ServicesScanned: len(services),
		Resources:      make([]*pb.Resource, 0),
		Errors:         errors,
		ServiceStats:   make(map[string]*ServiceScanStats),
	}
	
	// Aggregate resources and stats
	for service, resources := range results {
		batchResult.Resources = append(batchResult.Resources, resources...)
		batchResult.TotalResources += len(resources)
		
		batchResult.ServiceStats[service] = &ServiceScanStats{
			ResourceCount: len(resources),
			Success:       true,
		}
	}
	
	// Add error stats
	for service, err := range errors {
		batchResult.TotalErrors++
		if stats, exists := batchResult.ServiceStats[service]; exists {
			stats.Success = false
			stats.Error = err.Error()
		} else {
			batchResult.ServiceStats[service] = &ServiceScanStats{
				Success: false,
				Error:   err.Error(),
			}
		}
	}
	
	batchResult.Duration = batchResult.EndTime.Sub(batchResult.StartTime)
	
	return batchResult, nil
}

// BatchScanResult contains results from a batch scan
type BatchScanResult struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	ServicesScanned int
	TotalResources  int
	TotalErrors     int
	Resources       []*pb.Resource
	Errors          map[string]error
	ServiceStats    map[string]*ServiceScanStats
}

// ServiceScanStats contains statistics for a single service scan
type ServiceScanStats struct {
	ResourceCount int
	Success       bool
	Error         string
}

// ScannerLoader loads and initializes scanners
type ScannerLoader struct {
	registry     *ScannerRegistry
	scannerPaths []string
}

// NewScannerLoader creates a new scanner loader
func NewScannerLoader(registry *ScannerRegistry) *ScannerLoader {
	return &ScannerLoader{
		registry:     registry,
		scannerPaths: []string{},
	}
}

// AddPath adds a path to search for scanners
func (l *ScannerLoader) AddPath(path string) {
	l.scannerPaths = append(l.scannerPaths, path)
}

// LoadAll loads service metadata for rate limiting (no actual scanners)
func (l *ScannerLoader) LoadAll() error {
	log.Printf("Scanner loader: Using UnifiedScanner for all services")
	
	// Register service metadata for rate limiting
	services := []struct {
		name       string
		rateLimit  rate.Limit
		burstLimit int
	}{
		{"s3", rate.Limit(100), 200},
		{"ec2", rate.Limit(20), 40},
		{"lambda", rate.Limit(50), 100},
		{"rds", rate.Limit(20), 40},
		{"iam", rate.Limit(10), 20},
		{"dynamodb", rate.Limit(25), 50},
		{"kms", rate.Limit(10), 20},
		{"cloudformation", rate.Limit(15), 30},
		{"sns", rate.Limit(30), 60},
		{"sqs", rate.Limit(30), 60},
		{"apigateway", rate.Limit(20), 40},
		{"ecs", rate.Limit(20), 40},
		{"eks", rate.Limit(10), 20},
		{"elb", rate.Limit(20), 40},
		{"elbv2", rate.Limit(20), 40},
		{"route53", rate.Limit(15), 30},
		{"cloudwatch", rate.Limit(25), 50},
		{"logs", rate.Limit(25), 50},
		{"secretsmanager", rate.Limit(10), 20},
		{"ssm", rate.Limit(20), 40},
		{"acm", rate.Limit(10), 20},
		{"apigatewayv2", rate.Limit(20), 40},
		{"elasticloadbalancingv2", rate.Limit(20), 40},
		// Add more services as needed
	}
	
	// Register metadata for each service
	for _, svc := range services {
		metadata := &ScannerMetadata{
			ServiceName:          svc.name,
			RateLimit:           svc.rateLimit,
			BurstLimit:          svc.burstLimit,
			SupportsPagination:   true,
			SupportsParallelScan: true,
			RequiredPermissions:  getPermissionsForService(svc.name),
			ResourceTypes:        getResourceTypesForService(svc.name),
		}
		
		l.registry.RegisterServiceMetadata(svc.name, metadata)
	}
	
	log.Printf("Registered rate limiters for %d services", len(services))
	return nil
}


// SetUnifiedScanner sets the unified scanner for dynamic resource discovery
func (r *ScannerRegistry) SetUnifiedScanner(scanner UnifiedScannerProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unifiedScanner = scanner
	log.Printf("DEBUG: UnifiedScanner set on registry")
}

// SetUnifiedProvider sets the unified scanner provider for fallback (legacy method)
func (r *ScannerRegistry) SetUnifiedProvider(provider UnifiedScannerProvider) {
	r.SetUnifiedScanner(provider)
}


// Helper functions for generated scanner metadata
func getResourceTypesForService(service string) []string {
	switch service {
	case "s3":
		return []string{"Bucket", "Object"}
	case "ec2":
		return []string{"Instance", "Volume", "Snapshot", "SecurityGroup", "VPC"}
	case "lambda":
		return []string{"Function", "Layer"}
	case "kms":
		return []string{"Key", "Alias"}
	case "rds":
		return []string{"DBInstance", "DBCluster"}
	case "iam":
		return []string{"User", "Role", "Policy"}
	case "dynamodb":
		return []string{"Table"}
	default:
		return []string{}
	}
}

func getPermissionsForService(service string) []string {
	switch service {
	case "s3":
		return []string{"s3:ListBuckets", "s3:GetBucket*", "s3:ListObjects*"}
	case "ec2":
		return []string{"ec2:Describe*", "ec2:List*"}
	case "lambda":
		return []string{"lambda:ListFunctions", "lambda:GetFunction*"}
	case "kms":
		return []string{"kms:ListKeys", "kms:DescribeKey", "kms:GetKeyPolicy"}
	case "rds":
		return []string{"rds:DescribeDBInstances", "rds:DescribeDBClusters"}
	case "iam":
		return []string{"iam:ListUsers", "iam:ListRoles", "iam:ListPolicies"}
	case "dynamodb":
		return []string{"dynamodb:ListTables", "dynamodb:DescribeTable"}
	case "cloudformation":
		return []string{"cloudformation:ListStacks", "cloudformation:DescribeStacks"}
	case "sns":
		return []string{"sns:ListTopics", "sns:GetTopicAttributes"}
	case "sqs":
		return []string{"sqs:ListQueues", "sqs:GetQueueAttributes"}
	case "apigateway":
		return []string{"apigateway:GET"}
	case "ecs":
		return []string{"ecs:ListClusters", "ecs:DescribeClusters", "ecs:ListServices"}
	case "eks":
		return []string{"eks:ListClusters", "eks:DescribeCluster"}
	case "elb":
		return []string{"elasticloadbalancing:DescribeLoadBalancers"}
	case "elbv2":
		return []string{"elasticloadbalancing:DescribeLoadBalancers", "elasticloadbalancing:DescribeTargetGroups"}
	case "route53":
		return []string{"route53:ListHostedZones", "route53:ListResourceRecordSets"}
	case "cloudwatch":
		return []string{"cloudwatch:ListMetrics", "cloudwatch:DescribeAlarms"}
	case "logs":
		return []string{"logs:DescribeLogGroups", "logs:DescribeLogStreams"}
	default:
		return []string{fmt.Sprintf("%s:Describe*", service), fmt.Sprintf("%s:List*", service)}
	}
}

func getRateLimitForService(service string) rate.Limit {
	switch service {
	case "s3":
		return rate.Limit(100)
	case "ec2":
		return rate.Limit(20)
	case "lambda":
		return rate.Limit(50)
	case "kms":
		return rate.Limit(10)
	case "rds":
		return rate.Limit(20)
	case "iam":
		return rate.Limit(10)
	case "dynamodb":
		return rate.Limit(25)
	default:
		return rate.Limit(10)
	}
}

func getBurstLimitForService(service string) int {
	return int(getRateLimitForService(service) * 2)
}