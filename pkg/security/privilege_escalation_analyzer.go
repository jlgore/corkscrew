package security

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// PrivilegeEscalationAnalyzer analyzes privilege escalation paths across clouds
type PrivilegeEscalationAnalyzer struct {
	db               DatabaseInterface
	logger           *log.Logger
	riskThreshold    float64
}

// NewPrivilegeEscalationAnalyzer creates a new privilege escalation analyzer
func NewPrivilegeEscalationAnalyzer(db DatabaseInterface, logger *log.Logger) *PrivilegeEscalationAnalyzer {
	return &PrivilegeEscalationAnalyzer{
		db:            db,
		logger:        logger,
		riskThreshold: 0.6,
	}
}

// EscalationPath represents a privilege escalation path
type EscalationPath struct {
	ID                 string                 `json:"id"`
	RelationshipID     string                 `json:"relationship_id"`
	RelationshipType   string                 `json:"relationship_type"`
	PathType           string                 `json:"path_type"`
	EscalationSteps    []EscalationStep       `json:"escalation_steps"`
	EntryPoint         string                 `json:"entry_point"`
	TargetPrivilege    string                 `json:"target_privilege"`
	StepCount          int                    `json:"step_count"`
	ComplexityScore    float64                `json:"complexity_score"`
	FeasibilityScore   float64                `json:"feasibility_score"`
	RiskLevel          string                 `json:"risk_level"`
	RiskScore          float64                `json:"risk_score"`
	ImpactScore        float64                `json:"impact_score"`
	LikelihoodScore    float64                `json:"likelihood_score"`
	AttackVectors      []AttackVector         `json:"attack_vectors"`
	Prerequisites      []string               `json:"prerequisites"`
	Indicators         []string               `json:"indicators"`
	SourceProvider     string                 `json:"source_provider"`
	TargetProvider     string                 `json:"target_provider"`
	SourceAccountID    string                 `json:"source_account_id"`
	TargetAccountID    string                 `json:"target_account_id"`
	AffectedResources  []string               `json:"affected_resources"`
	Mitigations        []Mitigation           `json:"mitigations"`
	Controls           []SecurityControl      `json:"controls"`
	DetectionMethods   []DetectionMethod      `json:"detection_methods"`
	ResponseProcedures []ResponseProcedure    `json:"response_procedures"`
	ComplianceViolations []ComplianceViolation `json:"compliance_violations"`
	FrameworkMappings  []FrameworkMapping     `json:"framework_mappings"`
	DetectedAt         time.Time              `json:"detected_at"`
}

// EscalationStep represents a single step in an escalation path
type EscalationStep struct {
	StepNumber     int                    `json:"step_number"`
	Action         string                 `json:"action"`
	Description    string                 `json:"description"`
	Resource       string                 `json:"resource"`
	Permission     string                 `json:"permission"`
	Method         string                 `json:"method"`
	Prerequisites  []string               `json:"prerequisites"`
	Indicators     []string               `json:"indicators"`
	RiskScore      float64                `json:"risk_score"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// AttackVector represents a potential attack vector
type AttackVector struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	MITREID       string   `json:"mitre_id"`
	Techniques    []string `json:"techniques"`
	Platforms     []string `json:"platforms"`
	Prerequisites []string `json:"prerequisites"`
	Indicators    []string `json:"indicators"`
	RiskScore     float64  `json:"risk_score"`
}

// Mitigation represents a security mitigation
type Mitigation struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Type         string   `json:"type"` // preventive, detective, corrective
	Priority     string   `json:"priority"`
	Effort       string   `json:"effort"` // low, medium, high
	Effectiveness float64 `json:"effectiveness"`
	Implementation string `json:"implementation"`
}

// SecurityControl represents a security control
type SecurityControl struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // technical, administrative, physical
	Category    string `json:"category"`
	Framework   string `json:"framework"`
	Implemented bool   `json:"implemented"`
}

// DetectionMethod represents a detection method
type DetectionMethod struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"` // log_analysis, behavior_analysis, signature_based
	Tools       []string `json:"tools"`
	Indicators  []string `json:"indicators"`
	Accuracy    float64  `json:"accuracy"`
}

// ResponseProcedure represents an incident response procedure
type ResponseProcedure struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Phase       string   `json:"phase"` // preparation, identification, containment, eradication, recovery
	Steps       []string `json:"steps"`
	Stakeholders []string `json:"stakeholders"`
	Timeline    string   `json:"timeline"`
}

