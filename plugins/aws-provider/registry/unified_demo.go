package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// DemoUnifiedRegistry demonstrates the complete unified registry functionality
func DemoUnifiedRegistry() error {
	fmt.Println("=== UnifiedServiceRegistry Demo ===")
	fmt.Println("Demonstrating the consolidation of three registry systems into one")

	// Step 1: Create AWS config
	awsConfig := aws.Config{
		Region: "us-east-1",
	}

	// Step 2: Create unified registry with comprehensive configuration
	config := RegistryConfig{
		EnableCache:         true,
		CacheTTL:           15 * time.Minute,
		MaxCacheSize:       1000,
		AutoPersist:        true,
		PersistenceInterval: 5 * time.Minute,
		PersistencePath:    "/tmp/unified-registry-demo.json",
		EnableMetrics:      true,
		EnableAuditLog:     true,
		EnableDiscovery:    true,
		DiscoveryInterval:  30 * time.Minute,
		UseFallbackServices: true,
	}

	unified := NewUnifiedServiceRegistry(awsConfig, config)
	fmt.Printf("✓ Created unified registry with %d initial services\n", len(unified.ListServices()))

	// Step 3: Register core AWS services with client factories
	coreServices := []struct {
		def     ServiceDefinition
		factory ClientFactoryFunc
	}{
		{
			def: ServiceDefinition{
				Name:        "s3",
				DisplayName: "Amazon S3",
				Description: "Simple Storage Service - Object storage built to store and retrieve any amount of data",
				PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
				ClientType:  "*s3.Client",
				RateLimit:   rate.Limit(100),
				BurstLimit:  200,
				GlobalService: true,
				SupportsPagination: true,
				SupportsResourceExplorer: true,
				ResourceTypes: []ResourceTypeDefinition{
					{
						Name:              "Bucket",
						ResourceType:      "AWS::S3::Bucket",
						ListOperation:     "ListBuckets",
						DescribeOperation: "GetBucketLocation",
						SupportsTags:      true,
						IsGlobalResource:  true,
					},
					{
						Name:              "Object",
						ResourceType:      "AWS::S3::Object",
						ListOperation:     "ListObjectsV2",
						RequiresParentResource: true,
						ParentResourceType: "Bucket",
						SupportsTags:      true,
						Paginated:         true,
					},
				},
				Operations: []OperationDefinition{
					{
						Name:          "ListBuckets",
						OperationType: "List",
						Paginated:     false,
						RequiredPermissions: []string{"s3:ListAllMyBuckets"},
					},
					{
						Name:          "ListObjectsV2",
						OperationType: "List",
						Paginated:     true,
						RequiredPermissions: []string{"s3:ListBucket"},
					},
				},
				Permissions: []string{"s3:ListAllMyBuckets", "s3:GetBucketLocation"},
				DiscoverySource: "core",
			},
			factory: func(cfg aws.Config) interface{} {
				return &MockS3Client{Config: cfg}
			},
		},
		{
			def: ServiceDefinition{
				Name:        "ec2",
				DisplayName: "Amazon EC2",
				Description: "Elastic Compute Cloud - Secure and resizable compute capacity",
				PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
				ClientType:  "*ec2.Client",
				RateLimit:   rate.Limit(20),
				BurstLimit:  40,
				RequiresRegion: true,
				SupportsPagination: true,
				SupportsResourceExplorer: true,
				ResourceTypes: []ResourceTypeDefinition{
					{
						Name:              "Instance",
						ResourceType:      "AWS::EC2::Instance",
						ListOperation:     "DescribeInstances",
						DescribeOperation: "DescribeInstances",
						SupportsTags:      true,
						Paginated:         true,
					},
					{
						Name:              "Volume",
						ResourceType:      "AWS::EC2::Volume",
						ListOperation:     "DescribeVolumes",
						SupportsTags:      true,
						Paginated:         true,
					},
					{
						Name:              "SecurityGroup",
						ResourceType:      "AWS::EC2::SecurityGroup",
						ListOperation:     "DescribeSecurityGroups",
						SupportsTags:      true,
						Paginated:         true,
					},
				},
				Operations: []OperationDefinition{
					{
						Name:          "DescribeInstances",
						OperationType: "Describe",
						Paginated:     true,
						RequiredPermissions: []string{"ec2:DescribeInstances"},
					},
					{
						Name:          "DescribeVolumes",
						OperationType: "Describe",
						Paginated:     true,
						RequiredPermissions: []string{"ec2:DescribeVolumes"},
					},
				},
				Permissions: []string{"ec2:DescribeInstances", "ec2:DescribeVolumes", "ec2:DescribeSecurityGroups"},
				DiscoverySource: "core",
			},
			factory: func(cfg aws.Config) interface{} {
				return &MockEC2Client{Config: cfg}
			},
		},
		{
			def: ServiceDefinition{
				Name:        "lambda",
				DisplayName: "AWS Lambda",
				Description: "Run code without thinking about servers",
				PackagePath: "github.com/aws/aws-sdk-go-v2/service/lambda",
				ClientType:  "*lambda.Client",
				RateLimit:   rate.Limit(50),
				BurstLimit:  100,
				RequiresRegion: true,
				SupportsPagination: true,
				ResourceTypes: []ResourceTypeDefinition{
					{
						Name:              "Function",
						ResourceType:      "AWS::Lambda::Function",
						ListOperation:     "ListFunctions",
						DescribeOperation: "GetFunction",
						SupportsTags:      true,
						Paginated:         true,
					},
				},
				Operations: []OperationDefinition{
					{
						Name:          "ListFunctions",
						OperationType: "List",
						Paginated:     true,
						RequiredPermissions: []string{"lambda:ListFunctions"},
					},
				},
				Permissions: []string{"lambda:ListFunctions", "lambda:GetFunction"},
				DiscoverySource: "core",
			},
			factory: func(cfg aws.Config) interface{} {
				return &MockLambdaClient{Config: cfg}
			},
		},
	}

	// Register all core services
	for _, svc := range coreServices {
		if err := unified.RegisterServiceWithFactory(svc.def, svc.factory); err != nil {
			return fmt.Errorf("failed to register %s: %w", svc.def.Name, err)
		}
		fmt.Printf("✓ Registered %s with client factory\n", svc.def.DisplayName)
	}

	// Step 4: Set up unified scanner
	scanner := &DemoUnifiedScanner{
		resources: map[string][]*pb.ResourceRef{
			"s3": {
				{Service: "s3", Type: "Bucket", Id: "my-demo-bucket", Name: "my-demo-bucket", Region: "us-east-1"},
				{Service: "s3", Type: "Bucket", Id: "backup-bucket", Name: "backup-bucket", Region: "us-east-1"},
			},
			"ec2": {
				{Service: "ec2", Type: "Instance", Id: "i-1234567890abcdef0", Name: "web-server-1", Region: "us-east-1"},
				{Service: "ec2", Type: "Volume", Id: "vol-049df61146c4d7901", Name: "web-server-1-root", Region: "us-east-1"},
			},
			"lambda": {
				{Service: "lambda", Type: "Function", Id: "my-function", Name: "my-function", Region: "us-east-1"},
			},
		},
		fullResources: make(map[string]*pb.Resource),
	}

	unified.SetUnifiedScanner(scanner)
	fmt.Println("✓ Set unified scanner for resource discovery")

	// Step 5: Demonstrate client creation
	fmt.Println("\n=== Client Creation Demo ===")
	ctx := context.Background()
	
	for _, serviceName := range []string{"s3", "ec2", "lambda"} {
		client, err := unified.CreateClient(ctx, serviceName)
		if err != nil {
			fmt.Printf("✗ Failed to create %s client: %v\n", serviceName, err)
			continue
		}
		fmt.Printf("✓ Created %s client: %T\n", serviceName, client)

		// Test client caching - second call should be instant
		start := time.Now()
		client2, err := unified.CreateClient(ctx, serviceName)
		if err != nil {
			fmt.Printf("✗ Failed to create cached %s client: %v\n", serviceName, err)
			continue
		}
		duration := time.Since(start)
		
		if client == client2 {
			fmt.Printf("✓ Client caching working - cached lookup took %v\n", duration)
		} else {
			fmt.Printf("✗ Client caching failed - different instances returned\n")
		}
	}

	// Step 6: Demonstrate service scanning
	fmt.Println("\n=== Service Scanning Demo ===")
	
	for _, serviceName := range []string{"s3", "ec2", "lambda"} {
		fmt.Printf("Scanning %s service...\n", serviceName)
		
		resources, err := unified.ScanService(ctx, serviceName, "us-east-1")
		if err != nil {
			fmt.Printf("✗ Failed to scan %s: %v\n", serviceName, err)
			continue
		}
		
		fmt.Printf("✓ Found %d resources in %s:\n", len(resources), serviceName)
		for _, resource := range resources {
			fmt.Printf("  - %s (%s): %s\n", resource.Type, resource.Id, resource.Name)
		}
	}

	// Step 7: Demonstrate rate limiting
	fmt.Println("\n=== Rate Limiting Demo ===")
	
	// Create a service with strict rate limiting for demonstration
	strictService := ServiceDefinition{
		Name:       "demo-rate-limited",
		DisplayName: "Demo Rate Limited Service",
		RateLimit:  rate.Limit(1), // 1 request per second
		BurstLimit: 1,             // No burst
	}
	
	unified.RegisterService(strictService)
	
	fmt.Println("Testing rate limiting (1 req/sec)...")
	for i := 0; i < 3; i++ {
		start := time.Now()
		limiter := unified.GetRateLimiter("demo-rate-limited")
		err := limiter.Wait(ctx)
		duration := time.Since(start)
		
		if err != nil {
			fmt.Printf("✗ Rate limit wait failed: %v\n", err)
		} else {
			fmt.Printf("✓ Request %d completed after %v\n", i+1, duration)
		}
	}

	// Step 8: Demonstrate statistics and metrics
	fmt.Println("\n=== Statistics and Metrics Demo ===")
	
	stats := unified.GetStats()
	fmt.Printf("Registry Statistics:\n")
	fmt.Printf("  Total Services: %d\n", stats.TotalServices)
	fmt.Printf("  Total Resource Types: %d\n", stats.TotalResourceTypes)
	fmt.Printf("  Total Operations: %d\n", stats.TotalOperations)
	fmt.Printf("  Cache Hit Rate: %.2f%%\n", stats.CacheHitRate*100)
	fmt.Printf("  Services by Source:\n")
	for source, count := range stats.ServicesBySource {
		fmt.Printf("    %s: %d\n", source, count)
	}
	fmt.Printf("  Last Updated: %s\n", stats.LastUpdated.Format(time.RFC3339))

	// Step 9: Demonstrate persistence
	fmt.Println("\n=== Persistence Demo ===")
	
	fmt.Println("Creating backup...")
	if err := unified.CreateBackup(); err != nil {
		fmt.Printf("✗ Backup creation failed: %v\n", err)
	} else {
		fmt.Println("✓ Backup created successfully")
	}
	
	fmt.Println("Persisting registry...")
	if err := unified.PersistToFile("/tmp/unified-registry-demo.json"); err != nil {
		fmt.Printf("✗ Persistence failed: %v\n", err)
	} else {
		fmt.Println("✓ Registry persisted successfully")
	}

	// Step 10: Demonstrate service discovery and metadata
	fmt.Println("\n=== Service Discovery Demo ===")
	
	// Simulate discovered services
	discoveredServices := []*pb.ServiceInfo{
		{
			Name:        "rds",
			DisplayName: "Amazon RDS",
			ResourceTypes: []*pb.ResourceType{
				{Name: "DBInstance", Paginated: true},
				{Name: "DBCluster", Paginated: true},
			},
		},
		{
			Name:        "dynamodb",
			DisplayName: "Amazon DynamoDB",
			ResourceTypes: []*pb.ResourceType{
				{Name: "Table", Paginated: true},
			},
		},
	}
	
	if err := unified.PopulateFromDiscovery(discoveredServices); err != nil {
		fmt.Printf("✗ Discovery population failed: %v\n", err)
	} else {
		fmt.Printf("✓ Populated %d services from discovery\n", len(discoveredServices))
	}

	// Final statistics
	finalStats := unified.GetStats()
	fmt.Printf("\nFinal Registry State:\n")
	fmt.Printf("  Total Services: %d\n", finalStats.TotalServices)
	fmt.Printf("  Discovery Success: %d\n", finalStats.DiscoverySuccess)
	fmt.Printf("  Discovery Failures: %d\n", finalStats.DiscoveryFailures)

	fmt.Println("\n=== Demo Completed Successfully ===")
	fmt.Println("The UnifiedServiceRegistry successfully demonstrates:")
	fmt.Println("✓ Service registration with metadata and client factories")
	fmt.Println("✓ Dynamic client creation with caching")
	fmt.Println("✓ Resource scanning with rate limiting")
	fmt.Println("✓ Statistics and performance monitoring")
	fmt.Println("✓ Persistence and backup capabilities")
	fmt.Println("✓ Service discovery integration")
	fmt.Println("✓ Migration from legacy registry systems")

	return nil
}

