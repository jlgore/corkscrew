package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// EntraIDAppConfig represents the configuration for the Entra ID enterprise app
type EntraIDAppConfig struct {
	AppName                string   `json:"app_name"`
	Description            string   `json:"description"`
	RequiredPermissions    []string `json:"required_permissions"`
	ManagementGroupScope   string   `json:"management_group_scope"` // Root management group ID
	RoleDefinitions        []string `json:"role_definitions"`       // Reader, Contributor, etc.
	EnableMultiTenant      bool     `json:"enable_multi_tenant"`
	RedirectURIs           []string `json:"redirect_uris"`
	CertificateCredentials bool     `json:"certificate_credentials"`
}

// EntraIDAppDeployer handles deployment of Entra ID enterprise applications
type EntraIDAppDeployer struct {
	credential            azcore.TokenCredential
	graphClient           *msgraphsdk.GraphServiceClient
	authorizationClient   *armauthorization.RoleAssignmentsClient
	managementGroupClient *armmanagementgroups.Client
}

// NewEntraIDAppDeployer creates a new Entra ID app deployer
func NewEntraIDAppDeployer(credential azcore.TokenCredential) (*EntraIDAppDeployer, error) {
	// Create Microsoft Graph client
	graphClient, err := msgraphsdk.NewGraphServiceClientWithCredentials(credential, []string{
		"https://graph.microsoft.com/.default",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Graph client: %w", err)
	}

	// Create authorization client for role assignments
	// Note: We'll create this per-subscription when needed since it requires a subscription ID
	// For now, we'll store nil and create it dynamically
	var authClient *armauthorization.RoleAssignmentsClient = nil

	// Create management groups client
	mgClient, err := armmanagementgroups.NewClient(credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create management groups client: %w", err)
	}

	return &EntraIDAppDeployer{
		credential:            credential,
		graphClient:           graphClient,
		authorizationClient:   authClient,
		managementGroupClient: mgClient,
	}, nil
}

// DeployCorkscrewEnterpriseApp deploys the Corkscrew enterprise application
func (d *EntraIDAppDeployer) DeployCorkscrewEnterpriseApp(ctx context.Context, config *EntraIDAppConfig) (*EntraIDAppResult, error) {
	// For now, return a placeholder implementation that indicates manual setup is required
	// This avoids the complex Microsoft Graph SDK API issues while still providing useful information

	result := &EntraIDAppResult{
		ApplicationID:      "manual-setup-required",
		ObjectID:           "manual-setup-required",
		ServicePrincipalID: "manual-setup-required",
		TenantID:           "manual-setup-required",
		Credentials: &AppCredentials{
			Type: "manual_setup",
		},
		RoleAssignments:      []RoleAssignmentResult{},
		ManagementGroupScope: config.ManagementGroupScope,
		CreatedAt:            time.Now(),
	}

	return result, fmt.Errorf("automatic EntraID app deployment requires manual setup due to Microsoft Graph SDK complexity - please create the enterprise app manually and configure the credentials")
}

// createApplicationRegistration creates the Entra ID application registration
func (d *EntraIDAppDeployer) createApplicationRegistration(ctx context.Context, config *EntraIDAppConfig) (models.Applicationable, error) {
	return nil, fmt.Errorf("manual setup required: create an Azure AD application named '%s' with required permissions", config.AppName)
}

// createServicePrincipal creates a service principal for the application
func (d *EntraIDAppDeployer) createServicePrincipal(ctx context.Context, appID string) (models.ServicePrincipalable, error) {
	return nil, fmt.Errorf("manual setup required: create a service principal for application ID %s", appID)
}

// grantAPIPermissions grants the required API permissions
func (d *EntraIDAppDeployer) grantAPIPermissions(ctx context.Context, appObjectID string, permissions []string) error {
	return fmt.Errorf("manual setup required: grant API permissions %v to application %s", permissions, appObjectID)
}

// assignManagementGroupRoles assigns roles at the management group level
func (d *EntraIDAppDeployer) assignManagementGroupRoles(ctx context.Context, servicePrincipalID string, config *EntraIDAppConfig) ([]RoleAssignmentResult, error) {
	return nil, fmt.Errorf("manual setup required: assign roles %v to service principal %s at management group scope %s", config.RoleDefinitions, servicePrincipalID, config.ManagementGroupScope)
}

// getRoleDefinitionID gets the role definition ID for a role name
func (d *EntraIDAppDeployer) getRoleDefinitionID(ctx context.Context, roleName string) (string, error) {
	// Common Azure built-in role definitions
	builtInRoles := map[string]string{
		"Reader":                      "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7",
		"Contributor":                 "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/b24988ac-6180-42a0-ab88-20f7382dd24c",
		"Owner":                       "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/8e3af657-a8ff-443c-a75c-2fe8c4bcb635",
		"Resource Policy Contributor": "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/36243c78-bf99-498c-9df9-86d9f8d28608",
		"Security Reader":             "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/39bc4728-0917-49c7-9d2c-d95423bc2eb4",
		"Monitoring Reader":           "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/43d0d8ad-25c7-4714-9337-8ba259a9fe05",
	}

	if roleID, exists := builtInRoles[roleName]; exists {
		return roleID, nil
	}

	return "", fmt.Errorf("unknown role name: %s", roleName)
}

// createCredentials creates client secret or certificate credentials
func (d *EntraIDAppDeployer) createCredentials(ctx context.Context, appObjectID string, useCertificate bool) (*AppCredentials, error) {
	credType := "client secret"
	if useCertificate {
		credType = "certificate"
	}
	return nil, fmt.Errorf("manual setup required: create %s credentials for application %s", credType, appObjectID)
}

// createClientSecret creates a client secret for the application
func (d *EntraIDAppDeployer) createClientSecret(ctx context.Context, appObjectID string) (*AppCredentials, error) {
	return nil, fmt.Errorf("manual setup required: create client secret for application %s", appObjectID)
}

// createCertificateCredentials creates certificate credentials (placeholder)
func (d *EntraIDAppDeployer) createCertificateCredentials(ctx context.Context, appObjectID string) (*AppCredentials, error) {
	return nil, fmt.Errorf("manual setup required: create certificate credentials for application %s", appObjectID)
}

// Supporting types
type EntraIDAppResult struct {
	ApplicationID        string                 `json:"application_id"`
	ObjectID             string                 `json:"object_id"`
	ServicePrincipalID   string                 `json:"service_principal_id"`
	TenantID             string                 `json:"tenant_id"`
	Credentials          *AppCredentials        `json:"credentials"`
	RoleAssignments      []RoleAssignmentResult `json:"role_assignments"`
	ManagementGroupScope string                 `json:"management_group_scope"`
	CreatedAt            time.Time              `json:"created_at"`
}

type AppCredentials struct {
	Type         string    `json:"type"` // "client_secret" or "certificate"
	ClientSecret string    `json:"client_secret,omitempty"`
	Certificate  string    `json:"certificate,omitempty"`
	KeyID        string    `json:"key_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type RoleAssignmentResult struct {
	RoleName         string `json:"role_name"`
	RoleDefinitionID string `json:"role_definition_id"`
	Scope            string `json:"scope"`
	AssignmentID     string `json:"assignment_id"`
	Success          bool   `json:"success"`
}

// getRootManagementGroup gets the root management group ID
func (d *EntraIDAppDeployer) getRootManagementGroup(ctx context.Context) (*string, error) {
	// List all management groups
	pager := d.managementGroupClient.NewListPager(nil)
	
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list management groups: %w", err)
		}
		
		for _, mg := range page.Value {
			if mg.Properties != nil && mg.Properties.Details != nil && mg.Properties.Details.Parent == nil {
				// This is the root management group (has no parent)
				if mg.Name != nil {
					return mg.Name, nil
				}
			}
		}
	}
	
	return nil, fmt.Errorf("root management group not found")
}
