// Package providers contains provider capabilities that are shared by the CLI,
// API, persistence, and plugin layers. Provider plugins remain responsible for
// discovery schemas and runtime behavior; this catalog describes providers
// shipped by Corkscrew itself.
package providers

import "strings"

// Provider describes a provider shipped with Corkscrew.
type Provider struct {
	Name          string
	Description   string
	ResourceTable string
}

var shipped = []Provider{
	{Name: "aws", Description: "Amazon Web Services provider", ResourceTable: "aws_resources"},
	{Name: "azure", Description: "Microsoft Azure provider", ResourceTable: "azure_resources"},
	{Name: "gcp", Description: "Google Cloud Platform provider", ResourceTable: "gcp_resources"},
	{Name: "kubernetes", Description: "Kubernetes provider", ResourceTable: "kubernetes_resources"},
	{Name: "github", Description: "GitHub organization and repository posture provider", ResourceTable: "github_resources"},
	{Name: "cloudflare", Description: "Cloudflare edge, Workers, storage, and Zero Trust provider", ResourceTable: "cloudflare_resources"},
}

// Shipped returns a copy of the provider catalog in stable display order.
func Shipped() []Provider {
	result := make([]Provider, len(shipped))
	copy(result, shipped)
	return result
}

// Names returns shipped provider names in stable display order.
func Names() []string {
	names := make([]string, 0, len(shipped))
	for _, provider := range shipped {
		names = append(names, provider.Name)
	}
	return names
}

// Lookup returns a shipped provider by its case-insensitive name.
func Lookup(name string) (Provider, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, provider := range shipped {
		if provider.Name == name {
			return provider, true
		}
	}
	return Provider{}, false
}
