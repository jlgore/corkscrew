package orgscan

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// countingOrg is a fakeOrg that counts ListAccounts calls. accounts can be swapped between
// scans to model an account leaving the organization.
type countingOrg struct {
	mu       sync.Mutex
	accounts []string
	calls    atomic.Int32
}

func (c *countingOrg) ListAccounts(
	ctx context.Context, in *organizations.ListAccountsInput, opts ...func(*organizations.Options),
) (*organizations.ListAccountsOutput, error) {
	c.calls.Add(1)
	c.mu.Lock()
	accounts := append([]string(nil), c.accounts...)
	c.mu.Unlock()
	return fakeOrg{accounts: accounts}.ListAccounts(ctx, in, opts...)
}

func (c *countingOrg) setAccounts(accounts []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts = accounts
}

// countingBuilder returns a scannerFor that counts how often each account's scanner is built.
// Building a real scanner is what costs an sts:AssumeRole, so the count stands in for it.
func countingBuilder() (func(context.Context, string) (accountScanner, error), *sync.Map) {
	var builds sync.Map
	build := func(_ context.Context, acctID string) (accountScanner, error) {
		counter, _ := builds.LoadOrStore(acctID, &atomic.Int32{})
		counter.(*atomic.Int32).Add(1)
		return fakeAccount{id: acctID}, nil
	}
	return build, &builds
}

func buildCount(builds *sync.Map, acctID string) int32 {
	counter, ok := builds.Load(acctID)
	if !ok {
		return 0
	}
	return counter.(*atomic.Int32).Load()
}

// A scan lists the organization's accounts once, however many services it scans, and the
// next scan lists them again so it sees accounts that joined or left in between.
func TestScanListsAccountsOncePerScan(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	org := &countingOrg{accounts: []string{"111111111111", "222222222222"}}
	o.orgAPI = org
	o.scannerFor, _ = countingBuilder()

	scan := o.ForScan()
	for _, service := range []string{"kms", "s3", "ec2"} {
		refs, err := scan.ScanService(context.Background(), service)
		if err != nil {
			t.Fatalf("ScanService(%s): %v", service, err)
		}
		if len(refs) != 2 {
			t.Fatalf("ScanService(%s) refs = %d, want one per account", service, len(refs))
		}
	}
	if got := org.calls.Load(); got != 1 {
		t.Fatalf("ListAccounts calls in one scan = %d, want 1", got)
	}

	if _, err := o.ForScan().ScanService(context.Background(), "kms"); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := org.calls.Load(); got != 2 {
		t.Fatalf("ListAccounts calls after two scans = %d, want 2", got)
	}
}

// A failed account listing is reported by every service of the scan, as it was when each
// service listed for itself, so each service is still marked incomplete.
func TestScanReportsListingFailureForEveryService(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = fakeOrg{} // no accounts: the listing fails its filter check
	o.scannerFor, _ = countingBuilder()

	scan := o.ForScan()
	for _, service := range []string{"kms", "s3"} {
		if _, err := scan.ScanService(context.Background(), service); err == nil {
			t.Fatalf("ScanService(%s) err = nil, want the listing failure", service)
		}
	}
}

// Every resource of an account used to assume its role again. One scanner per account must
// serve the account's scans and describes, across scans, while the account stays listed.
func TestAccountScannerBuiltOncePerAccount(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = &countingOrg{accounts: []string{"111111111111", "222222222222"}}
	var builds *sync.Map
	o.scannerFor, builds = countingBuilder()

	for scanIndex := 0; scanIndex < 2; scanIndex++ {
		scan := o.ForScan()
		refs, err := scan.ScanService(context.Background(), "kms")
		if err != nil {
			t.Fatalf("ScanService: %v", err)
		}
		for _, ref := range refs {
			for describe := 0; describe < 3; describe++ {
				if _, err := scan.DescribeResource(context.Background(), ref); err != nil {
					t.Fatalf("DescribeResource(%s): %v", ref.Id, err)
				}
			}
		}
	}
	for _, acctID := range []string{"111111111111", "222222222222"} {
		if got := buildCount(builds, acctID); got != 1 {
			t.Fatalf("account %s scanner built %d times, want 1", acctID, got)
		}
	}
}

