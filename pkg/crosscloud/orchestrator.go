package crosscloud

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
)

// CrossCloudOrchestrator manages cross-cloud scanning and correlation
type CrossCloudOrchestrator struct {
	providers   map[string]Provider
	db          DatabaseInterface
	correlators []Correlator
	mu          sync.RWMutex
}

// Provider represents a cloud provider interface for cross-cloud operations
type Provider interface {
	GetName() string
	GetRegions() []string
	ScanResources(ctx context.Context, config ScanConfig) (*ScanResult, error)
	ExtractNetworkInfo(resource *models.Resource) (*NetworkInfo, error)
	ExtractIPAddresses(resource *models.Resource) ([]*models.IPAddress, error)
	ExtractDNSRecords(resource *models.Resource) ([]*models.DNSRecord, error)
}

// DatabaseInterface abstracts database operations for cross-cloud data
type DatabaseInterface interface {
	StoreResources(resources []*models.Resource) error
	StoreIPAddresses(addresses []*models.IPAddress) error
	StoreDNSRecords(records []*models.DNSRecord) error
	StoreCorrelations(correlations interface{}) error
	GetResourcesByProvider(provider string) ([]*models.Resource, error)
	GetIPAddressesByProvider(provider string) ([]*models.IPAddress, error)
	GetDNSRecordsByProvider(provider string) ([]*models.DNSRecord, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Correlator defines interface for correlation algorithms
type Correlator interface {
	GetName() string
	GetSupportedTypes() []string
	FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error)
}

// ScanConfig defines configuration for cross-cloud scanning
type ScanConfig struct {
	Providers        []string          `json:"providers"`
	Regions          []string          `json:"regions"`
	Services         []string          `json:"services"`
	IncludeRelations bool              `json:"include_relations"`
	ParallelScans    int               `json:"parallel_scans"`
	Timeout          time.Duration     `json:"timeout"`
	Filters          map[string]string `json:"filters"`
}

// ScanResult represents the result of a cross-cloud scan
type ScanResult struct {
	Provider      string              `json:"provider"`
	Region        string              `json:"region"`
	Resources     []*models.Resource  `json:"resources"`
	IPAddresses   []*models.IPAddress `json:"ip_addresses"`
	DNSRecords    []*models.DNSRecord `json:"dns_records"`
	Errors        []error             `json:"errors"`
	ScanDuration  time.Duration       `json:"scan_duration"`
	ResourceCount int                 `json:"resource_count"`
}

// CrossCloudCorrelation represents a correlation between resources across clouds
type CrossCloudCorrelation struct {
	ID                 string                 `json:"id"`
	SourceResourceID   string                 `json:"source_resource_id"`
	TargetResourceID   string                 `json:"target_resource_id"`
	SourceProvider     string                 `json:"source_provider"`
	TargetProvider     string                 `json:"target_provider"`
	CorrelationType    string                 `json:"correlation_type"`
	CorrelationSubtype string                 `json:"correlation_subtype"`
	CorrelationMethod  string                 `json:"correlation_method"`
	ConfidenceScore    float64                `json:"confidence_score"`
	Evidence           map[string]interface{} `json:"evidence"`
	MatchingAttributes map[string]interface{} `json:"matching_attributes"`
	Description        string                 `json:"description"`
	Status             string                 `json:"status"`
	Verified           bool                   `json:"verified"`
	DiscoveredAt       time.Time              `json:"discovered_at"`
}

// NetworkInfo represents network information extracted from resources
type NetworkInfo struct {
	VPCId            string   `json:"vpc_id"`
	SubnetIds        []string `json:"subnet_ids"`
	SecurityGroupIds []string `json:"security_group_ids"`
	PublicIPs        []string `json:"public_ips"`
	PrivateIPs       []string `json:"private_ips"`
	DNSNames         []string `json:"dns_names"`
	LoadBalancers    []string `json:"load_balancers"`
	Gateways         []string `json:"gateways"`
}

// NewCrossCloudOrchestrator creates a new cross-cloud orchestrator
func NewCrossCloudOrchestrator(db DatabaseInterface) *CrossCloudOrchestrator {
	return &CrossCloudOrchestrator{
		providers:   make(map[string]Provider),
		db:          db,
		correlators: make([]Correlator, 0),
	}
}

// RegisterProvider registers a cloud provider
func (o *CrossCloudOrchestrator) RegisterProvider(provider Provider) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.providers[provider.GetName()] = provider
}

// RegisterCorrelator registers a correlation algorithm
func (o *CrossCloudOrchestrator) RegisterCorrelator(correlator Correlator) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.correlators = append(o.correlators, correlator)
}

// GetProviders returns all registered providers
func (o *CrossCloudOrchestrator) GetProviders() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	providers := make([]string, 0, len(o.providers))
	for name := range o.providers {
		providers = append(providers, name)
	}
	return providers
}

