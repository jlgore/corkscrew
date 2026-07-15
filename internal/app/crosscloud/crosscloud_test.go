package crosscloud

import (
	"io"
	"reflect"
	"testing"
)

func TestCorrelationKindNormalizesAliases(t *testing.T) {
	cases := map[string]string{
		"":               "all",
		"all":            "all",
		"ips":            "ip",
		"topology":       "network",
		"lb":             "load-balancer",
		"vpn":            "connectivity",
		"peering":        "connectivity",
		"security-group": "security",
		"certificates":   "domain",
		"federation":     "identity",
	}
	for input, want := range cases {
		got, err := CorrelationKind(input)
		if err != nil {
			t.Fatalf("CorrelationKind(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("CorrelationKind(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := CorrelationKind("bogus"); err == nil {
		t.Fatal("CorrelationKind(bogus) should error")
	}
}

func TestParseCorrelationTypesDedupsAndDropsUnknown(t *testing.T) {
	got := ParseCorrelationTypes("ip,ips,bogus,dns")
	want := []string{"ip", "dns"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCorrelationTypes = %v, want %v", got, want)
	}

	if got := ParseCorrelationTypes(""); !reflect.DeepEqual(got, AllCorrelationKinds()) {
		t.Fatalf("empty types = %v, want all kinds", got)
	}
	if got := ParseCorrelationTypes("all"); !reflect.DeepEqual(got, AllCorrelationKinds()) {
		t.Fatalf("all types = %v, want all kinds", got)
	}
}

func TestCorrelateRejectsUnknownKindBeforeRunning(t *testing.T) {
	err := Correlate(Request{DBPath: "ignored.db", Kinds: []string{"bogus"}}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("Correlate with unknown kind should error before invoking the graph runner")
	}
}
