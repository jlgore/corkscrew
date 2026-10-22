// Package orgscan fans the Cloud Control scanner out across every account
// in an AWS Organization. It enumerates accounts via the Organizations
// API, assumes a per-account role using STS, and runs one
// CloudControlScanner per account in a bounded worker pool. Results
// from every account carry the AccountId of the account they came from.
//
// Wired in from aws_provider.go when CORKSCREW_AWS_ORG_SCAN=true.
package orgscan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/jlgore/corkscrew/internal/assert"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// Config controls how the org scan fans out.
type Config struct {
	// RoleName is the IAM role assumed in each member account.
	// Defaults to "CorkscrewScanRole" — matching the StackSet template.
	RoleName string
	// ExternalID, if set, is sent as sts:ExternalId during AssumeRole.
	ExternalID string
	// IncludeAccounts narrows the scan to a specific set of account IDs.
	// Empty means scan everything Organizations.ListAccounts returns
	// (after applying ExcludeAccounts).
	IncludeAccounts []string
	// ExcludeAccounts is a hard skip list applied after IncludeAccounts.
	ExcludeAccounts []string
	// MaxConcurrentAccounts caps how many accounts are scanned in parallel.
	// Default 5. Increase for large orgs, decrease if hitting STS limits.
	MaxConcurrentAccounts int
	// SessionName is the role-session-name used when assuming. Default
	// "corkscrew-org-scan". Surfaced in CloudTrail.
	SessionName string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		RoleName:              "CorkscrewScanRole",
		MaxConcurrentAccounts: 5,
		SessionName:           "corkscrew-org-scan",
	}
}

// OrgScanner enumerates accounts and runs a CloudControlScanner per
// account. Implements the same UnsupportedTypes() / SupportedServices()
// surface as CloudControlScanner so the rest of the provider doesn't
// have to know which mode is active.
type OrgScanner struct {
	cfg     aws.Config // management-account credentials
	stsAPI  stscreds.AssumeRoleAPIClient
	orgAPI  accountLister
	options Config

	// scannerFor builds the scanner for one member account. A field so tests
	// can substitute accounts whose role assumption fails. Callers go through
	// accountScanner, which caches what it builds.
	scannerFor func(ctx context.Context, acctID string) (accountScanner, error)

	// Resource Explorer is wired through to each per-account scanner if
	// non-nil. RE indexes are per-account, so the explorer here should be
	// constructed with creds for the account whose index it queries —
	// in practice, set this only for the management account; per-account
	// scanners are passed nil and fall back to per-type ListResources.
	explorer scanner.ResourceExplorer

	mu               sync.Mutex
	unsupportedTypes map[string]string
	// accountsListed is the set of accounts the latest listing returned, and
	// scanners holds one scanner per account in it, so the cache never holds
	// more scanners than the organization has listed accounts. Building a
	// scanner per resource cost one sts:AssumeRole per resource.
	accountsListed map[string]bool
	scanners       map[string]accountScanner
}

// accountScanner is what OrgScanner needs from a per-account scanner.
type accountScanner interface {
	ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error)
	UnsupportedTypes() map[string]string
}

// accountLister is the Organizations call targetAccounts pages through.
type accountLister interface {
	ListAccounts(ctx context.Context, in *organizations.ListAccountsInput,
		opts ...func(*organizations.Options)) (*organizations.ListAccountsOutput, error)
}

// New constructs an OrgScanner using cfg for Organizations + STS calls.
// cfg should be credentials in the management or delegated-admin account.
func New(cfg aws.Config, opts Config) *OrgScanner {
	if opts.RoleName == "" {
		opts.RoleName = "CorkscrewScanRole"
	}
	if opts.MaxConcurrentAccounts <= 0 {
		opts.MaxConcurrentAccounts = 5
	}
	if opts.SessionName == "" {
		opts.SessionName = "corkscrew-org-scan"
	}
	o := &OrgScanner{
		cfg:              cfg,
		stsAPI:           sts.NewFromConfig(cfg),
		orgAPI:           organizations.NewFromConfig(cfg),
		options:          opts,
		unsupportedTypes: map[string]string{},
		accountsListed:   map[string]bool{},
		scanners:         map[string]accountScanner{},
	}
	o.scannerFor = o.scannerForAccount
	return o
}

// SetResourceExplorer wires an RE client into the scanner. See note on the
// OrgScanner struct: this is generally only useful for management-account
// indexes; member-account scanners run without it.
func (o *OrgScanner) SetResourceExplorer(re scanner.ResourceExplorer) {
	o.explorer = re
}

// SupportedServices returns the union of services any per-account scanner
// would enumerate. Constructed from a transient management-account scanner
// so we don't run discovery N times.
func (o *OrgScanner) SupportedServices() []string {
	return scanner.NewCloudControlScanner(o.cfg).SupportedServices()
}

// DiscoveryErr reports the process-wide type discovery state that every
// per-account scanner shares; see scanner.DiscoveryErr.
func (o *OrgScanner) DiscoveryErr() error {
	return scanner.DiscoveryErr()
}

