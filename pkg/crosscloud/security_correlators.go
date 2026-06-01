package crosscloud

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// SecurityCorrelator handles cross-cloud security relationships and role analysis
type SecurityCorrelator struct {
	db               DatabaseInterface
	logger           *log.Logger
	confidenceThresh float64
}

// NewSecurityCorrelator creates a new security correlator
func NewSecurityCorrelator(db DatabaseInterface, logger *log.Logger) *SecurityCorrelator {
	return &SecurityCorrelator{
		db:               db,
		logger:           logger,
		confidenceThresh: 0.80,
	}
}

// CrossAccountRole represents a cross-account role relationship
type CrossAccountRole struct {
	ID                string                 `json:"id"`
	RoleArn           string                 `json:"role_arn"`
	RoleName          string                 `json:"role_name"`
	Provider          string                 `json:"provider"`
	Region            string                 `json:"region"`
	AccountID         string                 `json:"account_id"`
	TrustPolicy       map[string]interface{} `json:"trust_policy"`
	TrustedPrincipals []string               `json:"trusted_principals"`
	Permissions       []Permission           `json:"permissions"`
	Conditions        []TrustCondition       `json:"conditions"`
	Metadata          map[string]interface{} `json:"metadata"`
}

// Permission represents a permission granted by a role or policy
type Permission struct {
	Effect    string   `json:"effect"`    // Allow, Deny
	Actions   []string `json:"actions"`   // List of actions
	Resources []string `json:"resources"` // List of resources
	Principal string   `json:"principal"` // Principal (for resource policies)
}

// CrossAccountRelationship represents a detected cross-account relationship
type CrossAccountRelationship struct {
	ID               string           `json:"id"`
	SourceRole       CrossAccountRole `json:"source_role"`
	TargetRole       CrossAccountRole `json:"target_role"`
	RelationshipType string           `json:"relationship_type"`
	AssumptionChain  []string         `json:"assumption_chain"`
	Confidence       float64          `json:"confidence"`
	RiskScore        float64          `json:"risk_score"`
	Evidence         []string         `json:"evidence"`
	EscalationPaths  []EscalationPath `json:"escalation_paths"`
	DetectedAt       time.Time        `json:"detected_at"`
}

// EscalationPath represents a potential privilege escalation path
type EscalationPath struct {
	ID          string   `json:"id"`
	Steps       []string `json:"steps"`
	RiskLevel   string   `json:"risk_level"` // LOW, MEDIUM, HIGH, CRITICAL
	Description string   `json:"description"`
	Mitigations []string `json:"mitigations"`
}

// CorrelateSecurityRelationships finds cross-account security relationships
func (sc *SecurityCorrelator) CorrelateSecurityRelationships(ctx context.Context) ([]CrossAccountRelationship, error) {
	var relationships []CrossAccountRelationship

	// Extract cross-account roles from all clouds
	roles, err := sc.extractCrossAccountRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract cross-account roles: %w", err)
	}

	sc.logger.Printf("Found %d cross-account roles across clouds", len(roles))

	// Analyze cross-account role relationships
	crossAccountRels, err := sc.analyzeCrossAccountRoles(roles)
	if err != nil {
		sc.logger.Printf("Error analyzing cross-account roles: %v", err)
	} else {
		relationships = append(relationships, crossAccountRels...)
	}

	// Analyze service principal relationships
	servicePrincipalRels, err := sc.analyzeServicePrincipalRelationships(roles)
	if err != nil {
		sc.logger.Printf("Error analyzing service principal relationships: %v", err)
	} else {
		relationships = append(relationships, servicePrincipalRels...)
	}

	// Analyze role assumption chains
	chainRels, err := sc.analyzeRoleAssumptionChains(roles)
	if err != nil {
		sc.logger.Printf("Error analyzing role assumption chains: %v", err)
	} else {
		relationships = append(relationships, chainRels...)
	}

	// Detect privilege escalation paths
	for i := range relationships {
		relationships[i].EscalationPaths = sc.detectPrivilegeEscalationPaths(relationships[i])
		relationships[i].RiskScore = sc.calculateSecurityRiskScore(relationships[i])
	}

	sc.logger.Printf("Found %d cross-account security relationships", len(relationships))
	return relationships, nil
}

