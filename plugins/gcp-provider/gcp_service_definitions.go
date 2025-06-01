package main

import (
	"fmt"
	"strings"
)

// GetGCPServiceDefinitions returns predefined GCP service information for scanner generation
func GetGCPServiceDefinitions() map[string]*GCPServiceInfo {
	return map[string]*GCPServiceInfo{
		"compute": {
			Name:        "compute",
			ClientType:  "*compute.Service",
			PackageName: "compute",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Instance",
					TypeName:           "Instance",
					AssetInventoryType: "compute.googleapis.com/Instance",
					IsZonal:            true,
					ListCode: `
// List instances in zone
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	req := client.Instances.List(projectID, zone)
	if err := req.Pages(ctx, func(page *compute.InstanceList) error {
		for _, instance := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/zones/%s/instances/%s", projectID, zone, instance.Name),
				Name:    instance.Name,
				Type:    "compute.googleapis.com/Instance",
				Service: "compute",
				Region:  zone,
				BasicAttributes: map[string]string{
					"machine_type": s.extractLastSegment(instance.MachineType),
					"status":       instance.Status,
					"project_id":   projectID,
					"zone":         zone,
				},
			}
			
			// Add labels
			for k, v := range instance.Labels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		log.Printf("Failed to list instances in zone %s for project %s: %v", zone, projectID, err)
	}
}`,
					IDField:     "Name",
					NameField:   "Name",
					LabelsField: "Labels",
				},
				{
					Name:               "Disk",
					TypeName:           "Disk",
					AssetInventoryType: "compute.googleapis.com/Disk",
					IsZonal:            true,
					ListCode: `
// List disks in zone
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	req := client.Disks.List(projectID, zone)
	if err := req.Pages(ctx, func(page *compute.DiskList) error {
		for _, disk := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/zones/%s/disks/%s", projectID, zone, disk.Name),
				Name:    disk.Name,
				Type:    "compute.googleapis.com/Disk",
				Service: "compute",
				Region:  zone,
				BasicAttributes: map[string]string{
					"size_gb":    fmt.Sprintf("%d", disk.SizeGb),
					"disk_type":  s.extractLastSegment(disk.Type),
					"status":     disk.Status,
					"project_id": projectID,
					"zone":       zone,
				},
			}
			
			// Add labels
			for k, v := range disk.Labels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		log.Printf("Failed to list disks in zone %s for project %s: %v", zone, projectID, err)
	}
}`,
					IDField:     "Name",
					NameField:   "Name",
					LabelsField: "Labels",
				},
				{
					Name:               "Network",
					TypeName:           "Network",
					AssetInventoryType: "compute.googleapis.com/Network",
					IsGlobal:           true,
					ListCode: `
// List networks (global resource)
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
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
	}
}`,
					IDField:   "Name",
					NameField: "Name",
				},
				{
					Name:               "Subnetwork",
					TypeName:           "Subnetwork",
					AssetInventoryType: "compute.googleapis.com/Subnetwork",
					IsRegional:         true,
					ListCode: `
// List subnetworks in region
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	req := client.Subnetworks.List(projectID, region)
	if err := req.Pages(ctx, func(page *compute.SubnetworkList) error {
		for _, subnet := range page.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", projectID, region, subnet.Name),
				Name:    subnet.Name,
				Type:    "compute.googleapis.com/Subnetwork",
				Service: "compute",
				Region:  region,
				BasicAttributes: map[string]string{
					"cidr_range": subnet.IpCidrRange,
					"network":    s.extractLastSegment(subnet.Network),
					"project_id": projectID,
					"region":     region,
				},
			}
			
			if subnet.Description != "" {
				ref.BasicAttributes["description"] = subnet.Description
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		log.Printf("Failed to list subnetworks in region %s for project %s: %v", region, projectID, err)
	}
}`,
					IDField:   "Name",
					NameField: "Name",
				},
			},
		},
		"storage": {
			Name:        "storage",
			ClientType:  "*storage.Service",
			PackageName: "storage",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Bucket",
					TypeName:           "Bucket",
					AssetInventoryType: "storage.googleapis.com/Bucket",
					IsGlobal:           true,
					ListCode: `
// List Cloud Storage buckets (global resource)
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
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
					"storage_class":  bucket.StorageClass,
					"location_type":  bucket.LocationType,
					"project_id":     projectID,
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
	}
}`,
					IDField:     "Name",
					NameField:   "Name",
					LabelsField: "Labels",
				},
			},
		},
		"container": {
			Name:        "container",
			ClientType:  "*container.Service",
			PackageName: "container",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Cluster",
					TypeName:           "Cluster",
					AssetInventoryType: "container.googleapis.com/Cluster",
					IsRegional:         true,
					ListCode: `
// List GKE clusters in region
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	resp, err := client.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
	if err != nil {
		log.Printf("Failed to list clusters in region %s for project %s: %v", region, projectID, err)
		continue
	}
	
	for _, cluster := range resp.Clusters {
		ref := &pb.ResourceRef{
			Id:      fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, region, cluster.Name),
			Name:    cluster.Name,
			Type:    "container.googleapis.com/Cluster",
			Service: "container",
			Region:  region,
			BasicAttributes: map[string]string{
				"status":         cluster.Status,
				"current_nodes":  fmt.Sprintf("%d", cluster.CurrentNodeCount),
				"cluster_ipv4":   cluster.ClusterIpv4Cidr,
				"project_id":     projectID,
				"location":       region,
			},
		}
		
		// Add resource labels
		for k, v := range cluster.ResourceLabels {
			ref.BasicAttributes["label_"+k] = v
		}
		
		resources = append(resources, ref)
	}
}`,
					IDField:     "Name",
					NameField:   "Name",
					LabelsField: "ResourceLabels",
				},
				{
					Name:               "NodePool",
					TypeName:           "NodePool", 
					AssetInventoryType: "container.googleapis.com/NodePool",
					IsRegional:         true,
					ListCode: `
// List GKE node pools (requires listing clusters first)
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	clustersResp, err := client.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
	if err != nil {
		log.Printf("Failed to list clusters for node pools in region %s for project %s: %v", region, projectID, err)
		continue
	}
	
	for _, cluster := range clustersResp.Clusters {
		for _, nodePool := range cluster.NodePools {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/locations/%s/clusters/%s/nodePools/%s", projectID, region, cluster.Name, nodePool.Name),
				Name:    nodePool.Name,
				Type:    "container.googleapis.com/NodePool",
				Service: "container",
				Region:  region,
				BasicAttributes: map[string]string{
					"status":       nodePool.Status,
					"node_count":   fmt.Sprintf("%d", nodePool.InitialNodeCount),
					"cluster_name": cluster.Name,
					"project_id":   projectID,
					"location":     region,
				},
			}
			
			if nodePool.Config != nil && nodePool.Config.MachineType != "" {
				ref.BasicAttributes["machine_type"] = nodePool.Config.MachineType
			}
			
			resources = append(resources, ref)
		}
	}
}`,
					IDField:   "Name",
					NameField: "Name",
				},
			},
		},
		"bigquery": {
			Name:        "bigquery",
			ClientType:  "*bigquery.Service",
			PackageName: "bigquery",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Dataset",
					TypeName:           "Dataset",
					AssetInventoryType: "bigquery.googleapis.com/Dataset",
					IsGlobal:           true,
					ListCode: `
// List BigQuery datasets (global resource)
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	req := client.Datasets.List(projectID)
	if err := req.Pages(ctx, func(page *bigquery.DatasetList) error {
		for _, dataset := range page.Datasets {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/datasets/%s", projectID, dataset.Id),
				Name:    dataset.Id,
				Type:    "bigquery.googleapis.com/Dataset",
				Service: "bigquery",
				Region:  "global",
				BasicAttributes: map[string]string{
					"project_id": projectID,
					"dataset_id": dataset.Id,
				},
			}
			
			if dataset.FriendlyName != "" {
				ref.BasicAttributes["friendly_name"] = dataset.FriendlyName
			}
			
			// Add labels
			for k, v := range dataset.Labels {
				ref.BasicAttributes["label_"+k] = v
			}
			
			resources = append(resources, ref)
		}
		return nil
	}); err != nil {
		log.Printf("Failed to list datasets for project %s: %v", projectID, err)
	}
}`,
					IDField:     "Id",
					NameField:   "Id", 
					LabelsField: "Labels",
				},
				{
					Name:               "Table",
					TypeName:           "Table",
					AssetInventoryType: "bigquery.googleapis.com/Table",
					IsGlobal:           true,
					ListCode: `
// List BigQuery tables (requires listing datasets first)
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	datasetsReq := client.Datasets.List(projectID)
	if err := datasetsReq.Pages(ctx, func(datasetPage *bigquery.DatasetList) error {
		for _, dataset := range datasetPage.Datasets {
			tablesReq := client.Tables.List(projectID, dataset.Id)
			if err := tablesReq.Pages(ctx, func(tablePage *bigquery.TableList) error {
				for _, table := range tablePage.Tables {
					ref := &pb.ResourceRef{
						Id:      fmt.Sprintf("projects/%s/datasets/%s/tables/%s", projectID, dataset.Id, table.Id),
						Name:    table.Id,
						Type:    "bigquery.googleapis.com/Table",
						Service: "bigquery",
						Region:  "global",
						BasicAttributes: map[string]string{
							"project_id": projectID,
							"dataset_id": dataset.Id,
							"table_id":   table.Id,
							"table_type": table.Type,
						},
					}
					
					if table.FriendlyName != "" {
						ref.BasicAttributes["friendly_name"] = table.FriendlyName
					}
					
					// Add labels
					for k, v := range table.Labels {
						ref.BasicAttributes["label_"+k] = v
					}
					
					resources = append(resources, ref)
				}
				return nil
			}); err != nil {
				log.Printf("Failed to list tables in dataset %s for project %s: %v", dataset.Id, projectID, err)
			}
		}
		return nil
	}); err != nil {
		log.Printf("Failed to list datasets for project %s: %v", projectID, err)
	}
}`,
					IDField:     "Id",
					NameField:   "Id",
					LabelsField: "Labels",
				},
			},
		},
		"cloudsql": {
			Name:        "cloudsql",
			ClientType:  "*sqladmin.Service",
			PackageName: "sqladmin",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Instance",
					TypeName:           "DatabaseInstance",
					AssetInventoryType: "sqladmin.googleapis.com/Instance",
					IsRegional:         true,
					ListCode: `
// List Cloud SQL instances in region
projectIDs := s.getProjects()
for _, projectID := range projectIDs {
	req := client.Instances.List(projectID)
	resp, err := req.Context(ctx).Do()
	if err != nil {
		log.Printf("Failed to list SQL instances for project %s: %v", projectID, err)
		continue
	}
	
	for _, instance := range resp.Items {
		// Filter by region if specified
		if region != "global" && instance.Region != region {
			continue
		}
		
		ref := &pb.ResourceRef{
			Id:      fmt.Sprintf("projects/%s/instances/%s", projectID, instance.Name),
			Name:    instance.Name,
			Type:    "sqladmin.googleapis.com/Instance",
			Service: "cloudsql",
			Region:  instance.Region,
			BasicAttributes: map[string]string{
				"database_version": instance.DatabaseVersion,
				"tier":             instance.Settings.Tier,
				"state":            instance.State,
				"backend_type":     instance.BackendType,
				"instance_type":    instance.InstanceType,
				"project_id":       projectID,
			},
		}
		
		if instance.ConnectionName != "" {
			ref.BasicAttributes["connection_name"] = instance.ConnectionName
		}
		
		// Add IP addresses
		for _, ipAddr := range instance.IpAddresses {
			if ipAddr.Type == "PRIMARY" {
				ref.BasicAttributes["primary_ip"] = ipAddr.IpAddress
			}
		}
		
		// Add user labels
		for k, v := range instance.Settings.UserLabels {
			ref.BasicAttributes["label_"+k] = v
		}
		
		resources = append(resources, ref)
	}
}`,
					IDField:     "Name",
					NameField:   "Name",
					LabelsField: "Settings.UserLabels",
				},
			},
		},
	}
}

