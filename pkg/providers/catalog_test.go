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
		if !ok || provider.ResourceTable == "" {
			t.Fatalf("Lookup(%q) = %#v, %v", want[i], provider, ok)
		}
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
