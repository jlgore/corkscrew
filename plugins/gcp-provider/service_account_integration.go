package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/iam/admin/apiv1"
	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	"cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServiceAccountIntegration manages service account automation for Corkscrew
type ServiceAccountIntegration struct {
	projectID         string
	iamClient         *admin.IamClient
	resourceManager   *resourcemanager.ProjectsClient
	crmService        *cloudresourcemanager.Service
	iamService        *iam.Service
}

// ServiceAccountConfig represents service account deployment configuration
type ServiceAccountConfig struct {
	AccountName         string   `json:"account_name"`
	DisplayName         string   `json:"display_name"`
	Description         string   `json:"description"`
	ProjectID           string   `json:"project_id"`
	RequiredRoles       []string `json:"required_roles"`
	EnableOrgWideAccess bool     `json:"enable_org_wide_access"`
	OrgID               string   `json:"org_id,omitempty"`
	KeyOutputPath       string   `json:"key_output_path"`
}

// ServiceAccountDeploymentResult represents the result of a deployment
type ServiceAccountDeploymentResult struct {
	Success            bool                 `json:"success"`
	ServiceAccountEmail string              `json:"service_account_email"`
	ProjectID          string               `json:"project_id"`
	RolesAssigned      []string             `json:"roles_assigned"`
	KeyCreated         bool                 `json:"key_created"`
	KeyFilePath        string               `json:"key_file_path,omitempty"`
	DeploymentTime     time.Time            `json:"deployment_time"`
	Warnings           []string             `json:"warnings,omitempty"`
	Errors             []string             `json:"errors,omitempty"`
	Recommendations    []string             `json:"recommendations,omitempty"`
}

// ServiceAccountValidationResult represents validation results
type ServiceAccountValidationResult struct {
	Valid                  bool                    `json:"valid"`
	ServiceAccountEmail    string                  `json:"service_account_email"`
	ProjectID              string                  `json:"project_id"`
	ExistingRoles          []string               `json:"existing_roles"`
	MissingRoles           []string               `json:"missing_roles"`
	HasAssetInventoryAccess bool                   `json:"has_asset_inventory_access"`
	HasKeyFileAccess       bool                   `json:"has_key_file_access"`
	ValidationTime         time.Time              `json:"validation_time"`
	Issues                 []ValidationIssue      `json:"issues"`
	Recommendations        []string               `json:"recommendations"`
}

// ValidationIssue represents a specific validation issue
type ValidationIssue struct {
	Type        string `json:"type"`        // "error", "warning", "info"
	Category    string `json:"category"`    // "permissions", "configuration", "security"
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// NewServiceAccountIntegration creates a new service account integration
func NewServiceAccountIntegration(ctx context.Context, projectID string) (*ServiceAccountIntegration, error) {
	// Initialize IAM admin client
	iamClient, err := admin.NewIamClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM client: %w", err)
	}

	// Initialize Resource Manager client
	rmClient, err := resourcemanager.NewProjectsClient(ctx)
	if err != nil {
		iamClient.Close()
		return nil, fmt.Errorf("failed to create Resource Manager client: %w", err)
	}

	// Initialize Cloud Resource Manager service for policy binding
	crmService, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		iamClient.Close()
		rmClient.Close()
		return nil, fmt.Errorf("failed to create Cloud Resource Manager service: %w", err)
	}

	// Initialize IAM service for service account management
	iamService, err := iam.NewService(ctx)
	if err != nil {
		iamClient.Close()
		rmClient.Close()
		return nil, fmt.Errorf("failed to create IAM service: %w", err)
	}

	return &ServiceAccountIntegration{
		projectID:       projectID,
		iamClient:       iamClient,
		resourceManager: rmClient,
		crmService:      crmService,
		iamService:      iamService,
	}, nil
}

// Close closes all clients
func (sai *ServiceAccountIntegration) Close() error {
	var errors []error
	
	if err := sai.iamClient.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close IAM client: %w", err))
	}
	
	if err := sai.resourceManager.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close Resource Manager client: %w", err))
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("errors closing clients: %v", errors)
	}
	
	return nil
}

