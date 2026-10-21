package orgscan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// fakeOrg lists the given active accounts on one page, or, with
// repeatToken, returns the same continuation token on every page.
type fakeOrg struct {
	accounts    []string
	repeatToken bool
}

func (f fakeOrg) ListAccounts(
	context.Context, *organizations.ListAccountsInput, ...func(*organizations.Options),
) (*organizations.ListAccountsOutput, error) {
	out := &organizations.ListAccountsOutput{}
	for _, id := range f.accounts {
		out.Accounts = append(out.Accounts,
			orgtypes.Account{Id: aws.String(id), Status: orgtypes.AccountStatusActive})
	}
	if f.repeatToken {
		out.NextToken = aws.String("same-token")
	}
	return out, nil
}

// fakeAccount returns one ref per scan.
type fakeAccount struct{ id string }

func (f fakeAccount) ScanService(context.Context, string) ([]*pb.ResourceRef, error) {
	return []*pb.ResourceRef{{Type: "AWS::KMS::Key", Id: "key-" + f.id}}, nil
}

func (f fakeAccount) DescribeResource(context.Context, *pb.ResourceRef) (*pb.Resource, error) {
	return nil, errors.New("not faked")
}

func (f fakeAccount) UnsupportedTypes() map[string]string { return nil }

// One member account whose role cannot be assumed must not drop the other
// accounts, and must be reported as an error naming that account.
func TestScanServiceKeepsOtherAccountsWhenOneFails(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = fakeOrg{accounts: []string{"111111111111", "222222222222"}}
	o.scannerFor = func(_ context.Context, acctID string) (accountScanner, error) {
		if acctID == "222222222222" {
			return nil, errors.New("AccessDenied: sts:AssumeRole")
		}
		return fakeAccount{id: acctID}, nil
	}

	refs, err := o.ScanService(context.Background(), "kms")
	if len(refs) != 1 || refs[0].AccountId != "111111111111" {
		t.Fatalf("refs = %v, want the one ref of account 111111111111", refs)
	}
	leaves := scanner.FlattenErrors(err)
	if len(leaves) != 1 || !strings.Contains(leaves[0].Error(), "account:222222222222:") {
		t.Fatalf("errors = %v, want one naming account 222222222222", leaves)
	}
}

// A repeated ListAccounts token must fail the listing rather than loop: a
// truncated account list would silently skip accounts.
func TestTargetAccountsStopsOnRepeatedToken(t *testing.T) {
	o := New(awsCfgZero(t), Config{})
	o.orgAPI = fakeOrg{accounts: []string{"111111111111"}, repeatToken: true}

	if _, err := o.targetAccounts(context.Background()); !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("err = %v, want ErrTokenRepeated", err)
	}
}