// accountResult is one account's share of a ScanService fan-out.
type accountResult struct {
	acctID string
	refs   []*pb.ResourceRef
	err    error
}

// ScanService lists the targeted accounts and fans the scan out across them.
// A scan of several services should go through ForScan, which lists the
// accounts once for all of them.
func (o *OrgScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	accounts, err := o.targetAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return o.scanAccounts(ctx, accounts, serviceName)
}

// Scan is one scan's view of the organization: it lists the accounts on its
// first ScanService and reuses that list for every later service, where
// OrgScanner.ScanService lists them per service. Build one per scan with
// ForScan and drop it when the scan ends, so the next scan sees accounts
// that joined or left the organization. Every other method is the
// OrgScanner's, including the per-account scanner cache.
type Scan struct {
	*OrgScanner

	once     sync.Once
	accounts []string
	err      error
}

// ForScan returns a Scan for one scan of one or more services.
func (o *OrgScanner) ForScan() *Scan {
	return &Scan{OrgScanner: o}
}

// ScanService fans the scan out across the scan's accounts. A failed account
// listing is returned by every service, so each one is reported incomplete.
func (s *Scan) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	s.once.Do(func() {
		s.accounts, s.err = s.targetAccounts(ctx)
	})
	if s.err != nil {
		return nil, s.err
	}
	// targetAccounts turns an empty listing into an error, so success implies accounts.
	assert.That(len(s.accounts) > 0, "orgscan: a successful account listing has accounts")
	return s.scanAccounts(ctx, s.accounts, serviceName)
}

// scanAccounts fans the scan out across accounts. It returns
// the refs of every account that answered, even when others failed, joined
// with one "account:<id>: ..." error per failure so the caller can report
// the service as incomplete without discarding the accounts that worked.
func (o *OrgScanner) scanAccounts(
	ctx context.Context, accounts []string, serviceName string,
) ([]*pb.ResourceRef, error) {
	results := make(chan accountResult, len(accounts))
	sem := make(chan struct{}, o.options.MaxConcurrentAccounts)
	var wg sync.WaitGroup

	// A cancelled scan must stop starting accounts: without the ctx case the
	// loop would keep assuming roles until every account had been tried.
	var cancelled error
acctLoop:
	for _, acct := range accounts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			cancelled = ctx.Err()
			break acctLoop
		}
		wg.Add(1)
		go func(acctID string) {
			defer wg.Done()
			defer func() { <-sem }()

			refs, err := o.scanOneAccount(ctx, acctID, serviceName)
			if err != nil {
				log.Printf("orgscan: account %s service %s failed: %v", acctID, serviceName, err)
			}
			for _, r := range refs {
				r.AccountId = acctID
			}
			results <- accountResult{acctID: acctID, refs: refs, err: err}
		}(acct)
	}
	wg.Wait()
	close(results)

	var all []*pb.ResourceRef
	var errs []error
	if cancelled != nil {
		errs = append(errs, fmt.Errorf("orgscan: service %s: %w", serviceName, cancelled))
	}
	for res := range results {
		all = append(all, res.refs...)
		// One entry per failed type, each naming its account.
		for _, e := range scanner.FlattenErrors(res.err) {
			errs = append(errs, fmt.Errorf("account:%s: %w", res.acctID, e))
		}
	}
	return all, errors.Join(errs...)
}

// DescribeResource enriches a ref by re-assuming into the account the ref
// came from and calling Cloud Control's GetResource. The ref must carry
// AccountId — ScanService stamps it on every ref it returns.
func (o *OrgScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	if ref.AccountId == "" {
		return nil, fmt.Errorf("orgscan: ResourceRef missing AccountId; can't pick which account to assume")
	}
	sc, err := o.accountScanner(ctx, ref.AccountId)
	if err != nil {
		return nil, fmt.Errorf("orgscan: assume account %s: %w", ref.AccountId, err)
	}
	res, err := sc.DescribeResource(ctx, ref)
	if err != nil {
		return res, err
	}
	o.mergeUnsupported(sc.UnsupportedTypes())
	return res, nil
}

// GetMetrics returns a flat snapshot. Per-account metrics aren't aggregated
// here — keep this simple for the spike.
func (o *OrgScanner) GetMetrics() interface{} {
	return map[string]int64{
		"unsupported_type_count": int64(len(o.UnsupportedTypes())),
	}
}

// UnsupportedTypes returns the union of types every per-account scanner
// hit a TypeNotFoundException / UnsupportedActionException on.
func (o *OrgScanner) UnsupportedTypes() map[string]string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]string, len(o.unsupportedTypes))
	for k, v := range o.unsupportedTypes {
		out[k] = v
	}
	return out
}

func (o *OrgScanner) mergeUnsupported(other map[string]string) {
	if len(other) == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for k, v := range other {
		o.unsupportedTypes[k] = v
	}
}

// scanOneAccount assumes into acctID and runs a single CC scan there.
func (o *OrgScanner) scanOneAccount(ctx context.Context, acctID, serviceName string) ([]*pb.ResourceRef, error) {
	sc, err := o.accountScanner(ctx, acctID)
	if err != nil {
		return nil, fmt.Errorf("assume role: %w", err)
	}
	refs, err := sc.ScanService(ctx, serviceName)
	o.mergeUnsupported(sc.UnsupportedTypes())
	return refs, err
}

