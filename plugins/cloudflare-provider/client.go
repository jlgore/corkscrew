package main

import (
	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
)

func newCloudflareClient(auth *cloudflareauth.ResolvedAuth) *cloudflare.Client {
	var opts []option.RequestOption
	if auth.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(auth.BaseURL))
	}

	switch auth.Method {
	case cloudflareauth.AuthMethodOAuth, cloudflareauth.AuthMethodAPIToken:
		opts = append(opts, option.WithAPIToken(auth.AccessToken))
	case cloudflareauth.AuthMethodAPIKey:
		opts = append(opts, option.WithAPIKey(auth.APIKey), option.WithAPIEmail(auth.Email))
	}

	return cloudflare.NewClient(opts...)
}
