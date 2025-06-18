package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/bigquery/v2"
	"google.golang.org/api/sqladmin/v1"
	"google.golang.org/api/pubsub/v1"
	"google.golang.org/api/run/v1"
	"google.golang.org/api/cloudfunctions/v1"
	"google.golang.org/api/appengine/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnhancedResourceScanner provides comprehensive scanning for all GCP services
type EnhancedResourceScanner struct {
	clientFactory  *ClientFactory
	assetInventory *AssetInventoryClient
	scanners       map[string]ServiceScanner
	concurrency    int
}

// NewEnhancedResourceScanner creates a comprehensive scanner with all services
func NewEnhancedResourceScanner(cf *ClientFactory) *EnhancedResourceScanner {
	ers := &EnhancedResourceScanner{
		clientFactory: cf,
		scanners:      make(map[string]ServiceScanner),
		concurrency:   10, // Default concurrency
	}
	
	// Register all enhanced scanners
	ers.registerAllScanners()
	
	return ers
}

// registerAllScanners registers scanners for all supported GCP services
func (ers *EnhancedResourceScanner) registerAllScanners() {
	// Core infrastructure services
	ers.scanners["compute"] = NewEnhancedComputeScanner(ers.clientFactory)
	ers.scanners["storage"] = NewEnhancedStorageScanner(ers.clientFactory)
	ers.scanners["container"] = NewEnhancedContainerScanner(ers.clientFactory)
	
	// Data and analytics services
	ers.scanners["bigquery"] = NewBigQueryScanner(ers.clientFactory)
	ers.scanners["cloudsql"] = NewCloudSQLScanner(ers.clientFactory)
	
	// Application services
	ers.scanners["pubsub"] = NewPubSubScanner(ers.clientFactory)
	ers.scanners["run"] = NewCloudRunScanner(ers.clientFactory)
	ers.scanners["cloudfunctions"] = NewCloudFunctionsScanner(ers.clientFactory)
	ers.scanners["appengine"] = NewAppEngineScanner(ers.clientFactory)
	
	log.Printf("✅ Registered %d enhanced service scanners", len(ers.scanners))
}

// ScanAllServicesEnhanced performs comprehensive scanning with real results
func (ers *EnhancedResourceScanner) ScanAllServicesEnhanced(ctx context.Context) ([]*pb.Resource, error) {
	log.Printf("🚀 Starting enhanced scanning across %d services", len(ers.scanners))
	
	// Use semaphore for concurrency control
	sem := make(chan struct{}, ers.concurrency)
	resultChan := make(chan *serviceResult, len(ers.scanners))
	
	var wg sync.WaitGroup
	wg.Add(len(ers.scanners))
	
	// Scan all services concurrently
	for serviceName, scanner := range ers.scanners {
		go func(name string, s ServiceScanner) {
			defer wg.Done()
			
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release
			
			result := &serviceResult{service: name}
			
			refs, err := s.Scan(ctx)
			if err != nil {
				result.err = err
				log.Printf("⚠️ Scanner error for %s: %v", name, err)
			} else {
				// Convert ResourceRefs to Resources
				for _, ref := range refs {
					resource := convertRefToResourceEnhanced(ref)
					result.resources = append(result.resources, resource)
				}
				log.Printf("✅ Scanned %s: %d resources found", name, len(refs))
			}
			
			resultChan <- result
		}(serviceName, scanner)
	}
	
	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	// Collect results
	var allResources []*pb.Resource
	var errors []string
	
	for result := range resultChan {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", result.service, result.err))
		} else {
			allResources = append(allResources, result.resources...)
		}
	}
	
	log.Printf("🎯 Enhanced scan completed: %d total resources across %d services", 
		len(allResources), len(ers.scanners))
	
	if len(errors) > 0 {
		log.Printf("⚠️ Scan errors: %v", errors)
	}
	
	return allResources, nil
}

// BigQueryScanner implements scanning for BigQuery resources
type BigQueryScanner struct {
	clientFactory *ClientFactory
}

func NewBigQueryScanner(cf *ClientFactory) *BigQueryScanner {
	return &BigQueryScanner{clientFactory: cf}
}

