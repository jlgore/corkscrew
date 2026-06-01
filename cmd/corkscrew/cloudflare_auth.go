package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
)

func runCloudflare(args []string) {
	if len(args) == 0 {
		printCloudflareUsage()
		return
	}

	switch args[0] {
	case "auth":
		runCloudflareAuth(args[1:])
	case "login":
		runCloudflareLogin(args[1:])
	case "logout":
		if err := runCloudflareLogout(args[1:]); err != nil {
			fmt.Printf("Cloudflare logout failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown cloudflare command: %s\n", args[0])
		printCloudflareUsage()
	}
}

func runCloudflareAuth(args []string) {
	if len(args) == 0 {
		printCloudflareAuthUsage()
		return
	}

	switch args[0] {
	case "plan":
		if err := runCloudflareAuthPlan(args[1:]); err != nil {
			fmt.Printf("Cloudflare auth plan failed: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := runCloudflareAuthStatus(args[1:]); err != nil {
			fmt.Printf("Cloudflare auth status failed: %v\n", err)
			os.Exit(1)
		}
	case "verify":
		if err := runCloudflareAuthVerify(args[1:]); err != nil {
			fmt.Printf("Cloudflare auth verify failed: %v\n", err)
			os.Exit(1)
		}
	case "validate":
		if err := runCloudflareAuthValidate(args[1:]); err != nil {
			fmt.Printf("Cloudflare auth validate failed: %v\n", err)
			os.Exit(1)
		}
	case "bootstrap-token":
		if err := runCloudflareAuthBootstrapToken(args[1:]); err != nil {
			fmt.Printf("Cloudflare bootstrap-token failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown cloudflare auth command: %s\n", args[0])
		printCloudflareAuthUsage()
	}
}

func runCloudflareLogin(args []string) error {
	fs := flag.NewFlagSet("cloudflare login", flag.ContinueOnError)
	_ = fs.String("profile", cloudflareauth.DefaultProfileName, "OAuth profile name (unused for now)")
	services := fs.String("services", "", "Comma-separated services to plan scopes for")
	bundle := fs.String("bundle", "", "Permission bundle hint (e.g. full_readonly)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reqServices, _ := requestedServices(*services, *bundle)
	var plan *cloudflareauth.PermissionPlan
	if len(reqServices) > 0 {
		planner := &cloudflareauth.StaticPermissionPlanner{}
		var err error
		plan, err = planner.Plan(reqServices)
		if err != nil {
			return err
		}
	}

	fmt.Println("Cloudflare CLI login")
	fmt.Println()
	fmt.Println("Create a least-privilege API token from the Cloudflare dashboard:")
	fmt.Println("  https://dash.cloudflare.com/profile/api-tokens")
	fmt.Println()
	fmt.Println("Recommended scopes for your requested services:")
	if plan != nil && len(plan.Scopes) > 0 {
		for _, scope := range plan.Scopes {
			fmt.Printf("  - %s\n", scope)
		}
	} else {
		fmt.Println("  - account:read")
		fmt.Println("  - zone:read")
	}
	fmt.Println()
	fmt.Println("After creating the token, export it in your environment:")
	fmt.Println("  export CLOUDFLARE_API_TOKEN=your_token_here")
	fmt.Println()
	fmt.Println("To save it into a named OAuth profile for future use, run:")
	fmt.Println("  corkscrew cloudflare auth bootstrap-token")
	return nil
}

func runCloudflareAuthPlan(args []string) error {
	fs := flag.NewFlagSet("cloudflare auth plan", flag.ContinueOnError)
	services := fs.String("services", "", "Comma-separated Cloudflare scan services")
	bundle := fs.String("bundle", "", "Convenience permission bundle (for example: full_readonly)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	requested, err := requestedServices(*services, *bundle)
	if err != nil {
		return err
	}

	planner := &cloudflareauth.StaticPermissionPlanner{}
	plan, err := planner.Plan(requested)
	if err != nil {
		return err
	}

	fmt.Println("Services:")
	for _, service := range plan.Services {
		fmt.Printf("- %s\n", service)
	}

	fmt.Println()
	fmt.Println("Bundles:")
	for _, permissionBundle := range plan.Bundles {
		fmt.Printf("- %s: %s\n", permissionBundle.Name, permissionBundle.Description)
	}

	fmt.Println()
	fmt.Println("Scopes:")
	for _, scope := range plan.Scopes {
		fmt.Printf("- %s\n", scope)
	}

	return nil
}

func runCloudflareAuthStatus(args []string) error {
	fs := flag.NewFlagSet("cloudflare auth status", flag.ContinueOnError)
	profile := fs.String("profile", cloudflareauth.DefaultProfileName, "OAuth profile name")
	method := fs.String("method", "", "Auth method override (oauth, api_token, api_key)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	override := map[string]string{"auth.profile": *profile}
	if *method != "" {
		override["auth.method"] = *method
	}
	cfg, err := cloudflareauth.LoadConfig(override)
	if err != nil {
		return err
	}

	store := &cloudflareauth.FileOAuthStore{}
	resolver := &cloudflareauth.DefaultAuthResolver{
		Planner:       &cloudflareauth.StaticPermissionPlanner{},
		Store:         store,
		Validator:     &cloudflareauth.CloudflareTokenValidator{},
		AllowFallback: true,
	}
	resolved, err := resolver.Resolve(nil, cloudflareauth.ResolveAuthRequest{Config: cfg})
	if err != nil {
		fmt.Printf("Method: %s\n", cfg.Auth.Method)
		fmt.Printf("Status: not configured (%v)\n", err)
		return nil
	}

	fmt.Printf("Method: %s\n", resolved.Method)
	fmt.Printf("Source: %s\n", resolved.Source)

	if resolved.Method == cloudflareauth.AuthMethodOAuth {
		profileData, loadErr := store.Load(cfg.Auth.Profile)
		if loadErr == nil {
			fmt.Printf("Profile: %s\n", profileData.Profile)
			if profileData.Expired() {
				fmt.Println("Status: EXPIRED")
			} else {
				remaining := profileData.TimeUntilExpiry()
				if remaining > 0 {
					fmt.Printf("Expires in: %s\n", remaining.Round(time.Second))
				} else {
					fmt.Println("Expires: never")
				}
			}
		}
	}
	if len(resolved.Scopes) > 0 {
		fmt.Println("Scopes:")
		for _, scope := range resolved.Scopes {
			fmt.Printf("- %s\n", scope)
		}
	}
	fmt.Println("Validation: performed against Cloudflare API")
	return nil
}

func runCloudflareAuthVerify(args []string) error {
	fs := flag.NewFlagSet("cloudflare auth verify", flag.ContinueOnError)
	services := fs.String("services", "", "Comma-separated Cloudflare scan services")
	bundle := fs.String("bundle", "", "Convenience permission bundle")
	profile := fs.String("profile", cloudflareauth.DefaultProfileName, "OAuth profile name")
	method := fs.String("method", "", "Auth method override")
	if err := fs.Parse(args); err != nil {
		return err
	}

	requested, err := requestedServices(*services, *bundle)
	if err != nil {
		return err
	}

	planner := &cloudflareauth.StaticPermissionPlanner{}
	plan, err := planner.Plan(requested)
	if err != nil {
		return err
	}

	override := map[string]string{"auth.profile": *profile}
	if *method != "" {
		override["auth.method"] = *method
	}
	cfg, err := cloudflareauth.LoadConfig(override)
	if err != nil {
		return err
	}

	resolver := &cloudflareauth.DefaultAuthResolver{
		Planner:       planner,
		Store:         &cloudflareauth.FileOAuthStore{},
		Validator:     &cloudflareauth.CloudflareTokenValidator{},
		AllowFallback: true,
	}
	resolved, err := resolver.Resolve(nil, cloudflareauth.ResolveAuthRequest{Config: cfg, Services: requested})
	if err != nil {
		return err
	}

	result := cloudflareauth.VerifyResolvedAuth(plan, resolved)
	fmt.Printf("Auth method: %s\n", result.Method)
	fmt.Printf("Source: %s\n", result.Source)
	if !result.ScopeCheckStrict {
		fmt.Println("Result: auth material present; exact scope verification is only available for OAuth profiles right now")
		return nil
	}
	if len(result.MissingScopes) == 0 {
		fmt.Println("Result: OK")
		return nil
	}

	fmt.Println("Result: insufficient permissions")
	fmt.Println("Missing scopes:")
	for _, scope := range result.MissingScopes {
		fmt.Printf("- %s\n", scope)
	}
	return nil
}

func runCloudflareAuthValidate(args []string) error {
	fs := flag.NewFlagSet("cloudflare auth validate", flag.ContinueOnError)
	profile := fs.String("profile", cloudflareauth.DefaultProfileName, "OAuth profile name")
	method := fs.String("method", "", "Auth method override (oauth, api_token, api_key)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	override := map[string]string{"auth.profile": *profile}
	if *method != "" {
		override["auth.method"] = *method
	}
	cfg, err := cloudflareauth.LoadConfig(override)
	if err != nil {
		return err
	}

	resolver := &cloudflareauth.DefaultAuthResolver{
		Planner:       &cloudflareauth.StaticPermissionPlanner{},
		Store:         &cloudflareauth.FileOAuthStore{},
		Validator:     &cloudflareauth.CloudflareTokenValidator{},
		AllowFallback: true,
		Validate:      true,
	}
	resolved, err := resolver.Resolve(nil, cloudflareauth.ResolveAuthRequest{Config: cfg})
	if err != nil {
		return err
	}

	fmt.Printf("Method: %s\n", resolved.Method)
	fmt.Printf("Source: %s\n", resolved.Source)
	fmt.Println("Result: credentials accepted by Cloudflare API")
	return nil
}

func runCloudflareAuthBootstrapToken(args []string) error {
	fs := flag.NewFlagSet("cloudflare auth bootstrap-token", flag.ContinueOnError)
	services := fs.String("services", "", "Comma-separated Cloudflare scan services")
	bundle := fs.String("bundle", "", "Convenience permission bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}

	requested, err := requestedServices(*services, *bundle)
	if err != nil {
		return err
	}

	planner := &cloudflareauth.StaticPermissionPlanner{}
	plan, err := planner.Plan(requested)
	if err != nil {
		return err
	}

	fmt.Print(cloudflareauth.RenderTerraformBootstrap(plan))
	return nil
}

func runCloudflareLogout(args []string) error {
	fs := flag.NewFlagSet("cloudflare logout", flag.ContinueOnError)
	profile := fs.String("profile", cloudflareauth.DefaultProfileName, "OAuth profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := &cloudflareauth.FileOAuthStore{}
	if err := store.Delete(*profile); err != nil {
		return err
	}
	fmt.Printf("Deleted Cloudflare OAuth profile %q\n", *profile)
	return nil
}

func requestedServices(servicesRaw string, bundle string) ([]string, error) {
	if strings.TrimSpace(bundle) != "" {
		permissionBundle, ok := cloudflareauth.BundleByName(strings.TrimSpace(bundle))
		if !ok {
			return nil, fmt.Errorf("unknown bundle %q", bundle)
		}
		return permissionBundle.Services, nil
	}
	return cloudflareauth.ParseCSV(servicesRaw), nil
}

func printCloudflareUsage() {
	fmt.Println("Usage: corkscrew cloudflare <command>")
	fmt.Println("Commands: auth, login, logout")
	printCloudflareAuthUsage()
}

func printCloudflareAuthUsage() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Cloudflare auth commands:")
	fmt.Fprintln(w, "  plan\tShow required bundles and scopes for scan services")
	fmt.Fprintln(w, "  status\tShow current Cloudflare auth configuration")
	fmt.Fprintln(w, "  verify\tCheck current auth against required scopes")
	fmt.Fprintln(w, "  validate\tPing Cloudflare API to verify credentials")
	fmt.Fprintln(w, "  bootstrap-token\tPrint Terraform bootstrap stub for least-privilege tokens")
	w.Flush()
}
