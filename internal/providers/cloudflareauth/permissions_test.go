package cloudflareauth

import "testing"

func TestPlanDefaultsToAccountsAndZones(t *testing.T) {
	planner := &StaticPermissionPlanner{}
	plan, err := planner.Plan(nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Services) != 2 || plan.Services[0] != "accounts" || plan.Services[1] != "zones" {
		t.Fatalf("plan.Services = %#v, want accounts/zones", plan.Services)
	}
	if len(plan.Bundles) != 1 || plan.Bundles[0].Name != "core_read" {
		t.Fatalf("plan.Bundles = %#v, want core_read", plan.Bundles)
	}
}

func TestPlanCombinesBundlesAndScopes(t *testing.T) {
	planner := &StaticPermissionPlanner{}
	plan, err := planner.Plan([]string{"dns", "workers", "storage"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	wantScopes := map[string]bool{
		"account:read":        true,
		"dns:read":            true,
		"workers:read":        true,
		"workers.kv:read":     true,
		"workers.queues:read": true,
		"workers.r2:read":     true,
		"zone:read":           true,
	}
	for _, scope := range plan.Scopes {
		delete(wantScopes, scope)
	}
	if len(wantScopes) != 0 {
		t.Fatalf("missing expected scopes: %#v; got %#v", wantScopes, plan.Scopes)
	}
}

func TestPlanRejectsUnknownService(t *testing.T) {
	planner := &StaticPermissionPlanner{}
	if _, err := planner.Plan([]string{"not-real"}); err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestBundleByNameFullReadonly(t *testing.T) {
	bundle, ok := BundleByName("full_readonly")
	if !ok {
		t.Fatal("expected full_readonly bundle")
	}
	if len(bundle.Services) != len(CanonicalServices) {
		t.Fatalf("len(bundle.Services) = %d, want %d", len(bundle.Services), len(CanonicalServices))
	}
	if len(bundle.Scopes) == 0 {
		t.Fatal("expected full_readonly scopes to be populated")
	}
}

func TestVerifyResolvedAuthOAuthStrict(t *testing.T) {
	planner := &StaticPermissionPlanner{}
	plan, err := planner.Plan([]string{"dns", "workers"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	result := VerifyResolvedAuth(plan, &ResolvedAuth{
		Method: AuthMethodOAuth,
		Scopes: []string{"account:read", "dns:read", "zone:read"},
	})
	if !result.ScopeCheckStrict {
		t.Fatal("expected strict scope check for oauth")
	}
	if len(result.MissingScopes) != 1 || result.MissingScopes[0] != "workers:read" {
		t.Fatalf("result.MissingScopes = %#v, want workers:read", result.MissingScopes)
	}
}

func TestVerifyResolvedAuthTokenNonStrict(t *testing.T) {
	planner := &StaticPermissionPlanner{}
	plan, err := planner.Plan([]string{"dns"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	result := VerifyResolvedAuth(plan, &ResolvedAuth{Method: AuthMethodAPIToken})
	if result.ScopeCheckStrict {
		t.Fatal("expected non-strict scope check for api token")
	}
	if len(result.MissingScopes) != 0 {
		t.Fatalf("result.MissingScopes = %#v, want none", result.MissingScopes)
	}
}
