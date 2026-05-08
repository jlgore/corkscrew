package discovery

// ServiceAvailability classifies AWS services as global vs regional and
// recommends a default region for scans. The richer
// "is-service-available-in-region" lookups have been removed; callers that
// need that information go to the AWS SDK directly.
type ServiceAvailability struct{}

func NewServiceAvailability() *ServiceAvailability {
	return &ServiceAvailability{}
}

// IsGlobalService returns true if the service is global (not region-specific).
func (sa *ServiceAvailability) IsGlobalService(service string) bool {
	globalServices := map[string]bool{
		"iam":           true,
		"route53":       true,
		"cloudfront":    true,
		"waf":           true,
		"shield":        true,
		"organizations": true,
	}
	return globalServices[service]
}

// GetOptimalRegionForService suggests the best region for scanning a service.
func (sa *ServiceAvailability) GetOptimalRegionForService(service string) string {
	if sa.IsGlobalService(service) {
		return "us-east-1"
	}
	return "us-east-1"
}
