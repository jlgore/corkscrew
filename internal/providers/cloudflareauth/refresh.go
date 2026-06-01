package cloudflareauth

import (
	"context"
	"time"
)

// TokenRefresher attempts to refresh an expired OAuth token. Implementations may
// invoke Cloudflare's token endpoint or a custom identity provider.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, profile *OAuthProfile) (*OAuthProfile, error)
}

// NoopTokenRefresher never refreshes; it always returns the original profile.
// Use this when you do not have a refresh endpoint configured.
type NoopTokenRefresher struct{}

func (n *NoopTokenRefresher) RefreshToken(_ context.Context, profile *OAuthProfile) (*OAuthProfile, error) {
	return profile, nil
}

// Expired returns true if the profile expiry is non-zero and in the past.
// A zero Expiry means "never expires" so it is never considered expired.
func (p *OAuthProfile) Expired() bool {
	if p == nil {
		return true
	}
	return !p.Expiry.IsZero() && time.Now().After(p.Expiry)
}

// TimeUntilExpiry returns the remaining lifetime or a zero duration if the token
// never expires or is already expired.
func (p *OAuthProfile) TimeUntilExpiry() time.Duration {
	if p == nil || p.Expiry.IsZero() || p.Expired() {
		return 0
	}
	return time.Until(p.Expiry)
}
