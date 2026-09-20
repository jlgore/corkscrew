package correlation

import "testing"

func TestInferNetworkEvidenceAndAttach(t *testing.T) {
	evidence := InferNetworkEvidence(`{"publicIpAddress":"203.0.113.9","dnsHostname":"api.example.test","password":"not-an-ip"}`)
	if len(evidence) != 2 {
		t.Fatalf("evidence = %#v", evidence)
	}
	attributes, err := Attach(`{"provider":"fixture"}`, evidence...)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := ParseAttributes(attributes)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Version != Version || len(envelope.Evidence) != 2 {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestSecretEvidenceRejectsPayload(t *testing.T) {
	err := (Evidence{ID: "secret", Kind: KindSecret, Values: map[string]any{"secret_value": "plaintext"}}).Validate()
	if err == nil {
		t.Fatal("secret payload was accepted")
	}
}
