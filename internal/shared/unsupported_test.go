package shared

import "testing"

func TestUnsupportedOperationReason(t *testing.T) {
	reason := UnsupportedOperationReason("github", "handwritten scanner")
	if reason != "operation not supported by github provider: handwritten scanner" {
		t.Fatalf("unexpected reason: %q", reason)
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
