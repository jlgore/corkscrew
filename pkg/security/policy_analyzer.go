package security

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PolicyAnalyzer handles IAM policy similarity analysis across clouds
type PolicyAnalyzer struct {
	db               DatabaseInterface
	logger           *log.Logger
	similarityThresh float64
}

// DatabaseInterface defines database operations needed by the policy analyzer
type DatabaseInterface interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (RowsInterface, error)
	BeginTx(ctx context.Context, opts interface{}) (TxInterface, error)
}

type RowsInterface interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close() error
}

type TxInterface interface {
	Prepare(query string) (StmtInterface, error)
	Commit() error
	Rollback() error
}

type StmtInterface interface {
	Exec(args ...interface{}) (interface{}, error)
	Close() error
}

// NewPolicyAnalyzer creates a new policy analyzer
func NewPolicyAnalyzer(db DatabaseInterface, logger *log.Logger) *PolicyAnalyzer {
	return &PolicyAnalyzer{
		db:               db,
		logger:           logger,
		similarityThresh: 0.75,
	}
}

// PolicyDocument represents a parsed policy document
type PolicyDocument struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // inline, managed, resource
	Provider    string                 `json:"provider"`
	Region      string                 `json:"region"`
	AccountID   string                 `json:"account_id"`
	ResourceID  string                 `json:"resource_id"`
	Document    map[string]interface{} `json:"document"`
	Statements  []PolicyStatement      `json:"statements"`
	Permissions []Permission           `json:"permissions"`
	Hash        string                 `json:"hash"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// PolicyStatement represents an individual policy statement
type PolicyStatement struct {
	Sid       string      `json:"sid"`
	Effect    string      `json:"effect"`
	Principal interface{} `json:"principal,omitempty"`
	Action    interface{} `json:"action"`
	Resource  interface{} `json:"resource"`
	Condition interface{} `json:"condition,omitempty"`
}

// Permission represents a normalized permission
type Permission struct {
	Effect    string   `json:"effect"`
	Actions   []string `json:"actions"`
	Resources []string `json:"resources"`
	Principal string   `json:"principal,omitempty"`
}

// PolicySimilarity represents similarity between two policies
type PolicySimilarity struct {
	ID               string                 `json:"id"`
	SourcePolicy     PolicyDocument         `json:"source_policy"`
	TargetPolicy     PolicyDocument         `json:"target_policy"`
	SimilarityScore  float64                `json:"similarity_score"`
	SimilarityType   string                 `json:"similarity_type"`
	MatchingElements []string               `json:"matching_elements"`
	Differences      []string               `json:"differences"`
	RiskAssessment   PolicyRiskAssessment   `json:"risk_assessment"`
	Metadata         map[string]interface{} `json:"metadata"`
	DetectedAt       time.Time              `json:"detected_at"`
}

// PolicyRiskAssessment represents risk assessment for policy similarities
type PolicyRiskAssessment struct {
	RiskLevel       string   `json:"risk_level"`      // LOW, MEDIUM, HIGH, CRITICAL
	RiskScore       float64  `json:"risk_score"`      // 0-1
	RiskFactors     []string `json:"risk_factors"`    // List of risk factors
	Recommendations []string `json:"recommendations"` // Security recommendations
	ComplianceTags  []string `json:"compliance_tags"` // Compliance framework tags
}

// AnalyzePolicySimilarity analyzes policy similarities across clouds
func (pa *PolicyAnalyzer) AnalyzePolicySimilarity(ctx context.Context) ([]PolicySimilarity, error) {
	var similarities []PolicySimilarity

	// Extract policies from all clouds
	policies, err := pa.extractPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract policies: %w", err)
	}

	pa.logger.Printf("Found %d policies across clouds", len(policies))

	// Normalize and parse policies
	for i := range policies {
		policies[i] = pa.normalizePolicy(policies[i])
	}

	// Compare policies pairwise
	for i, sourcePolicy := range policies {
		for j, targetPolicy := range policies {
			if i >= j {
				continue
			}

			similarity := pa.comparePolicies(sourcePolicy, targetPolicy)
			if similarity != nil && similarity.SimilarityScore >= pa.similarityThresh {
				similarities = append(similarities, *similarity)
			}
		}
	}

	// Sort by similarity score (highest first)
	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].SimilarityScore > similarities[j].SimilarityScore
	})

	pa.logger.Printf("Found %d policy similarities", len(similarities))
	return similarities, nil
}

// extractPolicies extracts policy documents from all cloud resources
func (pa *PolicyAnalyzer) extractPolicies(ctx context.Context) ([]PolicyDocument, error) {
	var policies []PolicyDocument

	// Extract AWS policies
	awsPolicies, err := pa.extractAWSPolicies(ctx)
	if err != nil {
		pa.logger.Printf("Error extracting AWS policies: %v", err)
	} else {
		policies = append(policies, awsPolicies...)
	}

	// Extract Azure policies
	azurePolicies, err := pa.extractAzurePolicies(ctx)
	if err != nil {
		pa.logger.Printf("Error extracting Azure policies: %v", err)
	} else {
		policies = append(policies, azurePolicies...)
	}

	return policies, nil
}

// extractAWSPolicies extracts AWS IAM policies
func (pa *PolicyAnalyzer) extractAWSPolicies(ctx context.Context) ([]PolicyDocument, error) {
	var policies []PolicyDocument

	query := `
	SELECT id, name, type, raw_data, region, account_id
	FROM aws_resources 
	WHERE type IN ('AWS::IAM::Policy', 'AWS::IAM::Role', 'AWS::IAM::User', 'AWS::S3::BucketPolicy')
	  AND raw_data IS NOT NULL
	`

	rows, err := pa.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, resourceType, rawDataStr, region, accountID string
		if err := rows.Scan(&id, &name, &resourceType, &rawDataStr, &region, &accountID); err != nil {
			continue
		}

		var rawData map[string]interface{}
		if err := json.Unmarshal([]byte(rawDataStr), &rawData); err != nil {
			continue
		}

		extractedPolicies := pa.parseAWSPolicies(id, name, resourceType, rawData, region, accountID)
		policies = append(policies, extractedPolicies...)
	}

	return policies, nil
}

// parseAWSPolicies parses AWS policies from resource data
func (pa *PolicyAnalyzer) parseAWSPolicies(id, name, resourceType string, rawData map[string]interface{}, region, accountID string) []PolicyDocument {
	var policies []PolicyDocument

	base := PolicyDocument{
		Provider:   "aws",
		Region:     region,
		AccountID:  accountID,
		ResourceID: id,
		Metadata:   rawData,
	}

	switch resourceType {
	case "AWS::IAM::Policy":
		if policyDoc, ok := rawData["PolicyDocument"].(string); ok {
			policy := base
			policy.ID = fmt.Sprintf("aws-policy-%s", id)
			policy.Name = name
			policy.Type = "managed"
			if err := json.Unmarshal([]byte(policyDoc), &policy.Document); err == nil {
				policies = append(policies, policy)
			}
		}

	case "AWS::IAM::Role":
		// Trust policy
		if trustPolicyDoc, ok := rawData["AssumeRolePolicyDocument"].(string); ok {
			policy := base
			policy.ID = fmt.Sprintf("aws-trust-%s", id)
			policy.Name = fmt.Sprintf("%s-trust", name)
			policy.Type = "trust"
			if err := json.Unmarshal([]byte(trustPolicyDoc), &policy.Document); err == nil {
				policies = append(policies, policy)
			}
		}

		// Attached policies (inline)
		if inlinePolicies, ok := rawData["RolePolicyList"].([]interface{}); ok {
			for i, p := range inlinePolicies {
				if policyMap, ok := p.(map[string]interface{}); ok {
					if policyDoc, ok := policyMap["PolicyDocument"].(string); ok {
						policy := base
						policy.ID = fmt.Sprintf("aws-inline-%s-%d", id, i)
						policy.Name = fmt.Sprintf("%s-inline-%d", name, i)
						policy.Type = "inline"
						if err := json.Unmarshal([]byte(policyDoc), &policy.Document); err == nil {
							policies = append(policies, policy)
						}
					}
				}
			}
		}

	case "AWS::S3::BucketPolicy":
		if policy, ok := rawData["Policy"].(string); ok {
			policyDoc := base
			policyDoc.ID = fmt.Sprintf("aws-bucket-%s", id)
			policyDoc.Name = fmt.Sprintf("%s-bucket-policy", name)
			policyDoc.Type = "resource"
			if err := json.Unmarshal([]byte(policy), &policyDoc.Document); err == nil {
				policies = append(policies, policyDoc)
			}
		}
	}

	return policies
}

// extractAzurePolicies extracts Azure policies and role definitions
func (pa *PolicyAnalyzer) extractAzurePolicies(ctx context.Context) ([]PolicyDocument, error) {
	var policies []PolicyDocument

	query := `
	SELECT id, name, type, raw_data, location, subscription_id
	FROM azure_resources 
	WHERE type IN ('Microsoft.Authorization/policyDefinitions', 'Microsoft.Authorization/roleDefinitions', 'Microsoft.Authorization/roleAssignments')
	  AND raw_data IS NOT NULL
	`

	rows, err := pa.db.QueryContext(ctx, query)
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

		extractedPolicies := pa.parseAzurePolicies(id, name, resourceType, rawData, location, subscriptionID)
		policies = append(policies, extractedPolicies...)
	}

	return policies, nil
}

// parseAzurePolicies parses Azure policies from resource data
func (pa *PolicyAnalyzer) parseAzurePolicies(id, name, resourceType string, rawData map[string]interface{}, location, subscriptionID string) []PolicyDocument {
	var policies []PolicyDocument

	base := PolicyDocument{
		Provider:   "azure",
		Region:     location,
		AccountID:  subscriptionID,
		ResourceID: id,
		Metadata:   rawData,
	}

	switch resourceType {
	case "Microsoft.Authorization/policyDefinitions":
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			if policyRule, ok := props["policyRule"].(map[string]interface{}); ok {
				policy := base
				policy.ID = fmt.Sprintf("azure-policy-%s", id)
				policy.Name = name
				policy.Type = "managed"
				policy.Document = policyRule
				policies = append(policies, policy)
			}
		}

	case "Microsoft.Authorization/roleDefinitions":
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			policy := base
			policy.ID = fmt.Sprintf("azure-role-%s", id)
			policy.Name = name
			policy.Type = "role"
			policy.Document = props
			policies = append(policies, policy)
		}
	}

	return policies
}

// normalizePolicy normalizes policy documents for comparison
func (pa *PolicyAnalyzer) normalizePolicy(policy PolicyDocument) PolicyDocument {
	// Parse statements from document
	policy.Statements = pa.parseStatements(policy.Document)

	// Convert to normalized permissions
	policy.Permissions = pa.extractPermissions(policy.Statements, policy.Provider)

	// Generate hash for quick comparison
	policy.Hash = pa.generatePolicyHash(policy.Permissions)

	return policy
}

// parseStatements parses policy statements from document
func (pa *PolicyAnalyzer) parseStatements(document map[string]interface{}) []PolicyStatement {
	var statements []PolicyStatement

	// Handle AWS format
	if stmtList, ok := document["Statement"]; ok {
		switch stmt := stmtList.(type) {
		case []interface{}:
			for _, s := range stmt {
				if stmtMap, ok := s.(map[string]interface{}); ok {
					statements = append(statements, pa.parseStatement(stmtMap))
				}
			}
		case map[string]interface{}:
			statements = append(statements, pa.parseStatement(stmt))
		}
	}

	// Handle Azure format
	if ifBlock, ok := document["if"]; ok {
		if thenBlock, ok := document["then"]; ok {
			statement := PolicyStatement{
				Effect:    "Deny", // Azure policies are typically deny by default
				Action:    ifBlock,
				Resource:  "*",
				Condition: thenBlock,
			}
			statements = append(statements, statement)
		}
	}

	return statements
}

// parseStatement parses individual policy statement
func (pa *PolicyAnalyzer) parseStatement(stmt map[string]interface{}) PolicyStatement {
	statement := PolicyStatement{}

	if sid, ok := stmt["Sid"].(string); ok {
		statement.Sid = sid
	}

	if effect, ok := stmt["Effect"].(string); ok {
		statement.Effect = effect
	}

	if principal := stmt["Principal"]; principal != nil {
		statement.Principal = principal
	}

	if action := stmt["Action"]; action != nil {
		statement.Action = action
	}

	if resource := stmt["Resource"]; resource != nil {
		statement.Resource = resource
	}

	if condition := stmt["Condition"]; condition != nil {
		statement.Condition = condition
	}

	return statement
}

// extractPermissions extracts normalized permissions from statements
func (pa *PolicyAnalyzer) extractPermissions(statements []PolicyStatement, provider string) []Permission {
	var permissions []Permission

	for _, stmt := range statements {
		perm := Permission{
			Effect: stmt.Effect,
		}

		// Extract actions
		perm.Actions = pa.extractStringList(stmt.Action)

		// Extract resources
		perm.Resources = pa.extractStringList(stmt.Resource)

		// Extract principal (if present)
		if stmt.Principal != nil {
			principals := pa.extractStringList(stmt.Principal)
			if len(principals) > 0 {
				perm.Principal = principals[0] // Take first principal for simplicity
			}
		}

		// Normalize actions for cross-cloud comparison
		perm.Actions = pa.normalizeActions(perm.Actions, provider)

		permissions = append(permissions, perm)
	}

	return permissions
}

// extractStringList extracts string list from various JSON formats
func (pa *PolicyAnalyzer) extractStringList(value interface{}) []string {
	var result []string

	switch v := value.(type) {
	case string:
		result = append(result, v)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	case []string:
		result = v
	case map[string]interface{}:
		// Handle principals in object format
		for _, val := range v {
			if str, ok := val.(string); ok {
				result = append(result, str)
			} else if list, ok := val.([]interface{}); ok {
				for _, item := range list {
					if str, ok := item.(string); ok {
						result = append(result, str)
					}
				}
			}
		}
	}

	return result
}

// normalizeActions normalizes actions for cross-cloud comparison
func (pa *PolicyAnalyzer) normalizeActions(actions []string, provider string) []string {
	var normalized []string

	actionMap := map[string]map[string]string{
		"aws": {
			"s3:GetObject":           "storage:read",
			"s3:PutObject":           "storage:write",
			"s3:DeleteObject":        "storage:delete",
			"s3:ListBucket":          "storage:list",
			"iam:GetUser":            "identity:read",
			"iam:CreateUser":         "identity:write",
			"iam:DeleteUser":         "identity:delete",
			"ec2:DescribeInstances":  "compute:read",
			"ec2:RunInstances":       "compute:create",
			"ec2:TerminateInstances": "compute:delete",
		},
		"azure": {
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read":   "storage:read",
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write":  "storage:write",
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete": "storage:delete",
			"Microsoft.Authorization/*/read":                                         "identity:read",
			"Microsoft.Authorization/*/write":                                        "identity:write",
			"Microsoft.Compute/virtualMachines/read":                                 "compute:read",
			"Microsoft.Compute/virtualMachines/write":                                "compute:create",
			"Microsoft.Compute/virtualMachines/delete":                               "compute:delete",
		},
	}

	for _, action := range actions {
		if providerMap, ok := actionMap[provider]; ok {
			if normalizedAction, ok := providerMap[action]; ok {
				normalized = append(normalized, normalizedAction)
				continue
			}
		}

		// If no specific mapping, try pattern matching
		normalizedAction := pa.normalizeActionByPattern(action, provider)
		normalized = append(normalized, normalizedAction)
	}

	return normalized
}

// normalizeActionByPattern normalizes actions using pattern matching
func (pa *PolicyAnalyzer) normalizeActionByPattern(action, provider string) string {
	action = strings.ToLower(action)

	// Storage operations
	if matched, _ := regexp.MatchString(".*storage.*read.*|.*get.*object.*|.*blob.*read.*", action); matched {
		return "storage:read"
	}
	if matched, _ := regexp.MatchString(".*storage.*write.*|.*put.*object.*|.*blob.*write.*", action); matched {
		return "storage:write"
	}
	if matched, _ := regexp.MatchString(".*storage.*delete.*|.*delete.*object.*|.*blob.*delete.*", action); matched {
		return "storage:delete"
	}

	// Compute operations
	if matched, _ := regexp.MatchString(".*compute.*|.*instance.*|.*virtual.*machine.*", action); matched {
		if matched, _ := regexp.MatchString(".*read.*|.*describe.*|.*get.*", action); matched {
			return "compute:read"
		}
		if matched, _ := regexp.MatchString(".*create.*|.*run.*|.*start.*", action); matched {
			return "compute:create"
		}
		if matched, _ := regexp.MatchString(".*delete.*|.*terminate.*|.*stop.*", action); matched {
			return "compute:delete"
		}
	}

	// Identity operations
	if matched, _ := regexp.MatchString(".*iam.*|.*identity.*|.*authorization.*", action); matched {
		if matched, _ := regexp.MatchString(".*read.*|.*get.*|.*list.*", action); matched {
			return "identity:read"
		}
		if matched, _ := regexp.MatchString(".*write.*|.*create.*|.*put.*", action); matched {
			return "identity:write"
		}
		if matched, _ := regexp.MatchString(".*delete.*", action); matched {
			return "identity:delete"
		}
	}

	// Return original if no pattern matches
	return action
}

// generatePolicyHash generates a hash for policy comparison
func (pa *PolicyAnalyzer) generatePolicyHash(permissions []Permission) string {
	// Sort permissions for consistent hashing
	sort.Slice(permissions, func(i, j int) bool {
		return fmt.Sprintf("%v", permissions[i]) < fmt.Sprintf("%v", permissions[j])
	})

	data, _ := json.Marshal(permissions)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// comparePolicies compares two policies for similarity
func (pa *PolicyAnalyzer) comparePolicies(source, target PolicyDocument) *PolicySimilarity {
	// Skip same policy
	if source.ID == target.ID {
		return nil
	}

	// Quick hash comparison
	if source.Hash == target.Hash {
		return &PolicySimilarity{
			ID:               fmt.Sprintf("policy-sim-%s-%s", generateHash(source.ID), generateHash(target.ID)),
			SourcePolicy:     source,
			TargetPolicy:     target,
			SimilarityScore:  1.0,
			SimilarityType:   "identical",
			MatchingElements: []string{"Identical policies"},
			DetectedAt:       time.Now(),
		}
	}

	// Detailed comparison
	similarity := pa.calculateDetailedSimilarity(source, target)
	if similarity.SimilarityScore > 0 {
		similarity.ID = fmt.Sprintf("policy-sim-%s-%s", generateHash(source.ID), generateHash(target.ID))
		similarity.SourcePolicy = source
		similarity.TargetPolicy = target
		similarity.RiskAssessment = pa.assessPolicyRisk(similarity)
		similarity.DetectedAt = time.Now()
		return &similarity
	}

	return nil
}

// calculateDetailedSimilarity calculates detailed similarity between policies
func (pa *PolicyAnalyzer) calculateDetailedSimilarity(source, target PolicyDocument) PolicySimilarity {
	var similarity PolicySimilarity
	var score float64
	var matchingElements []string
	var differences []string

	// Compare permissions
	sourcePerms := make(map[string]bool)
	for _, perm := range source.Permissions {
		key := fmt.Sprintf("%s:%v:%v", perm.Effect, perm.Actions, perm.Resources)
		sourcePerms[key] = true
	}

	targetPerms := make(map[string]bool)
	for _, perm := range target.Permissions {
		key := fmt.Sprintf("%s:%v:%v", perm.Effect, perm.Actions, perm.Resources)
		targetPerms[key] = true
	}

	// Calculate intersection and union
	intersection := 0
	for key := range sourcePerms {
		if targetPerms[key] {
			intersection++
			matchingElements = append(matchingElements, fmt.Sprintf("Matching permission: %s", key))
		} else {
			differences = append(differences, fmt.Sprintf("Source only: %s", key))
		}
	}

	for key := range targetPerms {
		if !sourcePerms[key] {
			differences = append(differences, fmt.Sprintf("Target only: %s", key))
		}
	}

	union := len(sourcePerms) + len(targetPerms) - intersection
	if union > 0 {
		score = float64(intersection) / float64(union)
	}

	// Boost score for cross-cloud similarities
	if source.Provider != target.Provider && score > 0.3 {
		score += 0.2
		matchingElements = append(matchingElements, "Cross-cloud policy similarity")
	}

	similarity.SimilarityScore = score
	similarity.MatchingElements = matchingElements
	similarity.Differences = differences

	// Determine similarity type
	if score >= 0.9 {
		similarity.SimilarityType = "nearly_identical"
	} else if score >= 0.7 {
		similarity.SimilarityType = "highly_similar"
	} else if score >= 0.5 {
		similarity.SimilarityType = "moderately_similar"
	} else {
		similarity.SimilarityType = "low_similarity"
	}

	return similarity
}

// assessPolicyRisk assesses risk associated with policy similarities
func (pa *PolicyAnalyzer) assessPolicyRisk(similarity PolicySimilarity) PolicyRiskAssessment {
	assessment := PolicyRiskAssessment{
		RiskScore: 0.0,
	}

	// Cross-cloud similarities are higher risk
	if similarity.SourcePolicy.Provider != similarity.TargetPolicy.Provider {
		assessment.RiskScore += 0.3
		assessment.RiskFactors = append(assessment.RiskFactors, "Cross-cloud policy similarity")
		assessment.Recommendations = append(assessment.Recommendations, "Review cross-cloud access patterns")
	}

	// High similarity with broad permissions is risky
	if similarity.SimilarityScore > 0.8 {
		assessment.RiskScore += 0.2
		assessment.RiskFactors = append(assessment.RiskFactors, "High policy similarity")

		// Check for broad permissions
		for _, perm := range similarity.SourcePolicy.Permissions {
			if pa.hasBroadPermissions(perm) {
				assessment.RiskScore += 0.3
				assessment.RiskFactors = append(assessment.RiskFactors, "Broad permissions detected")
				assessment.Recommendations = append(assessment.Recommendations, "Apply principle of least privilege")
				break
			}
		}
	}

	// Trust policies with external accounts are risky
	if similarity.SourcePolicy.Type == "trust" || similarity.TargetPolicy.Type == "trust" {
		assessment.RiskScore += 0.2
		assessment.RiskFactors = append(assessment.RiskFactors, "Trust policy similarity")
		assessment.Recommendations = append(assessment.Recommendations, "Review trust relationships")
	}

	// Determine risk level
	if assessment.RiskScore >= 0.8 {
		assessment.RiskLevel = "CRITICAL"
	} else if assessment.RiskScore >= 0.6 {
		assessment.RiskLevel = "HIGH"
	} else if assessment.RiskScore >= 0.4 {
		assessment.RiskLevel = "MEDIUM"
	} else {
		assessment.RiskLevel = "LOW"
	}

	// Add general recommendations
	assessment.Recommendations = append(assessment.Recommendations, "Monitor policy usage", "Regular policy review")

	// Add compliance tags
	assessment.ComplianceTags = []string{"CIS", "SOC2", "PCI-DSS"}

	return assessment
}

// hasBroadPermissions checks if permission has broad scope
func (pa *PolicyAnalyzer) hasBroadPermissions(perm Permission) bool {
	// Check for wildcard actions
	for _, action := range perm.Actions {
		if action == "*" || strings.HasSuffix(action, ":*") {
			return true
		}
	}

	// Check for wildcard resources
	for _, resource := range perm.Resources {
		if resource == "*" {
			return true
		}
	}

	return false
}

// generateHash generates a hash for string input
func generateHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)[:8]
}

// PersistPolicySimilarities saves policy similarities to database
func (pa *PolicyAnalyzer) PersistPolicySimilarities(ctx context.Context, similarities []PolicySimilarity) error {
	if len(similarities) == 0 {
		return nil
	}

	tx, err := pa.db.BeginTx(ctx, nil)
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

	for _, sim := range similarities {
		evidenceJSON, _ := json.Marshal(sim.MatchingElements)
		metadataJSON, _ := json.Marshal(map[string]interface{}{
			"similarity_type": sim.SimilarityType,
			"differences":     sim.Differences,
			"risk_assessment": sim.RiskAssessment,
		})

		_, err := stmt.Exec(
			sim.ID,
			sim.SourcePolicy.ID, sim.SourcePolicy.Provider, sim.SourcePolicy.Region, sim.SourcePolicy.AccountID, "policy",
			sim.TargetPolicy.ID, sim.TargetPolicy.Provider, sim.TargetPolicy.Region, sim.TargetPolicy.AccountID, "policy",
			"policy_similarity", sim.SimilarityType, "automated_analysis", sim.SimilarityScore,
			string(evidenceJSON), string(metadataJSON),
			fmt.Sprintf("Policy similarity between %s (%s) and %s (%s)", sim.SourcePolicy.Name, sim.SourcePolicy.Provider, sim.TargetPolicy.Name, sim.TargetPolicy.Provider),
			"active", false,
			sim.DetectedAt, time.Now(), time.Now(),
		)
		if err != nil {
			pa.logger.Printf("Error persisting policy similarity %s: %v", sim.ID, err)
			continue
		}
	}

	return tx.Commit()
}