func (bqs *BigQueryScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := bqs.clientFactory.GetBigQueryClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create bigquery client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := bqs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List datasets
		datasets, err := client.Datasets.List(projectID).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list BigQuery datasets for project %s: %v", projectID, err)
			continue
		}
		
		for _, dataset := range datasets.Datasets {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/datasets/%s", projectID, dataset.Id),
				Name:    dataset.Id,
				Type:    "bigquery.googleapis.com/Dataset",
				Service: "bigquery",
				Region:  dataset.Location,
				BasicAttributes: map[string]string{
					"project_id":   projectID,
					"dataset_ref":  dataset.DatasetReference.DatasetId,
					"friendly_name": dataset.FriendlyName,
				},
			}
			
			if dataset.Labels != nil {
				for k, v := range dataset.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
			
			// List tables in this dataset
			tables, err := client.Tables.List(projectID, dataset.Id).Context(ctx).Do()
			if err != nil {
				log.Printf("Failed to list tables in dataset %s: %v", dataset.Id, err)
				continue
			}
			
			for _, table := range tables.Tables {
				tableRef := &pb.ResourceRef{
					Id:      fmt.Sprintf("projects/%s/datasets/%s/tables/%s", 
						   projectID, dataset.Id, table.TableReference.TableId),
					Name:    table.TableReference.TableId,
					Type:    "bigquery.googleapis.com/Table",
					Service: "bigquery",
					Region:  dataset.Location,
					BasicAttributes: map[string]string{
						"project_id": projectID,
						"dataset_id": dataset.Id,
						"table_type": table.Type,
					},
				}
				
				if table.FriendlyName != "" {
					tableRef.BasicAttributes["friendly_name"] = table.FriendlyName
				}
				
				resources = append(resources, tableRef)
			}
		}
	}
	
	return resources, nil
}

func (bqs *BigQueryScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	// Parse resource ID and get detailed information
	parts := strings.Split(resourceID, "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid BigQuery resource ID format")
	}
	
	projectID := parts[1]
	resourceType := parts[2] // "datasets" or "tables"
	
	client, err := bqs.clientFactory.GetBigQueryClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create bigquery client: %w", err)
	}
	
	switch resourceType {
	case "datasets":
		datasetID := parts[3]
		dataset, err := client.Datasets.Get(projectID, datasetID).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get dataset: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "bigquery",
			Type:         "bigquery.googleapis.com/Dataset",
			Id:           resourceID,
			Name:         dataset.Id,
			Region:       dataset.Location,
			Tags:         dataset.Labels,
			DiscoveredAt: timestamppb.Now(),
		}
		
		if rawData, err := json.Marshal(dataset); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
		
	default:
		return nil, fmt.Errorf("unsupported BigQuery resource type: %s", resourceType)
	}
}

// CloudSQLScanner implements scanning for Cloud SQL resources
type CloudSQLScanner struct {
	clientFactory *ClientFactory
}

func NewCloudSQLScanner(cf *ClientFactory) *CloudSQLScanner {
	return &CloudSQLScanner{clientFactory: cf}
}

func (css *CloudSQLScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := css.clientFactory.GetCloudSQLClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudsql client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := css.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List SQL instances
		instances, err := client.Instances.List(projectID).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list Cloud SQL instances for project %s: %v", projectID, err)
			continue
		}
		
		for _, instance := range instances.Items {
			ref := &pb.ResourceRef{
				Id:      fmt.Sprintf("projects/%s/instances/%s", projectID, instance.Name),
				Name:    instance.Name,
				Type:    "sqladmin.googleapis.com/Instance",
				Service: "cloudsql",
				Region:  instance.Region,
				BasicAttributes: map[string]string{
					"project_id":      projectID,
					"database_version": instance.DatabaseVersion,
					"state":           instance.State,
					"tier":            instance.Settings.Tier,
					"backend_type":    instance.BackendType,
				},
			}
			
			if instance.Settings != nil && instance.Settings.UserLabels != nil {
				for k, v := range instance.Settings.UserLabels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
			
			// List databases for this instance
			databases, err := client.Databases.List(projectID, instance.Name).Context(ctx).Do()
			if err != nil {
				log.Printf("Failed to list databases for instance %s: %v", instance.Name, err)
				continue
			}
			
			for _, database := range databases.Items {
				dbRef := &pb.ResourceRef{
					Id:      fmt.Sprintf("projects/%s/instances/%s/databases/%s", 
						   projectID, instance.Name, database.Name),
					Name:    database.Name,
					Type:    "sqladmin.googleapis.com/Database",
					Service: "cloudsql",
					Region:  instance.Region,
					BasicAttributes: map[string]string{
						"project_id":   projectID,
						"instance":     instance.Name,
						"charset":      database.Charset,
						"collation":    database.Collation,
					},
				}
				
				resources = append(resources, dbRef)
			}
		}
	}
	
	return resources, nil
}

