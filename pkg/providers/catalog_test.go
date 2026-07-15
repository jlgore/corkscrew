package providers

import "testing"

func TestCatalogIncludesEveryShippedProvider(t *testing.T) {
	want := []string{"aws", "azure", "gcp", "kubernetes", "github", "cloudflare"}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
		provider, ok := Lookup(want[i])
		if !ok || provider.Description == "" || provider.ResourceTable == "" {
			t.Fatalf("Lookup(%q) = %#v, %v", want[i], provider, ok)
		}
		byTable, ok := LookupByResourceTable(provider.ResourceTable)
		if !ok || byTable.Name != provider.Name {
			t.Fatalf("LookupByResourceTable(%q) = %#v, %v", provider.ResourceTable, byTable, ok)
		}
	}
	if !IsRegisteredResourceTable(CustomResourceTable) || IsRegisteredResourceTable("arbitrary_resources") {
		t.Fatal("registered resource table classification is incorrect")
	}
}

func TestCatalogResultsCannotMutateCatalog(t *testing.T) {
	names := Names()
	names[0] = "changed"
	if got := Names()[0]; got != "aws" {
		t.Fatalf("catalog mutated through Names: %q", got)
	}

	providers := Shipped()
	providers[0].Name = "changed"
	if got := Shipped()[0].Name; got != "aws" {
		t.Fatalf("catalog mutated through Shipped: %q", got)
	}
}
