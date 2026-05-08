package scanner

import (
	"fmt"
	"testing"
)

func TestParseCloudControlProperties(t *testing.T) {
	props, attrs := parseCloudControlProperties(`{
		"Arn": "arn:aws:s3:::example",
		"VersioningEnabled": true,
		"Count": 3,
		"Tags": [
			{"Key": "Env", "Value": "prod"},
			{"Key": "Owner", "Value": "team-a"}
		]
	}`)

	if props == nil {
		t.Fatal("expected non-nil props")
	}
	if attrs["Arn"] != "arn:aws:s3:::example" {
		t.Errorf("Arn missing: %v", attrs)
	}
	if attrs["VersioningEnabled"] != "true" {
		t.Errorf("bool attr wrong: %v", attrs)
	}
	if attrs["Count"] != "3" {
		t.Errorf("number attr wrong: %v", attrs)
	}

	tags := extractTagsFromProperties(props)
	if tags["Env"] != "prod" || tags["Owner"] != "team-a" {
		t.Errorf("tag list extraction failed: %v", tags)
	}
}

func TestExtractTagsMapShape(t *testing.T) {
	props, _ := parseCloudControlProperties(`{
		"Tags": {"Env": "stg", "Owner": "team-b"}
	}`)
	tags := extractTagsFromProperties(props)
	if tags["Env"] != "stg" || tags["Owner"] != "team-b" {
		t.Errorf("tag map extraction failed: %v", tags)
	}
}

func TestParseCloudControlEmpty(t *testing.T) {
	props, attrs := parseCloudControlProperties("")
	if props != nil || attrs != nil {
		t.Errorf("empty input should return nil/nil")
	}
}

func TestReFormatKey(t *testing.T) {
	cases := map[string]string{
		"AWS::S3::Bucket":                           "s3:bucket",
		"AWS::EC2::Instance":                        "ec2:instance",
		"AWS::ElasticLoadBalancingV2::LoadBalancer": "elasticloadbalancingv2:loadbalancer",
		"":                                          "",
		"AWS::S3":                                   "",
		"NotAWS::S3::Bucket":                        "",
	}
	for in, want := range cases {
		if got := reFormatKey(in); got != want {
			t.Errorf("reFormatKey(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestServiceFromCFNType(t *testing.T) {
	cases := map[string]string{
		"AWS::S3::Bucket":                            "s3",
		"AWS::EC2::Instance":                         "ec2",
		"AWS::IAM::Role":                             "iam",
		"AWS::ElasticLoadBalancingV2::LoadBalancer":  "elasticloadbalancing",
		"AWS::CloudFormation::Stack":                 "cloudformation",
		"":                                           "",
		"NotAWS::S3::Bucket":                         "",
		"AWS::":                                      "",
		"AWS::S3":                                    "",
	}
	for in, want := range cases {
		if got := serviceFromCFNType(in); got != want {
			t.Errorf("serviceFromCFNType(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestIsUnsupportedTypeErr(t *testing.T) {
	cases := map[string]bool{
		// Real strings observed in CloudControl error responses.
		"TypeNotFoundException: foo":                       true,
		"UnsupportedActionException: bar":                  true,
		"resource is not currently supported by ":          true,
		"GeneralServiceException: AWS::S3::AccessGrant Handler returned status FAILED: Access Grants Instance does not exist": true,
		"HandlerInternalFailureException: foo":             true,
		"HandlerErrorCode: InternalFailure":                true,
		"AccessDenied: User is not authorized":             true,
		"User: arn:aws:iam::1:user/x is not authorized to perform: cloudcontrol:ListResources": true,

		// Should NOT match (transient or unrelated):
		"Throttling: Rate exceeded":              false,
		"connection reset by peer":               false,
		"context deadline exceeded":              false,
		"":                                       false,
	}
	for msg, want := range cases {
		var err error
		if msg != "" {
			err = fmt.Errorf("%s", msg)
		}
		if got := isUnsupportedTypeErr(err); got != want {
			t.Errorf("isUnsupportedTypeErr(%q) = %v; want %v", msg, got, want)
		}
	}
}

func TestSupportedServicesNonEmpty(t *testing.T) {
	s := &CloudControlScanner{}
	got := s.SupportedServices()
	if len(got) == 0 {
		t.Fatal("SupportedServices returned empty list")
	}
	// Spot-check a couple of entries we expect.
	want := map[string]bool{"s3": false, "ec2": false, "iam": false}
	for _, svc := range got {
		if _, ok := want[svc]; ok {
			want[svc] = true
		}
	}
	for svc, found := range want {
		if !found {
			t.Errorf("expected %q in SupportedServices", svc)
		}
	}
}
