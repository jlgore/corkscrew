package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryClient_NewRegistryClient(t *testing.T) {
	client := NewRegistryClient()
	
	if client == nil {
		t.Fatal("Expected client to be created")
	}
	
	if client.userAgent != "Corkscrew-Registry-Client/1.0" {
		t.Errorf("Expected user agent 'Corkscrew-Registry-Client/1.0', got '%s'", client.userAgent)
	}
	
	if client.retryConfig.MaxRetries != 3 {
		t.Errorf("Expected max retries 3, got %d", client.retryConfig.MaxRetries)
	}
}

func TestRegistryClient_SearchPacks(t *testing.T) {
	client := NewRegistryClient().WithOfflineMode(true)
	
	// Initialize cache with test data
	client.registryCache = &RegistryCache{
		LastUpdated: time.Now(),
		TTL:         24 * time.Hour,
		Registries:  make(map[string]Registry),
		Packs: map[string]PackInfo{
			"test-org/aws-security": {
				Name:        "aws-security",
				Namespace:   "test-org/aws-security",
				Description: "AWS security compliance pack",
				Provider:    "aws",
				Frameworks:  []string{"ccc", "nist"},
				Tags:        []string{"security", "aws"},
				Categories:  []string{"security"},
				LastUpdated: time.Now(),
			},
			"test-org/azure-compliance": {
				Name:        "azure-compliance",
				Namespace:   "test-org/azure-compliance",
				Description: "Azure compliance pack",
				Provider:    "azure",
				Frameworks:  []string{"iso27001"},
				Tags:        []string{"compliance", "azure"},
				Categories:  []string{"governance"},
				LastUpdated: time.Now(),
			},
		},
		Version: "1.0",
	}
	
	tests := []struct {
		name     string
		criteria SearchCriteria
		expected int
	}{
		{
			name:     "search all packs",
			criteria: SearchCriteria{},
			expected: 2,
		},
		{
			name:     "search by provider",
			criteria: SearchCriteria{Provider: "aws"},
			expected: 1,
		},
		{
			name:     "search by framework",
			criteria: SearchCriteria{Framework: "ccc"},
			expected: 1,
		},
		{
			name:     "search by tag",
			criteria: SearchCriteria{Tags: []string{"security"}},
			expected: 1,
		},
		{
			name:     "search by query",
			criteria: SearchCriteria{Query: "azure"},
			expected: 1,
		},
		{
			name:     "search with no matches",
			criteria: SearchCriteria{Provider: "gcp"},
			expected: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := client.SearchPacks(ctx, tt.criteria)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}
			
			if len(result.Packs) != tt.expected {
				t.Errorf("Expected %d packs, got %d", tt.expected, len(result.Packs))
			}
			
			if result.Total != tt.expected {
				t.Errorf("Expected total %d, got %d", tt.expected, result.Total)
			}
		})
	}
}

func TestRegistryClient_UpdateRegistry(t *testing.T) {
	// Create mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/repositories") {
			// Mock repository search response
			response := map[string]interface{}{
				"total_count": 1,
				"items": []map[string]interface{}{
					{
						"name":        "test-compliance-pack",
						"full_name":   "test-org/test-compliance-pack",
						"description": "Test compliance pack",
						"clone_url":   "https://github.com/test-org/test-compliance-pack.git",
						"html_url":    "https://github.com/test-org/test-compliance-pack",
						"updated_at":  time.Now().Format(time.RFC3339),
						"topics":      []string{"corkscrew-compliance-pack"},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/contents/manifest.yaml") {
			// Mock manifest file response
			manifestContent := `apiVersion: v1
kind: QueryPack
metadata:
  name: test-pack
  version: 1.0.0
  description: Test pack
  provider: aws
`
			response := map[string]interface{}{
				"content":  manifestContent, // In real GitHub API, this would be base64 encoded
				"encoding": "base64",
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/releases") {
			// Mock releases response - we need to use the server variable in scope
			serverURL := "http://" + r.Host // Use the request host instead
			response := []map[string]interface{}{
				{
					"tag_name":     "v1.0.0",
					"name":         "v1.0.0",
					"published_at": time.Now().Format(time.RFC3339),
					"tarball_url":  serverURL + "/tarball/v1.0.0",
					"prerelease":   false,
					"draft":        false,
				},
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()
	
	// Create temporary cache directory
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "registry.json")
	
	client := NewRegistryClient()
	client.cachePath = cachePath
	client.baseURLs = []string{server.URL}
	
	ctx := context.Background()
	err := client.UpdateRegistry(ctx, true)
	if err != nil {
		t.Fatalf("UpdateRegistry failed: %v", err)
	}
	
	// Verify cache was created
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Error("Cache file was not created")
	}
	
	// Verify cache content
	if client.registryCache == nil {
		t.Error("Registry cache was not initialized")
	}
}

func TestRegistryClient_CacheManagement(t *testing.T) {
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "test-registry.json")
	
	client := NewRegistryClient()
	client.cachePath = cachePath
	
	// Initialize cache with test data
	client.registryCache = &RegistryCache{
		LastUpdated: time.Now(),
		TTL:         24 * time.Hour,
		Registries:  make(map[string]Registry),
		Packs: map[string]PackInfo{
			"test/pack": {
				Name:      "test-pack",
				Namespace: "test/pack",
				Provider:  "aws",
			},
		},
		Version: "1.0",
	}
	
	// Test save cache
	err := client.saveCache()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}
	
	// Verify file exists
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Error("Cache file was not created")
	}
	
	// Test load cache
	client.registryCache = nil
	err = client.loadCache()
	if err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}
	
	if client.registryCache == nil {
		t.Error("Cache was not loaded")
	}
	
	if len(client.registryCache.Packs) != 1 {
		t.Errorf("Expected 1 pack in cache, got %d", len(client.registryCache.Packs))
	}
	
	// Test clear cache
	err = client.ClearCache()
	if err != nil {
		t.Fatalf("Failed to clear cache: %v", err)
	}
	
	if len(client.registryCache.Packs) != 0 {
		t.Errorf("Expected 0 packs after clear, got %d", len(client.registryCache.Packs))
	}
}