// ComplianceViolation represents a compliance violation
type ComplianceViolation struct {
	Framework   string `json:"framework"`
	ControlID   string `json:"control_id"`
	ControlName string `json:"control_name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// FrameworkMapping represents a security framework mapping
type FrameworkMapping struct {
	Framework   string `json:"framework"`
	TechniqueID string `json:"technique_id"`
	TacticID    string `json:"tactic_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AnalyzePrivilegeEscalationPaths analyzes privilege escalation paths across all correlations
func (pea *PrivilegeEscalationAnalyzer) AnalyzePrivilegeEscalationPaths(ctx context.Context) ([]EscalationPath, error) {
	var escalationPaths []EscalationPath

	// Analyze identity federation escalation paths
	identityPaths, err := pea.analyzeIdentityFederationEscalation(ctx)
	if err != nil {
		pea.logger.Printf("Error analyzing identity federation escalation: %v", err)
	} else {
		escalationPaths = append(escalationPaths, identityPaths...)
	}

	// Analyze security role escalation paths
	rolePaths, err := pea.analyzeSecurityRoleEscalation(ctx)
	if err != nil {
		pea.logger.Printf("Error analyzing security role escalation: %v", err)
	} else {
		escalationPaths = append(escalationPaths, rolePaths...)
	}

	// Analyze policy similarity escalation paths
	policyPaths, err := pea.analyzePolicySimilarityEscalation(ctx)
	if err != nil {
		pea.logger.Printf("Error analyzing policy similarity escalation: %v", err)
	} else {
		escalationPaths = append(escalationPaths, policyPaths...)
	}

	// Analyze certificate-based escalation paths
	certPaths, err := pea.analyzeCertificateEscalation(ctx)
	if err != nil {
		pea.logger.Printf("Error analyzing certificate escalation: %v", err)
	} else {
		escalationPaths = append(escalationPaths, certPaths...)
	}

	// Enhance paths with additional analysis
	for i := range escalationPaths {
		escalationPaths[i] = pea.enhanceEscalationPath(escalationPaths[i])
	}

	pea.logger.Printf("Found %d privilege escalation paths", len(escalationPaths))
	return escalationPaths, nil
}

// analyzeIdentityFederationEscalation analyzes escalation paths from identity federation
func (pea *PrivilegeEscalationAnalyzer) analyzeIdentityFederationEscalation(ctx context.Context) ([]EscalationPath, error) {
	var paths []EscalationPath

	query := `
	SELECT id, source_cloud_provider, target_cloud_provider, source_account_id, target_account_id,
	       federation_type, trust_conditions, security_risk_level, security_risk_score
	FROM identity_federation_relationships
	WHERE status = 'active' AND security_risk_score >= ?
	`

	rows, err := pea.db.QueryContext(ctx, query, pea.riskThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, sourceProvider, targetProvider, sourceAccount, targetAccount string
		var federationType, riskLevel string
		var trustConditionsStr string
		var riskScore float64

		if err := rows.Scan(&id, &sourceProvider, &targetProvider, &sourceAccount, &targetAccount,
			&federationType, &trustConditionsStr, &riskLevel, &riskScore); err != nil {
			continue
		}

		// Parse trust conditions
		var trustConditions []interface{}
		if trustConditionsStr != "" {
			json.Unmarshal([]byte(trustConditionsStr), &trustConditions)
		}

		// Generate escalation paths based on federation type
		switch federationType {
		case "oidc_federation":
			paths = append(paths, pea.generateOIDCEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, trustConditions, riskScore)...)
		case "saml_federation":
			paths = append(paths, pea.generateSAMLEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, trustConditions, riskScore)...)
		case "trust_policy":
			paths = append(paths, pea.generateTrustPolicyEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, trustConditions, riskScore)...)
		}
	}

	return paths, nil
}

// generateOIDCEscalationPaths generates escalation paths for OIDC federation
func (pea *PrivilegeEscalationAnalyzer) generateOIDCEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount string, conditions []interface{}, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	// Basic OIDC token escalation path
	steps := []EscalationStep{
		{
			StepNumber:  1,
			Action:      "obtain_oidc_token",
			Description: "Obtain OIDC token from identity provider",
			Resource:    "OIDC Identity Provider",
			Permission:  "authenticate",
			Method:      "OIDC authentication flow",
			Prerequisites: []string{"Valid credentials", "Network access to IdP"},
			Indicators:    []string{"Authentication logs", "Token issuance events"},
			RiskScore:     0.3,
		},
		{
			StepNumber:  2,
			Action:      "assume_cross_cloud_role",
			Description: "Use OIDC token to assume role in target cloud",
			Resource:    "Cross-cloud IAM role",
			Permission:  "sts:AssumeRoleWithWebIdentity",
			Method:      "STS AssumeRoleWithWebIdentity",
			Prerequisites: []string{"Valid OIDC token", "Trust relationship configured"},
			Indicators:    []string{"AssumeRole API calls", "Cross-cloud authentication"},
			RiskScore:     0.6,
		},
		{
			StepNumber:  3,
			Action:      "escalate_privileges",
			Description: "Use assumed role to gain additional privileges",
			Resource:    "Target cloud resources",
			Permission:  "elevated_permissions",
			Method:      "Role permissions exploitation",
			Prerequisites: []string{"Assumed role", "Permissive role policies"},
			Indicators:    []string{"Unusual API activity", "Resource access patterns"},
			RiskScore:     0.9,
		},
	}

	path := EscalationPath{
		ID:               fmt.Sprintf("oidc-esc-%s", generateEscalationHash(relationshipID)),
		RelationshipID:   relationshipID,
		RelationshipType: "identity_federation",
		PathType:         "oidc_federation_escalation",
		EscalationSteps:  steps,
		EntryPoint:       "OIDC Identity Provider",
		TargetPrivilege:  "Cross-cloud administrative access",
		StepCount:        len(steps),
		SourceProvider:   sourceProvider,
		TargetProvider:   targetProvider,
		SourceAccountID:  sourceAccount,
		TargetAccountID:  targetAccount,
		DetectedAt:       time.Now(),
	}

	// Calculate path metrics
	path.ComplexityScore = pea.calculateComplexityScore(path)
	path.FeasibilityScore = pea.calculateFeasibilityScore(path)
	path.ImpactScore = baseRiskScore
	path.LikelihoodScore = pea.calculateLikelihoodScore(path, conditions)
	path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

	// Determine risk level
	if path.RiskScore >= 0.8 {
		path.RiskLevel = "CRITICAL"
	} else if path.RiskScore >= 0.6 {
		path.RiskLevel = "HIGH"
	} else if path.RiskScore >= 0.4 {
		path.RiskLevel = "MEDIUM"
	} else {
		path.RiskLevel = "LOW"
	}

	paths = append(paths, path)
	return paths
}

