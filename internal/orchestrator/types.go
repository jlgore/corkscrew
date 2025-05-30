package orchestrator

import (
	"time"
)

// DiscoveryResult contains the raw results from discovery sources
type DiscoveryResult struct {
	Provider      string                 `json:"provider"`
	DiscoveredAt  time.Time              `json:"discovered_at"`
	Sources       map[string]interface{} `json:"sources"`        // Source name -> raw data
	Metadata      map[string]interface{} `json:"metadata"`       // Provider-specific metadata
	Errors        []DiscoveryError       `json:"errors,omitempty"`
}

// DiscoveryError represents an error during discovery
type DiscoveryError struct {
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Retryable bool      `json:"retryable"`
}

// AnalysisResult contains provider-agnostic analysis results
type AnalysisResult struct {
	Provider      string                 `json:"provider"`
	AnalyzedAt    time.Time              `json:"analyzed_at"`
	Services      []ServiceInfo          `json:"services"`
	Resources     []ResourceInfo         `json:"resources"`
	Operations    []OperationInfo        `json:"operations"`
	Relationships []RelationshipInfo     `json:"relationships"`
	Metadata      map[string]interface{} `json:"metadata"` // Provider-specific data
	Warnings      []string               `json:"warnings,omitempty"`
}

// ServiceInfo represents a discovered service (cloud-agnostic)
type ServiceInfo struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Description string                 `json:"description"`
	Version     string                 `json:"version"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ResourceInfo represents a discovered resource type
type ResourceInfo struct {
	Name         string                 `json:"name"`
	Service      string                 `json:"service"`
	DisplayName  string                 `json:"display_name"`
	Description  string                 `json:"description"`
	Identifiers  []string               `json:"identifiers"`
	Attributes   []AttributeInfo        `json:"attributes"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// AttributeInfo describes a resource attribute
type AttributeInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// OperationInfo represents a discovered operation
type OperationInfo struct {
	Name         string                 `json:"name"`
	Service      string                 `json:"service"`
	ResourceType string                 `json:"resource_type"`
	Type         string                 `json:"type"` // List, Get, Create, Update, Delete
	Description  string                 `json:"description"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// RelationshipInfo describes relationships between resources
type RelationshipInfo struct {
	SourceResource string `json:"source_resource"`
	TargetResource string `json:"target_resource"`
	Type           string `json:"type"` // references, contains, depends_on
	Cardinality    string `json:"cardinality"` // one-to-one, one-to-many, many-to-many
	Description    string `json:"description"`
}

// GenerationResult contains code generation results
type GenerationResult struct {
	Provider     string              `json:"provider"`
	GeneratedAt  time.Time           `json:"generated_at"`
	Files        []GeneratedFile     `json:"files"`
	Stats        GenerationStats     `json:"stats"`
	Errors       []string            `json:"errors,omitempty"`
}

// GeneratedFile represents a generated code file
type GeneratedFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Template string `json:"template"`
	Service  string `json:"service"`
	Resource string `json:"resource,omitempty"`
}

// GenerationStats contains generation statistics
type GenerationStats struct {
	TotalFiles       int `json:"total_files"`
	TotalServices    int `json:"total_services"`
	TotalResources   int `json:"total_resources"`
	TotalOperations  int `json:"total_operations"`
	GenerationTimeMs int `json:"generation_time_ms"`
}

// PipelineStatus represents the current state of the discovery pipeline
type PipelineStatus struct {
	Provider        string                `json:"provider"`
	CurrentPhase    string                `json:"current_phase"` // discovering, analyzing, generating, complete
	Progress        float64               `json:"progress"`      // 0.0 to 1.0
	StartedAt       time.Time             `json:"started_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	CompletedAt     *time.Time            `json:"completed_at,omitempty"`
	PhaseDetails    map[string]PhaseInfo  `json:"phase_details"`
	Errors          []string              `json:"errors,omitempty"`
}

// PhaseInfo contains details about a pipeline phase
type PhaseInfo struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"` // pending, running, completed, failed
	Progress    float64    `json:"progress"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// CacheStrategy defines caching behavior
type CacheStrategy struct {
	Enabled    bool          `json:"enabled"`
	TTL        time.Duration `json:"ttl"`
	MaxSize    int64         `json:"max_size"` // bytes
	EvictPolicy string       `json:"evict_policy"` // LRU, LFU, FIFO
}

// SourceConfig is the interface for discovery source configuration
type SourceConfig interface {
	Validate() error
}

// FileInfo represents discovered file information (for GitHub source)
type FileInfo struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	Type         string    `json:"type"` // file, directory
	URL          string    `json:"url"`
	SHA          string    `json:"sha"`
	LastModified time.Time `json:"last_modified"`
}

// GitHubDiscoveryResult contains results from GitHub discovery
type GitHubDiscoveryResult struct {
	Files       []FileInfo        `json:"files"`
	Directories []string          `json:"directories"`
	Metadata    map[string]string `json:"metadata"`
}

// APIDiscoveryResult contains results from API discovery
type APIDiscoveryResult struct {
	StatusCode int                    `json:"status_code"`
	Headers    map[string][]string    `json:"headers"`
	Body       []byte                 `json:"body"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Type        string            `json:"type"` // bearer, basic, oauth, custom
	Credentials map[string]string `json:"credentials"`
}