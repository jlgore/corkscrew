package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ServiceAvailability manages AWS service availability by region
type ServiceAvailability struct {
	mu              sync.RWMutex
	regionCache     map[string][]string // region -> available services
	serviceCache    map[string][]string // service -> available regions
	lastUpdate      time.Time
	cacheDuration   time.Duration
}

// NewServiceAvailability creates a new service availability checker
func NewServiceAvailability() *ServiceAvailability {
	return &ServiceAvailability{
		regionCache:   make(map[string][]string),
		serviceCache:  make(map[string][]string),
		cacheDuration: 24 * time.Hour, // Cache for 24 hours
	}
}

// IsServiceAvailableInRegion checks if a service is available in a specific region
func (sa *ServiceAvailability) IsServiceAvailableInRegion(ctx context.Context, cfg aws.Config, service, region string) (bool, error) {
	sa.mu.RLock()
	if time.Since(sa.lastUpdate) < sa.cacheDuration {
		if services, ok := sa.regionCache[region]; ok {
			for _, s := range services {
				if s == service {
					sa.mu.RUnlock()
					return true, nil
				}
			}
			sa.mu.RUnlock()
			return false, nil
		}
	}
	sa.mu.RUnlock()

	// Need to refresh cache
	if err := sa.refreshRegionServices(ctx, cfg, region); err != nil {
		// On error, assume service is available
		return true, nil
	}

	// Check again after refresh
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	if services, ok := sa.regionCache[region]; ok {
		for _, s := range services {
			if s == service {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetAvailableRegionsForService returns all regions where a service is available
func (sa *ServiceAvailability) GetAvailableRegionsForService(ctx context.Context, cfg aws.Config, service string) ([]string, error) {
	sa.mu.RLock()
	if regions, ok := sa.serviceCache[service]; ok && time.Since(sa.lastUpdate) < sa.cacheDuration {
		sa.mu.RUnlock()
		return regions, nil
	}
	sa.mu.RUnlock()

	// Need to refresh all regions
	if err := sa.refreshAllRegions(ctx, cfg); err != nil {
		return nil, err
	}

	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.serviceCache[service], nil
}

// refreshRegionServices refreshes the service availability for a specific region
func (sa *ServiceAvailability) refreshRegionServices(ctx context.Context, cfg aws.Config, region string) error {
	// For now, we'll use a static mapping of known services per region
	// In a production system, this would query AWS APIs
	
	// Common services available in all regions
	commonServices := []string{
		"s3", "ec2", "iam", "cloudwatch", "sns", "sqs", "lambda", 
		"dynamodb", "rds", "vpc", "ecs", "eks", "cloudformation",
		"sts", "ssm", "secretsmanager", "kms",
	}

	// Services with limited regional availability
	regionalServices := map[string][]string{
		"us-east-1": append(commonServices, "route53", "cloudfront", "waf"),
		"us-west-2": append(commonServices, "sagemaker"),
		"eu-west-1": append(commonServices, "sagemaker"),
		// Add more regional mappings as needed
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()

	// Default to common services if region not in map
	if services, ok := regionalServices[region]; ok {
		sa.regionCache[region] = services
	} else {
		sa.regionCache[region] = commonServices
	}

	// Update service cache
	for _, service := range sa.regionCache[region] {
		if _, ok := sa.serviceCache[service]; !ok {
			sa.serviceCache[service] = []string{}
		}
		// Add region if not already present
		found := false
		for _, r := range sa.serviceCache[service] {
			if r == region {
				found = true
				break
			}
		}
		if !found {
			sa.serviceCache[service] = append(sa.serviceCache[service], region)
		}
	}

	sa.lastUpdate = time.Now()
	return nil
}

// refreshAllRegions refreshes service availability for all regions
func (sa *ServiceAvailability) refreshAllRegions(ctx context.Context, cfg aws.Config) error {
	// Get all regions
	regions, err := sa.getAllRegions(ctx, cfg)
	if err != nil {
		return err
	}

	// Refresh each region
	for _, region := range regions {
		if err := sa.refreshRegionServices(ctx, cfg, region); err != nil {
			// Continue on error
			continue
		}
	}

	return nil
}

// getAllRegions returns all available AWS regions
func (sa *ServiceAvailability) getAllRegions(ctx context.Context, cfg aws.Config) ([]string, error) {
	client := ec2.NewFromConfig(cfg)
	
	resp, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // Only get enabled regions
		Filters: []types.Filter{
			{
				Name:   aws.String("opt-in-status"),
				Values: []string{"opt-in-not-required", "opted-in"},
			},
		},
	})
	if err != nil {
		// Fallback to common regions
		return []string{
			"us-east-1", "us-east-2", "us-west-1", "us-west-2",
			"eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1",
			"ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-northeast-2",
			"sa-east-1", "ca-central-1",
		}, nil
	}

	regions := make([]string, 0, len(resp.Regions))
	for _, region := range resp.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}

	return regions, nil
}

// GetServiceEndpoint returns the endpoint URL for a service in a region
func (sa *ServiceAvailability) GetServiceEndpoint(service, region string) string {
	// Most services follow the pattern: service.region.amazonaws.com
	// With some exceptions
	switch service {
	case "s3":
		if region == "us-east-1" {
			return "s3.amazonaws.com"
		}
		return fmt.Sprintf("s3.%s.amazonaws.com", region)
	case "iam", "route53", "cloudfront", "waf":
		// Global services
		return fmt.Sprintf("%s.amazonaws.com", service)
	default:
		return fmt.Sprintf("%s.%s.amazonaws.com", service, region)
	}
}

// IsGlobalService returns true if the service is global (not region-specific)
func (sa *ServiceAvailability) IsGlobalService(service string) bool {
	globalServices := map[string]bool{
		"iam":        true,
		"route53":    true,
		"cloudfront": true,
		"waf":        true,
		"shield":     true,
		"organizations": true,
	}
	return globalServices[service]
}

// GetOptimalRegionForService suggests the best region for scanning a service
func (sa *ServiceAvailability) GetOptimalRegionForService(service string) string {
	if sa.IsGlobalService(service) {
		return "us-east-1" // Global services are typically in us-east-1
	}
	// Default to us-east-1 for other services
	return "us-east-1"
}