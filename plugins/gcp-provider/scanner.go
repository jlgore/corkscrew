package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResourceScanner handles resource scanning using GCP APIs
type ResourceScanner struct {
	clientFactory  *ClientFactory
	assetInventory *AssetInventoryClient
	scanners       map[string]ServiceScanner
}

// ServiceScanner interface for service-specific scanners
type ServiceScanner interface {
	Scan(ctx context.Context) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error)
}

// NewResourceScanner creates a new resource scanner
func NewResourceScanner(cf *ClientFactory) *ResourceScanner {
	rs := &ResourceScanner{
		clientFactory: cf,
		scanners:      make(map[string]ServiceScanner),
	}
	
	// Register service-specific scanners
	rs.registerScanners()
	
	return rs
}

// SetAssetInventory sets the asset inventory client for optimized scanning
func (rs *ResourceScanner) SetAssetInventory(ai *AssetInventoryClient) {
	rs.assetInventory = ai
}

// registerScanners registers all service-specific scanners
func (rs *ResourceScanner) registerScanners() {
	rs.scanners["compute"] = NewComputeScanner(rs.clientFactory)
	rs.scanners["storage"] = NewStorageScanner(rs.clientFactory)
	rs.scanners["container"] = NewContainerScanner(rs.clientFactory)
	// Add more scanners as needed
}

// ScanService scans resources for a specific service
func (rs *ResourceScanner) ScanService(ctx context.Context, service string) ([]*pb.ResourceRef, error) {
	// Try Asset Inventory first if available
	if rs.assetInventory != nil {
		assetTypes := mapServiceToAssetTypes(service)
		resources, err := rs.assetInventory.QueryAssetsByType(ctx, assetTypes)
		if err == nil {
			return resources, nil
		}
		// Fall back to API scanning on error
		log.Printf("Asset Inventory failed for service %s, falling back to API: %v", service, err)
	}
	
	// Use service-specific scanner
	scanner, ok := rs.scanners[service]
	if !ok {
		return nil, fmt.Errorf("no scanner available for service: %s", service)
	}
	
	return scanner.Scan(ctx)
}

// ScanAllServices scans resources across all enabled services
func (rs *ResourceScanner) ScanAllServices(ctx context.Context) ([]*pb.ResourceRef, error) {
	// Try Asset Inventory first if available  
	if rs.assetInventory != nil {
		resources, err := rs.assetInventory.QueryAllAssets(ctx)
		if err == nil {
			return resources, nil
		}
		// Fall back to API scanning on error
		log.Printf("Asset Inventory failed, falling back to API scanning: %v", err)
	}
	
	// Scan all services concurrently
	var wg sync.WaitGroup
	resultChan := make(chan scanServiceResult, len(rs.scanners))
	
	for serviceName, scanner := range rs.scanners {
		wg.Add(1)
		go func(name string, s ServiceScanner) {
			defer wg.Done()
			
			resources, err := s.Scan(ctx)
			resultChan <- scanServiceResult{
				service:   name,
				resources: resources,
				err:       err,
			}
		}(serviceName, scanner)
	}
	
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	// Collect results
	var allResources []*pb.ResourceRef
	for result := range resultChan {
		if result.err != nil {
			log.Printf("Error scanning %s: %v", result.service, result.err)
			continue
		}
		allResources = append(allResources, result.resources...)
	}
	
	return allResources, nil
}

