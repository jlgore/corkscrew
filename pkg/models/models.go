package models

import (
	"time"
)

// Region represents a cloud provider region
type Region struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Location    string                 `json:"location"`
	Available   bool                   `json:"available"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// Service represents a cloud service
type Service struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	Provider     string                 `json:"provider"`
	Region       string                 `json:"region"`
	Category     string                 `json:"category"`
	Description  string                 `json:"description"`
	Endpoints    []string               `json:"endpoints"`
	ResourceTypes []string              `json:"resource_types"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// Resource represents a cloud resource
type Resource struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Service      string                 `json:"service"`
	Provider     string                 `json:"provider"`
	Region       string                 `json:"region"`
	ARN          string                 `json:"arn,omitempty"`
	Status       string                 `json:"status"`
	CreatedAt    *time.Time             `json:"created_at,omitempty"`
	ModifiedAt   *time.Time             `json:"modified_at,omitempty"`
	Tags         map[string]string      `json:"tags,omitempty"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	RawData      interface{}            `json:"raw_data,omitempty"`
}

// ResourceCorrelation represents a relationship between resources
type ResourceCorrelation struct {
	ID              string                 `json:"id"`
	SourceID        string                 `json:"source_id"`
	TargetID        string                 `json:"target_id"`
	SourceResource  Resource               `json:"source_resource"`
	TargetResource  Resource               `json:"target_resource"`
	Type            string                 `json:"type"`
	RelationType    string                 `json:"relation_type"`
	Strength        float64                `json:"strength"`
	Confidence      float64                `json:"confidence"`
	Description     string                 `json:"description"`
	Metadata        map[string]interface{} `json:"metadata"`
	DiscoveredAt    time.Time              `json:"discovered_at"`
}

// CorrelationType defines the types of correlations between resources
type CorrelationType string

const (
	CorrelationTypeDependency  CorrelationType = "dependency"
	CorrelationTypeAssociation CorrelationType = "association"
	CorrelationTypeOwnership   CorrelationType = "ownership"
	CorrelationTypeNetworking  CorrelationType = "networking"
	CorrelationTypeSecurity    CorrelationType = "security"
	CorrelationTypeCompliance  CorrelationType = "compliance"
)

// Provider represents a cloud provider
type Provider struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	Description  string                 `json:"description"`
	Version      string                 `json:"version"`
	Capabilities []string               `json:"capabilities"`
	Regions      []Region               `json:"regions"`
	Services     []Service              `json:"services"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// DiscoveryMetadata contains metadata about the discovery process
type DiscoveryMetadata struct {
	StartTime        time.Time              `json:"start_time"`
	EndTime          time.Time              `json:"end_time"`
	Duration         time.Duration          `json:"duration"`
	ResourceCount    int                    `json:"resource_count"`
	ServiceCount     int                    `json:"service_count"`
	RegionCount      int                    `json:"region_count"`
	ProviderCount    int                    `json:"provider_count"`
	CorrelationCount int                    `json:"correlation_count"`
	ErrorCount       int                    `json:"error_count"`
	Errors           []string               `json:"errors"`
	Metadata         map[string]interface{} `json:"metadata"`
}