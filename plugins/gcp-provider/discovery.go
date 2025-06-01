package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/serviceusage/v1"
)

// ServiceDiscovery handles dynamic service discovery for GCP
type ServiceDiscovery struct {
	clientFactory      *ClientFactory
	libraryAnalyzer    *ClientLibraryAnalyzer
	hierarchyAnalyzer  *ResourceHierarchyAnalyzer
}

// NewServiceDiscovery creates a new service discovery instance
func NewServiceDiscovery(cf *ClientFactory) *ServiceDiscovery {
	// Initialize client library analyzer
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(os.Getenv("HOME"), "go")
	}
	
	libraryAnalyzer := NewClientLibraryAnalyzer(goPath, "cloud.google.com/go")
	
	sd := &ServiceDiscovery{
		clientFactory:   cf,
		libraryAnalyzer: libraryAnalyzer,
	}
	
	return sd
}

// DiscoverServices discovers all enabled GCP services across the provided projects
func (sd *ServiceDiscovery) DiscoverServices(ctx context.Context, projectIDs []string) ([]*pb.ServiceInfo, error) {
	// Initialize Service Usage API client
	client, err := sd.clientFactory.GetServiceUsageClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create service usage client: %w", err)
	}
	
	// Use a map to deduplicate services across projects
	serviceMap := make(map[string]*pb.ServiceInfo)
	
	// Discover services for each project
	for _, projectID := range projectIDs {
		projectServices, err := sd.discoverProjectServices(ctx, client, projectID)
		if err != nil {
			// Log error but continue with other projects
			fmt.Printf("Error discovering services for project %s: %v\n", projectID, err)
			continue
		}
		
		// Merge services
		for _, service := range projectServices {
			if existing, ok := serviceMap[service.Name]; ok {
				// Merge metadata if service already exists
				sd.mergeServiceInfo(existing, service)
			} else {
				serviceMap[service.Name] = service
			}
		}
	}
	
	// Convert map to slice
	var services []*pb.ServiceInfo
	for _, service := range serviceMap {
		services = append(services, service)
	}
	
	return services, nil
}

// DiscoverServicesWithLibraryAnalysis discovers services using client library analysis
func (sd *ServiceDiscovery) DiscoverServicesWithLibraryAnalysis(ctx context.Context, projectIDs []string) ([]*pb.ServiceInfo, error) {
	// Use library analyzer to discover services from GCP client libraries
	libraryServices, err := sd.libraryAnalyzer.AnalyzeGCPLibraries(ctx)
	if err != nil {
		fmt.Printf("Library analysis failed, falling back to API discovery: %v\n", err)
		return sd.DiscoverServices(ctx, projectIDs)
	}

	// Initialize hierarchy analyzer with discovered service mappings
	sd.hierarchyAnalyzer = NewResourceHierarchyAnalyzer(sd.libraryAnalyzer.ServiceMappings)
	
	// Analyze resource hierarchies
	if err := sd.hierarchyAnalyzer.AnalyzeResourceHierarchies(ctx); err != nil {
		fmt.Printf("Hierarchy analysis failed: %v\n", err)
	}

	// Enhance services with API-discovered information
	apiServices, err := sd.DiscoverServices(ctx, projectIDs)
	if err != nil {
		fmt.Printf("API discovery failed: %v\n", err)
		return libraryServices, nil
	}

	// Merge library-discovered and API-discovered services
	mergedServices := sd.mergeServiceDiscoveryResults(libraryServices, apiServices)
	
	return mergedServices, nil
}