// extractCrossAccountRoles extracts cross-account roles from all cloud resources
func (sc *SecurityCorrelator) extractCrossAccountRoles(ctx context.Context) ([]CrossAccountRole, error) {
	var roles []CrossAccountRole

	// Extract AWS IAM roles
	awsRoles, err := sc.extractAWSCrossAccountRoles(ctx)
	if err != nil {
		sc.logger.Printf("Error extracting AWS cross-account roles: %v", err)
	} else {
		roles = append(roles, awsRoles...)
	}

	// Extract Azure service principals and role assignments
	azureRoles, err := sc.extractAzureCrossAccountRoles(ctx)
	if err != nil {
		sc.logger.Printf("Error extracting Azure cross-account roles: %v", err)
	} else {
		roles = append(roles, azureRoles...)
	}

	return roles, nil
}

// extractAWSCrossAccountRoles extracts AWS IAM roles with cross-account trust
func (sc *SecurityCorrelator) extractAWSCrossAccountRoles(ctx context.Context) ([]CrossAccountRole, error) {
	var roles []CrossAccountRole

	query := `
	SELECT id, name, arn, raw_data, region, account_id
	FROM aws_resources 
	WHERE type = 'AWS::IAM::Role'
	  AND (
	    json_extract(raw_data, '$.AssumeRolePolicyDocument') LIKE '%arn:aws:iam::%'
	    OR json_extract(raw_data, '$.AssumeRolePolicyDocument') LIKE '%Principal%'
	  )
	`

	rows, err := sc.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, arn, rawDataStr, region, accountID string
		if err := rows.Scan(&id, &name, &arn, &rawDataStr, &region, &accountID); err != nil {
			continue
		}

		var rawData map[string]interface{}
		if err := json.Unmarshal([]byte(rawDataStr), &rawData); err != nil {
			continue
		}

		role := sc.parseAWSCrossAccountRole(id, name, arn, rawData, region, accountID)
		if role != nil && sc.isCrossAccountRole(*role) {
			roles = append(roles, *role)
		}
	}

	return roles, nil
}

// parseAWSCrossAccountRole parses AWS IAM role from resource data
func (sc *SecurityCorrelator) parseAWSCrossAccountRole(id, name, arn string, rawData map[string]interface{}, region, accountID string) *CrossAccountRole {
	role := &CrossAccountRole{
		ID:        id,
		RoleArn:   arn,
		RoleName:  name,
		Provider:  "aws",
		Region:    region,
		AccountID: accountID,
		Metadata:  rawData,
	}

	// Parse trust policy
	if policyStr, ok := rawData["AssumeRolePolicyDocument"].(string); ok {
		var policy map[string]interface{}
		if err := json.Unmarshal([]byte(policyStr), &policy); err == nil {
			role.TrustPolicy = policy
			role.TrustedPrincipals = sc.extractTrustedPrincipals(policy)
			role.Conditions = sc.extractTrustConditionsFromPolicy(policy)
		}
	}

	// Parse attached policies for permissions
	if policies, ok := rawData["AttachedManagedPolicies"].([]interface{}); ok {
		for _, p := range policies {
			if policyMap, ok := p.(map[string]interface{}); ok {
				if _, ok := policyMap["PolicyArn"].(string); ok {
					// Note: In a real implementation, we'd fetch the policy document
					role.Permissions = append(role.Permissions, Permission{
						Effect:    "Allow",
						Actions:   []string{"*"}, // Placeholder
						Resources: []string{"*"}, // Placeholder
					})
				}
			}
		}
	}

	return role
}

