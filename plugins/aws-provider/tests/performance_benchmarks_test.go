//go:build integration
// +build integration

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/require"
)

// BenchmarkCloudProvider provides performance benchmarks for CloudProvider operations
func BenchmarkCloudProvider(b *testing.B) {
	// Skip if not running benchmarks
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	// Create test provider
	provider, err := createBenchmarkProvider(b, cfg)
	require.NoError(b, err)

	ctx := context.Background()

	// Benchmark CloudProvider operations
	b.Run("BatchScan", func(b *testing.B) {
		benchmarkBatchScan(b, provider, ctx)
	})

	b.Run("ListResources", func(b *testing.B) {
		benchmarkListResources(b, provider, ctx)
	})

	b.Run("DescribeResource", func(b *testing.B) {
		benchmarkDescribeResource(b, provider, ctx)
	})

	b.Run("UnifiedScannerOperations", func(b *testing.B) {
		benchmarkUnifiedScannerOperations(b, provider, ctx)
	})

	b.Run("ConcurrentScanning", func(b *testing.B) {
		benchmarkConcurrentScanning(b, provider, ctx)
	})
}

// BenchmarkUnifiedScanner provides benchmarks specifically for UnifiedScanner components
func BenchmarkUnifiedScanner(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	scanner, err := createBenchmarkScanner(b, cfg)
	require.NoError(b, err)

	ctx := context.Background()

	// Benchmark UnifiedScanner components
	b.Run("ReflectionOperationDiscovery", func(b *testing.B) {
		benchmarkReflectionOperationDiscovery(b, scanner)
	})

	b.Run("FieldClassification", func(b *testing.B) {
		benchmarkFieldClassification(b, scanner)
	})

	b.Run("ResourceTypeInference", func(b *testing.B) {
		benchmarkResourceTypeInference(b, scanner)
	})

	b.Run("ConfigurationCollection", func(b *testing.B) {
		benchmarkConfigurationCollection(b, scanner, ctx)
	})

	b.Run("ServiceScanning", func(b *testing.B) {
		benchmarkServiceScanning(b, scanner, ctx)
	})
}

// BenchmarkS3ConfigurationCollection benchmarks the critical S3 configuration collection
func BenchmarkS3ConfigurationCollection(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	scanner, err := createS3BenchmarkScanner(b, cfg)
	require.NoError(b, err)

	ctx := context.Background()

	// Get a test bucket for benchmarking
	buckets, err := scanner.ListBuckets(ctx)
	if err != nil || len(buckets) == 0 {
		b.Skip("No S3 buckets available for benchmarking")
	}

	testBucket := buckets[0].Name

	b.Run("S3BasicScan", func(b *testing.B) {
		benchmarkS3BasicScan(b, scanner, ctx, testBucket)
	})

	b.Run("S3ConfigurationCollection", func(b *testing.B) {
		benchmarkS3ConfigurationCollection(b, scanner, ctx, testBucket)
	})

	b.Run("S3ConfigurationOperations", func(b *testing.B) {
		benchmarkS3ConfigurationOperations(b, scanner, ctx, testBucket)
	})
}

func createBenchmarkProvider(b *testing.B, cfg aws.Config) (*BenchmarkCloudProvider, error) {
	return &BenchmarkCloudProvider{
		cfg:    cfg,
		region: "us-east-1",
	}, nil
}

func createBenchmarkScanner(b *testing.B, cfg aws.Config) (*BenchmarkUnifiedScanner, error) {
	return &BenchmarkUnifiedScanner{
		cfg:    cfg,
		region: "us-east-1",
	}, nil
}

func createS3BenchmarkScanner(b *testing.B, cfg aws.Config) (*S3BenchmarkScanner, error) {
	return &S3BenchmarkScanner{
		cfg:    cfg,
		region: "us-east-1",
	}, nil
}

