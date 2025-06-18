package main

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/appengine/v1"
	"google.golang.org/api/bigquery/v2"
	"google.golang.org/api/cloudfunctions/v1"
	"google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/sqladmin/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/pubsub/v1"
	"google.golang.org/api/run/v1"
	"google.golang.org/api/serviceusage/v1"
	"google.golang.org/api/storage/v1"
)

// ClientFactory manages GCP API clients and credentials
type ClientFactory struct {
	mu         sync.RWMutex
	credential *google.Credentials
	projectIDs []string
	
	// Cached clients
	clients map[string]interface{}
}

// NewClientFactory creates a new client factory
func NewClientFactory() *ClientFactory {
	return &ClientFactory{
		clients: make(map[string]interface{}),
	}
}

// Initialize sets up authentication using Application Default Credentials
func (cf *ClientFactory) Initialize(ctx context.Context) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Try to find default credentials
	// This supports multiple authentication methods:
	// 1. GOOGLE_APPLICATION_CREDENTIALS environment variable
	// 2. gcloud auth application-default login
	// 3. GCE metadata service
	// 4. Cloud Shell built-in credentials
	creds, err := google.FindDefaultCredentials(ctx,
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/cloud-asset",
		"https://www.googleapis.com/auth/compute.readonly",
		"https://www.googleapis.com/auth/storage.readonly",
	)
	if err != nil {
		return fmt.Errorf("failed to find default credentials: %w", err)
	}
	
	cf.credential = creds
	
	// Try to detect project ID if not set
	if len(cf.projectIDs) == 0 {
		if projectID := cf.detectProjectID(); projectID != "" {
			cf.projectIDs = []string{projectID}
		}
	}
	
	return nil
}

// SetProjectIDs sets the project IDs to use
func (cf *ClientFactory) SetProjectIDs(projectIDs []string) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.projectIDs = projectIDs
}

// GetProjectIDs returns the configured project IDs
func (cf *ClientFactory) GetProjectIDs() []string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return cf.projectIDs
}

// detectProjectID attempts to detect the project ID from various sources
func (cf *ClientFactory) detectProjectID() string {
	// Try from credentials
	if cf.credential != nil && cf.credential.ProjectID != "" {
		return cf.credential.ProjectID
	}
	
	// Try from metadata service (when running on GCE/GKE)
	if metadata.OnGCE() {
		if projectID, err := metadata.ProjectID(); err == nil {
			return projectID
		}
	}
	
	return ""
}

// GetTokenSource returns an OAuth2 token source
func (cf *ClientFactory) GetTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	
	if cf.credential == nil {
		return nil, fmt.Errorf("credentials not initialized")
	}
	
	return cf.credential.TokenSource, nil
}

// GetServiceUsageClient returns a Service Usage API client
func (cf *ClientFactory) GetServiceUsageClient(ctx context.Context) (*serviceusage.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["serviceusage"]; ok {
		return client.(*serviceusage.Service), nil
	}
	
	// Create new client
	client, err := serviceusage.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create service usage client: %w", err)
	}
	
	// Cache it
	cf.clients["serviceusage"] = client
	
	return client, nil
}

// GetComputeClient returns a Compute Engine API client
func (cf *ClientFactory) GetComputeClient(ctx context.Context) (*compute.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["compute"]; ok {
		return client.(*compute.Service), nil
	}
	
	// Create new client
	client, err := compute.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %w", err)
	}
	
	// Cache it
	cf.clients["compute"] = client
	
	return client, nil
}

// GetStorageClient returns a Cloud Storage API client
func (cf *ClientFactory) GetStorageClient(ctx context.Context) (*storage.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["storage"]; ok {
		return client.(*storage.Service), nil
	}
	
	// Create new client
	client, err := storage.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	
	// Cache it
	cf.clients["storage"] = client
	
	return client, nil
}

// GetContainerClient returns a Kubernetes Engine API client
func (cf *ClientFactory) GetContainerClient(ctx context.Context) (*container.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["container"]; ok {
		return client.(*container.Service), nil
	}
	
	// Create new client
	client, err := container.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create container client: %w", err)
	}
	
	// Cache it
	cf.clients["container"] = client
	
	return client, nil
}

// GetResourceManagerClient returns a Resource Manager API client
func (cf *ClientFactory) GetResourceManagerClient(ctx context.Context) (*cloudresourcemanager.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["resourcemanager"]; ok {
		return client.(*cloudresourcemanager.Service), nil
	}
	
	// Create new client
	client, err := cloudresourcemanager.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource manager client: %w", err)
	}
	
	// Cache it
	cf.clients["resourcemanager"] = client
	
	return client, nil
}

// GetBigQueryClient returns a BigQuery API client
func (cf *ClientFactory) GetBigQueryClient(ctx context.Context) (*bigquery.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["bigquery"]; ok {
		return client.(*bigquery.Service), nil
	}
	
	// Create new client
	client, err := bigquery.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create bigquery client: %w", err)
	}
	
	// Cache it
	cf.clients["bigquery"] = client
	
	return client, nil
}

// GetCloudSQLClient returns a Cloud SQL API client
func (cf *ClientFactory) GetCloudSQLClient(ctx context.Context) (*sqladmin.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["cloudsql"]; ok {
		return client.(*sqladmin.Service), nil
	}
	
	// Create new client
	client, err := sqladmin.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudsql client: %w", err)
	}
	
	// Cache it
	cf.clients["cloudsql"] = client
	
	return client, nil
}

// GetPubSubClient returns a Pub/Sub API client
func (cf *ClientFactory) GetPubSubClient(ctx context.Context) (*pubsub.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["pubsub"]; ok {
		return client.(*pubsub.Service), nil
	}
	
	// Create new client
	client, err := pubsub.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}
	
	// Cache it
	cf.clients["pubsub"] = client
	
	return client, nil
}

// GetCloudRunClient returns a Cloud Run API client
func (cf *ClientFactory) GetCloudRunClient(ctx context.Context) (*run.APIService, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["run"]; ok {
		return client.(*run.APIService), nil
	}
	
	// Create new client
	client, err := run.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud run client: %w", err)
	}
	
	// Cache it
	cf.clients["run"] = client
	
	return client, nil
}

// GetCloudFunctionsClient returns a Cloud Functions API client
func (cf *ClientFactory) GetCloudFunctionsClient(ctx context.Context) (*cloudfunctions.Service, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["cloudfunctions"]; ok {
		return client.(*cloudfunctions.Service), nil
	}
	
	// Create new client
	client, err := cloudfunctions.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud functions client: %w", err)
	}
	
	// Cache it
	cf.clients["cloudfunctions"] = client
	
	return client, nil
}

// GetAppEngineClient returns an App Engine API client
func (cf *ClientFactory) GetAppEngineClient(ctx context.Context) (*appengine.APIService, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Check cache
	if client, ok := cf.clients["appengine"]; ok {
		return client.(*appengine.APIService), nil
	}
	
	// Create new client
	client, err := appengine.NewService(ctx, option.WithCredentials(cf.credential))
	if err != nil {
		return nil, fmt.Errorf("failed to create app engine client: %w", err)
	}
	
	// Cache it
	cf.clients["appengine"] = client
	
	return client, nil
}

// Close cleans up any resources
func (cf *ClientFactory) Close() error {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	// Clear cached clients
	cf.clients = make(map[string]interface{})
	
	return nil
}