package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// ScannerFunc is the function signature for service scanners
type ScannerFunc func(ctx context.Context, cfg aws.Config, region string) ([]*pb.Resource, error)

// ServiceScanner represents a scanner for a specific AWS service
type ServiceScanner interface {
	// ServiceName returns the name of the AWS service
	ServiceName() string
	
	// ResourceTypes returns the types of resources this scanner can discover
	ResourceTypes() []string
	
	// Scan performs the resource discovery
	Scan(ctx context.Context, cfg aws.Config, region string) ([]*pb.Resource, error)
	
	// SupportsPagination indicates if the scanner supports pagination
	SupportsPagination() bool
	
	// SupportsResourceExplorer indicates if the scanner can use Resource Explorer
	SupportsResourceExplorer() bool
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

// ScannerRegistry manages all available service scanners
type ScannerRegistry struct {
	mu       sync.RWMutex
	scanners map[string]ServiceScanner
	metadata map[string]*ScannerMetadata
	
	// Rate limiting
	limiters map[string]*rate.Limiter
	
	// Configuration
	defaultRateLimit  rate.Limit
	defaultBurstLimit int
}

// NewScannerRegistry creates a new scanner registry
func NewScannerRegistry() *ScannerRegistry {
	return &ScannerRegistry{
		scanners:          make(map[string]ServiceScanner),
		metadata:          make(map[string]*ScannerMetadata),
		limiters:          make(map[string]*rate.Limiter),
		defaultRateLimit:  rate.Limit(10), // 10 requests per second
		defaultBurstLimit: 20,
	}
}

// Register adds a scanner to the registry
func (r *ScannerRegistry) Register(scanner ServiceScanner, metadata *ScannerMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	serviceName := scanner.ServiceName()
	if _, exists := r.scanners[serviceName]; exists {
		return fmt.Errorf("scanner for service %s already registered", serviceName)
	}
	
	r.scanners[serviceName] = scanner
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
	
	return nil
}

// RegisterFunc registers a scanner function
func (r *ScannerRegistry) RegisterFunc(serviceName string, scanFunc ScannerFunc, metadata *ScannerMetadata) error {
	scanner := &functionScanner{
		serviceName: serviceName,
		scanFunc:    scanFunc,
		metadata:    metadata,
	}
	return r.Register(scanner, metadata)
}

// Get returns a scanner for the specified service
func (r *ScannerRegistry) Get(serviceName string) (ServiceScanner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	scanner, exists := r.scanners[serviceName]
	return scanner, exists
}

// GetMetadata returns metadata for a service scanner
func (r *ScannerRegistry) GetMetadata(serviceName string) (*ScannerMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	metadata, exists := r.metadata[serviceName]
	return metadata, exists
}

// ListServices returns all registered service names
func (r *ScannerRegistry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	services := make([]string, 0, len(r.scanners))
	for service := range r.scanners {
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

// ScanService scans a single service with rate limiting
func (r *ScannerRegistry) ScanService(ctx context.Context, serviceName string, cfg aws.Config, region string) ([]*pb.Resource, error) {
	scanner, exists := r.Get(serviceName)
	if !exists {
		return nil, fmt.Errorf("scanner not found for service: %s", serviceName)
	}
	
	// Apply rate limiting
	limiter := r.GetRateLimiter(serviceName)
	if err := limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}
	
	// Perform scan
	return scanner.Scan(ctx, cfg, region)
}

// ScanMultipleServices scans multiple services concurrently
func (r *ScannerRegistry) ScanMultipleServices(ctx context.Context, services []string, cfg aws.Config, region string, concurrency int) (map[string][]*pb.Resource, map[string]error) {
	results := make(map[string][]*pb.Resource)
	errors := make(map[string]error)
	
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	
	for _, service := range services {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			// Scan service
			resources, err := r.ScanService(ctx, svc, cfg, region)
			
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
	return results, errors
}

// functionScanner wraps a scanner function to implement ServiceScanner
type functionScanner struct {
	serviceName string
	scanFunc    ScannerFunc
	metadata    *ScannerMetadata
}

func (f *functionScanner) ServiceName() string {
	return f.serviceName
}

func (f *functionScanner) ResourceTypes() []string {
	if f.metadata != nil {
		return f.metadata.ResourceTypes
	}
	return []string{}
}

func (f *functionScanner) Scan(ctx context.Context, cfg aws.Config, region string) ([]*pb.Resource, error) {
	return f.scanFunc(ctx, cfg, region)
}

func (f *functionScanner) SupportsPagination() bool {
	return f.metadata != nil && f.metadata.SupportsPagination
}

func (f *functionScanner) SupportsResourceExplorer() bool {
	// Function scanners typically don't support Resource Explorer
	return false
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

// LoadAll loads all available scanners
func (l *ScannerLoader) LoadAll() error {
	// This would typically load scanners from:
	// 1. Generated scanner code
	// 2. Plugin directories
	// 3. Built-in scanners
	
	// For now, we'll register some example scanners
	// In production, this would dynamically load from generated code
	
	// Example: Register EC2 scanner
	ec2Scanner := NewEC2Scanner()
	ec2Metadata := &ScannerMetadata{
		ServiceName:          "ec2",
		ResourceTypes:        []string{"Instance", "Volume", "Snapshot", "SecurityGroup", "VPC"},
		SupportsPagination:   true,
		SupportsParallelScan: true,
		RequiredPermissions:  []string{"ec2:Describe*"},
		RateLimit:           rate.Limit(20),
		BurstLimit:          40,
	}
	
	if err := l.registry.Register(ec2Scanner, ec2Metadata); err != nil {
		return fmt.Errorf("failed to register EC2 scanner: %w", err)
	}
	
	// Register more scanners...
	
	return nil
}

// Example EC2 scanner implementation
type EC2Scanner struct{}

func NewEC2Scanner() *EC2Scanner {
	return &EC2Scanner{}
}

func (s *EC2Scanner) ServiceName() string {
	return "ec2"
}

func (s *EC2Scanner) ResourceTypes() []string {
	return []string{"Instance", "Volume", "Snapshot", "SecurityGroup", "VPC"}
}

func (s *EC2Scanner) Scan(ctx context.Context, cfg aws.Config, region string) ([]*pb.Resource, error) {
	// This would use the generated scanner code
	// For now, return a placeholder
	return []*pb.Resource{}, nil
}

func (s *EC2Scanner) SupportsPagination() bool {
	return true
}

func (s *EC2Scanner) SupportsResourceExplorer() bool {
	return true
}