func TestRegistryClient_OfflineMode(t *testing.T) {
	client := NewRegistryClient().WithOfflineMode(true)
	
	if !client.offlineMode {
		t.Error("Expected offline mode to be enabled")
	}
	
	ctx := context.Background()
	
	// Update should not make network requests in offline mode
	err := client.UpdateRegistry(ctx, true)
	if err != nil {
		t.Errorf("UpdateRegistry should not fail in offline mode: %v", err)
	}
}

func TestRegistryClient_SearchSorting(t *testing.T) {
	client := NewRegistryClient().WithOfflineMode(true)
	
	now := time.Now()
	client.registryCache = &RegistryCache{
		LastUpdated: now,
		TTL:         24 * time.Hour,
		Registries:  make(map[string]Registry),
		Packs: map[string]PackInfo{
			"test/alpha": {
				Name:        "alpha",
				Namespace:   "test/alpha",
				LastUpdated: now.Add(-2 * time.Hour),
				Downloads:   PackDownloadStats{Total: 100},
			},
			"test/beta": {
				Name:        "beta",
				Namespace:   "test/beta",
				LastUpdated: now.Add(-1 * time.Hour),
				Downloads:   PackDownloadStats{Total: 200},
			},
			"test/gamma": {
				Name:        "gamma",
				Namespace:   "test/gamma",
				LastUpdated: now,
				Downloads:   PackDownloadStats{Total: 50},
			},
		},
		Version: "1.0",
	}
	
	tests := []struct {
		name     string
		sort     string
		order    string
		expected []string // Expected order of pack names
	}{
		{
			name:     "sort by name asc",
			sort:     "name",
			order:    "asc",
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name:     "sort by name desc",
			sort:     "name",
			order:    "desc",
			expected: []string{"gamma", "beta", "alpha"},
		},
		{
			name:     "sort by downloads desc",
			sort:     "downloads",
			order:    "desc",
			expected: []string{"beta", "alpha", "gamma"},
		},
		{
			name:     "sort by updated desc",
			sort:     "updated",
			order:    "desc",
			expected: []string{"gamma", "beta", "alpha"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			criteria := SearchCriteria{
				Sort:  tt.sort,
				Order: tt.order,
			}
			
			result, err := client.SearchPacks(ctx, criteria)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}
			
			if len(result.Packs) != len(tt.expected) {
				t.Fatalf("Expected %d packs, got %d", len(tt.expected), len(result.Packs))
			}
			
			for i, expectedName := range tt.expected {
				if result.Packs[i].Name != expectedName {
					t.Errorf("Expected pack %d to be '%s', got '%s'", i, expectedName, result.Packs[i].Name)
				}
			}
		})
	}
}

func TestRegistryClient_SearchPagination(t *testing.T) {
	client := NewRegistryClient().WithOfflineMode(true)
	
	// Create test packs
	packs := make(map[string]PackInfo)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("pack-%02d", i)
		packs[fmt.Sprintf("test/%s", name)] = PackInfo{
			Name:      name,
			Namespace: fmt.Sprintf("test/%s", name),
			Provider:  "aws",
		}
	}
	
	client.registryCache = &RegistryCache{
		LastUpdated: time.Now(),
		TTL:         24 * time.Hour,
		Registries:  make(map[string]Registry),
		Packs:       packs,
		Version:     "1.0",
	}
	
	ctx := context.Background()
	
	// Test pagination
	criteria := SearchCriteria{
		Limit:  3,
		Offset: 2,
		Sort:   "name",
		Order:  "asc",
	}
	
	result, err := client.SearchPacks(ctx, criteria)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	
	if result.Total != 10 {
		t.Errorf("Expected total 10, got %d", result.Total)
	}
	
	if len(result.Packs) != 3 {
		t.Errorf("Expected 3 packs in result, got %d", len(result.Packs))
	}
	
	if result.Limit != 3 {
		t.Errorf("Expected limit 3, got %d", result.Limit)
	}
	
	if result.Offset != 2 {
		t.Errorf("Expected offset 2, got %d", result.Offset)
	}
	
	// Verify correct packs are returned (should be pack-02, pack-03, pack-04)
	expectedNames := []string{"pack-02", "pack-03", "pack-04"}
	for i, expected := range expectedNames {
		if result.Packs[i].Name != expected {
			t.Errorf("Expected pack %d to be '%s', got '%s'", i, expected, result.Packs[i].Name)
		}
	}
}

