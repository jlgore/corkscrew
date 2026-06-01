package cloudflareauth

import (
	"fmt"
	"sort"
)

var permissionBundles = []PermissionBundle{
	{
		Name:        "core_read",
		Services:    []string{"accounts", "zones"},
		Scopes:      []string{"account:read", "zone:read"},
		Description: "Account and zone inventory",
	},
	{
		Name:        "dns_read",
		Services:    []string{"dns"},
		Scopes:      []string{"dns:read", "zone:read"},
		Description: "DNS records and DNSSEC",
	},
	{
		Name:        "rulesets_read",
		Services:    []string{"rulesets", "delivery"},
		Scopes:      []string{"account:read", "zone:read", "rulesets:read"},
		Description: "Rulesets, transforms, and delivery policies",
	},
	{
		Name:        "workers_read",
		Services:    []string{"workers"},
		Scopes:      []string{"account:read", "workers:read"},
		Description: "Workers scripts, routes, and deployments",
	},
	{
		Name:        "storage_read",
		Services:    []string{"storage"},
		Scopes:      []string{"account:read", "workers.kv:read", "workers.queues:read", "workers.r2:read"},
		Description: "R2, KV, queues, and secrets stores",
	},
	{
		Name:        "data_read",
		Services:    []string{"data"},
		Scopes:      []string{"account:read", "d1:read"},
		Description: "D1 and other data-plane inventory",
	},
	{
		Name:        "pages_read",
		Services:    []string{"pages"},
		Scopes:      []string{"account:read", "pages:read"},
		Description: "Pages projects, domains, and deployments",
	},
	{
		Name:        "zero_trust_read",
		Services:    []string{"zero_trust_access", "zero_trust_network", "zero_trust_devices"},
		Scopes:      []string{"access:read", "account:read", "zero_trust:read"},
		Description: "Zero Trust access, network, and device inventory",
	},
	{
		Name:        "load_balancing_read",
		Services:    []string{"load_balancing"},
		Scopes:      []string{"account:read", "zone:read", "load_balancing:read"},
		Description: "Load balancers, monitors, pools, and health checks",
	},
	{
		Name:        "certs_read",
		Services:    []string{"edge_certs"},
		Scopes:      []string{"account:read", "ssl:read", "zone:read"},
		Description: "SSL, certificate, and hostname inventory",
	},
	{
		Name:        "observability_read",
		Services:    []string{"observability"},
		Scopes:      []string{"account:read", "logs:read"},
		Description: "Logpush, alerts, and audit inventory",
	},
	{
		Name:        "media_read",
		Services:    []string{"media"},
		Scopes:      []string{"account:read", "stream:read"},
		Description: "Stream, Images, and media-adjacent inventory",
	},
	{
		Name:        "security_read",
		Services:    []string{"security"},
		Scopes:      []string{"account:read", "security:read", "zone:read"},
		Description: "Security posture and scanner inventory",
	},
	{
		Name:        "email_read",
		Services:    []string{"email"},
		Scopes:      []string{"account:read", "email:read", "zone:read"},
		Description: "Email routing, sending, and security inventory",
	},
	{
		Name:        "apps_ai_read",
		Services:    []string{"apps_ai"},
		Scopes:      []string{"account:read", "ai:read"},
		Description: "AI, Turnstile, Zaraz, and app-layer inventory",
	},
	{
		Name:        "network_services_read",
		Services:    []string{"network_services"},
		Scopes:      []string{"account:read", "zone:read", "network:read"},
		Description: "Spectrum, registrar, DNS firewall, and network services",
	},
	{
		Name:        "specialized_read",
		Services:    []string{"specialized"},
		Scopes:      []string{"account:read", "specialized:read"},
		Description: "Specialized enterprise and niche product inventory",
	},
}

type PermissionPlanner interface {
	Plan(services []string) (*PermissionPlan, error)
}

type StaticPermissionPlanner struct{}

func (p *StaticPermissionPlanner) Plan(services []string) (*PermissionPlan, error) {
	requested, err := normalizeRequestedServices(services)
	if err != nil {
		return nil, err
	}

	bundles := make([]PermissionBundle, 0, len(permissionBundles))
	scopeSet := make(map[string]struct{})
	for _, bundle := range permissionBundles {
		if bundleMatches(bundle, requested) {
			bundles = append(bundles, bundle)
			for _, scope := range bundle.Scopes {
				scopeSet[scope] = struct{}{}
			}
		}
	}

	scopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	serviceList := make([]string, 0, len(requested))
	for service := range requested {
		serviceList = append(serviceList, service)
	}
	sort.Strings(serviceList)

	return &PermissionPlan{Services: serviceList, Bundles: bundles, Scopes: scopes}, nil
}

func BundleByName(name string) (PermissionBundle, bool) {
	if name == "full_readonly" {
		return fullReadonlyBundle(), true
	}
	for _, bundle := range permissionBundles {
		if bundle.Name == name {
			return bundle, true
		}
	}
	return PermissionBundle{}, false
}

func fullReadonlyBundle() PermissionBundle {
	scopeSet := make(map[string]struct{})
	for _, bundle := range permissionBundles {
		for _, scope := range bundle.Scopes {
			scopeSet[scope] = struct{}{}
		}
	}
	scopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return PermissionBundle{
		Name:        "full_readonly",
		Services:    append([]string(nil), CanonicalServices...),
		Scopes:      scopes,
		Description: "Convenience bundle for all current Cloudflare read-only scan groups",
	}
}

func bundleMatches(bundle PermissionBundle, requested map[string]struct{}) bool {
	for _, service := range bundle.Services {
		if _, ok := requested[service]; ok {
			return true
		}
	}
	return false
}

func normalizeRequestedServices(services []string) (map[string]struct{}, error) {
	if len(services) == 0 {
		services = []string{"accounts", "zones"}
	}
	known := make(map[string]struct{}, len(CanonicalServices))
	for _, service := range CanonicalServices {
		known[service] = struct{}{}
	}
	requested := make(map[string]struct{}, len(services))
	for _, service := range services {
		if service == "full_readonly" {
			for _, canonical := range CanonicalServices {
				requested[canonical] = struct{}{}
			}
			continue
		}
		if _, ok := known[service]; !ok {
			return nil, fmt.Errorf("unknown Cloudflare service %q", service)
		}
		requested[service] = struct{}{}
	}
	return requested, nil
}
