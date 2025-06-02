package compliance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackLoader_DiscoverPacks(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := t.TempDir()
	
	// Create test pack structure
	packDir := filepath.Join(tempDir, "test-org", "test-pack")
	err := os.MkdirAll(packDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create test manifest
	manifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: test-pack
  namespace: test-org/test
  version: 1.0.0
  description: Test pack
  provider: aws
spec:
  queries:
    - id: test-query
      title: Test Query
      description: A test query
      severity: HIGH
      category: security
      query_file: query.sql
      enabled: true
`
	
	manifestPath := filepath.Join(packDir, "manifest.yaml")
	err = os.WriteFile(manifestPath, []byte(manifest), 0644)
	if err != nil {
		t.Fatalf("Failed to write test manifest: %v", err)
	}
	
	// Create test SQL file
	sqlContent := "SELECT * FROM aws_resources WHERE type = 'AWS::S3::Bucket';"
	sqlPath := filepath.Join(packDir, "query.sql")
	err = os.WriteFile(sqlPath, []byte(sqlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test SQL: %v", err)
	}
	
	// Create loader with test search path
	loader := NewPackLoader().WithSearchPaths(tempDir).WithEmbeddedPacks(false)
	
	// Test discovery
	ctx := context.Background()
	packs, err := loader.DiscoverPacks(ctx)
	if err != nil {
		t.Fatalf("Failed to discover packs: %v", err)
	}
	
	if len(packs) != 1 {
		t.Fatalf("Expected 1 pack, got %d", len(packs))
	}
	
	if packs[0] != "test-org/test-pack" {
		t.Fatalf("Expected 'test-org/test-pack', got '%s'", packs[0])
	}
}

func TestPackLoader_LoadPack(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := t.TempDir()
	
	// Create test pack structure
	packDir := filepath.Join(tempDir, "test-org", "test-pack")
	err := os.MkdirAll(packDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create test manifest
	manifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: test-pack
  namespace: test-org/test
  version: 1.0.0
  description: Test pack
  provider: aws
spec:
  parameters:
    - name: test_param
      description: Test parameter
      type: string
      required: true
  queries:
    - id: test-query
      title: Test Query
      description: A test query
      severity: HIGH
      category: security
      query_file: query.sql
      parameters:
        - test_param
      enabled: true
`
	
	manifestPath := filepath.Join(packDir, "manifest.yaml")
	err = os.WriteFile(manifestPath, []byte(manifest), 0644)
	if err != nil {
		t.Fatalf("Failed to write test manifest: %v", err)
	}
	
	// Create test SQL file
	sqlContent := "SELECT * FROM aws_resources WHERE name = :test_param;"
	sqlPath := filepath.Join(packDir, "query.sql")
	err = os.WriteFile(sqlPath, []byte(sqlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test SQL: %v", err)
	}
	
	// Create loader with test search path
	loader := NewPackLoader().WithSearchPaths(tempDir).WithEmbeddedPacks(false)
	
	// Test loading
	ctx := context.Background()
	pack, err := loader.LoadPack(ctx, "test-org/test-pack")
	if err != nil {
		t.Fatalf("Failed to load pack: %v", err)
	}
	
	// Verify pack structure
	if pack.Metadata.Name != "test-pack" {
		t.Errorf("Expected name 'test-pack', got '%s'", pack.Metadata.Name)
	}
	
	if pack.Metadata.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", pack.Metadata.Version)
	}
	
	if len(pack.Queries) != 1 {
		t.Errorf("Expected 1 query, got %d", len(pack.Queries))
	}
	
	if len(pack.Parameters) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(pack.Parameters))
	}
	
	query := pack.Queries[0]
	if query.ID != "test-query" {
		t.Errorf("Expected query ID 'test-query', got '%s'", query.ID)
	}
	
	if query.SQL != sqlContent {
		t.Errorf("Expected SQL content '%s', got '%s'", sqlContent, query.SQL)
	}
}