// generateSAMLEscalationPaths generates escalation paths for SAML federation
func (pea *PrivilegeEscalationAnalyzer) generateSAMLEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount string, conditions []interface{}, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	steps := []EscalationStep{
		{
			StepNumber:  1,
			Action:      "obtain_saml_assertion",
			Description: "Obtain SAML assertion from identity provider",
			Resource:    "SAML Identity Provider",
			Permission:  "authenticate",
			Method:      "SAML authentication flow",
			Prerequisites: []string{"Valid credentials", "Access to SAML IdP"},
			Indicators:    []string{"SAML authentication logs", "Assertion generation"},
			RiskScore:     0.3,
		},
		{
			StepNumber:  2,
			Action:      "assume_saml_role",
			Description: "Use SAML assertion to assume role in target cloud",
			Resource:    "Cross-cloud IAM role",
			Permission:  "sts:AssumeRoleWithSAML",
			Method:      "STS AssumeRoleWithSAML",
			Prerequisites: []string{"Valid SAML assertion", "SAML provider configured"},
			Indicators:    []string{"AssumeRoleWithSAML calls", "SAML token validation"},
			RiskScore:     0.6,
		},
		{
			StepNumber:  3,
			Action:      "lateral_movement",
			Description: "Move laterally across cloud resources",
			Resource:    "Multi-cloud infrastructure",
			Permission:  "cross_resource_access",
			Method:      "Credential reuse and privilege abuse",
			Prerequisites: []string{"Elevated permissions", "Network connectivity"},
			Indicators:    []string{"Cross-resource access", "Unusual activity patterns"},
			RiskScore:     0.8,
		},
	}

	path := EscalationPath{
		ID:               fmt.Sprintf("saml-esc-%s", generateEscalationHash(relationshipID)),
		RelationshipID:   relationshipID,
		RelationshipType: "identity_federation",
		PathType:         "saml_federation_escalation",
		EscalationSteps:  steps,
		EntryPoint:       "SAML Identity Provider",
		TargetPrivilege:  "Multi-cloud resource access",
		StepCount:        len(steps),
		SourceProvider:   sourceProvider,
		TargetProvider:   targetProvider,
		SourceAccountID:  sourceAccount,
		TargetAccountID:  targetAccount,
		DetectedAt:       time.Now(),
	}

	// Calculate metrics
	path.ComplexityScore = pea.calculateComplexityScore(path)
	path.FeasibilityScore = pea.calculateFeasibilityScore(path)
	path.ImpactScore = baseRiskScore
	path.LikelihoodScore = pea.calculateLikelihoodScore(path, conditions)
	path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

	// Determine risk level
	if path.RiskScore >= 0.8 {
		path.RiskLevel = "CRITICAL"
	} else if path.RiskScore >= 0.6 {
		path.RiskLevel = "HIGH"
	} else {
		path.RiskLevel = "MEDIUM"
	}

	paths = append(paths, path)
	return paths
}

