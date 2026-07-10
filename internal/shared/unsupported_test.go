package shared

import "testing"

func TestUnsupportedOperationReason(t *testing.T) {
	reason := UnsupportedOperationReason("github", "handwritten scanner")
	if reason != "operation not supported by github provider: handwritten scanner" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestWithOptionalCapabilities(t *testing.T) {
	base := map[string]string{"scanning": "true"}

	capabilities := WithOptionalCapabilities(base, map[string]bool{
		OptionalGenerateServiceScanners: true,
		OptionalGenerateFromAnalysis:    false,
	})

	if capabilities["scanning"] != "true" {
		t.Fatalf("base capability was not preserved: %#v", capabilities)
	}
	if capabilities["optional.generate_service_scanners"] != "true" {
		t.Fatalf("expected generate_service_scanners=true, got %#v", capabilities)
	}
	if capabilities["optional.generate_from_analysis"] != "false" {
		t.Fatalf("expected generate_from_analysis=false, got %#v", capabilities)
	}
	if _, ok := base["optional.generate_service_scanners"]; ok {
		t.Fatalf("base map was mutated: %#v", base)
	}
}

func TestUnsupportedResponses(t *testing.T) {
	reason := UnsupportedOperationReason("test", "")

	if resp := UnsupportedGenerateScanners(reason); resp.GetGeneratedCount() != 0 || len(resp.GetErrors()) != 1 {
		t.Fatalf("unexpected generate scanners response: %#v", resp)
	}
	if resp := UnsupportedConfigureDiscovery(reason); resp.GetSuccess() || resp.GetError() != reason {
		t.Fatalf("unexpected configure response: %#v", resp)
	}
	if resp := UnsupportedAnalysis(reason); resp.GetSuccess() || resp.GetError() != reason {
		t.Fatalf("unexpected analysis response: %#v", resp)
	}
	if resp := UnsupportedGenerate(reason); resp.GetSuccess() || resp.GetError() != reason {
		t.Fatalf("unexpected generate response: %#v", resp)
	}
}