func (css *CloudSQLScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	parts := strings.Split(resourceID, "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid Cloud SQL resource ID format")
	}
	
	projectID := parts[1]
	instanceName := parts[3]
	
	client, err := css.clientFactory.GetCloudSQLClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudsql client: %w", err)
	}
	
	instance, err := client.Instances.Get(projectID, instanceName).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get SQL instance: %w", err)
	}
	
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      "cloudsql",
		Type:         "sqladmin.googleapis.com/Instance",
		Id:           resourceID,
		Name:         instance.Name,
		Region:       instance.Region,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}
	
	if instance.Settings != nil && instance.Settings.UserLabels != nil {
		resource.Tags = instance.Settings.UserLabels
	}
	
	if rawData, err := json.Marshal(instance); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// PubSubScanner implements scanning for Cloud Pub/Sub resources
type PubSubScanner struct {
	clientFactory *ClientFactory
}

func NewPubSubScanner(cf *ClientFactory) *PubSubScanner {
	return &PubSubScanner{clientFactory: cf}
}

func (pss *PubSubScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := pss.clientFactory.GetPubSubClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := pss.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List topics
		topics, err := client.Projects.Topics.List(fmt.Sprintf("projects/%s", projectID)).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list Pub/Sub topics for project %s: %v", projectID, err)
			continue
		}
		
		for _, topic := range topics.Topics {
			ref := &pb.ResourceRef{
				Id:      topic.Name,
				Name:    extractLastSegment(topic.Name),
				Type:    "pubsub.googleapis.com/Topic",
				Service: "pubsub",
				Region:  "global",
				BasicAttributes: map[string]string{
					"project_id": projectID,
				},
			}
			
			if topic.Labels != nil {
				for k, v := range topic.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
		}
		
		// List subscriptions
		subscriptions, err := client.Projects.Subscriptions.List(fmt.Sprintf("projects/%s", projectID)).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list Pub/Sub subscriptions for project %s: %v", projectID, err)
			continue
		}
		
		for _, subscription := range subscriptions.Subscriptions {
			ref := &pb.ResourceRef{
				Id:      subscription.Name,
				Name:    extractLastSegment(subscription.Name),
				Type:    "pubsub.googleapis.com/Subscription",
				Service: "pubsub",
				Region:  "global",
				BasicAttributes: map[string]string{
					"project_id": projectID,
					"topic":      extractLastSegment(subscription.Topic),
				},
			}
			
			if subscription.Labels != nil {
				for k, v := range subscription.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
		}
	}
	
	return resources, nil
}

func (pss *PubSubScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	client, err := pss.clientFactory.GetPubSubClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}
	
	// Determine if it's a topic or subscription
	if strings.Contains(resourceID, "/topics/") {
		topic, err := client.Projects.Topics.Get(resourceID).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get topic: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "pubsub",
			Type:         "pubsub.googleapis.com/Topic",
			Id:           resourceID,
			Name:         extractLastSegment(topic.Name),
			Region:       "global",
			Tags:         topic.Labels,
			DiscoveredAt: timestamppb.Now(),
		}
		
		if rawData, err := json.Marshal(topic); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
	} else if strings.Contains(resourceID, "/subscriptions/") {
		subscription, err := client.Projects.Subscriptions.Get(resourceID).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get subscription: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "pubsub",
			Type:         "pubsub.googleapis.com/Subscription",
			Id:           resourceID,
			Name:         extractLastSegment(subscription.Name),
			Region:       "global",
			Tags:         subscription.Labels,
			DiscoveredAt: timestamppb.Now(),
		}
		
		if rawData, err := json.Marshal(subscription); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
	}
	
	return nil, fmt.Errorf("unsupported Pub/Sub resource type")
}

// CloudRunScanner implements scanning for Cloud Run resources
type CloudRunScanner struct {
	clientFactory *ClientFactory
}

