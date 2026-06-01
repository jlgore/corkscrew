package cloudflareauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// In-memory store for testing.
type memStore struct {
	data map[string]*OAuthProfile
}

func newMemStore() *memStore {
	return &memStore{data: make(map[string]*OAuthProfile)}
}

func (m *memStore) Load(profile string) (*OAuthProfile, error) {
	p, ok := m.data[normalizeProfile(profile)]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

func (m *memStore) Save(profile *OAuthProfile) error {
	m.data[normalizeProfile(profile.Profile)] = profile
	return nil
}

func (m *memStore) Delete(profile string) error {
	delete(m.data, normalizeProfile(profile))
	return nil
}

func TestResolveOAuthSuccess(t *testing.T) {
	store := newMemStore()
	store.Save(&OAuthProfile{
		Profile:     "default",
		AccessToken: "tok",
		Scopes:      []string{"zone:read"},
	})

	resolver := &DefaultAuthResolver{Store: store}
	auth, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodOAuth, Profile: "default"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.Method != AuthMethodOAuth {
		t.Fatalf("method = %q, want oauth", auth.Method)
	}
	if auth.AccessToken != "tok" {
		t.Fatalf("token = %q, want tok", auth.AccessToken)
	}
}

func TestResolveOAuthExpiredFallsBackToAPIToken(t *testing.T) {
	store := newMemStore()
	store.Save(&OAuthProfile{
		Profile:     "default",
		AccessToken: "bad",
		Expiry:      time.Now().Add(-time.Hour),
	})

	t.Setenv("CLOUDFLARE_API_TOKEN", "fallback-token")

	resolver := &DefaultAuthResolver{Store: store, AllowFallback: true}
	auth, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodOAuth, Profile: "default"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.Method != AuthMethodAPIToken {
		t.Fatalf("expected fallback to api_token, got %s", auth.Method)
	}
	if auth.AccessToken != "fallback-token" {
		t.Fatalf("token = %q, want fallback-token", auth.AccessToken)
	}
}

func TestResolveOAuthExpiredNoFallback(t *testing.T) {
	store := newMemStore()
	store.Save(&OAuthProfile{
		Profile:     "default",
		AccessToken: "bad",
		Expiry:      time.Now().Add(-time.Hour),
	})

	resolver := &DefaultAuthResolver{Store: store, AllowFallback: false}
	_, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodOAuth, Profile: "default"}},
	})
	if err == nil {
		t.Fatal("expected error when oauth expired and fallback disabled")
	}
}

func TestResolveAutoPrefersOAuthThenAPIToken(t *testing.T) {
	store := newMemStore()
	store.Save(&OAuthProfile{
		Profile:     "default",
		AccessToken: "oauth-tok",
	})

	// API token env is set but OAuth profile exists.
	t.Setenv("CLOUDFLARE_API_TOKEN", "api-tok")

	resolver := &DefaultAuthResolver{Store: store}
	auth, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: ""}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.Method != AuthMethodOAuth {
		t.Fatalf("expected oauth, got %s", auth.Method)
	}
}

func TestResolveAutoFallsBackToAPIKey(t *testing.T) {
	store := newMemStore()
	t.Setenv("CLOUDFLARE_API_KEY", "key")
	t.Setenv("CLOUDFLARE_EMAIL", "me@example.com")

	resolver := &DefaultAuthResolver{Store: store}
	auth, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: ""}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.Method != AuthMethodAPIKey {
		t.Fatalf("expected api_key, got %s", auth.Method)
	}
}

func TestResolveAutoFailsWhenNothingConfigured(t *testing.T) {
	store := newMemStore()
	resolver := &DefaultAuthResolver{Store: store}
	_, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: ""}},
	})
	if err == nil {
		t.Fatal("expected error when no credentials configured")
	}
}

func TestResolveValidationFailsForBadToken(t *testing.T) {
	resolver := &DefaultAuthResolver{
		Store:    newMemStore(),
		Validate: true,
		Validator: &badValidator{},
	}
	// No credentials anywhere
	_, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodAPIToken, Token: "bogus"}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestResolveValidationSucceeds(t *testing.T) {
	resolver := &DefaultAuthResolver{
		Store:     newMemStore(),
		Validate:  true,
		Validator: &goodValidator{},
	}
	auth, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodAPIToken, Token: "good"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.Method != AuthMethodAPIToken {
		t.Fatalf("expected api_token, got %s", auth.Method)
	}
}

func TestResolveOAuthRefreshOnExpiry(t *testing.T) {
	store := newMemStore()
	store.Save(&OAuthProfile{
		Profile:      "default",
		AccessToken:  "old-tok",
		RefreshToken: "refresh-tok",
		Expiry:       time.Now().Add(-time.Hour),
	})

	refresher := &fakeRefresher{token: "new-tok"}
	resolver := &DefaultAuthResolver{
		Store:     store,
		Refresher: refresher,
	}

	_, err := resolver.Resolve(context.Background(), ResolveAuthRequest{
		Config: &CloudflareConfig{Auth: AuthConfig{Method: AuthMethodOAuth, Profile: "default", UseRefreshToken: true}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !refresher.called {
		t.Fatal("expected refresher to be called")
	}
	// Verify the store received the updated token
	updated, _ := store.Load("default")
	if updated.AccessToken != "new-tok" {
		t.Fatalf("expected updated token new-tok, got %s", updated.AccessToken)
	}
}

func TestOAuthProfileExpired(t *testing.T) {
	if (&OAuthProfile{Expiry: time.Now().Add(-time.Hour)}).Expired() != true {
		t.Fatal("expected expired profile")
	}
	if (&OAuthProfile{Expiry: time.Now().Add(time.Hour)}).Expired() != false {
		t.Fatal("expected non-expired profile")
	}
	if (&OAuthProfile{Expiry: time.Time{}}).Expired() != false {
		t.Fatal("zero expiry should not be expired")
	}
	if ((*OAuthProfile)(nil)).Expired() != true {
		t.Fatal("nil profile should be expired")
	}
}

func TestOAuthProfileTimeUntilExpiry(t *testing.T) {
	p := &OAuthProfile{Expiry: time.Now().Add(5 * time.Minute)}
	if p.TimeUntilExpiry() <= 0 {
		t.Fatal("expected positive time until expiry")
	}
	if (&OAuthProfile{Expiry: time.Time{}}).TimeUntilExpiry() != 0 {
		t.Fatal("zero expiry should return 0")
	}
	if (*OAuthProfile)(nil).TimeUntilExpiry() != 0 {
		t.Fatal("nil profile should return 0")
	}
}

type badValidator struct{}

func (b *badValidator) ValidateToken(_ context.Context, _ *ResolvedAuth) (*ValidationResult, error) {
	return &ValidationResult{Valid: false, Errors: []string{"bad token"}}, nil
}

type goodValidator struct{}

func (g *goodValidator) ValidateToken(_ context.Context, _ *ResolvedAuth) (*ValidationResult, error) {
	return &ValidationResult{Valid: true}, nil
}

type fakeRefresher struct {
	token string
	called bool
}

func (f *fakeRefresher) RefreshToken(_ context.Context, profile *OAuthProfile) (*OAuthProfile, error) {
	f.called = true
	return &OAuthProfile{
		Profile:     profile.Profile,
		AccessToken: f.token,
		Expiry:      time.Now().Add(time.Hour),
	}, nil
}
