package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// ServiceDiscovery handles dynamic discovery of AWS services
type ServiceDiscovery struct {
	mu              sync.RWMutex
	cache           map[string]*ServiceMetadata
	codeCache       map[string]*ServiceCode // Cache for downloaded service code
	analysisCache   map[string]*ServiceAnalysis // Cache for service analysis
	githubToken     string
	cacheExpiry     time.Duration
	lastDiscovery   time.Time
	analyzer        *generator.AWSAnalyzer
	codeAnalyzer    *CodeAnalyzer
	cachePath       string
	rateLimiter     *time.Ticker
	retryDelay      time.Duration
	maxRetries      int
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
	Url  string `json:"url"`
	Sha  string `json:"sha"`
}

// ServiceCode represents downloaded service SDK code
type ServiceCode struct {
	ServiceName    string              `json:"service_name"`
	ClientCode     string              `json:"client_code"`      // api_client.go content
	OperationFiles map[string]string   `json:"operation_files"` // api_op_*.go files
	TypesCode      string              `json:"types_code"`      // types.go content
	DownloadedAt   time.Time           `json:"downloaded_at"`
	Version        string              `json:"version"`         // Git commit SHA
}

// GitHubBlobResponse represents GitHub blob API response
type GitHubBlobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Sha      string `json:"sha"`
}