func TestPackLoader_LoadPackWithDependencies(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := t.TempDir()
	
	// Create dependency pack
	depPackDir := filepath.Join(tempDir, "test-org", "common")
	err := os.MkdirAll(depPackDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create dependency directory: %v", err)
	}
	
	depManifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: common
  namespace: test-org/common
  version: 1.0.0
  description: Common pack
  provider: aws
spec:
  queries:
    - id: common-query
      title: Common Query
      description: A common query
      severity: LOW
      category: governance
      query_file: common.sql
      enabled: true
`
	
	depManifestPath := filepath.Join(depPackDir, "manifest.yaml")
	err = os.WriteFile(depManifestPath, []byte(depManifest), 0644)
	if err != nil {
		t.Fatalf("Failed to write dependency manifest: %v", err)
	}
	
	depSqlPath := filepath.Join(depPackDir, "common.sql")
	err = os.WriteFile(depSqlPath, []byte("SELECT 'common' as type;"), 0644)
	if err != nil {
		t.Fatalf("Failed to write dependency SQL: %v", err)
	}
	
	// Create main pack with dependency
	mainPackDir := filepath.Join(tempDir, "test-org", "main-pack")
	err = os.MkdirAll(mainPackDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create main pack directory: %v", err)
	}
	
	mainManifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: main-pack
  namespace: test-org/main
  version: 1.0.0
  description: Main pack with dependency
  provider: aws
spec:
  depends_on:
    - name: common
      namespace: test-org/common
      version: ">=1.0.0"
      required: true
  queries:
    - id: main-query
      title: Main Query
      description: A main query
      severity: MEDIUM
      category: security
      query_file: main.sql
      enabled: true
`
	
	mainManifestPath := filepath.Join(mainPackDir, "manifest.yaml")
	err = os.WriteFile(mainManifestPath, []byte(mainManifest), 0644)
	if err != nil {
		t.Fatalf("Failed to write main manifest: %v", err)
	}
	
	mainSqlPath := filepath.Join(mainPackDir, "main.sql")
	err = os.WriteFile(mainSqlPath, []byte("SELECT 'main' as type;"), 0644)
	if err != nil {
		t.Fatalf("Failed to write main SQL: %v", err)
	}
	
	// Create loader with test search path
	loader := NewPackLoader().WithSearchPaths(tempDir).WithEmbeddedPacks(false)
	
	// Test loading main pack (should load dependency automatically)
	ctx := context.Background()
	pack, err := loader.LoadPack(ctx, "test-org/main-pack")
	if err != nil {
		t.Fatalf("Failed to load pack with dependency: %v", err)
	}
	
	// Verify main pack loaded
	if pack.Metadata.Name != "main-pack" {
		t.Errorf("Expected name 'main-pack', got '%s'", pack.Metadata.Name)
	}
	
	// Verify dependency was loaded
	loadedPacks := loader.GetLoadedPacks()
	if len(loadedPacks) != 2 {
		t.Errorf("Expected 2 loaded packs, got %d", len(loadedPacks))
	}
	
	// Check dependency graph
	depGraph := loader.GetDependencyGraph()
	mainDeps, exists := depGraph["test-org/main-pack"]
	if !exists {
		t.Error("Main pack not found in dependency graph")
	}
	
	if len(mainDeps) != 1 || mainDeps[0] != "test-org/common" {
		t.Errorf("Expected dependency 'test-org/common', got %v", mainDeps)
	}
}