// generateTrustPolicyEscalationPaths generates escalation paths for trust policies
func (pea *PrivilegeEscalationAnalyzer) generateTrustPolicyEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount string, conditions []interface{}, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	steps := []EscalationStep{
		{
			StepNumber:  1,
			Action:      "compromise_trusted_principal",
			Description: "Compromise credentials of trusted principal",
			Resource:    "Trusted principal identity",
			Permission:  "identity_access",
			Method:      "Credential theft or social engineering",
			Prerequisites: []string{"Target identification", "Attack vector"},
			Indicators:    []string{"Unusual login patterns", "Failed authentication attempts"},
			RiskScore:     0.7,
		},
		{
			StepNumber:  2,
			Action:      "assume_trusted_role",
			Description: "Assume role using compromised principal",
			Resource:    "Cross-account IAM role",
			Permission:  "sts:AssumeRole",
			Method:      "Role assumption with valid credentials",
			Prerequisites: []string{"Valid credentials", "Trust relationship"},
			Indicators:    []string{"Cross-account role assumption", "API activity"},
			RiskScore:     0.8,
		},
		{
			StepNumber:  3,
			Action:      "privilege_abuse",
			Description: "Abuse elevated privileges for unauthorized access",
			Resource:    "Target account resources",
			Permission:  "broad_permissions",
			Method:      "Permission exploitation",
			Prerequisites: []string{"Elevated role", "Knowledge of target resources"},
			Indicators:    []string{"Resource enumeration", "Data access", "Configuration changes"},
			RiskScore:     0.9,
		},
	}

	path := EscalationPath{
		ID:               fmt.Sprintf("trust-esc-%s", generateEscalationHash(relationshipID)),
		RelationshipID:   relationshipID,
		RelationshipType: "identity_federation",
		PathType:         "trust_policy_escalation",
		EscalationSteps:  steps,
		EntryPoint:       "Trusted principal credentials",
		TargetPrivilege:  "Cross-account administrative access",
		StepCount:        len(steps),
		SourceProvider:   sourceProvider,
		TargetProvider:   targetProvider,
		SourceAccountID:  sourceAccount,
		TargetAccountID:  targetAccount,
		DetectedAt:       time.Now(),
	}

	// Calculate metrics
	path.ComplexityScore = pea.calculateComplexityScore(path)
	path.FeasibilityScore = pea.calculateFeasibilityScore(path)
	path.ImpactScore = baseRiskScore
	path.LikelihoodScore = pea.calculateLikelihoodScore(path, conditions)
	path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

	// Determine risk level
	if path.RiskScore >= 0.8 {
		path.RiskLevel = "CRITICAL"
	} else if path.RiskScore >= 0.6 {
		path.RiskLevel = "HIGH"
	} else {
		path.RiskLevel = "MEDIUM"
	}

	paths = append(paths, path)
	return paths
}

// analyzeSecurityRoleEscalation analyzes escalation paths from security role relationships
func (pea *PrivilegeEscalationAnalyzer) analyzeSecurityRoleEscalation(ctx context.Context) ([]EscalationPath, error) {
	var paths []EscalationPath

	query := `
	SELECT id, source_cloud_provider, target_cloud_provider, source_account_id, target_account_id,
	       relationship_type, assumption_chain, risk_score, escalation_paths
	FROM security_role_relationships
	WHERE status = 'active' AND risk_score >= ?
	`

	rows, err := pea.db.QueryContext(ctx, query, pea.riskThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, sourceProvider, targetProvider, sourceAccount, targetAccount string
		var relationshipType, assumptionChainStr, escalationPathsStr string
		var riskScore float64

		if err := rows.Scan(&id, &sourceProvider, &targetProvider, &sourceAccount, &targetAccount,
			&relationshipType, &assumptionChainStr, &riskScore, &escalationPathsStr); err != nil {
			continue
		}

		// Parse assumption chain
		var assumptionChain []string
		if assumptionChainStr != "" {
			json.Unmarshal([]byte(assumptionChainStr), &assumptionChain)
		}

		// Generate role-based escalation paths
		rolePaths := pea.generateRoleEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, relationshipType, assumptionChain, riskScore)
		paths = append(paths, rolePaths...)
	}

	return paths, nil
}

// generateRoleEscalationPaths generates escalation paths for role relationships
func (pea *PrivilegeEscalationAnalyzer) generateRoleEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount, relationshipType string, assumptionChain []string, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	if relationshipType == "role_assumption_chain" && len(assumptionChain) > 2 {
		// Multi-hop role assumption escalation
		steps := []EscalationStep{}
		for i, roleArn := range assumptionChain {
			step := EscalationStep{
				StepNumber:  i + 1,
				Action:      "assume_role",
				Description: fmt.Sprintf("Assume role: %s", roleArn),
				Resource:    roleArn,
				Permission:  "sts:AssumeRole",
				Method:      "Role chaining",
				Prerequisites: []string{"Previous role credentials", "Trust relationship"},
				Indicators:    []string{"AssumeRole API calls", "Role chaining activity"},
				RiskScore:     0.3 + float64(i)*0.2, // Increasing risk with each hop
			}
			steps = append(steps, step)
		}

		path := EscalationPath{
			ID:               fmt.Sprintf("role-chain-esc-%s", generateEscalationHash(relationshipID)),
			RelationshipID:   relationshipID,
			RelationshipType: "security_role",
			PathType:         "role_assumption_chain",
			EscalationSteps:  steps,
			EntryPoint:       assumptionChain[0],
			TargetPrivilege:  assumptionChain[len(assumptionChain)-1],
			StepCount:        len(steps),
			SourceProvider:   sourceProvider,
			TargetProvider:   targetProvider,
			SourceAccountID:  sourceAccount,
			TargetAccountID:  targetAccount,
			DetectedAt:       time.Now(),
		}

		// Calculate metrics
		path.ComplexityScore = float64(len(assumptionChain)) / 10.0 // Normalize by max expected chain length
		path.FeasibilityScore = 1.0 - (float64(len(assumptionChain))-2)*0.1 // Lower feasibility with longer chains
		path.ImpactScore = baseRiskScore
		path.LikelihoodScore = path.FeasibilityScore * 0.8 // Reduce likelihood based on feasibility
		path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

		// Determine risk level
		if path.RiskScore >= 0.8 {
			path.RiskLevel = "CRITICAL"
		} else if path.RiskScore >= 0.6 {
			path.RiskLevel = "HIGH"
		} else {
			path.RiskLevel = "MEDIUM"
		}

		paths = append(paths, path)
	}

	return paths
}