// accountScanner returns acctID's scanner, building it on first use. The
// scanner is cached while acctID is in the latest account listing, so its
// credentials cache serves every scan and describe of the account instead of
// one sts:AssumeRole per resource. An account outside the listing (a
// describe of an arbitrary ref) gets a scanner that is not cached, which
// keeps the cache bounded by the organization's account count. A failed
// build is not cached either, so the next call tries again.
func (o *OrgScanner) accountScanner(ctx context.Context, acctID string) (accountScanner, error) {
	// DescribeResource and targetAccounts reject blank IDs from refs and the API.
	assert.That(acctID != "", "orgscan: accountScanner gets an account ID")
	o.mu.Lock()
	defer o.mu.Unlock()

	if sc, ok := o.scanners[acctID]; ok {
		return sc, nil
	}
	// Building under mu keeps two accounts' workers from both building one
	// account's scanner; it is cheap, since the role is assumed lazily.
	sc, err := o.scannerFor(ctx, acctID)
	if err != nil {
		return nil, err
	}
	assert.That(sc != nil, "orgscan: scannerFor returns a scanner or an error")
	if o.accountsListed[acctID] {
		o.scanners[acctID] = sc
	}
	assert.That(len(o.scanners) <= len(o.accountsListed),
		"orgscan: cached scanners are at most the listed accounts")
	return sc, nil
}

// retainAccounts records accounts as the latest listing and drops the cached
// scanners of accounts no longer in it.
func (o *OrgScanner) retainAccounts(accounts []string) {
	assert.That(len(accounts) > 0, "orgscan: retainAccounts gets the listed accounts")
	listed := make(map[string]bool, len(accounts))
	for _, acctID := range accounts {
		listed[acctID] = true
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	o.accountsListed = listed
	for acctID := range o.scanners {
		if !listed[acctID] {
			delete(o.scanners, acctID)
		}
	}
	assert.That(len(o.scanners) <= len(o.accountsListed),
		"orgscan: cached scanners are at most the listed accounts")
}

// cachedScannerCount returns how many account scanners are cached.
func (o *OrgScanner) cachedScannerCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.scanners)
}

// scannerForAccount builds a CloudControlScanner whose aws.Config is
// scoped to the assumed-role credentials for acctID. The role is assumed
// lazily, so an AssumeRole failure surfaces from the scanner's first call
// (its STS account lookup).
func (o *OrgScanner) scannerForAccount(_ context.Context, acctID string) (accountScanner, error) {
	cfg := o.cfg.Copy()
	cfg.Credentials = o.accountCredentials(acctID)
	return scanner.NewCloudControlScanner(cfg), nil
}

// accountCredentials returns the assumed-role credentials for acctID behind
// a credentials cache, so sts:AssumeRole runs once per credential lifetime
// rather than on every request. The cache is only shared if the scanner
// holding it is: accountScanner caches that.
func (o *OrgScanner) accountCredentials(acctID string) *aws.CredentialsCache {
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", acctID, o.options.RoleName)
	provider := stscreds.NewAssumeRoleProvider(o.stsAPI, roleArn, func(p *stscreds.AssumeRoleOptions) {
		p.RoleSessionName = o.options.SessionName
		if o.options.ExternalID != "" {
			p.ExternalID = aws.String(o.options.ExternalID)
		}
	})
	return aws.NewCredentialsCache(provider)
}

// targetAccounts returns the account IDs to scan after applying the
// include/exclude filters and dropping suspended accounts.
func (o *OrgScanner) targetAccounts(ctx context.Context) ([]string, error) {
	include := stringSet(o.options.IncludeAccounts)
	exclude := stringSet(o.options.ExcludeAccounts)

	var ids []string
	var nextToken *string
	guard := paging.New("organizations.ListAccounts", paging.PagesMax)
	for {
		resp, err := o.orgAPI.ListAccounts(ctx, &organizations.ListAccountsInput{NextToken: nextToken})
		if err != nil {
			return nil, fmt.Errorf("organizations.ListAccounts: %w", err)
		}
		for _, a := range resp.Accounts {
			if a.Status != orgtypes.AccountStatusActive {
				continue
			}
			id := aws.ToString(a.Id)
			if id == "" {
				// Skipping it would scan a truncated account list; trusting it would
				// reach accountScanner with no account to assume into.
				return nil, fmt.Errorf(
					"organizations.ListAccounts returned an account without an ID")
			}
			if len(include) > 0 && !include[id] {
				continue
			}
			if exclude[id] {
				continue
			}
			ids = append(ids, id)
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		// Scanning a truncated account list would silently skip accounts.
		if err := guard.Next(*resp.NextToken); err != nil {
			return nil, err
		}
		nextToken = resp.NextToken
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no active accounts matched the include/exclude filters")
	}
	o.retainAccounts(ids)
	return ids, nil
}

// stringSet builds a quick lookup set, ignoring blanks. Whitespace tolerated.
func stringSet(items []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out[s] = true
	}
	return out
}
