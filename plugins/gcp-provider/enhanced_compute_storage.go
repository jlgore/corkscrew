package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnhancedComputeScanner provides comprehensive Compute Engine scanning
type EnhancedComputeScanner struct {
	clientFactory *ClientFactory
}

func NewEnhancedComputeScanner(cf *ClientFactory) *EnhancedComputeScanner {
	return &EnhancedComputeScanner{clientFactory: cf}
}

func (ecs *EnhancedComputeScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := ecs.clientFactory.GetComputeClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := ecs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// Scan instances
		instanceRefs, err := ecs.scanInstances(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan instances for project %s: %v", projectID, err)
		} else {
			resources = append(resources, instanceRefs...)
		}
		
		// Scan disks
		diskRefs, err := ecs.scanDisks(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan disks for project %s: %v", projectID, err)
		} else {
			resources = append(resources, diskRefs...)
		}
		
		// Scan networks
		networkRefs, err := ecs.scanNetworks(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan networks for project %s: %v", projectID, err)
		} else {
			resources = append(resources, networkRefs...)
		}
		
		// Scan subnetworks
		subnetRefs, err := ecs.scanSubnetworks(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan subnetworks for project %s: %v", projectID, err)
		} else {
			resources = append(resources, subnetRefs...)
		}
		
		// Scan firewall rules
		firewallRefs, err := ecs.scanFirewallRules(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan firewall rules for project %s: %v", projectID, err)
		} else {
			resources = append(resources, firewallRefs...)
		}
		
		// Scan addresses
		addressRefs, err := ecs.scanAddresses(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan addresses for project %s: %v", projectID, err)
		} else {
			resources = append(resources, addressRefs...)
		}
		
		// Scan snapshots
		snapshotRefs, err := ecs.scanSnapshots(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan snapshots for project %s: %v", projectID, err)
		} else {
			resources = append(resources, snapshotRefs...)
		}
		
		// Scan images
		imageRefs, err := ecs.scanImages(ctx, client, projectID)
		if err != nil {
			log.Printf("Failed to scan images for project %s: %v", projectID, err)
		} else {
			resources = append(resources, imageRefs...)
		}
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanInstances(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
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
							"machine_type":    extractLastSegment(instance.MachineType),
							"status":          instance.Status,
							"self_link":       instance.SelfLink,
							"project_id":      projectID,
							"creation_timestamp": instance.CreationTimestamp,
							"cpu_platform":    instance.CpuPlatform,
							"can_ip_forward":  fmt.Sprintf("%t", instance.CanIpForward),
						},
					}
					
					// Add network interfaces info
					if len(instance.NetworkInterfaces) > 0 {
						ref.BasicAttributes["network"] = extractLastSegment(instance.NetworkInterfaces[0].Network)
						ref.BasicAttributes["subnetwork"] = extractLastSegment(instance.NetworkInterfaces[0].Subnetwork)
						if len(instance.NetworkInterfaces[0].AccessConfigs) > 0 {
							ref.BasicAttributes["external_ip"] = instance.NetworkInterfaces[0].AccessConfigs[0].NatIP
						}
					}
					
					// Add service accounts info
					if len(instance.ServiceAccounts) > 0 {
						ref.BasicAttributes["service_account"] = instance.ServiceAccounts[0].Email
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
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanDisks(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Disks.AggregatedList(projectID)
	if err := req.Pages(ctx, func(page *compute.DiskAggregatedList) error {
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
							"size_gb":       fmt.Sprintf("%d", disk.SizeGb),
							"type":          extractLastSegment(disk.Type),
							"status":        disk.Status,
							"self_link":     disk.SelfLink,
							"project_id":    projectID,
							"creation_timestamp": disk.CreationTimestamp,
						},
					}
					
					// Add attached users
					if len(disk.Users) > 0 {
						ref.BasicAttributes["attached_to"] = strings.Join(disk.Users, ",")
					}
					
					// Add source info
					if disk.SourceImage != "" {
						ref.BasicAttributes["source_image"] = extractLastSegment(disk.SourceImage)
					}
					if disk.SourceSnapshot != "" {
						ref.BasicAttributes["source_snapshot"] = extractLastSegment(disk.SourceSnapshot)
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
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanNetworks(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Networks.List(projectID)
	if err := req.Pages(ctx, func(page *compute.NetworkList) error {
		for _, network := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/global/networks/%s", projectID, network.Name),
				Name:    network.Name,
				Type:    "compute.googleapis.com/Network",
				Service: "compute",
				Region:  "global",
				BasicAttributes: map[string]string{
					"self_link":          network.SelfLink,
					"project_id":         projectID,
					"auto_create_subnets": fmt.Sprintf("%t", network.AutoCreateSubnetworks),
					"routing_mode":       network.RoutingConfig.RoutingMode,
					"creation_timestamp": network.CreationTimestamp,
				},
			}
			
			if network.Description != "" {
				ref.BasicAttributes["description"] = network.Description
			}
			
			if network.IPv4Range != "" {
				ref.BasicAttributes["ipv4_range"] = network.IPv4Range
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanSubnetworks(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Subnetworks.AggregatedList(projectID)
	if err := req.Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
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
							"cidr_range":         subnet.IpCidrRange,
							"network":            extractLastSegment(subnet.Network),
							"self_link":          subnet.SelfLink,
							"project_id":         projectID,
							"private_ip_google_access": fmt.Sprintf("%t", subnet.PrivateIpGoogleAccess),
							"creation_timestamp": subnet.CreationTimestamp,
						},
					}
					
					if subnet.Description != "" {
						ref.BasicAttributes["description"] = subnet.Description
					}
					
					if subnet.GatewayAddress != "" {
						ref.BasicAttributes["gateway_address"] = subnet.GatewayAddress
					}
					
					resources = append(resources, ref)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanFirewallRules(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Firewalls.List(projectID)
	if err := req.Pages(ctx, func(page *compute.FirewallList) error {
		for _, firewall := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/global/firewalls/%s", projectID, firewall.Name),
				Name:    firewall.Name,
				Type:    "compute.googleapis.com/Firewall",
				Service: "compute",
				Region:  "global",
				BasicAttributes: map[string]string{
					"self_link":          firewall.SelfLink,
					"project_id":         projectID,
					"direction":          firewall.Direction,
					"priority":           fmt.Sprintf("%d", firewall.Priority),
					"network":            extractLastSegment(firewall.Network),
					"creation_timestamp": firewall.CreationTimestamp,
				},
			}
			
			if firewall.Description != "" {
				ref.BasicAttributes["description"] = firewall.Description
			}
			
			// Add source/target info
			if len(firewall.SourceRanges) > 0 {
				ref.BasicAttributes["source_ranges"] = strings.Join(firewall.SourceRanges, ",")
			}
			if len(firewall.TargetTags) > 0 {
				ref.BasicAttributes["target_tags"] = strings.Join(firewall.TargetTags, ",")
			}
			
			// Add allowed/denied protocols
			if len(firewall.Allowed) > 0 {
				var protocols []string
				for _, allowed := range firewall.Allowed {
					protocols = append(protocols, allowed.IPProtocol)
				}
				ref.BasicAttributes["allowed_protocols"] = strings.Join(protocols, ",")
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanAddresses(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Addresses.AggregatedList(projectID)
	if err := req.Pages(ctx, func(page *compute.AddressAggregatedList) error {
		for region, addressList := range page.Items {
			if addressList.Addresses != nil {
				for _, address := range addressList.Addresses {
					ref := &pb.ResourceRef{
						Id:      fmt.Sprintf("projects/%s/regions/%s/addresses/%s", 
							    projectID, extractRegionFromURL(region), address.Name),
						Name:    address.Name,
						Type:    "compute.googleapis.com/Address",
						Service: "compute",
						Region:  extractRegionFromURL(region),
						BasicAttributes: map[string]string{
							"address":            address.Address,
							"address_type":       address.AddressType,
							"status":             address.Status,
							"self_link":          address.SelfLink,
							"project_id":         projectID,
							"creation_timestamp": address.CreationTimestamp,
						},
					}
					
					if address.Description != "" {
						ref.BasicAttributes["description"] = address.Description
					}
					
					if address.Users != nil && len(address.Users) > 0 {
						ref.BasicAttributes["used_by"] = strings.Join(address.Users, ",")
					}
					
					resources = append(resources, ref)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanSnapshots(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Snapshots.List(projectID)
	if err := req.Pages(ctx, func(page *compute.SnapshotList) error {
		for _, snapshot := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/global/snapshots/%s", projectID, snapshot.Name),
				Name:    snapshot.Name,
				Type:    "compute.googleapis.com/Snapshot",
				Service: "compute",
				Region:  "global",
				BasicAttributes: map[string]string{
					"source_disk":        extractLastSegment(snapshot.SourceDisk),
					"disk_size_gb":       fmt.Sprintf("%d", snapshot.DiskSizeGb),
					"storage_bytes":      fmt.Sprintf("%d", snapshot.StorageBytes),
					"status":             snapshot.Status,
					"self_link":          snapshot.SelfLink,
					"project_id":         projectID,
					"creation_timestamp": snapshot.CreationTimestamp,
				},
			}
			
			if snapshot.Description != "" {
				ref.BasicAttributes["description"] = snapshot.Description
			}
			
			// Add labels
			for k, v := range snapshot.Labels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) scanImages(ctx context.Context, client *compute.Service, projectID string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	req := client.Images.List(projectID)
	if err := req.Pages(ctx, func(page *compute.ImageList) error {
		for _, image := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/global/images/%s", projectID, image.Name),
				Name:    image.Name,
				Type:    "compute.googleapis.com/Image",
				Service: "compute",
				Region:  "global",
				BasicAttributes: map[string]string{
					"status":             image.Status,
					"disk_size_gb":       fmt.Sprintf("%d", image.DiskSizeGb),
					"archive_size_bytes": fmt.Sprintf("%d", image.ArchiveSizeBytes),
					"self_link":          image.SelfLink,
					"project_id":         projectID,
					"creation_timestamp": image.CreationTimestamp,
				},
			}
			
			if image.Description != "" {
				ref.BasicAttributes["description"] = image.Description
			}
			
			if image.Family != "" {
				ref.BasicAttributes["family"] = image.Family
			}
			
			if image.SourceDisk != "" {
				ref.BasicAttributes["source_disk"] = extractLastSegment(image.SourceDisk)
			}
			
			// Add labels
			for k, v := range image.Labels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	
	return resources, nil
}

func (ecs *EnhancedComputeScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Reuse the existing ComputeScanner's DescribeResource method
	cs := NewComputeScanner(ecs.clientFactory)
	return cs.DescribeResource(ctx, resourceID)
}

// EnhancedStorageScanner provides comprehensive Cloud Storage scanning
type EnhancedStorageScanner struct {
	clientFactory *ClientFactory
}

func NewEnhancedStorageScanner(cf *ClientFactory) *EnhancedStorageScanner {
	return &EnhancedStorageScanner{clientFactory: cf}
}

func (ess *EnhancedStorageScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := ess.clientFactory.GetStorageClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := ess.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List all buckets with enhanced metadata
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
						"storage_class":      bucket.StorageClass,
						"location_type":      bucket.LocationType,
						"self_link":          bucket.SelfLink,
						"project_id":         projectID,
						"time_created":       bucket.TimeCreated,
						"updated":            bucket.Updated,
						"metageneration":     fmt.Sprintf("%d", bucket.Metageneration),
						"project_number":     fmt.Sprintf("%d", bucket.ProjectNumber),
					},
				}
				
				// Add versioning info
				if bucket.Versioning != nil {
					ref.BasicAttributes["versioning_enabled"] = fmt.Sprintf("%t", bucket.Versioning.Enabled)
				}
				
				// Add encryption info
				if bucket.Encryption != nil && bucket.Encryption.DefaultKmsKeyName != "" {
					ref.BasicAttributes["kms_key"] = bucket.Encryption.DefaultKmsKeyName
				}
				
				// Add website config
				if bucket.Website != nil {
					if bucket.Website.MainPageSuffix != "" {
						ref.BasicAttributes["website_main_page"] = bucket.Website.MainPageSuffix
					}
					if bucket.Website.NotFoundPage != "" {
						ref.BasicAttributes["website_404_page"] = bucket.Website.NotFoundPage
					}
				}
				
				// Add CORS info
				if len(bucket.Cors) > 0 {
					ref.BasicAttributes["cors_enabled"] = "true"
				}
				
				// Add lifecycle management
				if bucket.Lifecycle != nil && len(bucket.Lifecycle.Rule) > 0 {
					ref.BasicAttributes["lifecycle_rules"] = fmt.Sprintf("%d", len(bucket.Lifecycle.Rule))
				}
				
				// Add IAM configuration
				if bucket.IamConfiguration != nil {
					if bucket.IamConfiguration.UniformBucketLevelAccess != nil {
						ref.BasicAttributes["uniform_bucket_level_access"] = fmt.Sprintf("%t", 
							bucket.IamConfiguration.UniformBucketLevelAccess.Enabled)
					}
				}
				
				// Add logging
				if bucket.Logging != nil && bucket.Logging.LogBucket != "" {
					ref.BasicAttributes["access_logging_bucket"] = bucket.Logging.LogBucket
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

func (ess *EnhancedStorageScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Parse resource ID
	parts := strings.Split(resourceID, "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid resource ID format")
	}
	
	bucketName := parts[3]
	
	client, err := ess.clientFactory.GetStorageClient(ctx)
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
		Tags:         bucket.Labels,
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Store comprehensive bucket metadata
	metadata := map[string]string{
		"storage_class":      bucket.StorageClass,
		"location_type":      bucket.LocationType,
		"time_created":       bucket.TimeCreated,
		"updated":            bucket.Updated,
		"metageneration":     fmt.Sprintf("%d", bucket.Metageneration),
		"project_number":     fmt.Sprintf("%d", bucket.ProjectNumber),
	}
	
	// Add versioning info
	if bucket.Versioning != nil {
		metadata["versioning_enabled"] = fmt.Sprintf("%t", bucket.Versioning.Enabled)
	}
	
	// Add encryption info
	if bucket.Encryption != nil && bucket.Encryption.DefaultKmsKeyName != "" {
		metadata["kms_key"] = bucket.Encryption.DefaultKmsKeyName
	}
	
	// Note: Metadata field not available in pb.Resource, storing in RawData
	
	// Store raw data
	if rawData, err := json.Marshal(bucket); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// EnhancedContainerScanner provides comprehensive GKE scanning
type EnhancedContainerScanner struct {
	clientFactory *ClientFactory
}

func NewEnhancedContainerScanner(cf *ClientFactory) *EnhancedContainerScanner {
	return &EnhancedContainerScanner{clientFactory: cf}
}

func (ecs *EnhancedContainerScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	// Reuse existing ContainerScanner but with enhanced metadata
	cs := NewContainerScanner(ecs.clientFactory)
	return cs.Scan(ctx)
}

func (ecs *EnhancedContainerScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Reuse existing ContainerScanner's DescribeResource method
	cs := NewContainerScanner(ecs.clientFactory)
	return cs.DescribeResource(ctx, resourceID)
}