// discoverProjectServices discovers services enabled in a specific project
func (sd *ServiceDiscovery) discoverProjectServices(ctx context.Context, client *serviceusage.Service, projectID string) ([]*pb.ServiceInfo, error) {
	projectName := fmt.Sprintf("projects/%s", projectID)
	
	var services []*pb.ServiceInfo
	pageToken := ""
	
	for {
		// List enabled services
		resp, err := client.Services.List(projectName).
			Filter("state:ENABLED").
			PageToken(pageToken).
			Context(ctx).
			Do()
		
		if err != nil {
			return nil, fmt.Errorf("failed to list services: %w", err)
		}
		
		for _, svc := range resp.Services {
			serviceInfo := sd.parseServiceInfo(svc, projectID)
			if serviceInfo != nil {
				services = append(services, serviceInfo)
			}
		}
		
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	
	// Add resource types for each service
	for _, service := range services {
		resourceTypeNames := sd.getResourceTypesForService(service.Name)
		service.ResourceTypes = make([]*pb.ResourceType, len(resourceTypeNames))
		for i, name := range resourceTypeNames {
			service.ResourceTypes[i] = &pb.ResourceType{
				Name: name,
				TypeName: name,
			}
		}
	}
	
	return services, nil
}

func (sd *ServiceDiscovery) parseServiceInfo(svc *serviceusage.GoogleApiServiceusageV1Service, projectID string) *pb.ServiceInfo {
	if svc == nil || svc.Config == nil {
		return nil
	}
	
	// Extract service name from the full service name
	// Format: projects/{project}/services/{service}
	parts := strings.Split(svc.Name, "/")
	serviceName := parts[len(parts)-1]
	
	// Remove .googleapis.com suffix to get short name
	shortName := strings.TrimSuffix(serviceName, ".googleapis.com")
	
	serviceInfo := &pb.ServiceInfo{
		Name:        shortName,
		DisplayName: svc.Config.Title,
		PackageName: serviceName,
		ClientType:  "google-cloud-go",
	}
	
	return serviceInfo
}

func (sd *ServiceDiscovery) extractDescription(config *serviceusage.GoogleApiServiceusageV1ServiceConfig) string {
	if config == nil {
		return ""
	}
	
	if config.Documentation != nil && config.Documentation.Summary != "" {
		return config.Documentation.Summary
	}
	
	return config.Title
}

func (sd *ServiceDiscovery) mergeServiceInfo(existing, new *pb.ServiceInfo) {
	// For now, just keep the existing service info
	// In the future, could merge resource types or other information
}

func (sd *ServiceDiscovery) getResourceTypesForService(service string) []string {
	// Predefined mapping of services to their common resource types
	serviceResourceTypes := map[string][]string{
		"compute": {
			"Instance", "Disk", "Network", "Subnetwork", 
			"Firewall", "Address", "Snapshot", "Image",
			"InstanceTemplate", "InstanceGroup", "HealthCheck",
			"BackendService", "UrlMap", "TargetHttpProxy",
			"TargetHttpsProxy", "SslCertificate", "ForwardingRule",
		},
		"storage": {
			"Bucket", "Object", "ObjectAccessControl",
			"BucketAccessControl", "DefaultObjectAccessControl",
		},
		"bigquery": {
			"Dataset", "Table", "View", "Model", "Routine",
			"Job", "Reservation", "CapacityCommitment",
		},
		"pubsub": {
			"Topic", "Subscription", "Schema", "Snapshot",
		},
		"container": {
			"Cluster", "NodePool", "Operation",
		},
		"cloudsql": {
			"Instance", "Database", "BackupRun", "User",
			"SslCert", "Operation",
		},
		"run": {
			"Service", "Revision", "Configuration", "Route",
			"DomainMapping", "Job", "Execution",
		},
		"cloudfunctions": {
			"Function", "Operation",
		},
		"appengine": {
			"Application", "Service", "Version", "Instance",
			"Firewall", "AuthorizedCertificate", "DomainMapping",
		},
		"dataflow": {
			"Job", "Template", "Snapshot",
		},
		"dataproc": {
			"Cluster", "Job", "WorkflowTemplate", "AutoscalingPolicy",
		},
		"spanner": {
			"Instance", "Database", "Backup", "Operation",
		},
		"firestore": {
			"Database", "Document", "Collection", "Index",
			"CollectionGroup", "Field",
		},
		"memorystore": {
			"Instance", "Operation",
		},
		"filestore": {
			"Instance", "Backup", "Operation",
		},
		"composer": {
			"Environment", "Operation",
		},
		"logging": {
			"LogEntry", "LogSink", "LogMetric", "LogBucket",
			"LogView", "LogExclusion",
		},
		"monitoring": {
			"AlertPolicy", "NotificationChannel", "UptimeCheckConfig",
			"Group", "MetricDescriptor", "MonitoredResourceDescriptor",
			"Dashboard", "Service", "ServiceLevelObjective",
		},
		"iam": {
			"ServiceAccount", "ServiceAccountKey", "Role",
			"Policy", "Binding",
		},
		"resourcemanager": {
			"Project", "Folder", "Organization", "Tag",
			"TagKey", "TagValue", "TagBinding",
		},
		"dns": {
			"ManagedZone", "ResourceRecordSet", "Policy",
			"Change",
		},
		"loadbalancing": {
			"BackendService", "HealthCheck", "UrlMap",
			"TargetHttpProxy", "TargetHttpsProxy", "ForwardingRule",
			"SslCertificate", "SslPolicy",
		},
		"vpn": {
			"Gateway", "Tunnel", "ExternalVpnGateway",
			"VpnConnection", "Route",
		},
		"secretmanager": {
			"Secret", "SecretVersion",
		},
		"artifactregistry": {
			"Repository", "Package", "Version", "Tag",
			"File",
		},
		"cloudkms": {
			"KeyRing", "CryptoKey", "CryptoKeyVersion",
			"ImportJob",
		},
		"cloudbuild": {
			"Build", "Trigger", "WorkerPool",
		},
		"cloudtasks": {
			"Queue", "Task",
		},
		"scheduler": {
			"Job",
		},
		"apigateway": {
			"Api", "ApiConfig", "Gateway",
		},
		"healthcare": {
			"Dataset", "DicomStore", "FhirStore", "Hl7V2Store",
		},
		"ml": {
			"Model", "Version", "Job", "Operation",
		},
		"notebooks": {
			"Instance", "Environment", "Schedule", "Execution",
		},
		"dataprep": {
			"Flow", "Recipe", "Job",
		},
		"datacatalog": {
			"EntryGroup", "Entry", "TagTemplate", "Tag",
			"PolicyTag", "Taxonomy",
		},
		"dataplex": {
			"Lake", "Zone", "Asset", "Task", "Job",
			"Environment", "Session",
		},
		"datastream": {
			"Stream", "ConnectionProfile", "PrivateConnection",
			"Route",
		},
		"eventarc": {
			"Trigger", "Channel", "Provider",
		},
		"gkehub": {
			"Membership", "Feature", "Fleet",
		},
		"networkconnectivity": {
			"Hub", "Spoke", "ServiceConnectionPolicy",
		},
		"networksecurity": {
			"AuthorizationPolicy", "ClientTlsPolicy", "ServerTlsPolicy",
			"SecurityProfile", "SecurityProfileGroup",
		},
		"privateca": {
			"CertificateAuthority", "Certificate", "CertificateTemplate",
			"CaPool",
		},
		"recaptchaenterprise": {
			"Key", "Assessment",
		},
		"retail": {
			"Catalog", "Product", "Branch",
		},
		"vision": {
			"Product", "ProductSet", "ReferenceImage",
		},
		"speech": {
			"CustomClass", "PhraseSet", "Model",
		},
		"translate": {
			"Glossary", "Model",
		},
		"video": {
			"Job", "Template",
		},
	}
	
	if types, ok := serviceResourceTypes[service]; ok {
		return types
	}
	
	// Return empty slice for unknown services
	return []string{}
}

// mergeServiceDiscoveryResults merges library-analyzed and API-discovered services
func (sd *ServiceDiscovery) mergeServiceDiscoveryResults(libraryServices, apiServices []*pb.ServiceInfo) []*pb.ServiceInfo {
	serviceMap := make(map[string]*pb.ServiceInfo)
	
	// Add library-discovered services first (more detailed)
	for _, service := range libraryServices {
		serviceMap[service.Name] = service
	}
	
	// Merge with API-discovered services
	for _, apiService := range apiServices {
		if existing, ok := serviceMap[apiService.Name]; ok {
			// Merge additional information from API discovery
			if existing.DisplayName == "" {
				existing.DisplayName = apiService.DisplayName
			}
			// Keep library-analyzed resource types as they're more detailed
		} else {
			// Add API-only discovered service
			serviceMap[apiService.Name] = apiService
		}
	}
	
	// Convert back to slice
	var mergedServices []*pb.ServiceInfo
	for _, service := range serviceMap {
		mergedServices = append(mergedServices, service)
	}
	
	return mergedServices
}

// GetServiceMapping returns the service mapping for a specific service
func (sd *ServiceDiscovery) GetServiceMapping(serviceName string) *ServiceMapping {
	if sd.libraryAnalyzer == nil {
		return nil
	}
	
	return sd.libraryAnalyzer.ServiceMappings[serviceName]
}

// GetHierarchyAnalyzer returns the hierarchy analyzer
func (sd *ServiceDiscovery) GetHierarchyAnalyzer() *ResourceHierarchyAnalyzer {
	return sd.hierarchyAnalyzer
}