// StreamScanResources streams resources as they are discovered
func (rs *ResourceScanner) StreamScanResources(ctx context.Context, services []string, resourceChan chan<- *pb.Resource) error {
	defer close(resourceChan)
	
	for _, service := range services {
		refs, err := rs.ScanService(ctx, service)
		if err != nil {
			log.Printf("Error scanning service %s: %v", service, err)
			continue
		}
		
		for _, ref := range refs {
			resource := convertRefToResource(ref)
			select {
			case resourceChan <- resource:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	
	return nil
}

// DescribeResource gets detailed information about a specific resource
func (rs *ResourceScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	scanner, ok := rs.scanners[ref.Service]
	if !ok {
		return nil, fmt.Errorf("no scanner available for service: %s", ref.Service)
	}
	
	return scanner.DescribeResource(ctx, ref.Id)
}

// ComputeScanner implements scanning for Compute Engine resources
type ComputeScanner struct {
	clientFactory *ClientFactory
}

func NewComputeScanner(cf *ClientFactory) *ComputeScanner {
	return &ComputeScanner{clientFactory: cf}
}

func (cs *ComputeScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := cs.clientFactory.GetComputeClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := cs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List instances across all zones
		req := client.Instances.AggregatedList(projectID)
		if err := req.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
			for zone, instanceList := range page.Items {
				if instanceList.Instances != nil {
					for _, instance := range instanceList.Instances {
						ref := &pb.ResourceRef{
							Id:      fmt.Sprintf("projects/%s/zones/%s/instances/%s", 
								    projectID, extractZoneFromURL(zone), instance.Name),
							Name:    instance.Name,
							Type:    "compute.googleapis.com/Instance",
							Service: "compute",
							Region:  extractZoneFromURL(zone),
							BasicAttributes: map[string]string{
								"machine_type": extractLastSegment(instance.MachineType),
								"status":       instance.Status,
								"self_link":    instance.SelfLink,
								"project_id":   projectID,
							},
						}
						
						// Add labels
						for k, v := range instance.Labels {
							ref.BasicAttributes["label_"+k] = v
						}
						
						resources = append(resources, ref)
					}
				}
			}
			return nil
		}); err != nil {
			log.Printf("Failed to list instances for project %s: %v", projectID, err)
			continue
		}
		
		// Also scan disks
		req2 := client.Disks.AggregatedList(projectID)
		if err := req2.Pages(ctx, func(page *compute.DiskAggregatedList) error {
			for zone, diskList := range page.Items {
				if diskList.Disks != nil {
					for _, disk := range diskList.Disks {
						ref := &pb.ResourceRef{
							Id:      fmt.Sprintf("projects/%s/zones/%s/disks/%s", 
								    projectID, extractZoneFromURL(zone), disk.Name),
							Name:    disk.Name,
							Type:    "compute.googleapis.com/Disk",
							Service: "compute",
							Region:  extractZoneFromURL(zone),
							BasicAttributes: map[string]string{
								"size_gb":    fmt.Sprintf("%d", disk.SizeGb),
								"type":       extractLastSegment(disk.Type),
								"status":     disk.Status,
								"self_link":  disk.SelfLink,
								"project_id": projectID,
							},
						}
						
						// Add labels
						for k, v := range disk.Labels {
							ref.BasicAttributes["label_"+k] = v
						}
						
						resources = append(resources, ref)
					}
				}
			}
			return nil
		}); err != nil {
			log.Printf("Failed to list disks for project %s: %v", projectID, err)
			continue
		}
		
		// Scan networks
		req3 := client.Networks.List(projectID)
		if err := req3.Pages(ctx, func(page *compute.NetworkList) error {
			for _, network := range page.Items {
				ref := &pb.ResourceRef{
					Id:      fmt.Sprintf("projects/%s/global/networks/%s", projectID, network.Name),
					Name:    network.Name,
					Type:    "compute.googleapis.com/Network",
					Service: "compute",
					Region:  "global",
					BasicAttributes: map[string]string{
						"self_link":  network.SelfLink,
						"project_id": projectID,
					},
				}
				
				if network.Description != "" {
					ref.BasicAttributes["description"] = network.Description
				}
				
				resources = append(resources, ref)
			}
			return nil
		}); err != nil {
			log.Printf("Failed to list networks for project %s: %v", projectID, err)
			continue
		}
		
		// Scan subnetworks
		req4 := client.Subnetworks.AggregatedList(projectID)
		if err := req4.Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
			for region, subnetList := range page.Items {
				if subnetList.Subnetworks != nil {
					for _, subnet := range subnetList.Subnetworks {
						ref := &pb.ResourceRef{
							Id:      fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", 
								    projectID, extractRegionFromURL(region), subnet.Name),
							Name:    subnet.Name,
							Type:    "compute.googleapis.com/Subnetwork",
							Service: "compute",
							Region:  extractRegionFromURL(region),
							BasicAttributes: map[string]string{
								"cidr_range":  subnet.IpCidrRange,
								"network":     extractLastSegment(subnet.Network),
								"self_link":   subnet.SelfLink,
								"project_id":  projectID,
							},
						}
						
						if subnet.Description != "" {
							ref.BasicAttributes["description"] = subnet.Description
						}
						
						resources = append(resources, ref)
					}
				}
			}
			return nil
		}); err != nil {
			log.Printf("Failed to list subnetworks for project %s: %v", projectID, err)
			continue
		}
	}
	
	return resources, nil
}