func benchmarkBatchScan(b *testing.B, provider *BenchmarkCloudProvider, ctx context.Context) {
	services := []string{"s3", "ec2", "lambda"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.BatchScan(ctx, services)
		if err != nil {
			b.Logf("BatchScan iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkListResources(b *testing.B, provider *BenchmarkCloudProvider, ctx context.Context) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.ListResources(ctx, "s3")
		if err != nil {
			b.Logf("ListResources iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkDescribeResource(b *testing.B, provider *BenchmarkCloudProvider, ctx context.Context) {
	// Get a resource to describe
	resources, err := provider.ListResources(ctx, "s3")
	if err != nil || len(resources) == 0 {
		b.Skip("No resources available for DescribeResource benchmark")
	}

	resource := resources[0]
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.DescribeResource(ctx, resource.ID, resource.Type, "s3")
		if err != nil {
			b.Logf("DescribeResource iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkUnifiedScannerOperations(b *testing.B, provider *BenchmarkCloudProvider, ctx context.Context) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate UnifiedScanner operations
		_, err := provider.UnifiedScannerScan(ctx, "s3")
		if err != nil {
			b.Logf("UnifiedScanner operation %d failed: %v", i, err)
		}
	}
}

func benchmarkConcurrentScanning(b *testing.B, provider *BenchmarkCloudProvider, ctx context.Context) {
	services := []string{"s3", "ec2", "lambda", "iam"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.ConcurrentScan(ctx, services)
		if err != nil {
			b.Logf("Concurrent scan iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkReflectionOperationDiscovery(b *testing.B, scanner *BenchmarkUnifiedScanner) {
	testClient := &MockAWSClient{}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scanner.DiscoverOperations(testClient)
	}
}

func benchmarkFieldClassification(b *testing.B, scanner *BenchmarkUnifiedScanner) {
	testFields := []string{
		"Id", "InstanceId", "FunctionName", "BucketName", "Name", "ResourceName",
		"Arn", "ResourceArn", "State", "Status", "Region", "AvailabilityZone",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, field := range testFields {
			_ = scanner.ClassifyField(field)
		}
	}
}

func benchmarkResourceTypeInference(b *testing.B, scanner *BenchmarkUnifiedScanner) {
	testCases := []struct {
		service string
		field   string
	}{
		{"ec2", "Instances"}, {"ec2", "Volumes"}, {"s3", "Buckets"},
		{"lambda", "Functions"}, {"rds", "DBInstances"}, {"iam", "Users"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_ = scanner.InferResourceType(tc.service, tc.field)
		}
	}
}

func benchmarkConfigurationCollection(b *testing.B, scanner *BenchmarkUnifiedScanner, ctx context.Context) {
	mockResource := &BenchmarkResource{
		Service: "s3",
		Type:    "Bucket",
		ID:      "test-bucket",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.CollectConfiguration(ctx, mockResource)
		if err != nil {
			b.Logf("Configuration collection iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkServiceScanning(b *testing.B, scanner *BenchmarkUnifiedScanner, ctx context.Context) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.ScanService(ctx, "s3")
		if err != nil {
			b.Logf("Service scanning iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkS3BasicScan(b *testing.B, scanner *S3BenchmarkScanner, ctx context.Context, bucketName string) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.BasicScan(ctx, bucketName)
		if err != nil {
			b.Logf("S3 basic scan iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkS3ConfigurationCollection(b *testing.B, scanner *S3BenchmarkScanner, ctx context.Context, bucketName string) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.CollectConfiguration(ctx, bucketName)
		if err != nil {
			b.Logf("S3 configuration collection iteration %d failed: %v", i, err)
		}
	}
}

func benchmarkS3ConfigurationOperations(b *testing.B, scanner *S3BenchmarkScanner, ctx context.Context, bucketName string) {
	operations := []string{
		"GetBucketEncryption", "GetBucketVersioning", "GetBucketLogging",
		"GetPublicAccessBlock", "GetBucketLocation", "GetBucketTagging",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, operation := range operations {
			_, err := scanner.ExecuteConfigOperation(ctx, bucketName, operation)
			if err != nil {
				// Some operations may fail, that's ok for benchmarking
			}
		}
	}
}

// Benchmark helper types

type BenchmarkCloudProvider struct {
	cfg    aws.Config
	region string
}

type BenchmarkUnifiedScanner struct {
	cfg    aws.Config
	region string
}

type S3BenchmarkScanner struct {
	cfg    aws.Config
	region string
}

type BenchmarkResource struct {
	Service string
	Type    string
	ID      string
}

type BenchmarkResult struct {
	Resources []BenchmarkResource
	Duration  time.Duration
}

type MockAWSClient struct{}

type S3Bucket struct {
	Name string
}

// BenchmarkCloudProvider methods

func (p *BenchmarkCloudProvider) BatchScan(ctx context.Context, services []string) (*BenchmarkResult, error) {
	start := time.Now()
	
	var resources []BenchmarkResource
	for _, service := range services {
		// Simulate batch scanning
		resources = append(resources, BenchmarkResource{
			Service: service,
			Type:    "MockResource",
			ID:      "mock-" + service + "-1",
		})
	}
	
	return &BenchmarkResult{
		Resources: resources,
		Duration:  time.Since(start),
	}, nil
}

func (p *BenchmarkCloudProvider) ListResources(ctx context.Context, service string) ([]BenchmarkResource, error) {
	// Simulate listing resources
	return []BenchmarkResource{
		{Service: service, Type: "MockType", ID: "mock-1"},
		{Service: service, Type: "MockType", ID: "mock-2"},
	}, nil
}

func (p *BenchmarkCloudProvider) DescribeResource(ctx context.Context, id, resourceType, service string) (*BenchmarkResource, error) {
	// Simulate describing a resource
	return &BenchmarkResource{
		Service: service,
		Type:    resourceType,
		ID:      id,
	}, nil
}

func (p *BenchmarkCloudProvider) UnifiedScannerScan(ctx context.Context, service string) (*BenchmarkResult, error) {
	start := time.Now()
	
	// Simulate UnifiedScanner operations
	time.Sleep(1 * time.Millisecond) // Simulate some work
	
	return &BenchmarkResult{
		Resources: []BenchmarkResource{
			{Service: service, Type: "ScannerResource", ID: "unified-1"},
		},
		Duration: time.Since(start),
	}, nil
}

func (p *BenchmarkCloudProvider) ConcurrentScan(ctx context.Context, services []string) (*BenchmarkResult, error) {
	start := time.Now()
	
	// Simulate concurrent scanning
	results := make(chan BenchmarkResource, len(services))
	
	for _, service := range services {
		go func(svc string) {
			// Simulate concurrent work
			time.Sleep(1 * time.Millisecond)
			results <- BenchmarkResource{
				Service: svc,
				Type:    "ConcurrentResource",
				ID:      "concurrent-" + svc + "-1",
			}
		}(service)
	}
	
	var resources []BenchmarkResource
	for i := 0; i < len(services); i++ {
		resources = append(resources, <-results)
	}
	
	return &BenchmarkResult{
		Resources: resources,
		Duration:  time.Since(start),
	}, nil
}

// BenchmarkUnifiedScanner methods

func (s *BenchmarkUnifiedScanner) DiscoverOperations(client interface{}) []string {
	// Simulate reflection-based operation discovery
	return []string{"ListResources", "DescribeResource", "GetConfiguration"}
}

func (s *BenchmarkUnifiedScanner) ClassifyField(fieldName string) string {
	// Simulate field classification logic
	fieldLower := strings.ToLower(fieldName)
	if strings.Contains(fieldLower, "id") {
		return "identifier"
	}
	if strings.Contains(fieldLower, "name") {
		return "name"
	}
	if strings.Contains(fieldLower, "arn") {
		return "arn"
	}
	return "other"
}

func (s *BenchmarkUnifiedScanner) InferResourceType(service, field string) string {
	// Simulate resource type inference
	fieldLower := strings.ToLower(field)
	if service == "ec2" && strings.Contains(fieldLower, "instance") {
		return "Instance"
	}
	if service == "s3" && strings.Contains(fieldLower, "bucket") {
		return "Bucket"
	}
	return "Resource"
}

func (s *BenchmarkUnifiedScanner) CollectConfiguration(ctx context.Context, resource *BenchmarkResource) (map[string]interface{}, error) {
	// Simulate configuration collection
	config := map[string]interface{}{
		"BasicInfo":      map[string]string{"Id": resource.ID, "Type": resource.Type},
		"Configuration":  map[string]string{"Setting1": "value1", "Setting2": "value2"},
		"Tags":          map[string]string{"Environment": "test"},
	}
	return config, nil
}

func (s *BenchmarkUnifiedScanner) ScanService(ctx context.Context, service string) ([]BenchmarkResource, error) {
	// Simulate service scanning
	return []BenchmarkResource{
		{Service: service, Type: "ScannedResource", ID: "scanned-1"},
		{Service: service, Type: "ScannedResource", ID: "scanned-2"},
	}, nil
}

// S3BenchmarkScanner methods

func (s *S3BenchmarkScanner) ListBuckets(ctx context.Context) ([]*S3Bucket, error) {
	// Return mock buckets for benchmarking
	return []*S3Bucket{
		{Name: "benchmark-bucket-1"},
		{Name: "benchmark-bucket-2"},
	}, nil
}

func (s *S3BenchmarkScanner) BasicScan(ctx context.Context, bucketName string) (*BenchmarkResource, error) {
	// Simulate basic bucket scanning
	return &BenchmarkResource{
		Service: "s3",
		Type:    "Bucket",
		ID:      bucketName,
	}, nil
}

func (s *S3BenchmarkScanner) CollectConfiguration(ctx context.Context, bucketName string) (map[string]interface{}, error) {
	// Simulate S3 configuration collection
	config := map[string]interface{}{
		"BucketLocation":  map[string]string{"LocationConstraint": s.region},
		"BucketEncryption": map[string]interface{}{"Rules": []interface{}{}},
		"BucketVersioning": map[string]string{"Status": "Enabled"},
		"PublicAccessBlock": map[string]bool{"BlockPublicAcls": true},
	}
	return config, nil
}

func (s *S3BenchmarkScanner) ExecuteConfigOperation(ctx context.Context, bucketName, operation string) (interface{}, error) {
	// Simulate individual configuration operations
	switch operation {
	case "GetBucketEncryption":
		return map[string]interface{}{"Rules": []interface{}{}}, nil
	case "GetBucketVersioning":
		return map[string]string{"Status": "Enabled"}, nil
	case "GetBucketLogging":
		return map[string]interface{}{}, nil
	case "GetPublicAccessBlock":
		return map[string]bool{"BlockPublicAcls": true}, nil
	case "GetBucketLocation":
		return map[string]string{"LocationConstraint": s.region}, nil
	case "GetBucketTagging":
		return map[string]interface{}{"TagSet": []interface{}{}}, nil
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

// BenchmarkMemoryUsage provides memory usage benchmarks
func BenchmarkMemoryUsage(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	provider, err := createBenchmarkProvider(b, cfg)
	require.NoError(b, err)

	ctx := context.Background()

	b.Run("LargeResultSet", func(b *testing.B) {
		// Test memory usage with large result sets
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate scanning service with many resources
			_, err := provider.BatchScan(ctx, []string{"ec2", "s3", "lambda", "iam", "rds"})
			if err != nil {
				b.Logf("Large result set iteration %d failed: %v", i, err)
			}
		}
	})

	b.Run("DeepConfigurationCollection", func(b *testing.B) {
		scanner, err := createBenchmarkScanner(b, cfg)
		require.NoError(b, err)

		resource := &BenchmarkResource{Service: "s3", Type: "Bucket", ID: "test-bucket"}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := scanner.CollectConfiguration(ctx, resource)
			if err != nil {
				b.Logf("Deep configuration collection iteration %d failed: %v", i, err)
			}
		}
	})
}