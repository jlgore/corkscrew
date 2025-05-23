package client

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew-generator/internal/shared"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
)

type PluginManager struct {
	mu        sync.RWMutex
	clients   map[string]*plugin.Client
	scanners  map[string]shared.Scanner
	pluginDir string
}

func NewPluginManager(pluginDir string) *PluginManager {
	return &PluginManager{
		clients:   make(map[string]*plugin.Client),
		scanners:  make(map[string]shared.Scanner),
		pluginDir: pluginDir,
	}
}

func (pm *PluginManager) LoadScanner(service string) (shared.Scanner, error) {
	pm.mu.RLock()
	if scanner, exists := pm.scanners[service]; exists {
		pm.mu.RUnlock()
		return scanner, nil
	}
	pm.mu.RUnlock()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if scanner, exists := pm.scanners[service]; exists {
		return scanner, nil
	}

	// Build plugin path
	pluginPath := filepath.Join(pm.pluginDir, fmt.Sprintf("corkscrew-%s", service))
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("plugin not found: %s", pluginPath)
	}

	// Create plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins:         shared.PluginMap,
		Cmd:             exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		SyncStdout: os.Stdout,
		SyncStderr: os.Stderr,
	})

	// Connect via gRPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to create plugin client: %w", err)
	}

	// Get the scanner
	raw, err := rpcClient.Dispense("scanner")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense scanner: %w", err)
	}

	scanner := raw.(shared.Scanner)
	pm.clients[service] = client
	pm.scanners[service] = scanner

	return scanner, nil
}

func (pm *PluginManager) UnloadScanner(service string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if client, exists := pm.clients[service]; exists {
		client.Kill()
		delete(pm.clients, service)
		delete(pm.scanners, service)
	}
}

func (pm *PluginManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, client := range pm.clients {
		client.Kill()
	}

	pm.clients = make(map[string]*plugin.Client)
	pm.scanners = make(map[string]shared.Scanner)
}

func (pm *PluginManager) ListLoadedScanners() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	services := make([]string, 0, len(pm.scanners))
	for service := range pm.scanners {
		services = append(services, service)
	}
	return services
}

func (pm *PluginManager) GetScannerInfo(service string) (*pb.ServiceInfoResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.GetServiceInfo(context.Background(), &pb.Empty{})
}

func (pm *PluginManager) ScanService(ctx context.Context, service string, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.Scan(ctx, req)
}

func (pm *PluginManager) GetServiceSchemas(service string) (*pb.SchemaResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.GetSchemas(context.Background(), &pb.Empty{})
}

// ScanMultipleServices scans multiple services concurrently
func (pm *PluginManager) ScanMultipleServices(ctx context.Context, services []string, req *pb.ScanRequest) (map[string]*pb.ScanResponse, error) {
	results := make(map[string]*pb.ScanResponse)
	errors := make(map[string]error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, service := range services {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()

			resp, err := pm.ScanService(ctx, svc, req)
			
			mu.Lock()
			if err != nil {
				errors[svc] = err
			} else {
				results[svc] = resp
			}
			mu.Unlock()
		}(service)
	}

	wg.Wait()

	// If there were any errors, return the first one
	if len(errors) > 0 {
		for service, err := range errors {
			return results, fmt.Errorf("service %s failed: %w", service, err)
		}
	}

	return results, nil
}
