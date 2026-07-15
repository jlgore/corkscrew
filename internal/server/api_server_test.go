package server

import (
	"context"
	"fmt"
	"testing"

	providerapp "github.com/jlgore/corkscrew/internal/app/providers"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

type catalogRuntime struct{}

func (catalogRuntime) List() []providerRuntime.Descriptor {
	providers := providercatalog.Shipped()
	descriptors := make([]providerRuntime.Descriptor, 0, len(providers))
	for _, provider := range providers {
		descriptors = append(descriptors, providerRuntime.Descriptor{
			Name: provider.Name, Description: provider.Description, Origin: providerRuntime.OriginOfficial,
		})
	}
	return descriptors
}

func (catalogRuntime) Open(context.Context, string, map[string]string) (providerRuntime.Session, error) {
	return nil, fmt.Errorf("not installed")
}

func TestListProvidersUsesShippedCatalog(t *testing.T) {
	server := &APIServer{providers: providerapp.NewApplication(catalogRuntime{})}
	response, err := server.ListProviders(context.Background(), &pb.APIListProvidersRequest{})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}

	want := providercatalog.Names()
	if len(response.Providers) != len(want) {
		t.Fatalf("provider count = %d, want %d", len(response.Providers), len(want))
	}
	for index, provider := range response.Providers {
		if provider.Name != want[index] {
			t.Fatalf("provider %d = %q, want %q", index, provider.Name, want[index])
		}
		if provider.Description == "" {
			t.Fatalf("provider %q has no description", provider.Name)
		}
	}
}
