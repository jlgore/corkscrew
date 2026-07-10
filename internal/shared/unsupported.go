package shared

import (
	"fmt"
	"strconv"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

const (
	OptionalGenerateServiceScanners = "generate_service_scanners"
	OptionalConfigureDiscovery      = "configure_discovery"
	OptionalAnalyzeDiscoveredData   = "analyze_discovered_data"
	OptionalGenerateFromAnalysis    = "generate_from_analysis"
)

// WithOptionalCapabilities returns a copy of capabilities augmented with
// canonical feature-detection flags for optional provider operations.
func WithOptionalCapabilities(capabilities map[string]string, supported map[string]bool) map[string]string {
	out := make(map[string]string, len(capabilities)+len(supported))
	for key, value := range capabilities {
		out[key] = value
	}
	for operation, ok := range supported {
		out["optional."+operation] = strconv.FormatBool(ok)
	}
	return out
}

// UnsupportedOperationReason returns the canonical message for optional
// provider operations that are present in the gRPC interface but not supported
// by a specific provider implementation.
func UnsupportedOperationReason(provider, detail string) string {
	if detail == "" {
		return fmt.Sprintf("operation not supported by %s provider", provider)
	}
	return fmt.Sprintf("operation not supported by %s provider: %s", provider, detail)
}

func UnsupportedGenerateScanners(reason string) *pb.GenerateScannersResponse {
	return &pb.GenerateScannersResponse{
		GeneratedCount: 0,
		Errors:         []string{reason},
	}
}

func UnsupportedConfigureDiscovery(reason string) *pb.ConfigureDiscoveryResponse {
	return &pb.ConfigureDiscoveryResponse{
		Success: false,
		Error:   reason,
	}
}

func UnsupportedAnalysis(reason string) *pb.AnalysisResponse {
	return &pb.AnalysisResponse{
		Success: false,
		Error:   reason,
	}
}

func UnsupportedGenerate(reason string) *pb.GenerateResponse {
	return &pb.GenerateResponse{
		Success: false,
		Error:   reason,
	}
}