func (cs *ComputeScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Parse resource ID to extract zone and instance name
	// Format: projects/{project}/zones/{zone}/instances/{instance}
	parts := strings.Split(resourceID, "/")
	if len(parts) < 6 {
		return nil, fmt.Errorf("invalid resource ID format")
	}
	
	projectID := parts[1]
	resourceType := parts[4] // "instances", "disks", etc.
	
	client, err := cs.clientFactory.GetComputeClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %w", err)
	}
	
	switch resourceType {
	case "instances":
		zone := parts[3]
		instanceName := parts[5]
		
		instance, err := client.Instances.Get(projectID, zone, instanceName).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get instance: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "compute",
			Type:         "compute.googleapis.com/Instance",
			Id:           resourceID,
			Name:         instance.Name,
			Region:       zone,
			Tags:         make(map[string]string),
			DiscoveredAt: timestamppb.Now(),
		}
		
		// Convert labels to tags
		for k, v := range instance.Labels {
			resource.Tags[k] = v
		}
		
		// Store raw data
		if rawData, err := json.Marshal(instance); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
		
	case "disks":
		zone := parts[3]
		diskName := parts[5]
		
		disk, err := client.Disks.Get(projectID, zone, diskName).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get disk: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "compute",
			Type:         "compute.googleapis.com/Disk",
			Id:           resourceID,
			Name:         disk.Name,
			Region:       zone,
			Tags:         make(map[string]string),
			DiscoveredAt: timestamppb.Now(),
		}
		
		// Convert labels to tags
		for k, v := range disk.Labels {
			resource.Tags[k] = v
		}
		
		// Store raw data
		if rawData, err := json.Marshal(disk); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
		
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// StorageScanner implements scanning for Cloud Storage resources
type StorageScanner struct {
	clientFactory *ClientFactory
}

func NewStorageScanner(cf *ClientFactory) *StorageScanner {
	return &StorageScanner{clientFactory: cf}
}

func (ss *StorageScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := ss.clientFactory.GetStorageClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := ss.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List all buckets
		req := client.Buckets.List(projectID)
		if err := req.Pages(ctx, func(page *storage.Buckets) error {
			for _, bucket := range page.Items {
				ref := &pb.ResourceRef{
					Id:      fmt.Sprintf("projects/%s/buckets/%s", projectID, bucket.Name),
					Name:    bucket.Name,
					Type:    "storage.googleapis.com/Bucket",
					Service: "storage",
					Region:  bucket.Location,
					BasicAttributes: map[string]string{
						"storage_class": bucket.StorageClass,
						"location_type": bucket.LocationType,
						"self_link":     bucket.SelfLink,
						"project_id":    projectID,
					},
				}
				
				// Add labels
				for k, v := range bucket.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
				
				resources = append(resources, ref)
			}
			return nil
		}); err != nil {
			log.Printf("Failed to list buckets for project %s: %v", projectID, err)
			continue
		}
	}
	
	return resources, nil
}

func (ss *StorageScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Parse resource ID
	// Format: projects/{project}/buckets/{bucket}
	parts := strings.Split(resourceID, "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid resource ID format")
	}
	
	_ = parts[1] // projectID not used in this context
	bucketName := parts[3]
	
	client, err := ss.clientFactory.GetStorageClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	
	bucket, err := client.Buckets.Get(bucketName).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}
	
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      "storage",
		Type:         "storage.googleapis.com/Bucket",
		Id:           resourceID,
		Name:         bucket.Name,
		Region:       bucket.Location,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Convert labels to tags
	for k, v := range bucket.Labels {
		resource.Tags[k] = v
	}
	
	// Store raw data
	if rawData, err := json.Marshal(bucket); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// ContainerScanner implements scanning for Kubernetes Engine resources
type ContainerScanner struct {
	clientFactory *ClientFactory
}

func NewContainerScanner(cf *ClientFactory) *ContainerScanner {
	return &ContainerScanner{clientFactory: cf}
}

