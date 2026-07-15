package scan

import (
	"reflect"
	"testing"
)

func TestPrepareExpandsAndNormalizesScanRequest(t *testing.T) {
	environment := map[string]string{
		"CORKSCREW_QUACK_URL":   " quack:db.example:9494 ",
		"CORKSCREW_QUACK_TOKEN": " environment-token ",
	}
	options, expansions := Prepare(Request{
		Provider:                " acme ",
		Services:                " common, s3, custom, ,lambda ",
		Regions:                 " us-east-1, , us-west-2 ",
		OutputFormat:            "json",
		ConfigPath:              " config.yaml ",
		MaxConcurrency:          7,
		DBProviderTableOverride: " custom_resources ",
		Namespace:               " default ",
		LabelSelector:           " app=api ",
		FieldSelector:           " status.phase=Running ",
		KubeconfigPath:          " /tmp/kubeconfig ",
		KubeContext:             " cluster-a ",
		IncludeRelationships:    true,
	}, func(key string) string { return environment[key] })

	if options.ProviderName != "acme" {
		t.Fatalf("provider = %q, want custom provider acme", options.ProviderName)
	}
	if want := []string{"s3", "ec2", "lambda", "rds", "iam", "custom"}; !reflect.DeepEqual(options.ServiceList, want) {
		t.Fatalf("services = %v, want %v", options.ServiceList, want)
	}
	if want := []string{"us-east-1", "us-west-2"}; !reflect.DeepEqual(options.ScopeList, want) {
		t.Fatalf("regions = %v, want %v", options.ScopeList, want)
	}
	if options.DatabasePath != "quack:db.example:9494" || options.QuackToken != "environment-token" {
		t.Fatalf("remote database = %q token=%q", options.DatabasePath, options.QuackToken)
	}
	if options.DBProviderTableOverride != "custom_resources" || options.Namespace != "default" {
		t.Fatalf("normalized options = %#v", options)
	}
	if len(expansions) != 1 || expansions[0].Name != "common" {
		t.Fatalf("expansions = %#v, want common", expansions)
	}
}

func TestPrepareExplicitDatabaseCredentialsOverrideEnvironment(t *testing.T) {
	options, _ := Prepare(Request{
		Provider:     "aws",
		DatabasePath: " local.duckdb ",
		QuackToken:   " explicit-token ",
	}, func(string) string { return "environment-value" })

	if options.DatabasePath != "local.duckdb" || options.QuackToken != "explicit-token" {
		t.Fatalf("options = %#v", options)
	}
}

func TestPrepareReturnsStructuredServiceExpansions(t *testing.T) {
	prepared, expansions := Prepare(Request{Provider: "custom-provider", Services: "storage,s3", Regions: "global"}, nil)
	if want := []string{"s3", "ebs", "efs", "fsx", "backup"}; !reflect.DeepEqual(prepared.ServiceList, want) {
		t.Fatalf("services = %v, want %v", prepared.ServiceList, want)
	}
	if len(expansions) != 1 || expansions[0].Name != "storage" {
		t.Fatalf("expansions = %#v", expansions)
	}
}

func TestServiceGroupsReturnsDefensiveCopy(t *testing.T) {
	groups := ServiceGroups()
	groups["common"][0] = "changed"
	groups["new"] = []string{"changed"}

	fresh := ServiceGroups()
	if fresh["common"][0] != "s3" {
		t.Fatalf("service group mutated through returned map: %v", fresh["common"])
	}
	if _, exists := fresh["new"]; exists {
		t.Fatal("service group catalog accepted caller mutation")
	}
}