func NewCloudRunScanner(cf *ClientFactory) *CloudRunScanner {
	return &CloudRunScanner{clientFactory: cf}
}

func (crs *CloudRunScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := crs.clientFactory.GetCloudRunClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud run client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := crs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List services across all locations
		parent := fmt.Sprintf("projects/%s/locations/-", projectID)
		services, err := client.Projects.Locations.Services.List(parent).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list Cloud Run services for project %s: %v", projectID, err)
			continue
		}
		
		for _, service := range services.Items {
			location := extractRegionFromRunName(service.Metadata.Name)
			ref := &pb.ResourceRef{
				Id:      service.Metadata.Name,
				Name:    service.Metadata.Name,
				Type:    "run.googleapis.com/Service",
				Service: "run",
				Region:  location,
				BasicAttributes: map[string]string{
					"project_id": projectID,
					"location":   location,
				},
			}
			
			if service.Metadata.Labels != nil {
				for k, v := range service.Metadata.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
		}
	}
	
	return resources, nil
}

func (crs *CloudRunScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	client, err := crs.clientFactory.GetCloudRunClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud run client: %w", err)
	}
	
	service, err := client.Projects.Locations.Services.Get(resourceID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get Cloud Run service: %w", err)
	}
	
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      "run",
		Type:         "run.googleapis.com/Service",
		Id:           resourceID,
		Name:         service.Metadata.Name,
		Region:       extractRegionFromRunName(service.Metadata.Name),
		Tags:         service.Metadata.Labels,
		DiscoveredAt: timestamppb.Now(),
	}
	
	if rawData, err := json.Marshal(service); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// CloudFunctionsScanner implements scanning for Cloud Functions resources
type CloudFunctionsScanner struct {
	clientFactory *ClientFactory
}

func NewCloudFunctionsScanner(cf *ClientFactory) *CloudFunctionsScanner {
	return &CloudFunctionsScanner{clientFactory: cf}
}

func (cfs *CloudFunctionsScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := cfs.clientFactory.GetCloudFunctionsClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud functions client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := cfs.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// List functions across all locations
		parent := fmt.Sprintf("projects/%s/locations/-", projectID)
		functions, err := client.Projects.Locations.Functions.List(parent).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list Cloud Functions for project %s: %v", projectID, err)
			continue
		}
		
		for _, function := range functions.Functions {
			location := extractLocationFromFunctionName(function.Name)
			ref := &pb.ResourceRef{
				Id:      function.Name,
				Name:    extractLastSegment(function.Name),
				Type:    "cloudfunctions.googleapis.com/Function",
				Service: "cloudfunctions",
				Region:  location,
				BasicAttributes: map[string]string{
					"project_id": projectID,
					"status":     function.Status,
					"runtime":    function.Runtime,
					"location":   location,
				},
			}
			
			if function.Labels != nil {
				for k, v := range function.Labels {
					ref.BasicAttributes["label_"+k] = v
				}
			}
			
			resources = append(resources, ref)
		}
	}
	
	return resources, nil
}

func (cfs *CloudFunctionsScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	client, err := cfs.clientFactory.GetCloudFunctionsClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud functions client: %w", err)
	}
	
	function, err := client.Projects.Locations.Functions.Get(resourceID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get Cloud Function: %w", err)
	}
	
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      "cloudfunctions",
		Type:         "cloudfunctions.googleapis.com/Function",
		Id:           resourceID,
		Name:         extractLastSegment(function.Name),
		Region:       extractLocationFromFunctionName(function.Name),
		Tags:         function.Labels,
		DiscoveredAt: timestamppb.Now(),
	}
	
	if rawData, err := json.Marshal(function); err == nil {
		resource.RawData = string(rawData)
	}
	
	return resource, nil
}

// AppEngineScanner implements scanning for App Engine resources
type AppEngineScanner struct {
	clientFactory *ClientFactory
}

func NewAppEngineScanner(cf *ClientFactory) *AppEngineScanner {
	return &AppEngineScanner{clientFactory: cf}
}

