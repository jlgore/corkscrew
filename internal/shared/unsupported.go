package shared

import (
	"fmt"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

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