// GenerateGCPServiceScanner generates a scanner for a specific GCP service
func GenerateGCPServiceScanner(serviceName string, outputDir string) error {
	services := GetGCPServiceDefinitions()
	service, exists := services[serviceName]
	if !exists {
		return fmt.Errorf("unknown GCP service: %s", serviceName)
	}

	generator, err := NewGCPScannerGenerator(GCPGenerationOptions{
		Services:    []string{serviceName},
		OutputDir:   outputDir,
		PackageName: "main",
		WithRetry:   true,
		WithMetrics: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create generator: %w", err)
	}

	return generator.GenerateService(service, GCPGenerationOptions{
		Services:    []string{serviceName},
		OutputDir:   outputDir,
		PackageName: "main",
		WithRetry:   true,
		WithMetrics: false,
	})
}

// GenerateAllGCPScanners generates scanners for all defined GCP services
func GenerateAllGCPScanners(outputDir string) error {
	services := GetGCPServiceDefinitions()

	generator, err := NewGCPScannerGenerator(GCPGenerationOptions{
		Services:    []string{"all"},
		OutputDir:   outputDir,
		PackageName: "main",
		WithRetry:   true,
		WithMetrics: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create generator: %w", err)
	}

	return generator.GenerateAllServices(services, GCPGenerationOptions{
		Services:    []string{"all"},
		OutputDir:   outputDir,
		PackageName: "main",
		WithRetry:   true,
		WithMetrics: false,
	})
}

// extractLastSegment is a helper function used in the generated templates
func extractLastSegment(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return url
}