func (aes *AppEngineScanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := aes.clientFactory.GetAppEngineClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create app engine client: %w", err)
	}
	
	var resources []*pb.ResourceRef
	projectIDs := aes.clientFactory.GetProjectIDs()
	
	for _, projectID := range projectIDs {
		// Get App Engine application
		app, err := client.Apps.Get(projectID).Context(ctx).Do()
		if err != nil {
			// App Engine might not be enabled for this project
			continue
		}
		
		// Add the application itself
		appRef := &pb.ResourceRef{
			Id:      fmt.Sprintf("apps/%s", projectID),
			Name:    projectID,
			Type:    "appengine.googleapis.com/Application",
			Service: "appengine",
			Region:  app.LocationId,
			BasicAttributes: map[string]string{
				"project_id":   projectID,
				"location_id":  app.LocationId,
				"serving_status": app.ServingStatus,
			},
		}
		
		resources = append(resources, appRef)
		
		// List services
		services, err := client.Apps.Services.List(projectID).Context(ctx).Do()
		if err != nil {
			log.Printf("Failed to list App Engine services for project %s: %v", projectID, err)
			continue
		}
		
		for _, service := range services.Services {
			serviceRef := &pb.ResourceRef{
				Id:      fmt.Sprintf("apps/%s/services/%s", projectID, service.Id),
				Name:    service.Id,
				Type:    "appengine.googleapis.com/Service",
				Service: "appengine",
				Region:  app.LocationId,
				BasicAttributes: map[string]string{
					"project_id": projectID,
					"app_id":     projectID,
				},
			}
			
			resources = append(resources, serviceRef)
			
			// List versions for this service
			versions, err := client.Apps.Services.Versions.List(projectID, service.Id).Context(ctx).Do()
			if err != nil {
				log.Printf("Failed to list versions for service %s: %v", service.Id, err)
				continue
			}
			
			for _, version := range versions.Versions {
				versionRef := &pb.ResourceRef{
					Id:      fmt.Sprintf("apps/%s/services/%s/versions/%s", 
						   projectID, service.Id, version.Id),
					Name:    version.Id,
					Type:    "appengine.googleapis.com/Version",
					Service: "appengine",
					Region:  app.LocationId,
					BasicAttributes: map[string]string{
						"project_id":    projectID,
						"service_id":    service.Id,
						"runtime":       version.Runtime,
						"serving_status": version.ServingStatus,
					},
				}
				
				resources = append(resources, versionRef)
			}
		}
	}
	
	return resources, nil
}

func (aes *AppEngineScanner) DescribeResource(ctx context.Context, resourceID string) (*pb.Resource, error) {
	client, err := aes.clientFactory.GetAppEngineClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create app engine client: %w", err)
	}
	
	parts := strings.Split(resourceID, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid App Engine resource ID format")
	}
	
	projectID := parts[1]
	
	if len(parts) == 2 {
		// Application resource
		app, err := client.Apps.Get(projectID).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get App Engine application: %w", err)
		}
		
		resource := &pb.Resource{
			Provider:     "gcp",
			Service:      "appengine",
			Type:         "appengine.googleapis.com/Application",
			Id:           resourceID,
			Name:         projectID,
			Region:       app.LocationId,
			Tags:         make(map[string]string),
			DiscoveredAt: timestamppb.Now(),
		}
		
		if rawData, err := json.Marshal(app); err == nil {
			resource.RawData = string(rawData)
		}
		
		return resource, nil
	}
	
	return nil, fmt.Errorf("unsupported App Engine resource type")
}

// Helper functions for enhanced scanners

func convertRefToResourceEnhanced(ref *pb.ResourceRef) *pb.Resource {
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
	
	// Extract labels as tags and preserve other attributes
	if ref.BasicAttributes != nil {
		for k, v := range ref.BasicAttributes {
			if strings.HasPrefix(k, "label_") {
				labelName := strings.TrimPrefix(k, "label_")
				resource.Tags[labelName] = v
			}
		}
		
		// Note: Metadata field not available in pb.Resource
		// Additional metadata is preserved in the RawData field when DescribeResource is called
	}
	
	return resource
}

func extractRegionFromRunName(name string) string {
	// Extract region from Cloud Run resource name
	// Format: projects/{project}/locations/{location}/services/{service}
	parts := strings.Split(name, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return "unknown"
}

func extractLocationFromFunctionName(name string) string {
	// Extract location from Cloud Function resource name  
	// Format: projects/{project}/locations/{location}/functions/{function}
	parts := strings.Split(name, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return "unknown"
}

type serviceResult struct {
	service   string
	resources []*pb.Resource
	err       error
}