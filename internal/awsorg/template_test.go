package awsorg

import (
	"strings"
	"testing"
)

func TestRenderRequiresOU(t *testing.T) {
	p := DefaultParams()
	if _, err := Render(p); err == nil {
		t.Fatal("expected error when no OrganizationalUnitIDs are set")
	}
}

func TestRenderRequiresRoleName(t *testing.T) {
	p := DefaultParams()
	p.RoleName = ""
	p.OrganizationalUnitIDs = []string{"r-1234"}
	if _, err := Render(p); err == nil {
		t.Fatal("expected error when RoleName is empty")
	}
}

func TestRenderHappyPath(t *testing.T) {
	p := DefaultParams()
	p.OrganizationalUnitIDs = []string{"r-abcd"}
	out, err := Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"AWSTemplateFormatVersion: '2010-09-09'",
		"AWS::CloudFormation::StackSet",
		"PermissionModel: SERVICE_MANAGED",
		"Enabled: true",
		"- r-abcd",
		"- us-east-1",
		"ParameterValue: CorkscrewScanRole",
		"arn:aws:iam::aws:policy/ReadOnlyAccess",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered template missing %q", want)
		}
	}
}

func TestRenderAutoDeployDisabled(t *testing.T) {
	p := DefaultParams()
	p.AutoDeployToNewAccounts = false
	p.OrganizationalUnitIDs = []string{"ou-1111-aaaa", "ou-2222-bbbb"}
	out, err := Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "Enabled: false") {
		t.Error("expected AutoDeployment Enabled: false")
	}
	if !strings.Contains(out, "- ou-1111-aaaa") || !strings.Contains(out, "- ou-2222-bbbb") {
		t.Error("expected both OU IDs in template")
	}
}

func TestRenderWithExternalIDAndTrustedPrincipal(t *testing.T) {
	p := DefaultParams()
	p.OrganizationalUnitIDs = []string{"r-9999"}
	p.TrustedPrincipalArn = "arn:aws:iam::123456789012:role/scan-runner"
	p.ExternalID = "secret-xyz"
	out, err := Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "ParameterValue: 'arn:aws:iam::123456789012:role/scan-runner'") {
		t.Error("TrustedPrincipalArn not propagated")
	}
	if !strings.Contains(out, "ParameterValue: 'secret-xyz'") {
		t.Error("ExternalID not propagated")
	}
}
