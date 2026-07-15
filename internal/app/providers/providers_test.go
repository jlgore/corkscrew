package providers

import (
	"context"
	"testing"

	appconfig "github.com/jlgore/corkscrew/internal/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	"github.com/jlgore/corkscrew/internal/shared"
)

type fakeRuntime struct{ session providerRuntime.Session }

func (f fakeRuntime) List() []providerRuntime.Descriptor {
	return []providerRuntime.Descriptor{f.session.Descriptor()}
}
func (f fakeRuntime) Open(context.Context, string, map[string]string) (providerRuntime.Session, error) {
	return f.session, nil
}

type fakeSession struct {
	descriptor providerRuntime.Descriptor
	provider   shared.CloudProvider
}

func (s fakeSession) Descriptor() providerRuntime.Descriptor { return s.descriptor }
func (s fakeSession) Provider() shared.CloudProvider         { return s.provider }
func (s fakeSession) Require(capability providerRuntime.Capability) error {
	if !s.descriptor.Supports(capability) {
		return context.Canceled
	}
	return nil
}

type fakeProvider struct{ shared.CloudProvider }

func (fakeProvider) GetProviderInfo(context.Context, *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{Name: "acme", Version: "1.0.0"}, nil
}

func TestGetInfoUsesRuntimeSession(t *testing.T) {
	t.Parallel()

	session := fakeSession{
		descriptor: providerRuntime.Descriptor{Name: "acme", Installed: true},
		provider:   fakeProvider{},
	}
	info, err := GetInfo(context.Background(), fakeRuntime{session: session}, "acme", nil)
	if err != nil {
		t.Fatalf("GetInfo(): %v", err)
	}
	if info.Descriptor.Name != "acme" || info.Runtime.Version != "1.0.0" {
		t.Fatalf("info = %#v", info)
	}
}

func TestApplicationOwnsProviderStatusAndConfigValidation(t *testing.T) {
	t.Parallel()

	session := fakeSession{
		descriptor: providerRuntime.Descriptor{Name: "acme", Installed: true},
		provider:   fakeProvider{},
	}
	application := NewApplication(fakeRuntime{session: session})
	status := application.Status(context.Background(), "acme", nil)
	if !status.Available || !status.Initialized || status.Error != "" {
		t.Fatalf("status = %#v", status)
	}

	validation := application.ValidateConfig(&appconfig.CorkscrewConfig{Providers: map[string]appconfig.CloudProviderConfig{
		"acme":    {Enabled: true, Regions: []string{""}, Services: []string{"widgets"}},
		"missing": {Enabled: true},
	}})
	if validation.EnabledProviders != 1 || len(validation.Errors) != 2 {
		t.Fatalf("validation = %#v", validation)
	}
}
