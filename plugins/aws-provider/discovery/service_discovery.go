package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// ServiceDiscovery handles dynamic discovery of AWS services
type ServiceDiscovery struct {
	mu            sync.RWMutex
	cache         map[string]*ServiceMetadata
	githubToken   string
	cacheExpiry   time.Duration
	lastDiscovery time.Time
	analyzer      *generator.AWSAnalyzer
}

// ServiceMetadata contains metadata about an AWS service
type ServiceMetadata struct {
	Name           string    `json:"name"`
	PackagePath    string    `json:"package_path"`
	Version        string    `json:"version"`
	LastUpdated    time.Time `json:"last_updated"`
	Operations     []string  `json:"operations"`
	Resources      []string  `json:"resources"`
	OperationCount int       `json:"operation_count"`
	ResourceCount  int       `json:"resource_count"`
}

// GitHubTreeResponse represents GitHub API tree response
type GitHubTreeResponse struct {
	Tree []GitHubTreeItem `json:"tree"`
}

// GitHubTreeItem represents a single item in GitHub tree
type GitHubTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

// NewAWSServiceDiscovery creates a new AWS service discovery instance
func NewAWSServiceDiscovery(githubToken string) *ServiceDiscovery {
	return &ServiceDiscovery{
		cache:       make(map[string]*ServiceMetadata),
		githubToken: githubToken,
		cacheExpiry: 24 * time.Hour, // Cache for 24 hours
		analyzer:    generator.NewAWSAnalyzer(),
	}
}

// DiscoverAWSServices discovers all AWS services from the aws-sdk-go-v2 repository
func (sd *ServiceDiscovery) DiscoverAWSServices(ctx context.Context, forceRefresh bool) ([]string, error) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Check if we need to refresh
	if !forceRefresh && time.Since(sd.lastDiscovery) < sd.cacheExpiry && len(sd.cache) > 0 {
		services := make([]string, 0, len(sd.cache))
		for name := range sd.cache {
			services = append(services, name)
		}
		sort.Strings(services)
		return services, nil
	}

	// Discover services from GitHub
	services, err := sd.fetchServicesFromGitHub(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services from GitHub: %w", err)
	}

	// Update cache with basic metadata
	for _, service := range services {
		metadata := &ServiceMetadata{
			Name:        service,
			PackagePath: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", service),
			LastUpdated: time.Now(),
		}
		
		// Add lightweight operation analysis
		sd.enrichServiceMetadata(metadata)
		sd.cache[service] = metadata
	}

	sd.lastDiscovery = time.Now()
	sort.Strings(services)
	return services, nil
}

// GetServiceMetadata returns metadata for a specific service
func (sd *ServiceDiscovery) GetServiceMetadata(serviceName string) (*ServiceMetadata, error) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	metadata, exists := sd.cache[serviceName]
	if !exists {
		return nil, fmt.Errorf("service %s not found in cache", serviceName)
	}

	return metadata, nil
}

// fetchServicesFromGitHub fetches the list of AWS services from GitHub API
func (sd *ServiceDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]string, error) {
	// GitHub API URL for aws-sdk-go-v2 service directory
	url := "https://api.github.com/repos/aws/aws-sdk-go-v2/git/trees/main?recursive=1"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add GitHub token if provided
	if sd.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", sd.githubToken))
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Corkscrew-AWS-Plugin-Discovery/1.0")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var treeResp GitHubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	// Extract service names from the tree
	services := make(map[string]bool)
	for _, item := range treeResp.Tree {
		if item.Type == "tree" && strings.HasPrefix(item.Path, "service/") {
			// Extract service name from path like "service/s3" or "service/ec2/types"
			pathParts := strings.Split(item.Path, "/")
			if len(pathParts) >= 2 {
				serviceName := pathParts[1]
				// Skip internal directories and ensure it's a valid service
				if !strings.HasPrefix(serviceName, ".") && 
				   !strings.Contains(serviceName, "internal") &&
				   serviceName != "types" {
					services[serviceName] = true
				}
			}
		}
	}

	// Convert map to slice
	serviceList := make([]string, 0, len(services))
	for service := range services {
		serviceList = append(serviceList, service)
	}

	return serviceList, nil
}

// UpdateServiceMetadata updates metadata for a specific service
func (sd *ServiceDiscovery) UpdateServiceMetadata(serviceName string, operations, resources []string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	metadata, exists := sd.cache[serviceName]
	if !exists {
		metadata = &ServiceMetadata{
			Name:        serviceName,
			PackagePath: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
		}
		sd.cache[serviceName] = metadata
	}

	metadata.Operations = operations
	metadata.Resources = resources
	metadata.LastUpdated = time.Now()

	return nil
}