// The scanner cache holds only listed accounts: an account that left the organization is
// dropped at the next listing, and an account that was never listed is not cached at all.
func TestAccountScannerCacheCappedAtListedAccounts(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	org := &countingOrg{accounts: []string{"111111111111", "222222222222"}}
	o.orgAPI = org
	var builds *sync.Map
	o.scannerFor, builds = countingBuilder()

	if _, err := o.ForScan().ScanService(context.Background(), "kms"); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if got := o.cachedScannerCount(); got != 2 {
		t.Fatalf("cached scanners = %d, want 2", got)
	}

	org.setAccounts([]string{"111111111111"})
	if _, err := o.ForScan().ScanService(context.Background(), "kms"); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := o.cachedScannerCount(); got != 1 {
		t.Fatalf("cached scanners after an account left = %d, want 1", got)
	}

	outsider := &pb.ResourceRef{Id: "key", AccountId: "333333333333"}
	for describe := 0; describe < 2; describe++ {
		if _, err := o.DescribeResource(context.Background(), outsider); err != nil {
			t.Fatalf("DescribeResource(outsider): %v", err)
		}
	}
	if got := o.cachedScannerCount(); got != 1 {
		t.Fatalf("cached scanners after an unlisted describe = %d, want 1", got)
	}
	if got := buildCount(builds, "333333333333"); got != 2 {
		t.Fatalf("unlisted account scanner built %d times, want 2 (never cached)", got)
	}
}

// countingSTS answers AssumeRole with credentials valid for an hour and counts the calls.
type countingSTS struct{ calls atomic.Int32 }

func (c *countingSTS) AssumeRole(
	context.Context, *sts.AssumeRoleInput, ...func(*sts.Options),
) (*sts.AssumeRoleOutput, error) {
	c.calls.Add(1)
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{
		AccessKeyId:     aws.String("AKIDEXAMPLE"),
		SecretAccessKey: aws.String("secret"),
		SessionToken:    aws.String("token"),
		Expiration:      aws.Time(time.Now().Add(time.Hour)),
	}}, nil
}

// An account's credentials are cached: retrieving them again within their lifetime must not
// call sts:AssumeRole again.
func TestAccountCredentialsAssumeRoleOnce(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	fake := &countingSTS{}
	o.stsAPI = fake

	credentials := o.accountCredentials("111111111111")
	for retrieve := 0; retrieve < 3; retrieve++ {
		got, err := credentials.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("Retrieve: %v", err)
		}
		if got.AccessKeyID != "AKIDEXAMPLE" {
			t.Fatalf("AccessKeyID = %q, want the assumed role's", got.AccessKeyID)
		}
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("AssumeRole calls = %d, want 1", got)
	}
}

// A builder failure is returned and not cached, so the next call tries again.
func TestAccountScannerBuildFailureNotCached(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = &countingOrg{accounts: []string{"111111111111"}}
	var attempts atomic.Int32
	o.scannerFor = func(_ context.Context, acctID string) (accountScanner, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("AccessDenied: sts:AssumeRole")
		}
		return fakeAccount{id: acctID}, nil
	}

	scan := o.ForScan()
	if _, err := scan.ScanService(context.Background(), "kms"); err == nil {
		t.Fatal("first ScanService err = nil, want the build failure")
	}
	if _, err := scan.ScanService(context.Background(), "s3"); err != nil {
		t.Fatalf("second ScanService: %v", err)
	}
	if got := o.cachedScannerCount(); got != 1 {
		t.Fatalf("cached scanners = %d, want 1", got)
	}
}
