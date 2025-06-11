package scanner

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// MockClientFactory for benchmarking
type MockClientFactory struct {
	clients map[string]interface{}
}

func NewMockClientFactory() *MockClientFactory {
	return &MockClientFactory{
		clients: make(map[string]interface{}),
	}
}

func (m *MockClientFactory) GetClient(service string) interface{} {
	return &MockAWSClient{service: service}
}

func (m *MockClientFactory) GetAvailableServices() []string {
	return []string{"ec2", "s3", "lambda", "rds", "dynamodb"}
}

// MockAWSClient simulates an AWS client
type MockAWSClient struct {
	service string
}

// Mock list operations
func (m *MockAWSClient) ListBuckets(ctx context.Context, input interface{}) (*MockListOutput, error) {
	return generateMockOutput(100), nil
}

func (m *MockAWSClient) DescribeInstances(ctx context.Context, input interface{}) (*MockListOutput, error) {
	return generateMockOutput(50), nil
}

func (m *MockAWSClient) ListFunctions(ctx context.Context, input interface{}) (*MockListOutput, error) {
	return generateMockOutput(30), nil
}

// MockListOutput simulates AWS API output
type MockListOutput struct {
	Items []MockResource
}

// MockResource simulates an AWS resource
type MockResource struct {
	ID           *string
	Name         *string
	ARN          *string
	Type         *string
	State        *string
	CreatedTime  *time.Time
	ModifiedTime *time.Time
	Tags         []MockTag
}

type MockTag struct {
	Key   *string
	Value *string
}

func generateMockOutput(count int) *MockListOutput {
	output := &MockListOutput{
		Items: make([]MockResource, count),
	}
	
	for i := 0; i < count; i++ {
		id := stringPtr(fmt.Sprintf("resource-%d", i))
		name := stringPtr(fmt.Sprintf("Resource %d", i))
		arn := stringPtr(fmt.Sprintf("arn:aws:service:region::resource/%s", *id))
		resourceType := stringPtr("Instance")
		state := stringPtr("running")
		now := time.Now()
		
		output.Items[i] = MockResource{
			ID:           id,
			Name:         name,
			ARN:          arn,
			Type:         resourceType,
			State:        state,
			CreatedTime:  &now,
			ModifiedTime: &now,
			Tags: []MockTag{
				{Key: stringPtr("Environment"), Value: stringPtr("test")},
				{Key: stringPtr("Owner"), Value: stringPtr("benchmark")},
			},
		}
	}
	
	return output
}

func stringPtr(s string) *string {
	return &s
}

// Benchmarks

// BenchmarkUnifiedScanner tests the original UnifiedScanner
func BenchmarkUnifiedScanner(b *testing.B) {
	factory := NewMockClientFactory()
	scanner := NewUnifiedScanner(factory)
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.ScanService(ctx, "ec2")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOptimizedUnifiedScanner tests the optimized scanner
func BenchmarkOptimizedUnifiedScanner(b *testing.B) {
	factory := NewMockClientFactory()
	scanner := NewOptimizedUnifiedScanner(factory)
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.ScanService(ctx, "ec2")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReflectionCache tests reflection caching performance
func BenchmarkReflectionCache(b *testing.B) {
	cache := NewReflectionCache(5 * time.Minute)
	client := &MockAWSClient{}
	
	b.Run("WithoutCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Direct reflection
			clientValue := reflect.ValueOf(client)
			method := clientValue.MethodByName("ListBuckets")
			if !method.IsValid() {
				b.Fatal("Method not found")
			}
		}
	})
	
	b.Run("WithCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			info := cache.GetMethodInfo("s3", "ListBuckets", client)
			if !info.IsValid {
				b.Fatal("Method not found")
			}
		}
	})
}

// BenchmarkBatchProcessor tests batch processing performance
func BenchmarkBatchProcessor(b *testing.B) {
	cache := NewReflectionCache(5 * time.Minute)
	processor := NewBatchProcessor(cache)
	
	// Create mock slice
	output := generateMockOutput(1000)
	items := reflect.ValueOf(output.Items)
	
	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			processor.ProcessBatch(context.Background(), items, "ec2", "Instances")
		}
	})
	
	b.Run("Concurrent", func(b *testing.B) {
		concurrentProcessor := NewConcurrentBatchProcessor(cache, 4)
		for i := 0; i < b.N; i++ {
			concurrentProcessor.ProcessConcurrent(context.Background(), items, "ec2", "Instances")
		}
	})
}

// BenchmarkResourceExtraction compares resource extraction methods
func BenchmarkResourceExtraction(b *testing.B) {
	scanner := NewUnifiedScanner(NewMockClientFactory())
	optimized := NewOptimizedUnifiedScanner(NewMockClientFactory())
	
	output := generateMockOutput(100)
	outputValue := reflect.ValueOf(output)
	
	b.Run("Original", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			scanner.extractResources(output, "ec2", "DescribeInstances")
		}
	})
	
	b.Run("Optimized", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			optimized.extractResourcesOptimized(output, "ec2", "DescribeInstances")
		}
	})
}

// BenchmarkConcurrentScanning tests concurrent scanning performance
func BenchmarkConcurrentScanning(b *testing.B) {
	factory := NewMockClientFactory()
	scanner := NewOptimizedUnifiedScanner(factory)
	ctx := context.Background()
	
	services := []string{"ec2", "s3", "lambda", "rds", "dynamodb"}
	
	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, service := range services {
				_, err := scanner.ScanService(ctx, service)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	
	b.Run("Concurrent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			results := make(chan []*pb.ResourceRef, len(services))
			errors := make(chan error, len(services))
			
			for _, service := range services {
				go func(svc string) {
					refs, err := scanner.ScanService(ctx, svc)
					if err != nil {
						errors <- err
					} else {
						results <- refs
					}
				}(service)
			}
			
			// Collect results
			for range services {
				select {
				case <-results:
				case err := <-errors:
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkMemoryUsage measures memory allocations
func BenchmarkMemoryUsage(b *testing.B) {
	factory := NewMockClientFactory()
	
	b.Run("UnifiedScanner", func(b *testing.B) {
		b.ReportAllocs()
		scanner := NewUnifiedScanner(factory)
		ctx := context.Background()
		
		for i := 0; i < b.N; i++ {
			scanner.ScanService(ctx, "ec2")
		}
	})
	
	b.Run("OptimizedScanner", func(b *testing.B) {
		b.ReportAllocs()
		scanner := NewOptimizedUnifiedScanner(factory)
		ctx := context.Background()
		
		for i := 0; i < b.N; i++ {
			scanner.ScanService(ctx, "ec2")
		}
	})
}