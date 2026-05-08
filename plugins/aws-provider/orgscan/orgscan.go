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
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	pb "github.com/jlgore/corkscrew/internal/proto"
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
	stsAPI  *sts.Client
	orgAPI  *organizations.Client
	options Config

	// Resource Explorer is wired through to each per-account scanner if
	// non-nil. RE indexes are per-account, so the explorer here should be
	// constructed with creds for the account whose index it queries —
	// in practice, set this only for the management account; per-account
	// scanners are passed nil and fall back to per-type ListResources.
	explorer scanner.ResourceExplorer

	mu               sync.Mutex
	unsupportedTypes map[string]string
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
	return &OrgScanner{
		cfg:              cfg,
		stsAPI:           sts.NewFromConfig(cfg),
		orgAPI:           organizations.NewFromConfig(cfg),
		options:          opts,
		unsupportedTypes: map[string]string{},
	}
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

// ScanService fans the scan out across every targeted account.
func (o *OrgScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	accounts, err := o.targetAccounts(ctx)
	if err != nil {
		return nil, err
	}

	results := make(chan []*pb.ResourceRef, len(accounts))
	sem := make(chan struct{}, o.options.MaxConcurrentAccounts)
	var wg sync.WaitGroup

	for _, acct := range accounts {
		wg.Add(1)
		sem <- struct{}{}
		go func(acctID string) {
			defer wg.Done()
			defer func() { <-sem }()

			refs, err := o.scanOneAccount(ctx, acctID, serviceName)
			if err != nil {
				log.Printf("orgscan: account %s service %s failed: %v", acctID, serviceName, err)
				return
			}
			for _, r := range refs {
				r.AccountId = acctID
			}
			results <- refs
		}(acct)
	}
	wg.Wait()
	close(results)

	var all []*pb.ResourceRef
	for refs := range results {
		all = append(all, refs...)
	}
	return all, nil
}

// DescribeResource enriches a ref by re-assuming into the account the ref
// came from and calling Cloud Control's GetResource. The ref must carry
// AccountId — ScanService stamps it on every ref it returns.
func (o *OrgScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	if ref.AccountId == "" {
		return nil, fmt.Errorf("orgscan: ResourceRef missing AccountId; can't pick which account to assume")
	}
	sc, err := o.scannerForAccount(ctx, ref.AccountId)
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
	sc, err := o.scannerForAccount(ctx, acctID)
	if err != nil {
		return nil, err
	}
	refs, err := sc.ScanService(ctx, serviceName)
	o.mergeUnsupported(sc.UnsupportedTypes())
	return refs, err
}

// scannerForAccount builds a CloudControlScanner whose aws.Config is
// scoped to the assumed-role credentials for acctID. Constructed fresh
// per call — STS credentials cache themselves, and the cost of creating
// CC clients is small.
func (o *OrgScanner) scannerForAccount(ctx context.Context, acctID string) (*scanner.CloudControlScanner, error) {
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", acctID, o.options.RoleName)
	provider := stscreds.NewAssumeRoleProvider(o.stsAPI, roleArn, func(p *stscreds.AssumeRoleOptions) {
		p.RoleSessionName = o.options.SessionName
		if o.options.ExternalID != "" {
			p.ExternalID = aws.String(o.options.ExternalID)
		}
	})

	cfg := o.cfg.Copy()
	cfg.Credentials = aws.NewCredentialsCache(provider)
	return scanner.NewCloudControlScanner(cfg), nil
}

// targetAccounts returns the account IDs to scan after applying the
// include/exclude filters and dropping suspended accounts.
func (o *OrgScanner) targetAccounts(ctx context.Context) ([]string, error) {
	include := stringSet(o.options.IncludeAccounts)
	exclude := stringSet(o.options.ExcludeAccounts)

	var ids []string
	var nextToken *string
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
		nextToken = resp.NextToken
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no active accounts matched the include/exclude filters")
	}
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
