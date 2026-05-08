package awsorg

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// BootstrapInput is what `corkscrew aws-org bootstrap-deploy` accepts.
type BootstrapInput struct {
	Params    TemplateParams
	StackName string // CloudFormation stack name in the management account.
}

// DefaultBootstrapInput supplies the same defaults as DefaultParams plus a
// stack name.
func DefaultBootstrapInput() BootstrapInput {
	return BootstrapInput{
		Params:    DefaultParams(),
		StackName: "corkscrew-org-bootstrap",
	}
}

// Bootstrap creates (or updates) a CloudFormation stack in the management
// account that defines the StackSet which provisions the per-account scan
// role across the organization.
//
// Prerequisites the caller is responsible for:
//   - Trusted access for CloudFormation StackSets is enabled in the org
//     (`aws organizations enable-aws-service-access --service-principal
//     stacksets.cloudformation.amazonaws.com`). Bootstrap returns a clear
//     error if the stack create fails because of this.
//   - The caller's credentials have CloudFormation + Organizations access
//     in the management or delegated-admin account.
func Bootstrap(ctx context.Context, cfg aws.Config, in BootstrapInput) (stackID string, err error) {
	if in.StackName == "" {
		in.StackName = "corkscrew-org-bootstrap"
	}

	// Resolve the management account ID — needed as a template parameter
	// even when TrustedPrincipalArn is set, because it's the trust fallback.
	stsClient := sts.NewFromConfig(cfg)
	id, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("sts.GetCallerIdentity: %w", err)
	}
	mgmtAccountID := aws.ToString(id.Account)

	body, err := Render(in.Params)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}

	cfn := cloudformation.NewFromConfig(cfg)
	out, err := cfn.CreateStack(ctx, &cloudformation.CreateStackInput{
		StackName:    aws.String(in.StackName),
		TemplateBody: aws.String(body),
		Capabilities: []cfntypes.Capability{
			cfntypes.CapabilityCapabilityNamedIam,
			cfntypes.CapabilityCapabilityIam,
		},
		Parameters: []cfntypes.Parameter{
			{
				ParameterKey:   aws.String("ManagementAccountId"),
				ParameterValue: aws.String(mgmtAccountID),
			},
		},
		OnFailure: cfntypes.OnFailureDelete,
	})
	if err != nil {
		return "", fmt.Errorf("cloudformation.CreateStack: %w", err)
	}
	return aws.ToString(out.StackId), nil
}

// ResolveOrgRoot returns the organization's root ID (r-xxxx). Useful when
// the user wants to deploy across the entire org and would rather not look
// up the root ID by hand.
func ResolveOrgRoot(ctx context.Context, cfg aws.Config) (string, error) {
	org := organizations.NewFromConfig(cfg)
	out, err := org.ListRoots(ctx, &organizations.ListRootsInput{})
	if err != nil {
		return "", fmt.Errorf("organizations.ListRoots: %w", err)
	}
	if len(out.Roots) == 0 {
		return "", fmt.Errorf("no organization roots returned")
	}
	return aws.ToString(out.Roots[0].Id), nil
}