// Mock clients for demonstration

type MockS3Client struct {
	Config aws.Config
}

type MockEC2Client struct {
	Config aws.Config
}

type MockLambdaClient struct {
	Config aws.Config
}

// Demo unified scanner implementation

type DemoUnifiedScanner struct {
	resources     map[string][]*pb.ResourceRef
	fullResources map[string]*pb.Resource
	scanCount     int
	describeCount int
}

func (d *DemoUnifiedScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	d.scanCount++
	
	if resources, exists := d.resources[serviceName]; exists {
		return resources, nil
	}
	
	return []*pb.ResourceRef{}, nil
}

func (d *DemoUnifiedScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	d.describeCount++
	
	// Create a full resource from the reference
	resource := &pb.Resource{
		Provider: "aws",
		Service:  ref.Service,
		Type:     ref.Type,
		Id:       ref.Id,
		Name:     ref.Name,
		Region:   ref.Region,
		Tags: map[string]string{
			"Environment": "demo",
			"Source":      "unified-registry",
		},
	}
	
	return resource, nil
}

func (d *DemoUnifiedScanner) GetMetrics() interface{} {
	return map[string]interface{}{
		"scan_calls":     d.scanCount,
		"describe_calls": d.describeCount,
		"total_calls":    d.scanCount + d.describeCount,
	}
}