// GetCachedServices returns all cached service names
func (sd *ServiceDiscovery) GetCachedServices() []string {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	services := make([]string, 0, len(sd.cache))
	for name := range sd.cache {
		services = append(services, name)
	}
	sort.Strings(services)
	return services
}

// ClearCache clears the service discovery cache
func (sd *ServiceDiscovery) ClearCache() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	sd.cache = make(map[string]*ServiceMetadata)
	sd.lastDiscovery = time.Time{}
}

// GetServiceCount returns the number of discovered services
func (sd *ServiceDiscovery) GetServiceCount() int {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	return len(sd.cache)
}

// IsServiceSupported checks if a service is supported
func (sd *ServiceDiscovery) IsServiceSupported(serviceName string) bool {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	_, exists := sd.cache[serviceName]
	return exists
}

// GetServicesWithFilter returns services matching a filter
func (sd *ServiceDiscovery) GetServicesWithFilter(filter func(string) bool) []string {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	var filtered []string
	for name := range sd.cache {
		if filter(name) {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return filtered
}

// GetPopularServices returns a list of commonly used AWS services
func (sd *ServiceDiscovery) GetPopularServices() []string {
	popular := []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam", "cloudformation",
		"ecs", "eks", "elasticloadbalancingv2", "route53", "cloudfront",
		"apigateway", "sns", "sqs", "kinesis", "redshift", "elasticache",
		"secretsmanager", "ssm", "cloudwatch", "logs", "xray",
	}

	// Filter to only include discovered services
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	var available []string
	for _, service := range popular {
		if _, exists := sd.cache[service]; exists {
			available = append(available, service)
		}
	}

	return available
}

// enrichServiceMetadata adds lightweight operation analysis to service metadata
func (sd *ServiceDiscovery) enrichServiceMetadata(metadata *ServiceMetadata) {
	// Get estimated operations for known services
	knownServices := sd.analyzer.GetKnownServices()
	if _, isKnown := knownServices[metadata.Name]; isKnown {
		// For known services, provide estimated counts based on service type
		metadata.Operations = sd.getEstimatedOperations(metadata.Name)
		metadata.OperationCount = len(metadata.Operations)
		metadata.Resources = sd.getEstimatedResources(metadata.Name)
		metadata.ResourceCount = len(metadata.Resources)
	} else {
		// For unknown services, provide conservative estimates
		metadata.Operations = []string{"List*", "Describe*", "Get*"}
		metadata.OperationCount = 15 // Conservative estimate
		metadata.Resources = []string{strings.Title(metadata.Name) + "Resource"}
		metadata.ResourceCount = 1
	}
}

// getEstimatedOperations returns estimated operations for a service
func (sd *ServiceDiscovery) getEstimatedOperations(serviceName string) []string {
	// Common operation patterns based on service type
	commonOps := []string{
		"List" + strings.Title(serviceName) + "*",
		"Describe" + strings.Title(serviceName) + "*",
		"Get" + strings.Title(serviceName) + "*",
	}

	// Service-specific patterns
	switch serviceName {
	case "s3":
		return append(commonOps, "ListBuckets", "ListObjects", "GetObject", "HeadObject", "GetBucketLocation")
	case "ec2":
		return append(commonOps, "DescribeInstances", "DescribeImages", "DescribeVpcs", "DescribeSubnets", "DescribeSecurityGroups")
	case "lambda":
		return append(commonOps, "ListFunctions", "GetFunction", "ListLayers", "ListEventSourceMappings")
	case "iam":
		return append(commonOps, "ListUsers", "ListRoles", "ListPolicies", "GetUser", "GetRole", "GetPolicy")
	case "rds":
		return append(commonOps, "DescribeDBInstances", "DescribeDBClusters", "DescribeDBSnapshots")
	case "dynamodb":
		return append(commonOps, "ListTables", "DescribeTable", "ListBackups", "DescribeBackup")
	default:
		return commonOps
	}
}

// getEstimatedResources returns estimated resource types for a service
func (sd *ServiceDiscovery) getEstimatedResources(serviceName string) []string {
	switch serviceName {
	case "s3":
		return []string{"Bucket", "Object"}
	case "ec2":
		return []string{"Instance", "VPC", "Subnet", "SecurityGroup", "Image", "Volume"}
	case "lambda":
		return []string{"Function", "Layer", "EventSourceMapping"}
	case "iam":
		return []string{"User", "Role", "Policy", "Group"}
	case "rds":
		return []string{"DBInstance", "DBCluster", "DBSnapshot", "DBClusterSnapshot"}
	case "dynamodb":
		return []string{"Table", "Backup", "Stream"}
	default:
		return []string{strings.Title(serviceName) + "Resource"}
	}
}