// analyzePolicySimilarityEscalation analyzes escalation paths from policy similarities
func (pea *PrivilegeEscalationAnalyzer) analyzePolicySimilarityEscalation(ctx context.Context) ([]EscalationPath, error) {
	var paths []EscalationPath

	query := `
	SELECT id, source_cloud_provider, target_cloud_provider, source_account_id, target_account_id,
	       similarity_type, risk_score, matching_elements
	FROM policy_similarity_analysis
	WHERE status = 'active' AND risk_score >= ?
	`

	rows, err := pea.db.QueryContext(ctx, query, pea.riskThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, sourceProvider, targetProvider, sourceAccount, targetAccount string
		var similarityType, matchingElementsStr string
		var riskScore float64

		if err := rows.Scan(&id, &sourceProvider, &targetProvider, &sourceAccount, &targetAccount,
			&similarityType, &riskScore, &matchingElementsStr); err != nil {
			continue
		}

		// Parse matching elements
		var matchingElements []string
		if matchingElementsStr != "" {
			json.Unmarshal([]byte(matchingElementsStr), &matchingElements)
		}

		// Generate policy-based escalation paths
		policyPaths := pea.generatePolicyEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, similarityType, matchingElements, riskScore)
		paths = append(paths, policyPaths...)
	}

	return paths, nil
}

// generatePolicyEscalationPaths generates escalation paths for policy similarities
func (pea *PrivilegeEscalationAnalyzer) generatePolicyEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount, similarityType string, matchingElements []string, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	steps := []EscalationStep{
		{
			StepNumber:  1,
			Action:      "identify_similar_policies",
			Description: "Identify similar policies across cloud providers",
			Resource:    "IAM policies",
			Permission:  "policy_enumeration",
			Method:      "Policy analysis and comparison",
			Prerequisites: []string{"Access to policy documents", "Analysis tools"},
			Indicators:    []string{"Policy enumeration activity", "Cross-cloud analysis"},
			RiskScore:     0.2,
		},
		{
			StepNumber:  2,
			Action:      "exploit_policy_similarity",
			Description: "Exploit similar policy patterns for unauthorized access",
			Resource:    "Cross-cloud resources",
			Permission:  "elevated_access",
			Method:      "Policy exploitation",
			Prerequisites: []string{"Understanding of policy patterns", "Valid credentials"},
			Indicators:    []string{"Unusual access patterns", "Cross-cloud resource access"},
			RiskScore:     0.7,
		},
		{
			StepNumber:  3,
			Action:      "privilege_expansion",
			Description: "Expand privileges using policy similarities",
			Resource:    "Target cloud resources",
			Permission:  "administrative_access",
			Method:      "Permission expansion",
			Prerequisites: []string{"Initial access", "Policy knowledge"},
			Indicators:    []string{"Permission escalation", "Administrative actions"},
			RiskScore:     0.8,
		},
	}

	path := EscalationPath{
		ID:               fmt.Sprintf("policy-esc-%s", generateEscalationHash(relationshipID)),
		RelationshipID:   relationshipID,
		RelationshipType: "policy_similarity",
		PathType:         "policy_similarity_exploitation",
		EscalationSteps:  steps,
		EntryPoint:       "Policy enumeration",
		TargetPrivilege:  "Cross-cloud administrative access",
		StepCount:        len(steps),
		SourceProvider:   sourceProvider,
		TargetProvider:   targetProvider,
		SourceAccountID:  sourceAccount,
		TargetAccountID:  targetAccount,
		DetectedAt:       time.Now(),
	}

	// Calculate metrics
	path.ComplexityScore = pea.calculateComplexityScore(path)
	path.FeasibilityScore = pea.calculateFeasibilityScore(path)
	path.ImpactScore = baseRiskScore
	path.LikelihoodScore = 0.5 // Moderate likelihood for policy-based attacks
	path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

	// Determine risk level
	if path.RiskScore >= 0.8 {
		path.RiskLevel = "CRITICAL"
	} else if path.RiskScore >= 0.6 {
		path.RiskLevel = "HIGH"
	} else {
		path.RiskLevel = "MEDIUM"
	}

	paths = append(paths, path)
	return paths
}

