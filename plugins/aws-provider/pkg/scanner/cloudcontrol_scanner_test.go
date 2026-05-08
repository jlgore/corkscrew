package scanner

import "testing"

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
