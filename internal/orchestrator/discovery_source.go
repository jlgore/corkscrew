package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DiscoverySource represents a source of service/resource information
type DiscoverySource interface {
	// Name returns the source identifier (github, api, local)
	Name() string
	
	// Discover performs discovery and returns raw data
	Discover(ctx context.Context, config SourceConfig) (interface{}, error)
	
	// Validate checks if the source is properly configured
	Validate() error
}

// GitHubSource discovers resources from GitHub repositories
type GitHubSource struct {
	Token      string
	BaseURL    string // Default: https://api.github.com
	HTTPClient *http.Client
}

// GitHubConfig is passed by providers to configure GitHub discovery
type GitHubConfig struct {
	Owner      string                  // e.g., "aws", "Azure", "googleapis"
	Repo       string                  // e.g., "aws-sdk-go-v2"
	Path       string                  // e.g., "service/"
	Branch     string                  // Default: "main"
	FileFilter func(path string) bool  // Provider-specific filter
	MaxDepth   int                     // Maximum directory depth to traverse
}

// Validate implements SourceConfig
func (c GitHubConfig) Validate() error {
	if c.Owner == "" {
		return fmt.Errorf("owner is required")
	}
	if c.Repo == "" {
		return fmt.Errorf("repo is required")
	}
	if c.Branch == "" {
		c.Branch = "main"
	}
	return nil
}

// NewGitHubSource creates a new GitHub discovery source
func NewGitHubSource(token string) *GitHubSource {
	return &GitHubSource{
		Token:   token,
		BaseURL: "https://api.github.com",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the source identifier
func (g *GitHubSource) Name() string {
	return "github"
}

// Validate checks if the source is properly configured
func (g *GitHubSource) Validate() error {
	if g.BaseURL == "" {
		return fmt.Errorf("BaseURL is required")
	}
	if g.HTTPClient == nil {
		return fmt.Errorf("HTTPClient is required")
	}
	return nil
}

// Discover performs GitHub tree traversal and returns file listings
func (g *GitHubSource) Discover(ctx context.Context, config SourceConfig) (interface{}, error) {
	githubConfig, ok := config.(GitHubConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type, expected GitHubConfig")
	}
	
	if err := githubConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	result := &GitHubDiscoveryResult{
		Files:       []FileInfo{},
		Directories: []string{},
		Metadata:    make(map[string]string),
	}
	
	// Get repository information
	repoURL := fmt.Sprintf("%s/repos/%s/%s", g.BaseURL, githubConfig.Owner, githubConfig.Repo)
	repoInfo, err := g.makeRequest(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo info: %w", err)
	}
	
	// Get the tree SHA for the branch
	branchURL := fmt.Sprintf("%s/repos/%s/%s/branches/%s", 
		g.BaseURL, githubConfig.Owner, githubConfig.Repo, githubConfig.Branch)
	branchInfo, err := g.makeRequest(ctx, branchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch info: %w", err)
	}
	
	// Extract commit SHA
	var branchData map[string]interface{}
	if err := json.Unmarshal(branchInfo, &branchData); err != nil {
		return nil, fmt.Errorf("failed to parse branch info: %w", err)
	}
	
	commitSHA, _ := branchData["commit"].(map[string]interface{})["sha"].(string)
	result.Metadata["commit"] = commitSHA
	result.Metadata["discovered_at"] = time.Now().UTC().Format(time.RFC3339)
	result.Metadata["branch"] = githubConfig.Branch
	
	// Get the tree
	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1",
		g.BaseURL, githubConfig.Owner, githubConfig.Repo, commitSHA)
	treeData, err := g.makeRequest(ctx, treeURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get tree: %w", err)
	}
	
	var tree map[string]interface{}
	if err := json.Unmarshal(treeData, &tree); err != nil {
		return nil, fmt.Errorf("failed to parse tree: %w", err)
	}
	
	// Process tree entries
	entries, ok := tree["tree"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid tree structure")
	}
	
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		
		path, _ := entryMap["path"].(string)
		entryType, _ := entryMap["type"].(string)
		
		// Check if path starts with configured path
		if githubConfig.Path != "" && !strings.HasPrefix(path, githubConfig.Path) {
			continue
		}
		
		// Check depth
		if githubConfig.MaxDepth > 0 {
			depth := strings.Count(path, "/")
			if githubConfig.Path != "" {
				depth -= strings.Count(githubConfig.Path, "/")
			}
			if depth > githubConfig.MaxDepth {
				continue
			}
		}
		
		// Apply file filter if provided
		if githubConfig.FileFilter != nil && !githubConfig.FileFilter(path) {
			continue
		}
		
		if entryType == "blob" {
			// It's a file
			fileInfo := FileInfo{
				Path: path,
				Name: getFileName(path),
				Type: "file",
				SHA:  entryMap["sha"].(string),
			}
			
			if size, ok := entryMap["size"].(float64); ok {
				fileInfo.Size = int64(size)
			}
			
			// Build file URL
			fileInfo.URL = fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
				g.BaseURL, githubConfig.Owner, githubConfig.Repo, path, githubConfig.Branch)
			
			result.Files = append(result.Files, fileInfo)
		} else if entryType == "tree" {
			// It's a directory
			result.Directories = append(result.Directories, path)
		}
	}
	
	// Store repo metadata
	var repoData map[string]interface{}
	if err := json.Unmarshal(repoInfo, &repoData); err == nil {
		if desc, ok := repoData["description"].(string); ok {
			result.Metadata["description"] = desc
		}
		if lang, ok := repoData["language"].(string); ok {
			result.Metadata["language"] = lang
		}
		if updated, ok := repoData["updated_at"].(string); ok {
			result.Metadata["repo_updated_at"] = updated
		}
	}
	
	return result, nil
}