// ScanAllProviders performs a comprehensive scan across all registered providers
func (o *CrossCloudOrchestrator) ScanAllProviders(ctx context.Context, config ScanConfig) (*CrossCloudScanResult, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	startTime := time.Now()
	result := &CrossCloudScanResult{
		StartTime: startTime,
		Results:   make(map[string]*ScanResult),
		Errors:    make([]error, 0),
	}

	// Determine which providers to scan
	providersToScan := config.Providers
	if len(providersToScan) == 0 {
		providersToScan = o.GetProviders()
	}

	// Create a channel for results
	resultChan := make(chan *ProviderScanResult, len(providersToScan))

	// Create a wait group for parallel scanning
	var wg sync.WaitGroup

	// Scan each provider in parallel
	for _, providerName := range providersToScan {
		provider, exists := o.providers[providerName]
		if !exists {
			result.Errors = append(result.Errors, fmt.Errorf("provider %s not registered", providerName))
			continue
		}

		wg.Add(1)
		go func(pName string, p Provider) {
			defer wg.Done()

			scanResult, err := p.ScanResources(ctx, config)
			resultChan <- &ProviderScanResult{
				Provider: pName,
				Result:   scanResult,
				Error:    err,
			}
		}(providerName, provider)
	}

	// Wait for all scans to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for providerResult := range resultChan {
		if providerResult.Error != nil {
			result.Errors = append(result.Errors,
				fmt.Errorf("provider %s: %w", providerResult.Provider, providerResult.Error))
		} else {
			result.Results[providerResult.Provider] = providerResult.Result
			result.TotalResources += providerResult.Result.ResourceCount
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// CorrelateResources finds correlations between resources across clouds
func (o *CrossCloudOrchestrator) CorrelateResources(ctx context.Context) (*CorrelationResult, error) {
	startTime := time.Now()

	// Get all resources from all providers
	allResources := make([]*models.Resource, 0)
	for providerName := range o.providers {
		resources, err := o.db.GetResourcesByProvider(providerName)
		if err != nil {
			return nil, fmt.Errorf("failed to get resources for provider %s: %w", providerName, err)
		}
		allResources = append(allResources, resources...)
	}

	// Run all correlators
	allCorrelations := make([]*CrossCloudCorrelation, 0)
	for _, correlator := range o.correlators {
		correlations, err := correlator.FindCorrelations(ctx, allResources)
		if err != nil {
			return nil, fmt.Errorf("correlator %s failed: %w", correlator.GetName(), err)
		}
		allCorrelations = append(allCorrelations, correlations...)
	}

	// Store correlations
	if err := o.db.StoreCorrelations(allCorrelations); err != nil {
		return nil, fmt.Errorf("failed to store correlations: %w", err)
	}

	return &CorrelationResult{
		TotalResources:    len(allResources),
		TotalCorrelations: len(allCorrelations),
		Duration:          time.Since(startTime),
		Correlations:      allCorrelations,
	}, nil
}

// ExtractNetworkInformation extracts network information from all resources
func (o *CrossCloudOrchestrator) ExtractNetworkInformation(ctx context.Context) error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for providerName, provider := range o.providers {
		resources, err := o.db.GetResourcesByProvider(providerName)
		if err != nil {
			return fmt.Errorf("failed to get resources for provider %s: %w", providerName, err)
		}

		var allIPs []*models.IPAddress
		var allDNS []*models.DNSRecord

		for _, resource := range resources {
			// Extract IP addresses
			ips, err := provider.ExtractIPAddresses(resource)
			if err != nil {
				continue // Skip resources that can't be processed
			}
			allIPs = append(allIPs, ips...)

			// Extract DNS records
			dns, err := provider.ExtractDNSRecords(resource)
			if err != nil {
				continue // Skip resources that can't be processed
			}
			allDNS = append(allDNS, dns...)
		}

		// Store extracted information
		if len(allIPs) > 0 {
			if err := o.db.StoreIPAddresses(allIPs); err != nil {
				return fmt.Errorf("failed to store IP addresses for provider %s: %w", providerName, err)
			}
		}

		if len(allDNS) > 0 {
			if err := o.db.StoreDNSRecords(allDNS); err != nil {
				return fmt.Errorf("failed to store DNS records for provider %s: %w", providerName, err)
			}
		}
	}

	return nil
}

// Supporting types for results

// CrossCloudScanResult represents the result of scanning all providers
type CrossCloudScanResult struct {
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	Duration       time.Duration          `json:"duration"`
	Results        map[string]*ScanResult `json:"results"`
	TotalResources int                    `json:"total_resources"`
	Errors         []error                `json:"errors"`
}

// ProviderScanResult represents the result of scanning a single provider
type ProviderScanResult struct {
	Provider string
	Result   *ScanResult
	Error    error
}

// CorrelationResult represents the result of correlation analysis
type CorrelationResult struct {
	TotalResources    int                      `json:"total_resources"`
	TotalCorrelations int                      `json:"total_correlations"`
	Duration          time.Duration            `json:"duration"`
	Correlations      []*CrossCloudCorrelation `json:"correlations"`
}