// NewAWSServiceDiscovery creates a new AWS service discovery instance
func NewAWSServiceDiscovery(githubToken string) *ServiceDiscovery {
	cachePath := filepath.Join(os.TempDir(), "corkscrew-aws-sdk-cache")
	os.MkdirAll(cachePath, 0755)
	
	return &ServiceDiscovery{
		cache:         make(map[string]*ServiceMetadata),
		codeCache:     make(map[string]*ServiceCode),
		analysisCache: make(map[string]*ServiceAnalysis),
		githubToken:   githubToken,
		cacheExpiry:   24 * time.Hour, // Cache for 24 hours
		analyzer:      generator.NewAWSAnalyzer(),
		codeAnalyzer:  NewCodeAnalyzer(),
		cachePath:     cachePath,
		rateLimiter:   time.NewTicker(time.Second / 10), // 10 requests per second
		retryDelay:    time.Second,
		maxRetries:    3,
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
	// Try to fetch and analyze actual service code
	ctx := context.Background()
	serviceCode, err := sd.FetchServiceCode(ctx, metadata.Name)
	if err == nil && serviceCode != nil {
		// Analyze actual code to extract operations and resources
		operations, resources := sd.analyzeServiceCode(serviceCode)
		if len(operations) > 0 {
			metadata.Operations = operations
			metadata.OperationCount = len(operations)
		}
		if len(resources) > 0 {
			metadata.Resources = resources
			metadata.ResourceCount = len(resources)
		}
		// Update version from actual code
		if serviceCode.Version != "" {
			metadata.Version = serviceCode.Version
		}
	} else {
		// Fallback to estimates for known services
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

// FetchServiceCode downloads all Go files for a service from GitHub
func (sd *ServiceDiscovery) FetchServiceCode(ctx context.Context, serviceName string) (*ServiceCode, error) {
	// Check in-memory cache first
	sd.mu.RLock()
	if cached, exists := sd.codeCache[serviceName]; exists {
		if time.Since(cached.DownloadedAt) < sd.cacheExpiry {
			sd.mu.RUnlock()
			return cached, nil
		}
	}
	sd.mu.RUnlock()

	// Try to load from disk cache
	cached, err := sd.LoadCachedServiceCode(serviceName)
	if err == nil && time.Since(cached.DownloadedAt) < sd.cacheExpiry {
		// Update in-memory cache
		sd.mu.Lock()
		sd.codeCache[serviceName] = cached
		sd.mu.Unlock()
		return cached, nil
	}

	// Fetch from GitHub
	fmt.Printf("Fetching service code for %s from GitHub...\n", serviceName)
	
	// Get the commit SHA first for versioning
	commitSHA, err := sd.getLatestCommitSHA(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit SHA: %w", err)
	}

	serviceCode := &ServiceCode{
		ServiceName:    serviceName,
		OperationFiles: make(map[string]string),
		Version:        commitSHA,
		DownloadedAt:   time.Now(),
	}

	// Get list of files in the service directory
	files, err := sd.listServiceFiles(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to list service files: %w", err)
	}

	// Download relevant files with rate limiting
	for _, file := range files {
		// Rate limit
		<-sd.rateLimiter.C
		
		fileName := filepath.Base(file.Path)
		
		// Determine file type and download
		switch {
		case fileName == "api_client.go":
			content, err := sd.downloadFileWithRetry(ctx, file.Url)
			if err != nil {
				fmt.Printf("Warning: Failed to download %s: %v\n", fileName, err)
				continue
			}
			serviceCode.ClientCode = content
			
		case fileName == "types.go":
			content, err := sd.downloadFileWithRetry(ctx, file.Url)
			if err != nil {
				fmt.Printf("Warning: Failed to download %s: %v\n", fileName, err)
				continue
			}
			serviceCode.TypesCode = content
			
		case strings.HasPrefix(fileName, "api_op_") && strings.HasSuffix(fileName, ".go"):
			content, err := sd.downloadFileWithRetry(ctx, file.Url)
			if err != nil {
				fmt.Printf("Warning: Failed to download %s: %v\n", fileName, err)
				continue
			}
			serviceCode.OperationFiles[fileName] = content
		}
	}

	// Cache the downloaded code
	if err := sd.CacheServiceCode(serviceName, serviceCode); err != nil {
		fmt.Printf("Warning: Failed to cache service code: %v\n", err)
	}

	// Update in-memory cache
	sd.mu.Lock()
	sd.codeCache[serviceName] = serviceCode
	sd.mu.Unlock()

	fmt.Printf("Successfully fetched code for %s: %d operation files\n", 
		serviceName, len(serviceCode.OperationFiles))
	
	return serviceCode, nil
}

// CacheServiceCode saves downloaded code to local filesystem
func (sd *ServiceDiscovery) CacheServiceCode(serviceName string, code *ServiceCode) error {
	serviceCachePath := filepath.Join(sd.cachePath, serviceName)
	if err := os.MkdirAll(serviceCachePath, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Save metadata
	metadataPath := filepath.Join(serviceCachePath, "metadata.json")
	metadataBytes, err := json.MarshalIndent(code, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Save client code
	if code.ClientCode != "" {
		clientPath := filepath.Join(serviceCachePath, "api_client.go")
		if err := os.WriteFile(clientPath, []byte(code.ClientCode), 0644); err != nil {
			return fmt.Errorf("failed to write client code: %w", err)
		}
	}

	// Save types code
	if code.TypesCode != "" {
		typesPath := filepath.Join(serviceCachePath, "types.go")
		if err := os.WriteFile(typesPath, []byte(code.TypesCode), 0644); err != nil {
			return fmt.Errorf("failed to write types code: %w", err)
		}
	}

	// Save operation files
	opsPath := filepath.Join(serviceCachePath, "operations")
	if len(code.OperationFiles) > 0 {
		if err := os.MkdirAll(opsPath, 0755); err != nil {
			return fmt.Errorf("failed to create operations directory: %w", err)
		}
		
		for fileName, content := range code.OperationFiles {
			filePath := filepath.Join(opsPath, fileName)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write operation file %s: %w", fileName, err)
			}
		}
	}

	return nil
}

// LoadCachedServiceCode loads previously cached service code
func (sd *ServiceDiscovery) LoadCachedServiceCode(serviceName string) (*ServiceCode, error) {
	serviceCachePath := filepath.Join(sd.cachePath, serviceName)
	metadataPath := filepath.Join(serviceCachePath, "metadata.json")

	// Check if cache exists
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no cached data for service %s", serviceName)
	}

	// Load metadata
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var code ServiceCode
	if err := json.Unmarshal(metadataBytes, &code); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	// Validate cache freshness
	if time.Since(code.DownloadedAt) > sd.cacheExpiry {
		return nil, fmt.Errorf("cached data is stale")
	}

	// Load client code
	clientPath := filepath.Join(serviceCachePath, "api_client.go")
	if data, err := os.ReadFile(clientPath); err == nil {
		code.ClientCode = string(data)
	}

	// Load types code
	typesPath := filepath.Join(serviceCachePath, "types.go")
	if data, err := os.ReadFile(typesPath); err == nil {
		code.TypesCode = string(data)
	}

	// Load operation files
	opsPath := filepath.Join(serviceCachePath, "operations")
	if files, err := os.ReadDir(opsPath); err == nil {
		code.OperationFiles = make(map[string]string)
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") {
				filePath := filepath.Join(opsPath, file.Name())
				if data, err := os.ReadFile(filePath); err == nil {
					code.OperationFiles[file.Name()] = string(data)
				}
			}
		}
	}

	return &code, nil
}

// Helper method to get latest commit SHA for a service
func (sd *ServiceDiscovery) getLatestCommitSHA(ctx context.Context, serviceName string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/aws/aws-sdk-go-v2/commits?path=service/%s&per_page=1", serviceName)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if sd.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", sd.githubToken))
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Corkscrew-AWS-Plugin-Discovery/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return "", err
	}

	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found")
	}

	return commits[0].SHA, nil
}

