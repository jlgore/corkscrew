package compliance

import (
	"fmt"
	"path/filepath"
	"time"
)

// QueryPack represents a collection of compliance queries
type QueryPack struct {
	Metadata PackMetadata
	Queries  []ComplianceQuery
	Parameters []PackParameter
}

// PackMetadata contains metadata about a query pack
type PackMetadata struct {
	APIVersion  string            `yaml:"apiVersion"`
	Kind        string            `yaml:"kind"`
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Author      string            `yaml:"author"`
	Tags        []string          `yaml:"tags"`
	Provider    string            `yaml:"provider"` // aws, azure, gcp, etc.
	Resources   []string          `yaml:"resources"` // s3, ec2, etc.
}

// ComplianceQuery represents a single compliance check
type ComplianceQuery struct {
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Severity     string            `yaml:"severity"` // HIGH, MEDIUM, LOW
	Category     string            `yaml:"category"` // security, cost, operations, etc.
	Tags         []string          `yaml:"tags"`
	File         string            `yaml:"file"`
	SQL          string            `yaml:"-"` // Loaded from file
	Parameters   []string          `yaml:"parameters"`
	DependsOn    []string          `yaml:"depends_on"`
	Remediation  RemediationInfo   `yaml:"remediation"`
	References   []Reference       `yaml:"references"`
}

// PackParameter defines a parameter that can be used across queries
type PackParameter struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Type        string      `yaml:"type"` // string, int, bool, list
	Default     interface{} `yaml:"default"`
	Required    bool        `yaml:"required"`
}

// RemediationInfo provides guidance on fixing compliance issues
type RemediationInfo struct {
	Description string   `yaml:"description"`
	Steps       []string `yaml:"steps"`
	Links       []string `yaml:"links"`
	Terraform   string   `yaml:"terraform"