// analyzeCertificateEscalation analyzes escalation paths from certificate correlations
func (pea *PrivilegeEscalationAnalyzer) analyzeCertificateEscalation(ctx context.Context) ([]EscalationPath, error) {
	var paths []EscalationPath

	query := `
	SELECT id, source_cloud_provider, target_cloud_provider, source_account_id, target_account_id,
	       correlation_type, security_risk_score, shared_secrets
	FROM certificate_correlations
	WHERE status = 'active' AND security_risk_score >= ?
	`

	rows, err := pea.db.QueryContext(ctx, query, pea.riskThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, sourceProvider, targetProvider, sourceAccount, targetAccount string
		var correlationType, sharedSecretsStr string
		var riskScore float64

		if err := rows.Scan(&id, &sourceProvider, &targetProvider, &sourceAccount, &targetAccount,
			&correlationType, &riskScore, &sharedSecretsStr); err != nil {
			continue
		}

		// Generate certificate-based escalation paths
		certPaths := pea.generateCertificateEscalationPaths(id, sourceProvider, targetProvider, sourceAccount, targetAccount, correlationType, riskScore)
		paths = append(paths, certPaths...)
	}

	return paths, nil
}

// generateCertificateEscalationPaths generates escalation paths for certificate correlations
func (pea *PrivilegeEscalationAnalyzer) generateCertificateEscalationPaths(relationshipID, sourceProvider, targetProvider, sourceAccount, targetAccount, correlationType string, baseRiskScore float64) []EscalationPath {
	var paths []EscalationPath

	steps := []EscalationStep{
		{
			StepNumber:  1,
			Action:      "compromise_certificate",
			Description: "Compromise shared certificate or private key",
			Resource:    "SSL/TLS certificate",
			Permission:  "certificate_access",
			Method:      "Certificate theft or key compromise",
			Prerequisites: []string{"Access to certificate store", "Extraction capabilities"},
			Indicators:    []string{"Certificate access logs", "Key store access"},
			RiskScore:     0.6,
		},
		{
			StepNumber:  2,
			Action:      "impersonate_service",
			Description: "Impersonate service using compromised certificate",
			Resource:    "Target service",
			Permission:  "service_impersonation",
			Method:      "Certificate-based authentication",
			Prerequisites: []string{"Valid certificate", "Network access"},
			Indicators:    []string{"Unusual certificate usage", "Service impersonation"},
			RiskScore:     0.8,
		},
		{
			StepNumber:  3,
			Action:      "access_resources",
			Description: "Access protected resources using impersonated service",
			Resource:    "Protected resources",
			Permission:  "resource_access",
			Method:      "Authenticated access",
			Prerequisites: []string{"Service credentials", "Resource permissions"},
			Indicators:    []string{"Resource access", "Data exfiltration"},
			RiskScore:     0.9,
		},
	}

	path := EscalationPath{
		ID:               fmt.Sprintf("cert-esc-%s", generateEscalationHash(relationshipID)),
		RelationshipID:   relationshipID,
		RelationshipType: "certificate_correlation",
		PathType:         "certificate_based_escalation",
		EscalationSteps:  steps,
		EntryPoint:       "Certificate compromise",
		TargetPrivilege:  "Service impersonation and resource access",
		StepCount:        len(steps),
		SourceProvider:   sourceProvider,
		TargetProvider:   targetProvider,
		SourceAccountID:  sourceAccount,
		TargetAccountID:  targetAccount,
		DetectedAt:       time.Now(),
	}

	// Calculate metrics
	path.ComplexityScore = pea.calculateComplexityScore(path)
	path.FeasibilityScore = pea.calculateFeasibilityScore(path)
	path.ImpactScore = baseRiskScore
	path.LikelihoodScore = 0.4 // Moderate likelihood for certificate-based attacks
	path.RiskScore = (path.ImpactScore + path.LikelihoodScore) / 2

	// Determine risk level
	if path.RiskScore >= 0.8 {
		path.RiskLevel = "CRITICAL"
	} else if path.RiskScore >= 0.6 {
		path.RiskLevel = "HIGH"
	} else {
		path.RiskLevel = "MEDIUM"
	}

	paths = append(paths, path)
	return paths
}

// enhanceEscalationPath enhances escalation path with additional analysis
func (pea *PrivilegeEscalationAnalyzer) enhanceEscalationPath(path EscalationPath) EscalationPath {
	// Add attack vectors
	path.AttackVectors = pea.identifyAttackVectors(path)

	// Add mitigations
	path.Mitigations = pea.identifyMitigations(path)

	// Add security controls
	path.Controls = pea.identifySecurityControls(path)

	// Add detection methods
	path.DetectionMethods = pea.identifyDetectionMethods(path)

	// Add response procedures
	path.ResponseProcedures = pea.identifyResponseProcedures(path)

	// Add compliance violations
	path.ComplianceViolations = pea.identifyComplianceViolations(path)

	// Add framework mappings
	path.FrameworkMappings = pea.identifyFrameworkMappings(path)

	// Set affected resources
	path.AffectedResources = pea.identifyAffectedResources(path)

	// Set prerequisites and indicators
	path.Prerequisites = pea.aggregatePrerequisites(path)
	path.Indicators = pea.aggregateIndicators(path)

	return path
}

// Helper methods for calculating metrics and identifying components

func (pea *PrivilegeEscalationAnalyzer) calculateComplexityScore(path EscalationPath) float64 {
	// Base complexity on number of steps and cross-cloud nature
	baseComplexity := float64(path.StepCount) / 10.0
	if path.SourceProvider != path.TargetProvider {
		baseComplexity += 0.3 // Cross-cloud adds complexity
	}
	if baseComplexity > 1.0 {
		baseComplexity = 1.0
	}
	return baseComplexity
}

