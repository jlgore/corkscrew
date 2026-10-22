package orgscan

import (
	"context"
	"strings"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

// An account ID comes from the Organizations API, so a blank one is cloud data and must be
// an error; otherwise it would reach accountScanner's assertion.
func TestTargetAccountsRejectsAccountWithoutID(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = fakeOrg{accounts: []string{"111111111111", ""}}
	_, err := o.targetAccounts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "without an ID") {
		t.Fatalf("err = %v, want an error for the account without an ID", err)
	}
}

func TestAccountScannerAssertsInvariants(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	asserttest.FailsWith(t, "account ID", func() {
		_, _ = o.accountScanner(context.Background(), "")
	})

	o.scannerFor = func(context.Context, string) (accountScanner, error) { return nil, nil }
	asserttest.FailsWith(t, "scanner or an error", func() {
		_, _ = o.accountScanner(context.Background(), "111111111111")
	})

	asserttest.FailsWith(t, "listed accounts", func() { o.retainAccounts(nil) })
}

// The cache only holds listed accounts, so a cache larger than the listing is a bug.
func TestRetainAccountsAssertsCacheWithinListing(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.scanners["111111111111"] = fakeAccount{id: "111111111111"}
	o.scanners["222222222222"] = fakeAccount{id: "222222222222"}
	o.accountsListed = map[string]bool{"111111111111": true}
	o.scannerFor = func(_ context.Context, acctID string) (accountScanner, error) {
		return fakeAccount{id: acctID}, nil
	}
	asserttest.FailsWith(t, "cached scanners", func() {
		_, _ = o.accountScanner(context.Background(), "333333333333")
	})
}
