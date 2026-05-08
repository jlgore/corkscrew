package orgscan

import (
	"reflect"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func awsCfgZero(_ *testing.T) aws.Config { return aws.Config{Region: "us-east-1"} }

func TestStringSetIgnoresBlanks(t *testing.T) {
	got := stringSet([]string{"111111111111", "  ", "", " 222222222222 "})
	want := map[string]bool{"111111111111": true, "222222222222": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stringSet = %v, want %v", got, want)
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.RoleName != "CorkscrewScanRole" {
		t.Errorf("default RoleName = %q", c.RoleName)
	}
	if c.MaxConcurrentAccounts != 5 {
		t.Errorf("default MaxConcurrentAccounts = %d", c.MaxConcurrentAccounts)
	}
	if c.SessionName == "" {
		t.Errorf("default SessionName must be non-empty")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	if o.options.RoleName != "CorkscrewScanRole" {
		t.Errorf("RoleName default not applied: %q", o.options.RoleName)
	}
	if o.options.MaxConcurrentAccounts != 5 {
		t.Errorf("MaxConcurrentAccounts default not applied: %d", o.options.MaxConcurrentAccounts)
	}
}

func TestUnsupportedTypesUnion(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.mergeUnsupported(map[string]string{"AWS::Foo::Bar": "missing"})
	o.mergeUnsupported(map[string]string{"AWS::Baz::Qux": "missing"})
	got := o.UnsupportedTypes()
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"AWS::Baz::Qux", "AWS::Foo::Bar"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("UnsupportedTypes keys = %v, want %v", keys, want)
	}
}