func (pea *PrivilegeEscalationAnalyzer) calculateFeasibilityScore(path EscalationPath) float64 {
	// Base feasibility on path type and complexity
	feasibility := 0.8 // Base feasibility
	
	// Reduce feasibility for complex paths
	feasibility -= path.ComplexityScore * 0.3
	
	// Adjust based on path type
	switch path.PathType {
	case "oidc_federation_escalation":
		feasibility *= 0.8 // OIDC attacks are moderately feasible
	case "saml_federation_escalation":
		feasibility *= 0.7 // SAML attacks are somewhat complex
	case "trust_policy_escalation":
		feasibility *= 0.9 // Trust policy attacks are often feasible
	case "role_assumption_chain":
		feasibility *= 0.6 // Role chains require more steps
	case "policy_similarity_exploitation":
		feasibility *= 0.5 // Policy attacks require expertise
	case "certificate_based_escalation":
		feasibility *= 0.4 // Certificate attacks are complex
	}
	
	if feasibility < 0.0 {
		feasibility = 0.0
	}
	return feasibility
}

func (pea *PrivilegeEscalationAnalyzer) calculateLikelihoodScore(path EscalationPath, conditions []interface{}) float64 {
	likelihood := path.FeasibilityScore
	
	// Reduce likelihood if there are strong conditions
	if len(conditions) > 0 {
		likelihood *= 0.8
	}
	
	// Adjust for cross-cloud scenarios (less likely but higher impact)
	if path.SourceProvider != path.TargetProvider {
		likelihood *= 0.7
	}
	
	return likelihood
}

func (pea *PrivilegeEscalationAnalyzer) identifyAttackVectors(path EscalationPath) []AttackVector {
	var vectors []AttackVector
	
	// Common attack vectors based on path type
	switch path.PathType {
	case "oidc_federation_escalation":
		vectors = append(vectors, AttackVector{
			ID:          "T1078.004",
			Name:        "Cloud Accounts",
			Description: "Adversaries may obtain and abuse credentials of cloud accounts",
			MITREID:     "T1078.004",
			Techniques:  []string{"Valid Accounts", "Cloud Accounts"},
			Platforms:   []string{"AWS", "Azure", "GCP"},
			RiskScore:   0.8,
		})
	case "trust_policy_escalation":
		vectors = append(vectors, AttackVector{
			ID:          "T1548.005",
			Name:        "Abuse Elevation Control Mechanism",
			Description: "Adversaries may circumvent mechanisms designed to control elevate privileges",
			MITREID:     "T1548.005",
			Techniques:  []string{"Abuse Elevation Control Mechanism", "Temporary Elevated Cloud Access"},
			Platforms:   []string{"AWS", "Azure", "GCP"},
			RiskScore:   0.9,
		})
	}
	
	return vectors
}