func TestRegistryClient_GetCacheInfo(t *testing.T) {
	client := NewRegistryClient().WithOfflineMode(true)
	
	// Explicitly set cache to nil to test empty state
	client.registryCache = nil
	
	// Test with empty cache
	info, err := client.GetCacheInfo()
	if err != nil {
		t.Fatalf("GetCacheInfo failed: %v", err)
	}
	
	status, exists := info["status"]
	if !exists || status != "empty" {
		t.Errorf("Expected status 'empty', got '%v'", status)
	}
	
	// Test with populated cache
	client.registryCache = &RegistryCache{
		LastUpdated: time.Now(),
		TTL:         24 * time.Hour,
		Registries:  make(map[string]Registry),
		Packs: map[string]PackInfo{
			"test/pack1": {Name: "pack1"},
			"test/pack2": {Name: "pack2"},
		},
		Version: "1.0",
	}
	
	info, err = client.GetCacheInfo()
	if err != nil {
		t.Fatalf("GetCacheInfo failed: %v", err)
	}
	
	if info["pack_count"] != 2 {
		t.Errorf("Expected pack_count 2, got %v", info["pack_count"])
	}
	
	if info["version"] != "1.0" {
		t.Errorf("Expected version '1.0', got '%v'", info["version"])
	}
	
	if info["offline_mode"] != true {
		t.Errorf("Expected offline_mode true, got %v", info["offline_mode"])
	}
}

func TestRegistryClient_RetryLogic(t *testing.T) {
	// Count requests
	requestCount := 0
	
	// Create server that fails first 2 requests, succeeds on 3rd
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		
		if requestCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		
		// Success response for any request
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	
	client := NewRegistryClient()
	client.retryConfig = RetryConfig{
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
		Backoff:    1.5,
	}
	
	// Test the retry logic directly with doRequestWithRetry
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	
	resp, err := client.doRequestWithRetry(req)
	if err != nil {
		t.Fatalf("Request should succeed after retries: %v", err)
	}
	defer resp.Body.Close()
	
	if requestCount != 3 {
		t.Errorf("Expected 3 requests (2 failures + 1 success), got %d", requestCount)
	}
}

func TestMatchesCriteria(t *testing.T) {
	client := NewRegistryClient()
	
	pack := PackInfo{
		Name:        "aws-security",
		Description: "AWS security compliance pack",
		Provider:    "aws",
		Frameworks:  []string{"ccc", "nist"},
		Tags:        []string{"security", "compliance"},
		Categories:  []string{"security"},
		Namespace:   "org/aws-security",
	}
	
	tests := []struct {
		name     string
		criteria SearchCriteria
		expected bool
	}{
		{
			name:     "empty criteria matches all",
			criteria: SearchCriteria{},
			expected: true,
		},
		{
			name:     "query matches name",
			criteria: SearchCriteria{Query: "aws"},
			expected: true,
		},
		{
			name:     "query matches description",
			criteria: SearchCriteria{Query: "security"},
			expected: true,
		},
		{
			name:     "query matches tag",
			criteria: SearchCriteria{Query: "compliance"},
			expected: true,
		},
		{
			name:     "query no match",
			criteria: SearchCriteria{Query: "azure"},
			expected: false,
		},
		{
			name:     "provider matches",
			criteria: SearchCriteria{Provider: "aws"},
			expected: true,
		},
		{
			name:     "provider no match",
			criteria: SearchCriteria{Provider: "azure"},
			expected: false,
		},
		{
			name:     "framework matches",
			criteria: SearchCriteria{Framework: "ccc"},
			expected: true,
		},
		{
			name:     "framework no match",
			criteria: SearchCriteria{Framework: "iso27001"},
			expected: false,
		},
		{
			name:     "category matches",
			criteria: SearchCriteria{Category: "security"},
			expected: true,
		},
		{
			name:     "namespace prefix matches",
			criteria: SearchCriteria{Namespace: "org"},
			expected: true,
		},
		{
			name:     "tags match (all required)",
			criteria: SearchCriteria{Tags: []string{"security"}},
			expected: true,
		},
		{
			name:     "tags no match (missing required tag)",
			criteria: SearchCriteria{Tags: []string{"security", "missing"}},
			expected: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.matchesCriteria(pack, tt.criteria)
			if result != tt.expected {
				t.Errorf("Expected %t, got %t", tt.expected, result)
			}
		})
	}
}