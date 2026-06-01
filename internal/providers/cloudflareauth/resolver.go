package cloudflareauth

import (
	"context"
	"fmt"
	"strings"
)

type ResolveAuthRequest struct {
	Config   *CloudflareConfig
	Services []string
}

type AuthResolver interface {
	Resolve(context.Context, ResolveAuthRequest) (*ResolvedAuth, error)
}

// DefaultAuthResolver resolves Cloudflare credentials using a configurable chain.
//
// The resolution order depends on cfg.Auth.Method and AllowFallback:
//   - AuthMethodOAuth: try OAuth profile, optionally refresh on expiry, then fall
//     back to API token and API key if AllowFallback is true.
//   - AuthMethodAPIToken: try API token, then fall back to API key.
//   - AuthMethodAPIKey: try API key only.
//
// When Validate is true the resolver pings a Cloudflare endpoint to verify the
// resolved credentials before returning them.
type DefaultAuthResolver struct {
	Planner  PermissionPlanner
	Store    OAuthStore
	Refresher TokenRefresher
	Validator TokenValidator

	AllowFallback bool // if true, attempt next auth method on failure
	Validate      bool // if true, validate credentials against the API
}

func (r *DefaultAuthResolver) Resolve(ctx context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	if req.Config == nil {
		return nil, fmt.Errorf("missing Cloudflare config")
	}
	if r.Planner == nil {
		r.Planner = &StaticPermissionPlanner{}
	}
	if r.Store == nil {
		r.Store = &FileOAuthStore{}
	}
	if r.Refresher == nil {
		r.Refresher = &NoopTokenRefresher{}
	}
	if r.Validator == nil {
		r.Validator = &NoopTokenValidator{}
	}

	auth, err := r.resolveChained(ctx, req)
	if err != nil {
		return nil, err
	}

	if r.Validate {
		result, vErr := r.Validator.ValidateToken(ctx, auth)
		if vErr != nil {
			return nil, fmt.Errorf("validate %s auth: %w", auth.Method, vErr)
		}
		if !result.Valid {
			msg := strings.Join(result.Errors, "; ")
			if msg == "" {
				msg = "credentials rejected by Cloudflare API"
			}
			return nil, fmt.Errorf("invalid %s auth: %s", auth.Method, msg)
		}
	}

	return auth, nil
}

func (r *DefaultAuthResolver) resolveChained(ctx context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	switch req.Config.Auth.Method {
	case AuthMethodOAuth:
		auth, err := r.resolveOAuth(ctx, req)
		if err == nil {
			return auth, nil
		}
		if !r.AllowFallback {
			return nil, err
		}
		// Fall through to API token
		if tokenAuth, tokenErr := r.resolveAPIToken(ctx, req); tokenErr == nil {
			return tokenAuth, nil
		}
		// Fall through to API key
		if keyAuth, keyErr := r.resolveAPIKey(ctx, req); keyErr == nil {
			return keyAuth, nil
		}
		return nil, fmt.Errorf("oauth failed: %w", err)

	case AuthMethodAPIToken:
		auth, err := r.resolveAPIToken(ctx, req)
		if err == nil {
			return auth, nil
		}
		if !r.AllowFallback {
			return nil, err
		}
		if keyAuth, keyErr := r.resolveAPIKey(ctx, req); keyErr == nil {
			return keyAuth, nil
		}
		return nil, err

	case AuthMethodAPIKey:
		return r.resolveAPIKey(ctx, req)

	case "":
		return r.resolveAuto(ctx, req)

	default:
		return nil, fmt.Errorf("unsupported auth method %q", req.Config.Auth.Method)
	}
}

// resolveAuto tries to find any valid credential starting with API token, then API key.
func (r *DefaultAuthResolver) resolveAuto(ctx context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	// Try OAuth first if a profile exists
	if auth, err := r.resolveOAuth(ctx, req); err == nil {
		return auth, nil
	}
	if auth, err := r.resolveAPIToken(ctx, req); err == nil {
		return auth, nil
	}
	if auth, err := r.resolveAPIKey(ctx, req); err == nil {
		return auth, nil
	}
	return nil, fmt.Errorf("no Cloudflare credentials found; set CLOUDFLARE_API_TOKEN, configure API_KEY+EMAIL, or set up an OAuth profile")
}

func (r *DefaultAuthResolver) resolveOAuth(ctx context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	profileName := normalizeProfile(req.Config.Auth.Profile)
	profile, err := r.Store.Load(profileName)
	if err != nil {
		return nil, fmt.Errorf("load oauth profile %q: %w", profileName, err)
	}

	if profile.Expired() {
		// Attempt refresh if configured
		if req.Config.Auth.UseRefreshToken && profile.RefreshToken != "" {
			refreshed, refreshErr := r.Refresher.RefreshToken(ctx, profile)
			if refreshErr != nil {
				return nil, fmt.Errorf("oauth token for profile %q expired and refresh failed: %w", profileName, refreshErr)
			}
			// Persist refreshed tokens
			if saveErr := r.Store.Save(refreshed); saveErr != nil {
				// Non-fatal: return the refreshed tokens anyway.
				_ = saveErr
			}
			profile = refreshed
		} else {
			return nil, fmt.Errorf("oauth token for profile %q has expired", profileName)
		}
	}

	return &ResolvedAuth{
		Method:       AuthMethodOAuth,
		AccessToken:  profile.AccessToken,
		Scopes:       append([]string(nil), profile.Scopes...),
		AccountHints: append([]string(nil), profile.AccountHints...),
		BaseURL:      firstNonEmpty(req.Config.Auth.BaseURL, profile.BaseURL),
		Source:       "oauth_profile:" + profileName,
	}, nil
}