func TestPackLoader_CacheInvalidation(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := t.TempDir()
	
	// Create test pack structure
	packDir := filepath.Join(tempDir, "test-org", "cache-test")
	err := os.MkdirAll(packDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create test manifest
	manifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: cache-test
  namespace: test-org/cache
  version: 1.0.0
  description: Cache test pack
  provider: aws
spec:
  queries:
    - id: cache-query
      title: Cache Query
      description: A cache test query
      severity: HIGH
      category: security
      query_file: cache.sql
      enabled: true
`
	
	manifestPath := filepath.Join(packDir, "manifest.yaml")
	err = os.WriteFile(manifestPath, []byte(manifest), 0644)
	if err != nil {
		t.Fatalf("Failed to write test manifest: %v", err)
	}
	
	// Create test SQL file
	sqlPath := filepath.Join(packDir, "cache.sql")
	err = os.WriteFile(sqlPath, []byte("SELECT 'original' as version;"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test SQL: %v", err)
	}
	
	// Create loader with short cache expiry
	loader := NewPackLoader().
		WithSearchPaths(tempDir).
		WithEmbeddedPacks(false).
		WithCacheExpiry(100 * time.Millisecond)
	
	ctx := context.Background()
	
	// Load pack first time
	pack1, err := loader.LoadPack(ctx, "test-org/cache-test")
	if err != nil {
		t.Fatalf("Failed to load pack first time: %v", err)
	}
	
	if pack1.Queries[0].SQL != "SELECT 'original' as version;" {
		t.Error("First load did not return original SQL")
	}
	
	// Verify it's cached
	loadedPacks := loader.GetLoadedPacks()
	if len(loadedPacks) != 1 {
		t.Error("Pack was not cached")
	}
	
	// Modify SQL file
	err = os.WriteFile(sqlPath, []byte("SELECT 'modified' as version;"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify SQL file: %v", err)
	}
	
	// Load again immediately (should return cached version)
	pack2, err := loader.LoadPack(ctx, "test-org/cache-test")
	if err != nil {
		t.Fatalf("Failed to load pack second time: %v", err)
	}
	
	if pack2.Queries[0].SQL != "SELECT 'original' as version;" {
		t.Error("Second load did not return cached SQL")
	}
	
	// Invalidate cache
	loader.InvalidateCache("test-org/cache-test")
	
	// Load again (should return modified version)
	pack3, err := loader.LoadPack(ctx, "test-org/cache-test")
	if err != nil {
		t.Fatalf("Failed to load pack third time: %v", err)
	}
	
	if pack3.Queries[0].SQL != "SELECT 'modified' as version;" {
		t.Error("Third load did not return modified SQL")
	}
}

func TestValidateManifest(t *testing.T) {
	loader := NewPackLoader()
	
	tests := []struct {
		name        string
		manifest    *PackManifest
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid manifest",
			manifest: &PackManifest{
				APIVersion: "v1",
				Kind:       "QueryPack",
				Metadata: PackMetadata{
					Name:     "test",
					Version:  "1.0.0",
					Provider: "aws",
				},
				Spec: PackSpec{
					Queries: []QuerySpec{
						{
							ID:        "test-query",
							Title:     "Test Query",
							Severity:  "HIGH",
							QueryFile: "test.sql",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing name",
			manifest: &PackManifest{
				APIVersion: "v1",
				Kind:       "QueryPack",
				Metadata: PackMetadata{
					Version:  "1.0.0",
					Provider: "aws",
				},
			},
			expectError: true,
			errorMsg:    "metadata.name is required",
		},
		{
			name: "invalid version",
			manifest: &PackManifest{
				APIVersion: "v1",
				Kind:       "QueryPack",
				Metadata: PackMetadata{
					Name:     "test",
					Version:  "invalid",
					Provider: "aws",
				},
			},
			expectError: true,
			errorMsg:    "invalid version format",
		},
		{
			name: "duplicate query ID",
			manifest: &PackManifest{
				APIVersion: "v1",
				Kind:       "QueryPack",
				Metadata: PackMetadata{
					Name:     "test",
					Version:  "1.0.0",
					Provider: "aws",
				},
				Spec: PackSpec{
					Queries: []QuerySpec{
						{
							ID:        "duplicate",
							Title:     "First Query",
							Severity:  "HIGH",
							QueryFile: "test1.sql",
						},
						{
							ID:        "duplicate",
							Title:     "Second Query",
							Severity:  "MEDIUM",
							QueryFile: "test2.sql",
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "duplicate",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loader.ValidateManifest(tt.manifest)
			
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}