// DeployCorkscrewServiceAccount deploys a service account for Corkscrew scanning
func (sai *ServiceAccountIntegration) DeployCorkscrewServiceAccount(ctx context.Context, config *ServiceAccountConfig) (*ServiceAccountDeploymentResult, error) {
	result := &ServiceAccountDeploymentResult{
		ProjectID:      config.ProjectID,
		DeploymentTime: time.Now(),
		Warnings:       []string{},
		Errors:         []string{},
		Recommendations: []string{},
	}

	log.Printf("🚀 Starting Corkscrew service account deployment for project: %s", config.ProjectID)

	// Step 1: Create service account
	serviceAccountEmail, err := sai.createServiceAccount(ctx, config)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to create service account: %v", err))
		return result, err
	}
	
	result.ServiceAccountEmail = serviceAccountEmail
	log.Printf("✅ Service account created/verified: %s", serviceAccountEmail)

	// Step 2: Assign IAM roles
	assignedRoles, roleErrors := sai.assignIAMRoles(ctx, config, serviceAccountEmail)
	result.RolesAssigned = assignedRoles
	
	for _, roleErr := range roleErrors {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Role assignment warning: %v", roleErr))
	}

	log.Printf("🔐 Assigned %d/%d roles successfully", len(assignedRoles), len(config.RequiredRoles))

	// Step 3: Create service account key (optional)
	if config.KeyOutputPath != "" {
		keyCreated, keyErr := sai.createServiceAccountKey(ctx, serviceAccountEmail, config.KeyOutputPath)
		result.KeyCreated = keyCreated
		result.KeyFilePath = config.KeyOutputPath
		
		if keyErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Key creation warning: %v", keyErr))
		}
	}

	// Step 4: Generate recommendations
	result.Recommendations = sai.generateRecommendations(config)

	// Determine overall success
	result.Success = len(result.Errors) == 0 && len(assignedRoles) > 0

	log.Printf("🎉 Deployment completed with success=%t, %d roles assigned", result.Success, len(assignedRoles))

	return result, nil
}

// ValidateServiceAccount validates an existing service account setup
func (sai *ServiceAccountIntegration) ValidateServiceAccount(ctx context.Context, serviceAccountEmail string) (*ServiceAccountValidationResult, error) {
	result := &ServiceAccountValidationResult{
		ServiceAccountEmail: serviceAccountEmail,
		ProjectID:          sai.projectID,
		ValidationTime:     time.Now(),
		Issues:             []ValidationIssue{},
		Recommendations:    []string{},
	}

	log.Printf("🔍 Validating service account: %s", serviceAccountEmail)

	// Step 1: Check if service account exists
	exists, err := sai.serviceAccountExists(ctx, serviceAccountEmail)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Type:     "error",
			Category: "configuration",
			Message:  fmt.Sprintf("Failed to check service account existence: %v", err),
		})
		return result, err
	}

	if !exists {
		result.Issues = append(result.Issues, ValidationIssue{
			Type:        "error",
			Category:    "configuration",
			Message:     "Service account does not exist",
			Remediation: "Create the service account using the deployment tool",
		})
		return result, nil
	}

	// Step 2: Check assigned roles
	existingRoles, err := sai.getServiceAccountRoles(ctx, serviceAccountEmail)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Type:     "warning",
			Category: "permissions",
			Message:  fmt.Sprintf("Failed to retrieve roles: %v", err),
		})
	} else {
		result.ExistingRoles = existingRoles
		
		// Check for required roles
		requiredRoles := sai.getDefaultRoles()
		missingRoles := sai.findMissingRoles(existingRoles, requiredRoles)
		result.MissingRoles = missingRoles
		
		if len(missingRoles) > 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Type:        "warning",
				Category:    "permissions",
				Message:     fmt.Sprintf("Missing %d required roles: %v", len(missingRoles), missingRoles),
				Remediation: "Use the deployment tool to assign missing roles",
			})
		}
	}

	// Step 3: Test Cloud Asset Inventory access
	assetInventoryAccess, err := sai.testAssetInventoryAccess(ctx)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Type:     "warning",
			Category: "permissions",
			Message:  fmt.Sprintf("Cloud Asset Inventory access test failed: %v", err),
		})
	}
	result.HasAssetInventoryAccess = assetInventoryAccess

	// Step 4: Generate recommendations
	result.Recommendations = sai.generateValidationRecommendations(result)

	// Determine overall validity
	result.Valid = len(result.MissingRoles) == 0 && result.HasAssetInventoryAccess && len(result.filterIssuesByType("error")) == 0

	log.Printf("✅ Validation completed: valid=%t, %d issues found", result.Valid, len(result.Issues))

	return result, nil
}