// extractTrustedPrincipals extracts trusted principals from trust policy
func (sc *SecurityCorrelator) extractTrustedPrincipals(policy map[string]interface{}) []string {
	var principals []string

	if statements, ok := policy["Statement"].([]interface{}); ok {
		for _, stmt := range statements {
			if stmtMap, ok := stmt.(map[string]interface{}); ok {
				if principal, ok := stmtMap["Principal"]; ok {
					switch p := principal.(type) {
					case string:
						principals = append(principals, p)
					case map[string]interface{}:
						for _, value := range p {
							if str, ok := value.(string); ok {
								principals = append(principals, str)
							} else if slice, ok := value.([]interface{}); ok {
								for _, item := range slice {
									if str, ok := item.(string); ok {
										principals = append(principals, str)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return principals
}

// extractTrustConditionsFromPolicy extracts trust conditions from IAM policy
func (sc *SecurityCorrelator) extractTrustConditionsFromPolicy(policy map[string]interface{}) []TrustCondition {
	var conditions []TrustCondition

	if statements, ok := policy["Statement"].([]interface{}); ok {
		for _, stmt := range statements {
			if stmtMap, ok := stmt.(map[string]interface{}); ok {
				if conditionMap, ok := stmtMap["Condition"].(map[string]interface{}); ok {
					for operator, condition := range conditionMap {
						if condMap, ok := condition.(map[string]interface{}); ok {
							for field, value := range condMap {
								conditions = append(conditions, TrustCondition{
									Type:     "trust_policy",
									Field:    field,
									Operator: operator,
									Value:    value,
								})
							}
						}
					}
				}
			}
		}
	}

	return conditions
}

// isCrossAccountRole determines if a role has cross-account trust
func (sc *SecurityCorrelator) isCrossAccountRole(role CrossAccountRole) bool {
	for _, principal := range role.TrustedPrincipals {
		// Check for different account IDs in ARNs
		if strings.Contains(principal, "arn:aws:iam::") && !strings.Contains(principal, role.AccountID) {
			return true
		}
		// Check for external identity providers
		if strings.Contains(principal, "oidc") || strings.Contains(principal, "saml") {
			return true
		}
		// Check for service principals from different accounts
		if strings.Contains(principal, ".amazonaws.com") && !strings.Contains(principal, role.AccountID) {
			return true
		}
	}
	return false
}

// extractAzureCrossAccountRoles extracts Azure service principals with cross-tenant access
func (sc *SecurityCorrelator) extractAzureCrossAccountRoles(ctx context.Context) ([]CrossAccountRole, error) {
	var roles []CrossAccountRole

	query := `
	SELECT id, name, type, raw_data, location, subscription_id
	FROM azure_resources 
	WHERE type IN ('Microsoft.Authorization/roleAssignments', 'Microsoft.ManagedIdentity/userAssignedIdentities')
	`

	rows, err := sc.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, resourceType, rawDataStr, location, subscriptionID string
		if err := rows.Scan(&id, &name, &resourceType, &rawDataStr, &location, &subscriptionID); err != nil {
			continue
		}

		var rawData map[string]interface{}
		if err := json.Unmarshal([]byte(rawDataStr), &rawData); err != nil {
			continue
		}

		role := sc.parseAzureCrossAccountRole(id, name, resourceType, rawData, location, subscriptionID)
		if role != nil {
			roles = append(roles, *role)
		}
	}

	return roles, nil
}

// parseAzureCrossAccountRole parses Azure service principal from resource data
func (sc *SecurityCorrelator) parseAzureCrossAccountRole(id, name, resourceType string, rawData map[string]interface{}, location, subscriptionID string) *CrossAccountRole {
	role := &CrossAccountRole{
		ID:        id,
		RoleName:  name,
		Provider:  "azure",
		Region:    location,
		AccountID: subscriptionID,
		Metadata:  rawData,
	}

	if resourceType == "Microsoft.Authorization/roleAssignments" {
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			role.RoleArn = id
			if principalID, ok := props["principalId"].(string); ok {
				role.TrustedPrincipals = []string{principalID}
			}
			if scope, ok := props["scope"].(string); ok {
				role.Permissions = []Permission{{
					Effect:    "Allow",
					Actions:   []string{"*"}, // Placeholder - would need to resolve role definition
					Resources: []string{scope},
				}}
			}
		}
	} else if resourceType == "Microsoft.ManagedIdentity/userAssignedIdentities" {
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			role.RoleArn = id
			if principalID, ok := props["principalId"].(string); ok {
				role.TrustedPrincipals = []string{principalID}
			}
		}
	}

	return role
}

// analyzeCrossAccountRoles analyzes relationships between cross-account roles
func (sc *SecurityCorrelator) analyzeCrossAccountRoles(roles []CrossAccountRole) ([]CrossAccountRelationship, error) {
	var relationships []CrossAccountRelationship

	// Group roles by provider and account
	rolesByProvider := make(map[string][]CrossAccountRole)
	for _, role := range roles {
		key := fmt.Sprintf("%s:%s", role.Provider, role.AccountID)
		rolesByProvider[key] = append(rolesByProvider[key], role)
	}

	// Find cross-account relationships
	for sourceKey, sourceRoles := range rolesByProvider {
		for targetKey, targetRoles := range rolesByProvider {
			if sourceKey == targetKey {
				continue
			}

			// Analyze relationships between roles in different accounts
			for _, sourceRole := range sourceRoles {
				for _, targetRole := range targetRoles {
					rel := sc.analyzeRoleRelationship(sourceRole, targetRole)
					if rel != nil && rel.Confidence >= sc.confidenceThresh {
						relationships = append(relationships, *rel)
					}
				}
			}
		}
	}

	return relationships, nil
}

// analyzeRoleRelationship analyzes the relationship between two roles
func (sc *SecurityCorrelator) analyzeRoleRelationship(sourceRole, targetRole CrossAccountRole) *CrossAccountRelationship {
	var evidence []string
	var confidence float64

	// Check if source role trusts target role's account
	for _, principal := range sourceRole.TrustedPrincipals {
		if strings.Contains(principal, targetRole.AccountID) {
			evidence = append(evidence, fmt.Sprintf("Source role trusts principal from target account: %s", principal))
			confidence += 0.8
		}
	}

	// Check for matching conditions or constraints
	conditionMatch := sc.analyzeConditionMatches(sourceRole.Conditions, targetRole.Conditions)
	if conditionMatch > 0 {
		evidence = append(evidence, "Matching trust conditions detected")
		confidence += conditionMatch * 0.2
	}

	if confidence < sc.confidenceThresh {
		return nil
	}

	relationshipID := fmt.Sprintf("sec-%s-%s", generateSecurityHash(sourceRole.ID), generateSecurityHash(targetRole.ID))

	return &CrossAccountRelationship{
		ID:               relationshipID,
		SourceRole:       sourceRole,
		TargetRole:       targetRole,
		RelationshipType: "cross_account_trust",
		Confidence:       confidence,
		Evidence:         evidence,
		DetectedAt:       time.Now(),
	}
}

// analyzeConditionMatches analyzes matching conditions between roles
func (sc *SecurityCorrelator) analyzeConditionMatches(conditions1, conditions2 []TrustCondition) float64 {
	if len(conditions1) == 0 || len(conditions2) == 0 {
		return 0
	}

	matches := 0
	total := len(conditions1)

	for _, c1 := range conditions1 {
		for _, c2 := range conditions2 {
			if c1.Field == c2.Field && c1.Operator == c2.Operator {
				matches++
				break
			}
		}
	}

	return float64(matches) / float64(total)
}

// analyzeServicePrincipalRelationships analyzes service principal relationships
func (sc *SecurityCorrelator) analyzeServicePrincipalRelationships(roles []CrossAccountRole) ([]CrossAccountRelationship, error) {
	var relationships []CrossAccountRelationship

	// Find service principals that can assume roles across accounts
	for _, role := range roles {
		for _, principal := range role.TrustedPrincipals {
			if sc.isServicePrincipal(principal) {
				// Find other roles that might be related to this service principal
				relatedRoles := sc.findRolesForServicePrincipal(roles, principal)
				for _, relatedRole := range relatedRoles {
					if relatedRole.ID != role.ID {
						rel := &CrossAccountRelationship{
							ID:               fmt.Sprintf("sp-%s-%s", generateSecurityHash(role.ID), generateSecurityHash(relatedRole.ID)),
							SourceRole:       role,
							TargetRole:       relatedRole,
							RelationshipType: "service_principal_access",
							Confidence:       0.85,
							Evidence:         []string{fmt.Sprintf("Service principal %s can access both roles", principal)},
							DetectedAt:       time.Now(),
						}
						relationships = append(relationships, *rel)
					}
				}
			}
		}
	}

	return relationships, nil
}

// isServicePrincipal determines if a principal is a service principal
func (sc *SecurityCorrelator) isServicePrincipal(principal string) bool {
	servicePatterns := []string{
		"\\.amazonaws\\.com$",
		"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", // UUID pattern for Azure
		"@.*\\.iam\\.gserviceaccount\\.com$",                             // GCP service account
	}

	for _, pattern := range servicePatterns {
		if matched, _ := regexp.MatchString(pattern, principal); matched {
			return true
		}
	}

	return false
}

// findRolesForServicePrincipal finds roles that trust a specific service principal
func (sc *SecurityCorrelator) findRolesForServicePrincipal(roles []CrossAccountRole, principal string) []CrossAccountRole {
	var relatedRoles []CrossAccountRole

	for _, role := range roles {
		for _, trustedPrincipal := range role.TrustedPrincipals {
			if trustedPrincipal == principal {
				relatedRoles = append(relatedRoles, role)
				break
			}
		}
	}

	return relatedRoles
}

// analyzeRoleAssumptionChains analyzes multi-hop role assumption chains
func (sc *SecurityCorrelator) analyzeRoleAssumptionChains(roles []CrossAccountRole) ([]CrossAccountRelationship, error) {
	var relationships []CrossAccountRelationship

	// Build assumption graph
	assumptionGraph := sc.buildAssumptionGraph(roles)

	// Find chains longer than direct relationships
	for sourceRoleID, _ := range assumptionGraph {
		chains := sc.findAssumptionChains(assumptionGraph, sourceRoleID, 3) // Max depth of 3
		for _, chain := range chains {
			if len(chain) > 2 { // Only chains with intermediate hops
				sourceRole := sc.findRoleByID(roles, chain[0])
				targetRole := sc.findRoleByID(roles, chain[len(chain)-1])

				if sourceRole != nil && targetRole != nil {
					rel := &CrossAccountRelationship{
						ID:               fmt.Sprintf("chain-%s-%s", generateSecurityHash(sourceRole.ID), generateSecurityHash(targetRole.ID)),
						SourceRole:       *sourceRole,
						TargetRole:       *targetRole,
						RelationshipType: "role_assumption_chain",
						AssumptionChain:  chain,
						Confidence:       0.9,
						Evidence:         []string{fmt.Sprintf("Role assumption chain: %s", strings.Join(chain, " -> "))},
						DetectedAt:       time.Now(),
					}
					relationships = append(relationships, *rel)
				}
			}
		}
	}

	return relationships, nil
}

// buildAssumptionGraph builds a graph of role assumption relationships
func (sc *SecurityCorrelator) buildAssumptionGraph(roles []CrossAccountRole) map[string][]string {
	graph := make(map[string][]string)

	for _, role := range roles {
		for _, principal := range role.TrustedPrincipals {
			// Find roles that match this principal
			for _, otherRole := range roles {
				if sc.principalMatchesRole(principal, otherRole) {
					graph[otherRole.ID] = append(graph[otherRole.ID], role.ID)
				}
			}
		}
	}

	return graph
}

// principalMatchesRole determines if a principal can assume a role
func (sc *SecurityCorrelator) principalMatchesRole(principal string, role CrossAccountRole) bool {
	// Check if principal ARN matches role ARN pattern
	if strings.Contains(principal, "arn:aws:iam::") && strings.Contains(principal, ":role/") {
		return strings.Contains(principal, role.RoleName)
	}

	// Check for service principal matches
	if sc.isServicePrincipal(principal) {
		// Service principals can potentially assume roles in their account
		return role.AccountID != "" && strings.Contains(principal, role.AccountID)
	}

	return false
}

// findAssumptionChains finds assumption chains using DFS
func (sc *SecurityCorrelator) findAssumptionChains(graph map[string][]string, start string, maxDepth int) [][]string {
	var chains [][]string
	visited := make(map[string]bool)

	var dfs func(current string, path []string, depth int)
	dfs = func(current string, path []string, depth int) {
		if depth >= maxDepth || visited[current] {
			return
		}

		visited[current] = true
		newPath := append(path, current)

		if targets, exists := graph[current]; exists {
			for _, target := range targets {
				if !visited[target] {
					chains = append(chains, append(newPath, target))
					dfs(target, newPath, depth+1)
				}
			}
		}

		delete(visited, current)
	}

	dfs(start, []string{}, 0)
	return chains
}

// findRoleByID finds a role by its ID
func (sc *SecurityCorrelator) findRoleByID(roles []CrossAccountRole, id string) *CrossAccountRole {
	for _, role := range roles {
		if role.ID == id {
			return &role
		}
	}
	return nil
}

// detectPrivilegeEscalationPaths detects potential privilege escalation paths
func (sc *SecurityCorrelator) detectPrivilegeEscalationPaths(relationship CrossAccountRelationship) []EscalationPath {
	var paths []EscalationPath

	// Analyze trust policy conditions for weaknesses
	if len(relationship.SourceRole.Conditions) == 0 {
		paths = append(paths, EscalationPath{
			ID:          fmt.Sprintf("no-conditions-%s", generateSecurityHash(relationship.ID)),
			Steps:       []string{"Assume role without conditions", "Gain elevated permissions"},
			RiskLevel:   "HIGH",
			Description: "Role can be assumed without additional conditions",
			Mitigations: []string{"Add condition constraints", "Enable MFA requirements"},
		})
	}

	// Check for overly permissive principals
	for _, principal := range relationship.SourceRole.TrustedPrincipals {
		if principal == "*" {
			paths = append(paths, EscalationPath{
				ID:          fmt.Sprintf("wildcard-principal-%s", generateSecurityHash(relationship.ID)),
				Steps:       []string{"Use wildcard principal", "Assume role from any account", "Escalate privileges"},
				RiskLevel:   "CRITICAL",
				Description: "Role trusts any principal (*)",
				Mitigations: []string{"Specify explicit trusted principals", "Add condition constraints"},
			})
		}
	}

	// Check for cross-account admin permissions
	for _, permission := range relationship.TargetRole.Permissions {
		if sc.isAdminPermission(permission) {
			paths = append(paths, EscalationPath{
				ID:          fmt.Sprintf("admin-permissions-%s", generateSecurityHash(relationship.ID)),
				Steps:       []string{"Assume cross-account role", "Gain administrative permissions", "Full account access"},
				RiskLevel:   "HIGH",
				Description: "Cross-account role has administrative permissions",
				Mitigations: []string{"Apply principle of least privilege", "Use permission boundaries"},
			})
		}
	}

	// Check for role assumption chains
	if len(relationship.AssumptionChain) > 2 {
		paths = append(paths, EscalationPath{
			ID:          fmt.Sprintf("assumption-chain-%s", generateSecurityHash(relationship.ID)),
			Steps:       relationship.AssumptionChain,
			RiskLevel:   "MEDIUM",
			Description: "Multi-hop role assumption chain detected",
			Mitigations: []string{"Limit role assumption depth", "Monitor assumption chains"},
		})
	}

	return paths
}

// isAdminPermission determines if a permission grants administrative access
func (sc *SecurityCorrelator) isAdminPermission(permission Permission) bool {
	adminPatterns := []string{
		"\\*:FullAccess",
		"\\*:\\*",
		".*:Admin.*",
		".*:FullAccess",
		"iam:.*",
		"organizations:.*",
	}

	for _, action := range permission.Actions {
		for _, pattern := range adminPatterns {
			if matched, _ := regexp.MatchString(pattern, action); matched {
				return true
			}
		}
	}

	return false
}

// calculateSecurityRiskScore calculates a risk score for the security relationship
func (sc *SecurityCorrelator) calculateSecurityRiskScore(relationship CrossAccountRelationship) float64 {
	var score float64

	// Base score from escalation paths
	for _, path := range relationship.EscalationPaths {
		switch path.RiskLevel {
		case "CRITICAL":
			score += 0.4
		case "HIGH":
			score += 0.3
		case "MEDIUM":
			score += 0.2
		case "LOW":
			score += 0.1
		}
	}

	// Add score for cross-provider relationships (higher risk)
	if relationship.SourceRole.Provider != relationship.TargetRole.Provider {
		score += 0.2
	}

	// Add score for assumption chains
	if len(relationship.AssumptionChain) > 2 {
		score += 0.1 * float64(len(relationship.AssumptionChain)-2)
	}

	// Normalize to 0-1 range
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// generateSecurityHash generates a hash for security-related IDs
func generateSecurityHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)[:8]
}

// PersistSecurityRelationships saves security relationships to database
func (sc *SecurityCorrelator) PersistSecurityRelationships(ctx context.Context, relationships []CrossAccountRelationship) error {
	if len(relationships) == 0 {
		return nil
	}

	tx, err := sc.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT OR REPLACE INTO cross_cloud_correlations (
		id, source_resource_id, source_provider, source_region, source_account_id, source_resource_type,
		target_resource_id, target_provider, target_region, target_account_id, target_resource_type,
		correlation_type, correlation_subtype, correlation_method, confidence_score,
		evidence, matching_attributes, description, status, verified,
		discovered_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, rel := range relationships {
		evidenceJSON, _ := json.Marshal(rel.Evidence)
		metadataJSON, _ := json.Marshal(map[string]interface{}{
			"risk_score":       rel.RiskScore,
			"escalation_paths": rel.EscalationPaths,
			"assumption_chain": rel.AssumptionChain,
		})

		_, err := stmt.Exec(
			rel.ID,
			rel.SourceRole.ID, rel.SourceRole.Provider, rel.SourceRole.Region, rel.SourceRole.AccountID, "security_role",
			rel.TargetRole.ID, rel.TargetRole.Provider, rel.TargetRole.Region, rel.TargetRole.AccountID, "security_role",
			"security_relationship", rel.RelationshipType, "automated_analysis", rel.Confidence,
			string(evidenceJSON), string(metadataJSON),
			fmt.Sprintf("Cross-account security relationship between %s and %s", rel.SourceRole.Provider, rel.TargetRole.Provider),
			"active", false,
			rel.DetectedAt, time.Now(), time.Now(),
		)
		if err != nil {
			sc.logger.Printf("Error persisting security relationship %s: %v", rel.ID, err)
			continue
		}
	}

	return tx.Commit()
}
