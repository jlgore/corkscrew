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
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"display_name"`
	Provider      string                 `json:"provider"`
	Region        string                 `json:"region"`
	Category      string                 `json:"category"`
	Description   string                 `json:"description"`
	Endpoints     []string               `json:"endpoints"`
	ResourceTypes []string               `json:"resource_types"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// Resource represents a cloud resource
type Resource struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Service    string                 `json:"service"`
	Provider   string                 `json:"provider"`
	Region     string                 `json:"region"`
	ARN        string                 `json:"arn,omitempty"`
	Status     string                 `json:"status"`
	CreatedAt  *time.Time             `json:"created_at,omitempty"`
	ModifiedAt *time.Time             `json:"modified_at,omitempty"`
	ScannedAt  *time.Time             `json:"scanned_at,omitempty"`
	Tags       map[string]string      `json:"tags,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	RawData    interface{}            `json:"raw_data,omitempty"`

	// Cross-cloud attributes
	CrossCloudID       string                 `json:"cross_cloud_id,omitempty"`
	IPAddresses        []IPAddress            `json:"ip_addresses,omitempty"`
	DNSNames           []DNSRecord            `json:"dns_names,omitempty"`
	NetworkInterfaces  []NetworkInterface     `json:"network_interfaces,omitempty"`
	SecurityGroups     []SecurityGroupRef     `json:"security_groups,omitempty"`
	LoadBalancers      []LoadBalancerRef      `json:"load_balancers,omitempty"`
	Certificates       []CertificateRef       `json:"certificates,omitempty"`
	IAMRoles           []IAMRoleRef           `json:"iam_roles,omitempty"`
	VPNConnections     []VPNConnectionRef     `json:"vpn_connections,omitempty"`
	DirectConnections  []DirectConnectionRef  `json:"direct_connections,omitempty"`
	PeeringConnections []PeeringConnectionRef `json:"peering_connections,omitempty"`
}

// ResourceCorrelation represents a relationship between resources
type ResourceCorrelation struct {
	ID             string                 `json:"id"`
	SourceID       string                 `json:"source_id"`
	TargetID       string                 `json:"target_id"`
	SourceResource Resource               `json:"source_resource"`
	TargetResource Resource               `json:"target_resource"`
	Type           string                 `json:"type"`
	RelationType   string                 `json:"relation_type"`
	Strength       float64                `json:"strength"`
	Confidence     float64                `json:"confidence"`
	Description    string                 `json:"description"`
	Metadata       map[string]interface{} `json:"metadata"`
	DiscoveredAt   time.Time              `json:"discovered_at"`
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
	CorrelationTypeCrossCloud  CorrelationType = "cross_cloud"
)

// Cross-cloud supporting types

// IPAddress represents an IP address associated with a resource
type IPAddress struct {
	Address    string `json:"address"`
	Type       string `json:"type"`    // public, private, elastic, reserved
	Version    string `json:"version"` // ipv4, ipv6
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
	Scope      string `json:"scope"` // global, regional, local
}

// DNSRecord represents a DNS record associated with a resource
type DNSRecord struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // A, AAAA, CNAME, MX, etc.
	Values     []string `json:"values"`
	TTL        int      `json:"ttl"`
	Provider   string   `json:"provider"`
	Zone       string   `json:"zone"`
	ResourceID string   `json:"resource_id"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	MacAddress  string      `json:"mac_address,omitempty"`
	IPAddresses []IPAddress `json:"ip_addresses"`
	SubnetID    string      `json:"subnet_id"`
	VpcID       string      `json:"vpc_id"`
	Provider    string      `json:"provider"`
	Region      string      `json:"region"`
	Status      string      `json:"status"`
}

// SecurityGroupRef represents a reference to a security group
type SecurityGroupRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	VpcID    string `json:"vpc_id,omitempty"`
}

// LoadBalancerRef represents a reference to a load balancer
type LoadBalancerRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // application, network, gateway, classic
	Provider string `json:"provider"`
	Region   string `json:"region"`
	DNS      string `json:"dns,omitempty"`
}

// CertificateRef represents a reference to an SSL/TLS certificate
type CertificateRef struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	Region          string     `json:"region"`
	Domain          string     `json:"domain"`
	SubjectAltNames []string   `json:"subject_alt_names,omitempty"`
	Issuer          string     `json:"issuer"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Status          string     `json:"status"`
}

// IAMRoleRef represents a reference to an IAM role
type IAMRoleRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ARN         string `json:"arn"`
	Provider    string `json:"provider"`
	AccountID   string `json:"account_id,omitempty"`
	Path        string `json:"path,omitempty"`
	TrustPolicy string `json:"trust_policy,omitempty"`
}

// VPNConnectionRef represents a reference to a VPN connection
type VPNConnectionRef struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	Type            string `json:"type"` // site-to-site, client, inter-region
	Status          string `json:"status"`
	LocalGatewayID  string `json:"local_gateway_id"`
	RemoteGatewayID string `json:"remote_gateway_id,omitempty"`
	RemoteProvider  string `json:"remote_provider,omitempty"`
	RemoteRegion    string `json:"remote_region,omitempty"`
}

// DirectConnectionRef represents a reference to a direct connection (AWS Direct Connect, Azure ExpressRoute, etc.)
type DirectConnectionRef struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Region         string `json:"region"`
	Type           string `json:"type"` // dedicated, hosted, express_route, interconnect
	Status         string `json:"status"`
	Bandwidth      string `json:"bandwidth"`
	Location       string `json:"location"`
	RemoteProvider string `json:"remote_provider,omitempty"`
	RemoteRegion   string `json:"remote_region,omitempty"`
}

// PeeringConnectionRef represents a reference to a peering connection
type PeeringConnectionRef struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	Status          string `json:"status"`
	LocalVpcID      string `json:"local_vpc_id"`
	RemoteVpcID     string `json:"remote_vpc_id"`
	RemoteProvider  string `json:"remote_provider,omitempty"`
	RemoteRegion    string `json:"remote_region,omitempty"`
	RemoteAccountID string `json:"remote_account_id,omitempty"`
}

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