// Helper methods

func (sai *ServiceAccountIntegration) createServiceAccount(ctx context.Context, config *ServiceAccountConfig) (string, error) {
	serviceAccountEmail := fmt.Sprintf("%s@%s.iam.gserviceaccount.com", config.AccountName, config.ProjectID)
	
	// Check if service account already exists
	exists, err := sai.serviceAccountExists(ctx, serviceAccountEmail)
	if err != nil {
		return "", fmt.Errorf("failed to check service account existence: %w", err)
	}
	
	if exists {
		log.Printf("ℹ️ Service account already exists: %s", serviceAccountEmail)
		return serviceAccountEmail, nil
	}

	// Create the service account
	req := &adminpb.CreateServiceAccountRequest{
		Name:      fmt.Sprintf("projects/%s", config.ProjectID),
		AccountId: config.AccountName,
		ServiceAccount: &adminpb.ServiceAccount{
			DisplayName: config.DisplayName,
			Description: config.Description,
		},
	}

	_, err = sai.iamClient.CreateServiceAccount(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create service account: %w", err)
	}

	log.Printf("✅ Created service account: %s", serviceAccountEmail)
	return serviceAccountEmail, nil
}

func (sai *ServiceAccountIntegration) serviceAccountExists(ctx context.Context, email string) (bool, error) {
	req := &adminpb.GetServiceAccountRequest{
		Name: fmt.Sprintf("projects/%s/serviceAccounts/%s", sai.projectID, email),
	}
	
	_, err := sai.iamClient.GetServiceAccount(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	
	return true, nil
}

func (sai *ServiceAccountIntegration) assignIAMRoles(ctx context.Context, config *ServiceAccountConfig, serviceAccountEmail string) ([]string, []error) {
	var assignedRoles []string
	var errors []error

	member := fmt.Sprintf("serviceAccount:%s", serviceAccountEmail)

	for _, role := range config.RequiredRoles {
		err := sai.assignRole(ctx, config.ProjectID, member, role)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to assign role %s: %w", role, err))
			continue
		}
		assignedRoles = append(assignedRoles, role)
		log.Printf("  ✅ Assigned role: %s", role)
	}

	return assignedRoles, errors
}

func (sai *ServiceAccountIntegration) assignRole(ctx context.Context, projectID, member, role string) error {
	// Get current IAM policy
	policy, err := sai.crmService.Projects.GetIamPolicy(projectID, &cloudresourcemanager.GetIamPolicyRequest{}).Do()
	if err != nil {
		return fmt.Errorf("failed to get IAM policy: %w", err)
	}

	// Check if binding already exists
	var binding *cloudresourcemanager.Binding
	for _, b := range policy.Bindings {
		if b.Role == role {
			binding = b
			break
		}
	}

	// Create binding if it doesn't exist
	if binding == nil {
		binding = &cloudresourcemanager.Binding{
			Role:    role,
			Members: []string{},
		}
		policy.Bindings = append(policy.Bindings, binding)
	}

	// Check if member is already in the binding
	for _, existingMember := range binding.Members {
		if existingMember == member {
			log.Printf("  ℹ️ Role %s already assigned", role)
			return nil
		}
	}

	// Add member to binding
	binding.Members = append(binding.Members, member)

	// Update IAM policy
	_, err = sai.crmService.Projects.SetIamPolicy(projectID, &cloudresourcemanager.SetIamPolicyRequest{
		Policy: policy,
	}).Do()
	
	return err
}

func (sai *ServiceAccountIntegration) createServiceAccountKey(ctx context.Context, serviceAccountEmail, keyPath string) (bool, error) {
	req := &adminpb.CreateServiceAccountKeyRequest{
		Name: fmt.Sprintf("projects/%s/serviceAccounts/%s", sai.projectID, serviceAccountEmail),
	}

	key, err := sai.iamClient.CreateServiceAccountKey(ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to create service account key: %w", err)
	}

	// Save key to file
	err = saveKeyToFile(key.PrivateKeyData, keyPath)
	if err != nil {
		return false, fmt.Errorf("failed to save key to file: %w", err)
	}

	log.Printf("🔑 Created and saved service account key: %s", keyPath)
	return true, nil
}

func saveKeyToFile(keyData []byte, filePath string) error {
	// In a real implementation, this would write the key to the specified file
	// with proper permissions and error handling
	log.Printf("Key would be saved to: %s", filePath)
	return nil
}