// makeRequest makes an HTTP request to GitHub API
func (g *GitHubSource) makeRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "token "+g.Token)
	}
	
	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}
	
	return io.ReadAll(resp.Body)
}

// APISource discovers resources through HTTP APIs
type APISource struct {
	Endpoint   string
	AuthConfig AuthConfig
	HTTPClient *http.Client
}

// APIConfig is passed by providers for API-based discovery
type APIConfig struct {
	Method      string
	Path        string
	Headers     map[string]string
	QueryParams map[string]string
	Body        interface{}
	Parser      func([]byte) (interface{}, error) // Provider-specific parser
}

// Validate implements SourceConfig
func (c APIConfig) Validate() error {
	if c.Method == "" {
		c.Method = "GET"
	}
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// NewAPISource creates a new API discovery source
func NewAPISource(endpoint string, auth AuthConfig) *APISource {
	return &APISource{
		Endpoint:   endpoint,
		AuthConfig: auth,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the source identifier
func (a *APISource) Name() string {
	return "api"
}

// Validate checks if the source is properly configured
func (a *APISource) Validate() error {
	if a.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if a.HTTPClient == nil {
		return fmt.Errorf("HTTPClient is required")
	}
	return nil
}

// Discover performs API calls and returns responses
func (a *APISource) Discover(ctx context.Context, config SourceConfig) (interface{}, error) {
	apiConfig, ok := config.(APIConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type, expected APIConfig")
	}
	
	if err := apiConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	// Build URL
	url := a.Endpoint + apiConfig.Path
	if len(apiConfig.QueryParams) > 0 {
		params := []string{}
		for k, v := range apiConfig.QueryParams {
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		}
		url += "?" + strings.Join(params, "&")
	}
	
	// Prepare body
	var bodyReader io.Reader
	if apiConfig.Body != nil {
		bodyBytes, err := json.Marshal(apiConfig.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(bodyBytes))
	}
	
	// Create request
	req, err := http.NewRequestWithContext(ctx, apiConfig.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers
	for k, v := range apiConfig.Headers {
		req.Header.Set(k, v)
	}
	
	// Apply authentication
	if err := a.applyAuth(req); err != nil {
		return nil, fmt.Errorf("failed to apply auth: %w", err)
	}
	
	// Execute request
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	result := &APIDiscoveryResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
		Metadata: map[string]interface{}{
			"url":           url,
			"method":        apiConfig.Method,
			"discovered_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	
	// Apply custom parser if provided
	if apiConfig.Parser != nil {
		parsed, err := apiConfig.Parser(body)
		if err != nil {
			return nil, fmt.Errorf("parser failed: %w", err)
		}
		result.Metadata["parsed"] = parsed
	}
	
	return result, nil
}

// applyAuth applies authentication to the request
func (a *APISource) applyAuth(req *http.Request) error {
	switch a.AuthConfig.Type {
	case "bearer":
		token, ok := a.AuthConfig.Credentials["token"]
		if !ok {
			return fmt.Errorf("bearer token not found in credentials")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		
	case "basic":
		username, ok1 := a.AuthConfig.Credentials["username"]
		password, ok2 := a.AuthConfig.Credentials["password"]
		if !ok1 || !ok2 {
			return fmt.Errorf("username or password not found in credentials")
		}
		req.SetBasicAuth(username, password)
		
	case "oauth":
		token, ok := a.AuthConfig.Credentials["access_token"]
		if !ok {
			return fmt.Errorf("access_token not found in credentials")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		
	case "custom":
		// Apply all credentials as headers
		for k, v := range a.AuthConfig.Credentials {
			req.Header.Set(k, v)
		}
		
	case "":
		// No auth
		
	default:
		return fmt.Errorf("unsupported auth type: %s", a.AuthConfig.Type)
	}
	
	return nil
}

// getFileName extracts the file name from a path
func getFileName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}