func (cs *ContainerScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := cs.clientFactory.GetContainerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create container client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := cs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List all clusters
		parent := fmt.Sprintf("projects/%s/locations/-", projectID)
		resp, err := client.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list clusters for project %s: %v", projectID, err)
			continue
		}
		
		for _, cluster := range resp.Clusters {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/locations/%s/clusters/%s", 
					    projectID, cluster.Location, cluster.Name),
				Name:    cluster.Name,
				Type:    "container.googleapis.com/Cluster",
				Service: "container",
				Region:  cluster.Location,
				BasicAttributes: map[string]string{
					"status":         cluster.Status,
					"cluster_ipv4":   cluster.ClusterIpv4Cidr,
					"current_nodes":  fmt.Sprintf("%d", cluster.CurrentNodeCount),
					"self_link":      cluster.SelfLink,
					"project_id":     projectID,
				},
			}
			
			// Add labels
			for k, v := range cluster.ResourceLabels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
			
			// Also list node pools for this cluster
			for _, nodePool := range cluster.NodePools {
				npRef := &pb.ResourceRef{
					Id:      fmt.Sprintf("projects/%s/locations/%s/clusters/%s/nodePools/%s", 
						    projectID, cluster.Location, cluster.Name, nodePool.Name),
					Name:    nodePool.Name,
					Type:    "container.googleapis.com/NodePool",
					Service: "container",
					Region:  cluster.Location,
					BasicAttributes: map[string]string{
						"status":       nodePool.Status,
						"node_count":   fmt.Sprintf("%d", nodePool.InitialNodeCount),
						"cluster_name": cluster.Name,
						"self_link":    nodePool.SelfLink,
						"project_id":   projectID,
					},
				}
				
				if nodePool.Config != nil && nodePool.Config.MachineType != "" {
					npRef.BasicAttributes["machine_type"] = nodePool.Config.MachineType
				}
				
				resources = append(resources, npRef)
			}
		}
	}
	
	return resources, nil
}

func (cs *ContainerScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Parse resource ID
	// Format: projects/{project}/locations/{location}/clusters/{cluster}
	parts := strings.Split(resourceID, "/")
	if len(parts) < 6 {
		return nil, fmt.Errorf("invalid resource ID format")
	}
	
	projectID := parts[1]
	location := parts[3]
	clusterName := parts[5]
	
	client, err := cs.clientFactory.GetContainerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create container client: %w", err)
	}
	
	name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
	cluster, err := client.Projects.Locations.Clusters.Get(name).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}
	
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      "container",
		Type:         "container.googleapis.com/Cluster",
		Id:           resourceID,
		Name:         cluster.Name,
		Region:       cluster.Location,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Convert labels to tags
	for k, v := range cluster.ResourceLabels {
		resource.Tags[k] = v
	}
	
	// Store raw data
	if rawData, err := json.Marshal(cluster); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// Helper functions

func extractZoneFromURL(zoneURL string) string {
	parts := strings.Split(zoneURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return zoneURL
}

func extractRegionFromURL(regionURL string) string {
	parts := strings.Split(regionURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return regionURL
}

// extractLastSegment is defined in gcp_service_definitions.go

func convertRefToResource(ref *pb.ResourceRef) *pb.Resource {
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      ref.Service,
		Type:         ref.Type,
		Id:           ref.Id,
		Name:         ref.Name,
		Region:       ref.Region,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Extract labels as tags
	if ref.BasicAttributes != nil {
		for k, v := range ref.BasicAttributes {
			if strings.HasPrefix(k, "label_") {
				labelName := strings.TrimPrefix(k, "label_")
				resource.Tags[labelName] = v
			}
		}
		
		// Store raw resource data if available
		if rawData, ok := ref.BasicAttributes["resource_data"]; ok {
			resource.RawData = rawData
		}
	}
	
	return resource
}

// mapServiceToAssetTypes maps service names to Cloud Asset Inventory types
func mapServiceToAssetTypes(service string) []string {
	serviceToAssetTypes := map[string][]string{
		"compute": {
			"compute.googleapis.com/Instance",
			"compute.googleapis.com/Disk", 
			"compute.googleapis.com/Network",
			"compute.googleapis.com/Subnetwork",
			"compute.googleapis.com/Firewall",
			"compute.googleapis.com/Address",
			"compute.googleapis.com/Snapshot",
			"compute.googleapis.com/Image",
		},
		"storage": {
			"storage.googleapis.com/Bucket",
		},
		"container": {
			"container.googleapis.com/Cluster",
			"container.googleapis.com/NodePool",
		},
	}
	
	if types, ok := serviceToAssetTypes[service]; ok {
		return types
	}
	
	return []string{fmt.Sprintf("%s.googleapis.com/*", service)}
}

type scanServiceResult struct {
	service   string
	resources []*pb.ResourceRef
	err       error
}