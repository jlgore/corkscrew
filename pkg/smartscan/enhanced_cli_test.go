package smartscan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/jlgore/corkscrew/internal/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type initializationTestProvider struct {
	shared.CloudProvider
	request  *pb.InitializeRequest
	response *pb.InitializeResponse
	err      error
}

func (p *initializationTestProvider) Initialize(_ context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.request = req
	return p.response, p.err
}

func TestBuildProviderInitializationConfigMergesYAMLAndScanOverrides(t *testing.T) {
	config := &SmartScanConfiguration{CorkscrewConfig: &appconfig.CorkscrewConfig{
		Providers: map[string]appconfig.CloudProviderConfig{
			"aws": {
				Config: map[string]string{
					"region":               "configured-region",
					"auth.secret.provider": "vault",
					"auth.secret.path":     "aws/prod",
				},
			},
		},
	}}

	got := buildProviderInitializationConfig(config, EnhancedScanOptions{
		Provider:       "aws",
		KubeconfigPath: "/tmp/kubeconfig",
		KubeContext:    "cluster-a",
	}, []string{"us-west-2"})

	if got["auth.secret.provider"] != "vault" || got["auth.secret.path"] != "aws/prod" {
		t.Fatalf("provider auth config was not preserved: %#v", got)
	}
	if got["region"] != "us-west-2" {
		t.Fatalf("region = %q, want CLI-resolved us-west-2", got["region"])
	}
	if got["kubeconfig_path"] != "/tmp/kubeconfig" || got["contexts"] != "cluster-a" {
		t.Fatalf("scan overrides were not applied: %#v", got)
	}
}

func TestInitializeProviderForScanForwardsConfig(t *testing.T) {
	provider := &initializationTestProvider{response: &pb.InitializeResponse{Success: true}}
	config := map[string]string{
		"region":                "us-east-1",
		"auth.secret.provider":  "vault",
		"auth.secret.path":      "aws/prod",
		"auth.secret.token_env": "CORKSCREW_VAULT_TOKEN",
	}

	if err := initializeProviderForScan(context.Background(), provider, "aws", config); err != nil {
		t.Fatalf("initializeProviderForScan() error = %v", err)
	}
	if provider.request.GetProvider() != "aws" {
		t.Fatalf("request.Provider = %q, want aws", provider.request.GetProvider())
	}
	if provider.request.GetConfig()["auth.secret.path"] != "aws/prod" {
		t.Fatalf("request.Config = %#v", provider.request.GetConfig())
	}
}

func TestInitializeProviderForScanRejectsUnsuccessfulResponse(t *testing.T) {
	provider := &initializationTestProvider{response: &pb.InitializeResponse{
		Success: false,
		Error:   "vault token not configured",
	}}

	err := initializeProviderForScan(context.Background(), provider, "aws", nil)
	if err == nil || !strings.Contains(err.Error(), "vault token not configured") {
		t.Fatalf("initializeProviderForScan() error = %v", err)
	}
}

func TestInitializeProviderForScanRejectsNilResponse(t *testing.T) {
	provider := &initializationTestProvider{}
	err := initializeProviderForScan(context.Background(), provider, "aws", nil)
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("initializeProviderForScan() error = %v", err)
	}
}

func TestInitializeProviderForScanWrapsTransportError(t *testing.T) {
	provider := &initializationTestProvider{err: errors.New("rpc unavailable")}
	err := initializeProviderForScan(context.Background(), provider, "aws", nil)
	if err == nil || !strings.Contains(err.Error(), "rpc unavailable") {
		t.Fatalf("initializeProviderForScan() error = %v", err)
	}
}

func TestSaveResultsToTimestampedFile(t *testing.T) {
	tempDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	results := &AggregatedResults{
		AllResources: []*pb.Resource{
			{
				Provider: "aws",
				Service:  "s3",
				Type:     "Bucket",
				Id:       "bucket-1",
				Name:     "bucket-1",
			},
		},
		Summary: &ScanSummary{TotalResources: 1},
	}

	filename, err := saveResultsToTimestampedFile(results, "aws")
	if err != nil {
		t.Fatalf("saveResultsToTimestampedFile returned error: %v", err)
	}
	if !strings.HasPrefix(filename, "enhanced-scan-aws-") || !strings.HasSuffix(filename, ".json") {
		t.Fatalf("filename = %q, want timestamped aws JSON filename", filename)
	}

	data, err := os.ReadFile(filepath.Join(tempDir, filename))
	if err != nil {
		t.Fatalf("read saved results: %v", err)
	}

	var saved AggregatedResults
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("saved results are not valid JSON: %v", err)
	}
	if len(saved.AllResources) != 1 || saved.AllResources[0].Id != "bucket-1" {
		t.Fatalf("saved resources = %#v, want bucket-1", saved.AllResources)
	}
	if saved.Summary == nil || saved.Summary.TotalResources != 1 {
		t.Fatalf("saved summary = %#v, want total resources 1", saved.Summary)
	}
}
