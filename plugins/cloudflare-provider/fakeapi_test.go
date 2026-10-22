package main

import (
	"testing"

	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
)

// Baseline fixture IDs, from testdata/fakeapi/baseline.json.
const (
	fixtureAccountID = "9a7806061c88ada191ed06f989cc3dac"
	fixtureZoneID    = "023e105f4ecef8ad9ca31a8372d0c353"
)

// newFakeAPI starts a fake Cloudflare API seeded with the baseline account.
func newFakeAPI(t *testing.T) *fakecf.Server {
	t.Helper()
	srv := fakecf.New(t)
	srv.Load("testdata/fakeapi/baseline.json")
	return srv
}

// newTestProvider returns an initialized provider whose client talks to srv, configured the
// way a real scan configures it apart from auth.
func newTestProvider(t *testing.T, srv *fakecf.Server) *CloudflareProvider {
	t.Helper()
	return &CloudflareProvider{
		initialized: true,
		config:      &cloudflareauth.CloudflareConfig{},
		planner:     &cloudflareauth.StaticPermissionPlanner{},
		client:      newCloudflareClient(testAuth(srv.URL())),
	}
}