// Helper method to list files in a service directory
func (sd *ServiceDiscovery) listServiceFiles(ctx context.Context, serviceName string) ([]GitHubTreeItem, error) {
	url := fmt.Sprintf("https://api.github.com/repos/aws/aws-sdk-go-v2/contents/service/%s", serviceName)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if sd.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", sd.githubToken))
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Corkscrew-AWS-Plugin-Discovery/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var files []GitHubTreeItem
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}

	// Filter for relevant files
	var relevantFiles []GitHubTreeItem
	for _, file := range files {
		if file.Type == "file" && strings.HasSuffix(file.Path, ".go") {
			fileName := filepath.Base(file.Path)
			if fileName == "api_client.go" || fileName == "types.go" || 
			   (strings.HasPrefix(fileName, "api_op_") && !strings.Contains(fileName, "_test")) {
				relevantFiles = append(relevantFiles, file)
			}
		}
	}

	return relevantFiles, nil
}

// Helper method to download a file with retry logic
func (sd *ServiceDiscovery) downloadFileWithRetry(ctx context.Context, url string) (string, error) {
	var lastErr error
	delay := sd.retryDelay
	
	for attempt := 0; attempt <= sd.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
				delay *= 2 // Exponential backoff
			}
		}

		content, err := sd.downloadFile(ctx, url)
		if err == nil {
			return content, nil
		}

		lastErr = err
		
		// Check if error is retryable
		if strings.Contains(err.Error(), "rate limit") {
			fmt.Printf("Rate limit hit, waiting %v before retry...\n", delay)
			continue
		}
	}

	return "", fmt.Errorf("failed after %d retries: %w", sd.maxRetries, lastErr)
}

// Helper method to download a single file
func (sd *ServiceDiscovery) downloadFile(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if sd.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", sd.githubToken))
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Corkscrew-AWS-Plugin-Discovery/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Check for rate limit
		if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining == "0" {
			return "", fmt.Errorf("GitHub API rate limit exceeded")
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// GitHub returns file content as base64-encoded JSON
	var blob GitHubBlobResponse
	if err := json.Unmarshal(body, &blob); err != nil {
		// Try direct content
		return string(body), nil
	}

	if blob.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(blob.Content)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 content: %w", err)
		}
		return string(decoded), nil
	}

	return blob.Content, nil
}

// analyzeServiceCode extracts operations and resources from downloaded service code
func (sd *ServiceDiscovery) analyzeServiceCode(code *ServiceCode) (operations []string, resources []string) {
	operations = make([]string, 0)
	resources = make([]string, 0)
	resourceMap := make(map[string]bool)

	// Extract operations from operation files
	for fileName, _ := range code.OperationFiles {
		// Extract operation name from filename (e.g., api_op_ListBuckets.go -> ListBuckets)
		if strings.HasPrefix(fileName, "api_op_") && strings.HasSuffix(fileName, ".go") {
			opName := strings.TrimSuffix(strings.TrimPrefix(fileName, "api_op_"), ".go")
			operations = append(operations, opName)
		}
	}

	// Extract resource types from types.go
	if code.TypesCode != "" {
		// Look for struct definitions that represent resources
		lines := strings.Split(code.TypesCode, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Look for type definitions
			if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, " struct") {
				// Extract the type name
				parts := strings.Fields(trimmed)
				if len(parts) >= 3 {
					typeName := parts[1]
					// Filter for likely resource types (common patterns)
					if isLikelyResource(typeName) {
						resourceMap[typeName] = true
					}
				}
			}
		}
	}

	// Convert resource map to slice
	for resource := range resourceMap {
		resources = append(resources, resource)
	}

	// Sort for consistency
	sort.Strings(operations)
	sort.Strings(resources)

	return operations, resources
}

// isLikelyResource determines if a type name is likely a resource
func isLikelyResource(typeName string) bool {
	// Skip input/output types
	if strings.HasSuffix(typeName, "Input") || strings.HasSuffix(typeName, "Output") {
		return false
	}
	
	// Skip error and exception types
	if strings.Contains(typeName, "Error") || strings.Contains(typeName, "Exception") {
		return false
	}
	
	// Skip request/response types
	if strings.Contains(typeName, "Request") || strings.Contains(typeName, "Response") {
		return false
	}
	
	// Common resource patterns
	resourcePatterns := []string{
		"Instance", "Bucket", "Table", "Function", "Role", "Policy",
		"Volume", "Snapshot", "Image", "Group", "User", "Key",
		"Queue", "Topic", "Stream", "Database", "Cluster", "Node",
		"Certificate", "Domain", "Record", "Zone", "Stack", "Template",
		"Pipeline", "Repository", "Distribution", "Cache", "Layer",
	}
	
	for _, pattern := range resourcePatterns {
		if strings.Contains(typeName, pattern) {
			return true
		}
	}
	
	// Also include types that don't have common suffixes (likely domain objects)
	if !strings.Contains(typeName, "_") && len(typeName) > 3 {
		firstChar := typeName[0:1]
		if firstChar == strings.ToUpper(firstChar) {
			return true
		}
	}
	
	return false
}

