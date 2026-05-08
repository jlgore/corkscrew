package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/internal/awsorg"
)

func runAWSOrg(args []string) {
	if len(args) < 1 {
		printAWSOrgUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "bootstrap-template":
		runAWSOrgBootstrapTemplate(args[1:])
	case "bootstrap-deploy":
		runAWSOrgBootstrapDeploy(args[1:])
	case "help", "--help", "-h":
		printAWSOrgUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown aws-org subcommand: %s\n", args[0])
		printAWSOrgUsage()
		os.Exit(1)
	}
}

func printAWSOrgUsage() {
	fmt.Println("Usage: corkscrew aws-org <subcommand> [flags]")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  bootstrap-template   Print a CloudFormation StackSet YAML template to stdout.")
	fmt.Println("  bootstrap-deploy     Create the StackSet directly via CloudFormation.")
	fmt.Println()
	fmt.Println("Both subcommands accept the same StackSet flags:")
	fmt.Println("  --role-name           IAM role name created in each account (default CorkscrewScanRole).")
	fmt.Println("  --trusted-principal   ARN allowed to AssumeRole; defaults to mgmt account root if empty.")
	fmt.Println("  --external-id         Optional sts:ExternalId requirement for AssumeRole.")
	fmt.Println("  --ous                 Comma-separated OU IDs (or 'root' to resolve the org root automatically).")
	fmt.Println("  --auto-deploy         If true, new accounts in the targeted OUs auto-receive the role (default true).")
	fmt.Println("  --home-region         Region the stack instances live in. IAM is global; default us-east-1.")
	fmt.Println()
	fmt.Println("bootstrap-deploy additionally accepts:")
	fmt.Println("  --stack-name          Name of the management-account stack (default corkscrew-org-bootstrap).")
}

type orgFlags struct {
	roleName    string
	trustedArn  string
	externalID  string
	ousCSV      string
	autoDeploy  bool
	homeRegion  string
	stackName   string
}

func bindOrgFlags(fs *flag.FlagSet, withDeployFlags bool) *orgFlags {
	f := &orgFlags{}
	d := awsorg.DefaultParams()
	fs.StringVar(&f.roleName, "role-name", d.RoleName, "IAM role name in each member account")
	fs.StringVar(&f.trustedArn, "trusted-principal", "", "ARN allowed to AssumeRole; empty = mgmt-account root")
	fs.StringVar(&f.externalID, "external-id", "", "sts:ExternalId required on AssumeRole")
	fs.StringVar(&f.ousCSV, "ous", "root", "Comma-separated OU IDs, or 'root' for entire org")
	fs.BoolVar(&f.autoDeploy, "auto-deploy", d.AutoDeployToNewAccounts, "Auto-deploy to new accounts added to targeted OUs")
	fs.StringVar(&f.homeRegion, "home-region", d.HomeRegion, "Region for stack instances (IAM is global)")
	if withDeployFlags {
		fs.StringVar(&f.stackName, "stack-name", "corkscrew-org-bootstrap", "Management-account stack name")
	}
	return f
}

// resolveOUs converts the --ous flag value into a slice of OU IDs.
// "root" triggers a one-time organizations.ListRoots lookup to discover
// the org root; everything else is treated as a comma-separated list.
func resolveOUs(ctx context.Context, raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	needRoot := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "root" {
			needRoot = true
			continue
		}
		out = append(out, p)
	}
	if needRoot {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("load AWS config: %w", err)
		}
		root, err := awsorg.ResolveOrgRoot(ctx, cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, root)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no OUs resolved")
	}
	return out, nil
}

func paramsFromFlags(f *orgFlags, ous []string) awsorg.TemplateParams {
	return awsorg.TemplateParams{
		RoleName:                f.roleName,
		TrustedPrincipalArn:     f.trustedArn,
		ExternalID:              f.externalID,
		OrganizationalUnitIDs:   ous,
		AutoDeployToNewAccounts: f.autoDeploy,
		HomeRegion:              f.homeRegion,
	}
}

func runAWSOrgBootstrapTemplate(args []string) {
	fs := flag.NewFlagSet("bootstrap-template", flag.ExitOnError)
	f := bindOrgFlags(fs, false)
	_ = fs.Parse(args)

	ctx := context.Background()
	ous, err := resolveOUs(ctx, f.ousCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve OUs: %v\n", err)
		os.Exit(1)
	}
	yaml, err := awsorg.Render(paramsFromFlags(f, ous))
	if err != nil {
		fmt.Fprintf(os.Stderr, "render template: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(yaml)
}

func runAWSOrgBootstrapDeploy(args []string) {
	fs := flag.NewFlagSet("bootstrap-deploy", flag.ExitOnError)
	f := bindOrgFlags(fs, true)
	_ = fs.Parse(args)

	ctx := context.Background()
	ous, err := resolveOUs(ctx, f.ousCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve OUs: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load AWS config: %v\n", err)
		os.Exit(1)
	}
	stackID, err := awsorg.Bootstrap(ctx, cfg, awsorg.BootstrapInput{
		Params:    paramsFromFlags(f, ous),
		StackName: f.stackName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap-deploy: %v\n", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Common causes:")
		fmt.Fprintln(os.Stderr, "  - Trusted access for CloudFormation StackSets is not enabled in the org.")
		fmt.Fprintln(os.Stderr, "    Run: aws organizations enable-aws-service-access \\")
		fmt.Fprintln(os.Stderr, "             --service-principal stacksets.cloudformation.amazonaws.com")
		fmt.Fprintln(os.Stderr, "  - Caller is not in the management or delegated-admin account.")
		fmt.Fprintln(os.Stderr, "  - A stack named "+f.stackName+" already exists; pass --stack-name to use a different name.")
		os.Exit(1)
	}
	fmt.Printf("StackSet bootstrap stack created: %s\n", stackID)
	fmt.Println("CloudFormation will now create the StackSet and roll out the per-account role to every targeted account.")
}
