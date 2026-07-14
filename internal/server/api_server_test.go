package server

import (
	"context"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func TestListProvidersUsesShippedCatalog(t *testing.T) {
	server := &APIServer{}
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
