package discovery

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAnalyzeServiceOperations tests the full operation analysis functionality
func TestAnalyzeServiceOperations(t *testing.T) {
	// Skip if no GitHub token is available
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		t.Skip("GITHUB_TOKEN not set, skipping service operation analysis test")
	}

	sd := NewAWSServiceDiscovery(githubToken)
	ctx := context.Background()

	t.Run("AnalyzeEC2Service", func(t *testing.T) {
		analysis, err := sd.AnalyzeServiceOperations(ctx, "ec2")
		if err != nil {
			t.Fatalf("Failed to analyze EC2 service: %v", err)
		}

		// Verify basic analysis structure
		if analysis.ServiceName != "ec2" {
			t.Errorf("Expected service name 'ec2', got '%s'", analysis.ServiceName)
		}

		if len(analysis.Operations) == 0 {
			t.Error("No operations found for EC2")
		}

		// Check for expected EC2 operations
		expectedOps := map[string]bool{
			"DescribeInstances": false,
			"DescribeImages":    false,
			"DescribeVpcs":      false,
			"RunInstances":      false,
		}

		for _, op := range analysis.Operations {
			if _, expected := expectedOps[op.Name]; expected {
				expectedOps[op.Name] = true
			}
		}

		for opName, found := range expectedOps {
			if !found {
				t.Errorf("Expected operation %s not found", opName)
			}
		}

		// Check pagination
		var paginatedCount int
		for _, op := range analysis.Operations {
			if op.IsPaginated {
				paginatedCount++
			}
		}

		if paginatedCount == 0 {
			t.Error("No paginated operations found, expected several")
		}

		t.Logf("EC2 Analysis Summary:")
		t.Logf("  Total Operations: %d", len(analysis.Operations))
		t.Logf("  Paginated Operations: %d", paginatedCount)
		t.Logf("  Resource Types: %d", len(analysis.ResourceTypes))
		t.Logf("  Relationships: %d", len(analysis.Relationships))

		// Print sample operations
		t.Log("Sample Operations:")
		for i, op := range analysis.Operations {
			if i < 10 {
				t.Logf("  - %s (%s) - Paginated: %v", op.Name, op.Type, op.IsPaginated)
			}
		}

		// Print resource types
		t.Log("Resource Types:")
		for i, res := range analysis.ResourceTypes {
			if i < 10 {
				t.Logf("  - %s (ID: %s, Name: %s)", res.Name, res.IdentifierField, res.NameField)
			}
		}
	})

	t.Run("AnalyzeS3Service", func(t *testing.T) {
		analysis, err := sd.AnalyzeServiceOperations(ctx, "s3")
		if err != nil {
			t.Fatalf("Failed to analyze S3 service: %v", err)
		}

		// Check for S3-specific operations
		foundListBuckets := false
		foundGetObject := false
		foundPutObject := false

		for _, op := range analysis.Operations {
			switch op.Name {
			case "ListBuckets":
				foundListBuckets = true
				if op.Type != "List" {
					t.Errorf("ListBuckets should be type List, got %s", op.Type)
				}
			case "GetObject":
				foundGetObject = true
				if op.Type != "Get" {
					t.Errorf("GetObject should be type Get, got %s", op.Type)
				}
			case "PutObject":
				foundPutObject = true
				if op.Type != "Put" {
					t.Errorf("PutObject should be type Put, got %s", op.Type)
				}
			}
		}

		if !foundListBuckets {
			t.Error("ListBuckets operation not found")
		}
		if !foundGetObject {
			t.Error("GetObject operation not found")
		}
		if !foundPutObject {
			t.Error("PutObject operation not found")
		}

		// Check for Bucket resource type
		foundBucket := false
		for _, res := range analysis.ResourceTypes {
			if res.Name == "Bucket" || res.GoTypeName == "Bucket" {
				foundBucket = true
				break
			}
		}

		if !foundBucket {
			t.Error("Bucket resource type not found")
		}

		t.Logf("S3 Analysis Summary:")
		t.Logf("  Total Operations: %d", len(analysis.Operations))
		t.Logf("  Resource Types: %d", len(analysis.ResourceTypes))
	})
}

// TestOperationTypeDetection tests the operation type detection logic
func TestOperationTypeDetection(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"ListBuckets", "List"},
		{"DescribeInstances", "Describe"},
		{"GetObject", "Get"},
		{"CreateBucket", "Create"},
		{"UpdateBucketPolicy", "Update"},
		{"DeleteObject", "Delete"},
		{"PutBucketPolicy", "Put"},
		{"TagResource", "Tag"},
		{"UntagResource", "Untag"},
		{"EnableLogging", "Enable"},
		{"DisableLogging", "Disable"},
		{"StartInstance", "Start"},
		{"StopInstance", "Stop"},
		{"UnknownOperation", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opType := GetOperationType(tt.name)
			if opType.String() != tt.expected {
				t.Errorf("Expected operation type %s for %s, got %s", 
					tt.expected, tt.name, opType.String())
			}
		})
	}
}

