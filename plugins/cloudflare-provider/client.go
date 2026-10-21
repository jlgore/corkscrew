package main

import (
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
)

// The SDK defaults are no per-attempt timeout and 10 retries with backoff up to 30s, which can
// spend the host's whole 5-minute scope deadline on one call and report only a scope-wide
// timeout. Set both explicitly so a slow or failing call surfaces as that listing's error.
const (
	// requestTimeout bounds one attempt. The SDK does not retry an attempt that times out, so
	// with the backoff below a failing call ends within about 75s, well inside the scope.
	requestTimeout = 60 * time.Second
	// retriesMax keeps the SDK's 2s, 4s, 8s backoff for transient 429/5xx but not its tail of
	// 30s waits. A Retry-After from Cloudflare is still honored and still stops at the scope
	// context, which the SDK selects on while waiting.
	retriesMax = 3
)

func newCloudflareClient(auth *cloudflareauth.ResolvedAuth) *cloudflare.Client {
	return cloudflare.NewClient(clientOptions(auth, requestTimeout)...)
}

// clientOptions takes the attempt timeout as a parameter so tests can exercise it in
// milliseconds rather than a minute.
func clientOptions(
	auth *cloudflareauth.ResolvedAuth, timeout time.Duration,
) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithRequestTimeout(timeout),
		option.WithMaxRetries(retriesMax),
	}
	if auth.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(auth.BaseURL))
	}

	switch auth.Method {
	case cloudflareauth.AuthMethodOAuth, cloudflareauth.AuthMethodAPIToken:
		opts = append(opts, option.WithAPIToken(auth.AccessToken))
	case cloudflareauth.AuthMethodAPIKey:
		opts = append(opts, option.WithAPIKey(auth.APIKey), option.WithAPIEmail(auth.Email))
	}
	return opts
}