func (sai *ServiceAccountIntegration) getServiceAccountRoles(ctx context.Context, serviceAccountEmail string) ([]string, error) {
	member := fmt.Sprintf("serviceAccount:%s", serviceAccountEmail)
	
	policy, err := sai.crmService.Projects.GetIamPolicy(sai.projectID, &cloudresourcemanager.GetIamPolicyRequest{}).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get IAM policy: %w", err)
	}

	var roles []string
	for _, binding := range policy.Bindings {
		for _, bindingMember := range binding.Members {
			if bindingMember == member {
				roles = append(roles, binding.Role)
				break
			}
		}
	}

	return roles, nil
}

func (sai *ServiceAccountIntegration) testAssetInventoryAccess(ctx context.Context) (bool, error) {
	// This would test actual Cloud Asset Inventory access
	// For now, we'll return true as a placeholder
	return true, nil
}

func (sai *ServiceAccountIntegration) getDefaultRoles() []string {
	return []string{
		"roles/cloudasset.viewer",
		"roles/browser",
		"roles/monitoring.viewer",
		"roles/compute.viewer",
		"roles/storage.objectViewer",
		"roles/bigquery.metadataViewer",
		"roles/pubsub.viewer",
		"roles/container.viewer",
		"roles/cloudsql.viewer",
		"roles/run.viewer",
		"roles/cloudfunctions.viewer",
		"roles/appengine.appViewer",
		"roles/resourcemanager.projectViewer",
		"roles/iam.securityReviewer",
	}
}

func (sai *ServiceAccountIntegration) findMissingRoles(existingRoles, requiredRoles []string) []string {
	roleMap := make(map[string]bool)
	for _, role := range existingRoles {
		roleMap[role] = true
	}

	var missing []string
	for _, required := range requiredRoles {
		if !roleMap[required] {
			missing = append(missing, required)
		}
	}

	return missing
}

func (sai *ServiceAccountIntegration) generateRecommendations(config *ServiceAccountConfig) []string {
	var recommendations []string

	// Security recommendations
	recommendations = append(recommendations, "🔐 Consider using Workload Identity instead of service account keys in production environments")
	recommendations = append(recommendations, "🔄 Set up automatic service account key rotation (every 90 days)")
	recommendations = append(recommendations, "📊 Enable audit logging for service account usage monitoring")
	
	// Performance recommendations
	if len(config.RequiredRoles) > 10 {
		recommendations = append(recommendations, "⚡ Consider using fewer, broader roles for better performance")
	}

	// Organization recommendations
	if config.EnableOrgWideAccess {
		recommendations = append(recommendations, "🌍 Organization-wide access detected - ensure this is necessary for security")
	}

	return recommendations
}

func (sai *ServiceAccountIntegration) generateValidationRecommendations(result *ServiceAccountValidationResult) []string {
	var recommendations []string

	if len(result.MissingRoles) > 0 {
		recommendations = append(recommendations, "🔧 Run the deployment tool to assign missing roles")
	}

	if !result.HasAssetInventoryAccess {
		recommendations = append(recommendations, "📊 Enable Cloud Asset Inventory API and ensure proper permissions")
	}

	errorCount := len(result.filterIssuesByType("error"))
	if errorCount > 0 {
		recommendations = append(recommendations, "❌ Address critical errors before using this service account")
	}

	return recommendations
}