// TestResourceTypeExtraction tests resource type extraction from operation names
func TestResourceTypeExtraction(t *testing.T) {
	tests := []struct {
		opName       string
		expectedType string
	}{
		{"ListBuckets", "Bucket"},
		{"DescribeInstances", "Instance"},
		{"GetObject", "Object"},
		{"CreateSecurityGroup", "SecurityGroup"},
		{"DeleteSnapshot", "Snapshot"},
		{"UpdateFunctionCode", "FunctionCode"},
	}

	for _, tt := range tests {
		t.Run(tt.opName, func(t *testing.T) {
			opType := GetOperationType(tt.opName)
			resourceType := ExtractResourceType(tt.opName, opType)
			if resourceType != tt.expectedType {
				t.Errorf("Expected resource type %s for %s, got %s",
					tt.expectedType, tt.opName, resourceType)
			}
		})
	}
}

// TestCodeAnalyzerWithMockData tests the code analyzer with mock service code
func TestCodeAnalyzerWithMockData(t *testing.T) {
	analyzer := NewCodeAnalyzer()
	
	mockCode := &ServiceCode{
		ServiceName: "testservice",
		ClientCode: `package testservice

import (
	"context"
)

type Client struct {
	options Options
}
`,
		OperationFiles: map[string]string{
			"api_op_ListBuckets.go": `package testservice

import (
	"context"
)

type ListBucketsInput struct {
	// MaxResults is the maximum number of results to return
	MaxResults *int32
	
	// NextToken is for pagination
	NextToken *string
}

type ListBucketsOutput struct {
	// Buckets contains the list of buckets
	Buckets []Bucket
	
	// NextToken for pagination
	NextToken *string
}

func (c *Client) ListBuckets(ctx context.Context, params *ListBucketsInput, optFns ...func(*Options)) (*ListBucketsOutput, error) {
	// Implementation
	return nil, nil
}`,
			"api_op_GetBucket.go": `package testservice

type GetBucketInput struct {
	// BucketName is required
	BucketName *string ` + "`" + `required:"true"` + "`" + `
	
	// VersionId is optional
	VersionId *string
}

type GetBucketOutput struct {
	Bucket *Bucket
}`,
		},
		TypesCode: `package testservice

type Bucket struct {
	// Name is the bucket name
	Name *string
	
	// ARN is the bucket ARN
	ARN *string
	
	// CreationDate when the bucket was created
	CreationDate *time.Time
	
	// Tags associated with the bucket
	Tags []Tag
	
	// Region where the bucket is located
	Region *string
}

type Tag struct {
	Key   *string
	Value *string
}

type Object struct {
	// Key is the object key
	Key *string
	
	// BucketName references the bucket
	BucketName *string
	
	// Size of the object
	Size *int64
}`,
		DownloadedAt: time.Now(),
		Version:      "test-version",
	}

	analysis, err := analyzer.AnalyzeServiceCode(mockCode)
	if err != nil {
		t.Fatalf("Failed to analyze mock code: %v", err)
	}

	// Verify operations
	if len(analysis.Operations) != 2 {
		t.Errorf("Expected 2 operations, got %d", len(analysis.Operations))
	}

	// Check ListBuckets operation
	var listOp *OperationAnalysis
	for i := range analysis.Operations {
		if analysis.Operations[i].Name == "ListBuckets" {
			listOp = &analysis.Operations[i]
			break
		}
	}

	if listOp == nil {
		t.Fatal("ListBuckets operation not found")
	}

	if !listOp.IsPaginated {
		t.Error("ListBuckets should be marked as paginated")
	}

	if listOp.PaginationField != "NextToken" {
		t.Errorf("Expected pagination field NextToken, got %s", listOp.PaginationField)
	}

	// Check resource types
	foundBucket := false
	foundObject := false
	for _, res := range analysis.ResourceTypes {
		switch res.Name {
		case "Bucket":
			foundBucket = true
			if res.NameField != "Name" {
				t.Errorf("Expected Bucket name field to be 'Name', got %s", res.NameField)
			}
			if res.ARNField != "ARN" {
				t.Errorf("Expected Bucket ARN field to be 'ARN', got %s", res.ARNField)
			}
			if res.TagsField != "Tags" {
				t.Errorf("Expected Bucket tags field to be 'Tags', got %s", res.TagsField)
			}
		case "Object":
			foundObject = true
		}
	}

	if !foundBucket {
		t.Error("Bucket resource type not found")
	}
	if !foundObject {
		t.Error("Object resource type not found")
	}
}

// TestServiceOperationSummary tests the operation summary functionality
func TestServiceOperationSummary(t *testing.T) {
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		t.Skip("GITHUB_TOKEN not set, skipping service operation summary test")
	}

	sd := NewAWSServiceDiscovery(githubToken)
	ctx := context.Background()

	summary, err := sd.GetServiceOperationSummary(ctx, "s3")
	if err != nil {
		t.Fatalf("Failed to get operation summary: %v", err)
	}

	t.Log("S3 Operation Summary:")
	for opType, count := range summary {
		t.Logf("  %s: %d", opType, count)
	}

	if summary["Total"] == 0 {
		t.Error("No operations found in summary")
	}

	// S3 should have List, Get, Put, Delete operations
	expectedTypes := []string{"List", "Get", "Put", "Delete"}
	for _, opType := range expectedTypes {
		if count, exists := summary[opType]; !exists || count == 0 {
			t.Errorf("Expected %s operations in S3, but found %d", opType, count)
		}
	}
}