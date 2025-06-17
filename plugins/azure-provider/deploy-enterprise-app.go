package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

func deployMain() {
	var (
		appName        = flag.String("app-name", "Corkscrew-Scanner", "Name of the enterprise application")
		description    = flag.String("description", "Corkscrew cloud resource scanner and analyzer", "Description of the app")
		mgScope        = flag.String("management-group", "", "Management group scope (empty for root)")
		roles          = flag.String("roles", "Reader", "Comma-separated list of roles to assign")
		useCert        = flag.Bool("use-certificate", false, "Use certificate credentials instead of client secret")
		outputFile     = flag.String("output", "corkscrew-app.json", "Output file for app details")
	)
	flag.Parse()

	ctx := context.Background()

	// Create credential
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Failed to create credential: %v", err)
	}

	// Create deployer
	deployer, err := NewEntraIDAppDeployer(cred)
	if err != nil {
		log.Fatalf("Failed to create deployer: %v", err)
	}

	// Parse roles
	roleList := []string{}
	if *roles != "" {
		roleList = parseCSV(*roles)
	}

	// Create config
	config := &EntraIDAppConfig{
		AppName:              *appName,
		Description:          *description,
		ManagementGroupScope: *mgScope,
		RoleDefinitions:      roleList,
		CertificateCredentials: *useCert,
		RequiredPermissions: []string{
			"User.Read",
			"Directory.Read.All",
			"Group.Read.All",
			"Application.Read.All",
		},
		RedirectURIs: []string{
			"https://localhost:8080/callback",
			"http://localhost:8080/callback",
		},
	}

	log.Printf("Deploying enterprise application: %s", config.AppName)
	log.Printf("Management Group Scope: %s", func() string {
		if config.ManagementGroupScope == "" {
			return "root (tenant-wide)"
		}
		return config.ManagementGroupScope
	}())
	log.Printf("Roles to assign: %v", config.RoleDefinitions)

	// Deploy the app
	result, err := deployer.DeployCorkscrewEnterpriseApp(ctx, config)
	if err != nil {
		log.Fatalf("Failed to deploy enterprise app: %v", err)
	}

	// Save result to file
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal result: %v", err)
	}

	err = os.WriteFile(*outputFile, data, 0600)
	if err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}

	log.Printf("Enterprise application deployed successfully!")
	log.Printf("Application ID: %s", result.ApplicationID)
	log.Printf("Service Principal ID: %s", result.ServicePrincipalID)
	log.Printf("Details saved to: %s", *outputFile)
	
	// Display next steps
	fmt.Println("\n=== Next Steps ===")
	fmt.Println("1. Grant admin consent for the API permissions in Azure Portal")
	fmt.Println("2. Wait a few minutes for the service principal to propagate")
	fmt.Println("3. Use the credentials in the output file to configure Corkscrew")
	
	if result.Credentials != nil && result.Credentials.ClientSecret != "" {
		fmt.Println("\n=== IMPORTANT: Save these credentials securely! ===")
		fmt.Printf("Client Secret: %s\n", result.Credentials.ClientSecret)
		fmt.Println("This secret will not be shown again!")
	}
}

func parseCSV(s string) []string {
	var result []string
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}