func (pea *PrivilegeEscalationAnalyzer) identifyMitigations(path EscalationPath) []Mitigation {
	return []Mitigation{
		{
			ID:           "M1026",
			Name:         "Privileged Account Management",
			Description:  "Manage the creation, modification, use, and permissions associated to privileged accounts",
			Type:         "preventive",
			Priority:     "high",
			Effort:       "medium",
			Effectiveness: 0.8,
		},
		{
			ID:           "M1018",
			Name:         "User Account Management",
			Description:  "Manage the creation, modification, use, and permissions associated to user accounts",
			Type:         "preventive",
			Priority:     "high",
			Effort:       "low",
			Effectiveness: 0.7,
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifySecurityControls(path EscalationPath) []SecurityControl {
	return []SecurityControl{
		{
			ID:          "IAM-01",
			Name:        "Multi-Factor Authentication",
			Description: "Require MFA for all privileged accounts",
			Type:        "technical",
			Category:    "access_control",
			Framework:   "CIS",
			Implemented: false,
		},
		{
			ID:          "IAM-02",
			Name:        "Principle of Least Privilege",
			Description: "Grant minimum necessary permissions",
			Type:        "administrative",
			Category:    "access_control",
			Framework:   "NIST",
			Implemented: false,
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifyDetectionMethods(path EscalationPath) []DetectionMethod {
	return []DetectionMethod{
		{
			ID:          "DM-001",
			Name:        "Cross-Account Activity Monitoring",
			Description: "Monitor for unusual cross-account access patterns",
			Type:        "behavior_analysis",
			Tools:       []string{"CloudTrail", "Azure Monitor", "GCP Audit Logs"},
			Indicators:  []string{"Cross-account AssumeRole", "Unusual API activity"},
			Accuracy:    0.8,
		},
		{
			ID:          "DM-002",
			Name:        "Federation Authentication Monitoring",
			Description: "Monitor federation authentication events",
			Type:        "log_analysis",
			Tools:       []string{"SIEM", "Log Analytics"},
			Indicators:  []string{"SAML assertions", "OIDC token usage"},
			Accuracy:    0.7,
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifyResponseProcedures(path EscalationPath) []ResponseProcedure {
	return []ResponseProcedure{
		{
			ID:          "RP-001",
			Name:        "Identity Compromise Response",
			Description: "Response procedures for compromised identities",
			Phase:       "containment",
			Steps:       []string{"Disable compromised accounts", "Revoke tokens", "Reset credentials"},
			Stakeholders: []string{"Security Team", "Cloud Administrators"},
			Timeline:    "< 1 hour",
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifyComplianceViolations(path EscalationPath) []ComplianceViolation {
	return []ComplianceViolation{
		{
			Framework:   "SOC2",
			ControlID:   "CC6.1",
			ControlName: "Logical and Physical Access Controls",
			Severity:    "high",
			Description: "Inadequate access controls allowing privilege escalation",
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifyFrameworkMappings(path EscalationPath) []FrameworkMapping {
	return []FrameworkMapping{
		{
			Framework:   "MITRE ATT&CK",
			TechniqueID: "T1078",
			TacticID:    "TA0001",
			Name:        "Valid Accounts",
			Description: "Adversaries may obtain and abuse credentials of existing accounts",
		},
	}
}

func (pea *PrivilegeEscalationAnalyzer) identifyAffectedResources(path EscalationPath) []string {
	return []string{
		"Cross-cloud IAM roles",
		"Identity providers",
		"Target cloud resources",
		"Service accounts",
	}
}

func (pea *PrivilegeEscalationAnalyzer) aggregatePrerequisites(path EscalationPath) []string {
	var prerequisites []string
	for _, step := range path.EscalationSteps {
		prerequisites = append(prerequisites, step.Prerequisites...)
	}
	return pea.deduplicateStrings(prerequisites)
}

func (pea *PrivilegeEscalationAnalyzer) aggregateIndicators(path EscalationPath) []string {
	var indicators []string
	for _, step := range path.EscalationSteps {
		indicators = append(indicators, step.Indicators...)
	}
	return pea.deduplicateStrings(indicators)
}

func (pea *PrivilegeEscalationAnalyzer) deduplicateStrings(slice []string) []string {
	keys := make(map[string]bool)
	var result []string
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	return result
}

func generateEscalationHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)[:8]
}

// PersistEscalationPaths saves escalation paths to database
func (pea *PrivilegeEscalationAnalyzer) PersistEscalationPaths(ctx context.Context, paths []EscalationPath) error {
	if len(paths) == 0 {
		return nil
	}

	tx, err := pea.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT OR REPLACE INTO privilege_escalation_paths (
		id, relationship_id, relationship_type, path_type, escalation_steps, entry_point, target_privilege,
		step_count, complexity_score, feasibility_score, risk_level, risk_score, impact_score, likelihood_score,
		attack_vectors, prerequisites, indicators, source_cloud_provider, target_cloud_provider,
		source_account_id, target_account_id, affected_resources, mitigations, controls,
		detection_methods, response_procedures, compliance_violations, framework_mappings,
		detection_method, confidence_score, detected_at, created_at, updated_at, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, path := range paths {
		// Convert complex fields to JSON
		escalationStepsJSON, _ := json.Marshal(path.EscalationSteps)
		attackVectorsJSON, _ := json.Marshal(path.AttackVectors)
		prerequisitesJSON, _ := json.Marshal(path.Prerequisites)
		indicatorsJSON, _ := json.Marshal(path.Indicators)
		affectedResourcesJSON, _ := json.Marshal(path.AffectedResources)
		mitigationsJSON, _ := json.Marshal(path.Mitigations)
		controlsJSON, _ := json.Marshal(path.Controls)
		detectionMethodsJSON, _ := json.Marshal(path.DetectionMethods)
		responseProceduresJSON, _ := json.Marshal(path.ResponseProcedures)
		complianceViolationsJSON, _ := json.Marshal(path.ComplianceViolations)
		frameworkMappingsJSON, _ := json.Marshal(path.FrameworkMappings)
		
		metadata := map[string]interface{}{
			"source_provider": path.SourceProvider,
			"target_provider": path.TargetProvider,
		}
		metadataJSON, _ := json.Marshal(metadata)

		_, err := stmt.Exec(
			path.ID, path.RelationshipID, path.RelationshipType, path.PathType,
			string(escalationStepsJSON), path.EntryPoint, path.TargetPrivilege,
			path.StepCount, path.ComplexityScore, path.FeasibilityScore,
			path.RiskLevel, path.RiskScore, path.ImpactScore, path.LikelihoodScore,
			string(attackVectorsJSON), string(prerequisitesJSON), string(indicatorsJSON),
			path.SourceProvider, path.TargetProvider, path.SourceAccountID, path.TargetAccountID,
			string(affectedResourcesJSON), string(mitigationsJSON), string(controlsJSON),
			string(detectionMethodsJSON), string(responseProceduresJSON),
			string(complianceViolationsJSON), string(frameworkMappingsJSON),
			"automated_analysis", 0.9, path.DetectedAt, time.Now(), time.Now(),
			string(metadataJSON),
		)
		if err != nil {
			pea.logger.Printf("Error persisting escalation path %s: %v", path.ID, err)
			continue
		}
	}

	return tx.Commit()
}