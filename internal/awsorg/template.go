// Package awsorg contains the CloudFormation StackSet template that
// provisions a per-account scan role across an AWS Organization, plus
// helpers for emitting the template and deploying the StackSet.
package awsorg

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateParams parameterize the StackSet emission. Only RoleName has a
// hard default; everything else is conditional.
type TemplateParams struct {
	// RoleName is the IAM role created in every member account.
	RoleName string
	// TrustedPrincipalArn is the principal allowed to AssumeRole. If empty,
	// the management account root is trusted (anyone in the mgmt account
	// with sts:AssumeRole on this role name can use it).
	TrustedPrincipalArn string
	// ExternalID, if set, is required as sts:ExternalId on AssumeRole.
	ExternalID string
	// OrganizationalUnitIDs is the list of OUs the StackSet deploys to.
	// Use the org root ID (r-xxxx) to deploy across the entire org.
	OrganizationalUnitIDs []string
	// AutoDeployToNewAccounts enables the StackSet's AutoDeployment feature
	// (new accounts added to the targeted OUs receive the role automatically).
	AutoDeployToNewAccounts bool
	// HomeRegion is the single region the StackSet's instances are created
	// in. Defaults to us-east-1. IAM is global, so one region is sufficient.
	HomeRegion string
}

// DefaultParams returns sensible starting values for TemplateParams.
func DefaultParams() TemplateParams {
	return TemplateParams{
		RoleName:                "CorkscrewScanRole",
		AutoDeployToNewAccounts: true,
		HomeRegion:              "us-east-1",
	}
}

// Render produces the CloudFormation YAML for the StackSet.
func Render(p TemplateParams) (string, error) {
	if p.RoleName == "" {
		return "", fmt.Errorf("RoleName is required")
	}
	if len(p.OrganizationalUnitIDs) == 0 {
		return "", fmt.Errorf("at least one OrganizationalUnitID is required (use the org root id 'r-xxxx' for org-wide)")
	}
	if p.HomeRegion == "" {
		p.HomeRegion = "us-east-1"
	}

	tmpl, err := template.New("stackset").Parse(stackSetTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		TemplateParams
		OUList string
	}{
		TemplateParams: p,
		OUList:         strings.Join(p.OrganizationalUnitIDs, ","),
	}); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// stackSetTemplate is the outer CloudFormation document. It defines a
// service-managed StackSet whose inline TemplateBody provisions the IAM
// role in every member account. Two layers because:
//
//   - The outer template is what the user deploys ONCE in the management
//     (or delegated-admin) account.
//   - The inner TemplateBody is what CloudFormation pushes to each member.
//
// Trust model: the inner role trusts TrustedPrincipalArn when set;
// otherwise it falls back to "arn:aws:iam::<mgmt-account>:root", letting
// any principal in the management account with sts:AssumeRole use it.
//
// Permissions: AWS-managed ReadOnlyAccess. This includes cloudcontrol's
// list/get plus every per-service Describe/Get/List that GetResource
// needs under the hood.
const stackSetTemplate = `AWSTemplateFormatVersion: '2010-09-09'
Description: Corkscrew org-wide scan role - StackSet that deploys an IAM role to every member account.

Parameters:
  ManagementAccountId:
    Type: String
    Description: Account ID of the management (or delegated-admin) account that runs corkscrew. Used as the trust fallback when TrustedPrincipalArn is empty.
    AllowedPattern: '^\d{12}$'

Resources:
  CorkscrewScanRoleStackSet:
    Type: AWS::CloudFormation::StackSet
    Properties:
      StackSetName: corkscrew-scan-role
      Description: Provisions {{.RoleName}} in every member account so corkscrew can run cross-account scans.
      PermissionModel: SERVICE_MANAGED
      AutoDeployment:
        Enabled: {{if .AutoDeployToNewAccounts}}true{{else}}false{{end}}
        RetainStacksOnAccountRemoval: false
      Capabilities:
        - CAPABILITY_NAMED_IAM
      Parameters:
        - ParameterKey: RoleName
          ParameterValue: {{.RoleName}}
        - ParameterKey: TrustedPrincipalArn
          ParameterValue: '{{.TrustedPrincipalArn}}'
        - ParameterKey: ExternalId
          ParameterValue: '{{.ExternalID}}'
        - ParameterKey: ManagementAccountId
          ParameterValue: !Ref ManagementAccountId
      StackInstancesGroup:
        - DeploymentTargets:
            OrganizationalUnitIds:
{{- range .OrganizationalUnitIDs}}
              - {{.}}
{{- end}}
          Regions:
            - {{.HomeRegion}}
      TemplateBody: |
        AWSTemplateFormatVersion: '2010-09-09'
        Description: Corkscrew per-account scan role.
        Parameters:
          RoleName:
            Type: String
          TrustedPrincipalArn:
            Type: String
            Default: ''
          ExternalId:
            Type: String
            Default: ''
            NoEcho: true
          ManagementAccountId:
            Type: String
        Conditions:
          HasSpecificPrincipal: !Not [!Equals [!Ref TrustedPrincipalArn, '']]
          HasExternalId: !Not [!Equals [!Ref ExternalId, '']]
        Resources:
          CorkscrewRole:
            Type: AWS::IAM::Role
            Properties:
              RoleName: !Ref RoleName
              MaxSessionDuration: 3600
              AssumeRolePolicyDocument:
                Version: '2012-10-17'
                Statement:
                  - Effect: Allow
                    Principal:
                      AWS: !If
                        - HasSpecificPrincipal
                        - !Ref TrustedPrincipalArn
                        - !Sub arn:aws:iam::${ManagementAccountId}:root
                    Action: sts:AssumeRole
                    Condition: !If
                      - HasExternalId
                      - StringEquals:
                          sts:ExternalId: !Ref ExternalId
                      - !Ref AWS::NoValue
              ManagedPolicyArns:
                - arn:aws:iam::aws:policy/ReadOnlyAccess
        Outputs:
          RoleArn:
            Description: ARN of the corkscrew scan role
            Value: !GetAtt CorkscrewRole.Arn

Outputs:
  StackSetName:
    Value: !Ref CorkscrewScanRoleStackSet
    Description: Name of the StackSet that maintains the per-account corkscrew scan role.
`