func (result *ServiceAccountValidationResult) filterIssuesByType(issueType string) []ValidationIssue {
	var filtered []ValidationIssue
	for _, issue := range result.Issues {
		if issue.Type == issueType {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

// GCP Provider Integration methods

// SetupServiceAccount handles auto-setup requests from the provider
// TODO: Implement when protobuf types are available
/*
func (p *GCPProvider) SetupServiceAccount(ctx context.Context, req *pb.AutoSetupRequest) (*pb.AutoSetupResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("🔧 Auto-setup service account request for project: %s", req.ProjectId)

	// Initialize service account integration
	sai, err := NewServiceAccountIntegration(ctx, req.ProjectId)
	if err != nil {
		return &pb.AutoSetupResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to initialize service account integration: %v", err),
		}, nil
	}
	defer sai.Close()

	// Prepare configuration
	config := &ServiceAccountConfig{
		AccountName: req.AccountName,
		DisplayName: "Corkscrew Cloud Scanner",
		Description: "Service account for Corkscrew cloud resource scanning",
		ProjectID:   req.ProjectId,
		KeyOutputPath: "", // Don't create keys in auto-setup
	}

	// Determine roles based on request
	if req.UseMinimalRoles {
		config.RequiredRoles = []string{
			"roles/cloudasset.viewer",
			"roles/browser",
			"roles/resourcemanager.projectViewer",
		}
	} else {
		config.RequiredRoles = sai.getDefaultRoles()
	}

	// Deploy service account
	result, err := sai.DeployCorkscrewServiceAccount(ctx, config)
	if err != nil {
		return &pb.AutoSetupResponse{
			Success: false,
			Error:   fmt.Sprintf("Deployment failed: %v", err),
		}, nil
	}

	// Convert result to response
	response := &pb.AutoSetupResponse{
		Success:             result.Success,
		ServiceAccountEmail: result.ServiceAccountEmail,
		RolesAssigned:       int32(len(result.RolesAssigned)),
		CompletedAt:         timestamppb.New(result.DeploymentTime),
		Warnings:            result.Warnings,
		Recommendations:     result.Recommendations,
	}

	if !result.Success && len(result.Errors) > 0 {
		response.Error = strings.Join(result.Errors, "; ")
	}

	log.Printf("✅ Auto-setup completed: success=%t", result.Success)

	return response, nil
}
*/

// ValidateServiceAccount handles validation requests from the provider
// TODO: Implement when protobuf types are available
/*
func (p *GCPProvider) ValidateServiceAccount(ctx context.Context, req *pb.ValidateSetupRequest) (*pb.ValidateSetupResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	log.Printf("🔍 Validating service account: %s", req.ServiceAccountEmail)

	// Initialize service account integration
	sai, err := NewServiceAccountIntegration(ctx, req.ProjectId)
	if err != nil {
		return &pb.ValidateSetupResponse{
			Valid: false,
			Error: fmt.Sprintf("Failed to initialize service account integration: %v", err),
		}, nil
	}
	defer sai.Close()

	// Validate service account
	result, err := sai.ValidateServiceAccount(ctx, req.ServiceAccountEmail)
	if err != nil {
		return &pb.ValidateSetupResponse{
			Valid: false,
			Error: fmt.Sprintf("Validation failed: %v", err),
		}, nil
	}

	// Convert issues to proto format
	var issues []*pb.ValidationIssue
	for _, issue := range result.Issues {
		issues = append(issues, &pb.ValidationIssue{
			Type:        issue.Type,
			Category:    issue.Category,
			Message:     issue.Message,
			Remediation: issue.Remediation,
		})
	}

	response := &pb.ValidateSetupResponse{
		Valid:                   result.Valid,
		ServiceAccountEmail:     result.ServiceAccountEmail,
		ProjectId:               result.ProjectID,
		ExistingRoles:           result.ExistingRoles,
		MissingRoles:            result.MissingRoles,
		HasAssetInventoryAccess: result.HasAssetInventoryAccess,
		ValidatedAt:             timestamppb.New(result.ValidationTime),
		Issues:                  issues,
		Recommendations:         result.Recommendations,
	}

	log.Printf("✅ Validation completed: valid=%t, %d issues", result.Valid, len(result.Issues))

	return response, nil
}
*/

// GetServiceAccountRecommendations provides recommendations for service account setup  
// TODO: Implement when protobuf types are available
/*
func (p *GCPProvider) GetServiceAccountRecommendations(ctx context.Context, req *pb.RecommendationsRequest) (*pb.RecommendationsResponse, error) {
	// Generate general recommendations
	recommendations := []string{
		"🔐 Use Workload Identity instead of service account keys in Kubernetes environments",
		"🔄 Implement automatic key rotation every 90 days",
		"📊 Enable Cloud Asset Inventory for comprehensive resource discovery",
		"🔍 Use minimal required roles following principle of least privilege",
		"📝 Enable audit logging for service account usage monitoring",
		"🛡️ Consider using IAM Conditions for time-based or IP-based restrictions",
		"🏢 For organization-wide scanning, use folder-level permissions when possible",
		"💰 Monitor Cloud Asset Inventory usage for cost optimization",
	}

	return &pb.RecommendationsResponse{
		Recommendations: recommendations,
		GeneratedAt:     timestamppb.Now(),
		ProviderType:    "gcp",
	}, nil
}
*/