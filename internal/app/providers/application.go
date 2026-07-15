package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	appconfig "github.com/jlgore/corkscrew/internal/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
)

// Application owns provider runtime lifecycle and exposes provider workflows
// to CLI and server adapters.
type Application struct {
	runtime Runtime
	close   func() error
}

// OpenDefault constructs the manifest-driven runtime behind the provider
// application seam.
func OpenDefault(warnings io.Writer) (*Application, error) {
	runtime, err := providerRuntime.NewDefaultRuntime(warnings)
	if err != nil {
		return nil, err
	}
	return &Application{runtime: runtime, close: runtime.Close}, nil
}

// NewApplication adapts a runtime for tests and alternate composition roots.
// The caller retains ownership of the supplied runtime.
func NewApplication(runtime Runtime) *Application {
	return &Application{runtime: runtime}
}

func (a *Application) Close() error {
	if a == nil || a.close == nil {
		return nil
	}
	return a.close()
}

func (a *Application) List() []providerRuntime.Descriptor {
	if a == nil || a.runtime == nil {
		return nil
	}
	return List(a.runtime)
}

func (a *Application) Descriptor(name string) (providerRuntime.Descriptor, bool) {
	for _, descriptor := range a.List() {
		if descriptor.Name == name {
			return descriptor, true
		}
	}
	return providerRuntime.Descriptor{}, false
}

type Status struct {
	Available   bool
	Initialized bool
	Error       string
}

func (a *Application) Status(ctx context.Context, name string, config map[string]string) Status {
	descriptor, found := a.Descriptor(name)
	if !found || !descriptor.Installed {
		return Status{Error: fmt.Sprintf("provider %q is not installed", name)}
	}
	if _, err := a.GetInfo(ctx, name, config); err != nil {
		return Status{Available: true, Error: err.Error()}
	}
	return Status{Available: true, Initialized: true}
}

func (a *Application) GetInfo(ctx context.Context, name string, config map[string]string) (Info, error) {
	if a == nil || a.runtime == nil {
		return Info{}, fmt.Errorf("provider runtime is unavailable")
	}
	return GetInfo(ctx, a.runtime, name, config)
}

func (a *Application) DiscoverServices(ctx context.Context, name string, config map[string]string, request *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if a == nil || a.runtime == nil {
		return nil, fmt.Errorf("provider runtime is unavailable")
	}
	return DiscoverServices(ctx, a.runtime, name, config, request)
}

func (a *Application) ListResources(ctx context.Context, name string, config map[string]string, request *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if a == nil || a.runtime == nil {
		return nil, fmt.Errorf("provider runtime is unavailable")
	}
	return ListResources(ctx, a.runtime, name, config, request)
}

func (a *Application) DescribeResource(ctx context.Context, name string, config map[string]string, request *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if a == nil || a.runtime == nil {
		return nil, fmt.Errorf("provider runtime is unavailable")
	}
	return DescribeResource(ctx, a.runtime, name, config, request)
}

func (a *Application) GetSchemas(ctx context.Context, name string, config map[string]string, request *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if a == nil || a.runtime == nil {
		return nil, fmt.Errorf("provider runtime is unavailable")
	}
	return GetSchemas(ctx, a.runtime, name, config, request)
}

type ConfigValidation struct {
	Errors           []string
	EnabledProviders int
}

// ValidateConfig applies provider registration and provider-scope validation
// without leaking runtime policy into the CLI adapter.
func (a *Application) ValidateConfig(config *appconfig.CorkscrewConfig) ConfigValidation {
	if config == nil {
		return ConfigValidation{Errors: []string{"configuration is required"}}
	}
	registered := make(map[string]bool)
	for _, descriptor := range a.List() {
		registered[descriptor.Name] = true
	}
	names := make([]string, 0, len(config.Providers))
	for name := range config.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	result := ConfigValidation{}
	for _, name := range names {
		if !registered[name] {
			result.Errors = append(result.Errors, fmt.Sprintf("provider %s is not registered", name))
			continue
		}
		providerConfig := config.Providers[name]
		if providerConfig.Enabled {
			result.EnabledProviders++
		}
		for _, region := range providerConfig.Regions {
			if strings.TrimSpace(region) == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("provider %s has an empty region entry", name))
			}
		}
		for _, service := range providerConfig.Services {
			if strings.TrimSpace(service) == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("provider %s has an empty service entry", name))
			}
		}
	}
	return result
}