// AnalyzeService performs deep analysis of a specific service by downloading and analyzing its code
func (sd *ServiceDiscovery) AnalyzeService(ctx context.Context, serviceName string) (*ServiceMetadata, error) {
	// Check if service exists in cache first
	sd.mu.RLock()
	metadata, exists := sd.cache[serviceName]
	sd.mu.RUnlock()
	
	if !exists {
		metadata = &ServiceMetadata{
			Name:        serviceName,
			PackagePath: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
			LastUpdated: time.Now(),
		}
	}

	// Fetch and analyze the service code
	serviceCode, err := sd.FetchServiceCode(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service code: %w", err)
	}

	// Analyze the code
	operations, resources := sd.analyzeServiceCode(serviceCode)
	
	// Update metadata
	metadata.Operations = operations
	metadata.OperationCount = len(operations)
	metadata.Resources = resources
	metadata.ResourceCount = len(resources)
	metadata.Version = serviceCode.Version
	metadata.LastUpdated = time.Now()

	// Update cache
	sd.mu.Lock()
	sd.cache[serviceName] = metadata
	sd.mu.Unlock()

	return metadata, nil
}

// GetCodeCache returns the current state of the code cache for debugging
func (sd *ServiceDiscovery) GetCodeCache() map[string]*ServiceCode {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	
	// Return a copy to avoid concurrent access issues
	cache := make(map[string]*ServiceCode)
	for k, v := range sd.codeCache {
		cache[k] = v
	}
	return cache
}

// AnalyzeServiceOperations performs deep operation analysis of a service
func (sd *ServiceDiscovery) AnalyzeServiceOperations(ctx context.Context, serviceName string) (*ServiceAnalysis, error) {
	// Check analysis cache first
	sd.mu.RLock()
	if cached, exists := sd.analysisCache[serviceName]; exists {
		if time.Since(cached.LastAnalyzed) < sd.cacheExpiry {
			sd.mu.RUnlock()
			return cached, nil
		}
	}
	sd.mu.RUnlock()

	// Try to load cached analysis from disk
	cachedAnalysis, err := sd.loadCachedAnalysis(serviceName)
	if err == nil && time.Since(cachedAnalysis.LastAnalyzed) < sd.cacheExpiry {
		// Update in-memory cache
		sd.mu.Lock()
		sd.analysisCache[serviceName] = cachedAnalysis
		sd.mu.Unlock()
		return cachedAnalysis, nil
	}

	// Fetch service code
	fmt.Printf("Performing deep analysis of %s service operations...\n", serviceName)
	serviceCode, err := sd.FetchServiceCode(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service code: %w", err)
	}

	// Analyze the code
	analysis, err := sd.codeAnalyzer.AnalyzeServiceCode(serviceCode)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze service code: %w", err)
	}

	// Cache the analysis
	if err := sd.cacheAnalysis(serviceName, analysis); err != nil {
		fmt.Printf("Warning: Failed to cache analysis: %v\n", err)
	}

	// Update in-memory cache
	sd.mu.Lock()
	sd.analysisCache[serviceName] = analysis
	sd.mu.Unlock()

	return analysis, nil
}

// cacheAnalysis saves service analysis to disk
func (sd *ServiceDiscovery) cacheAnalysis(serviceName string, analysis *ServiceAnalysis) error {
	analysisPath := filepath.Join(sd.cachePath, serviceName, "analysis.json")
	
	// Ensure directory exists
	dir := filepath.Dir(analysisPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal analysis to JSON
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	// Write to file
	if err := os.WriteFile(analysisPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write analysis cache: %w", err)
	}

	return nil
}

// loadCachedAnalysis loads cached service analysis from disk
func (sd *ServiceDiscovery) loadCachedAnalysis(serviceName string) (*ServiceAnalysis, error) {
	analysisPath := filepath.Join(sd.cachePath, serviceName, "analysis.json")

	// Check if file exists
	if _, err := os.Stat(analysisPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no cached analysis for service %s", serviceName)
	}

	// Read file
	data, err := os.ReadFile(analysisPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read analysis cache: %w", err)
	}

	// Unmarshal analysis
	var analysis ServiceAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis: %w", err)
	}

	return &analysis, nil
}

// GetServiceOperationSummary returns a summary of operations for a service
func (sd *ServiceDiscovery) GetServiceOperationSummary(ctx context.Context, serviceName string) (map[string]int, error) {
	analysis, err := sd.AnalyzeServiceOperations(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	summary := make(map[string]int)
	for _, op := range analysis.Operations {
		summary[op.Type]++
	}

	summary["Total"] = len(analysis.Operations)
	summary["Paginated"] = 0
	for _, op := range analysis.Operations {
		if op.IsPaginated {
			summary["Paginated"]++
		}
	}

	return summary, nil
}
