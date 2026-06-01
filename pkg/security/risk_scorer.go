package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// SecurityRiskScorer provides comprehensive security risk scoring and assessment
type SecurityRiskScorer struct {
	db     DatabaseInterface
	logger *log.Logger
}

// NewSecurityRiskScorer creates a new security risk scorer
func NewSecurityRiskScorer(db DatabaseInterface, logger *log.Logger) *SecurityRiskScorer {
	return &SecurityRiskScorer{
		db:     db,
		logger: logger,
	}
}

// RiskAssessment represents a comprehensive security risk assessment
type RiskAssessment struct {
	ID                       string                 `json:"id"`
	AssessmentType           string                 `json:"assessment_type"`
	ScopeDefinition          map[string]interface{} `json:"scope_definition"`
	OverallRiskScore         float64                `json:"overall_risk_score"`
	RiskLevel                string                 `json:"risk_level"`
	RiskTrend                string                 `json:"risk_trend"`
	IdentityRiskScore        float64                `json:"identity_risk_score"`
	NetworkRiskScore         float64                `json:"network_risk_score"`
	DataRiskScore            float64                `json:"data_risk_score"`
	ComplianceRiskScore      float64                `json:"compliance_risk_score"`
	OperationalRiskScore     float64                `json:"operational_risk_score"`
	HighRiskFactors          []RiskFactor           `json:"high_risk_factors"`
	MediumRiskFactors        []RiskFactor           `json:"medium_risk_factors"`
	LowRiskFactors           []RiskFactor           `json:"low_risk_factors"`
	FindingsSummary          FindingsSummary        `json:"findings_summary"`
	CrossCloudSummary        CrossCloudSummary      `json:"cross_cloud_summary"`
	ImmediateActions         []Action               `json:"immediate_actions"`
	ShortTermRecommendations []Action               `json:"short_term_recommendations"`
	LongTermRecommendations  []Action               `json:"long_term_recommendations"`
	ComplianceFrameworks     []ComplianceFramework  `json:"compliance_frameworks"`
	ComplianceScores         map[string]float64     `json:"compliance_scores"`
	ComplianceGaps           []ComplianceGap        `json:"compliance_gaps"`
	AssessmentMetadata       AssessmentMetadata     `json:"assessment_metadata"`
	BaselineAssessmentID     string                 `json:"baseline_assessment_id"`
	PreviousAssessmentID     string                 `json:"previous_assessment_id"`
	ImprovementScore         float64                `json:"improvement_score"`
	Status                   string                 `json:"status"`
	AssessmentVersion        string                 `json:"assessment_version"`
	AssessmentStartTime      time.Time              `json:"assessment_start_time"`
	AssessmentEndTime        time.Time              `json:"assessment_end_time"`
	CreatedAt                time.Time              `json:"created_at"`
	UpdatedAt                time.Time              `json:"updated_at"`
	ExecutiveSummary         string                 `json:"executive_summary"`
	DetailedReportURL        string                 `json:"detailed_report_url"`
}

// RiskFactor represents an individual risk factor
type RiskFactor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Severity     string   `json:"severity"`
	Impact       float64  `json:"impact"`
	Likelihood   float64  `json:"likelihood"`
	RiskScore    float64  `json:"risk_score"`
	Evidence     []string `json:"evidence"`
	Mitigation   string   `json:"mitigation"`
	ResourceType string   `json:"resource_type"`
	Provider     string   `json:"provider"`
}

// FindingsSummary represents a summary of security findings
type FindingsSummary struct {
	CriticalFindings int `json:"critical_findings"`
	HighFindings     int `json:"high_findings"`
	MediumFindings   int `json:"medium_findings"`
	LowFindings      int `json:"low_findings"`
	TotalFindings    int `json:"total_findings"`
}

// CrossCloudSummary represents cross-cloud specific risk summary
type CrossCloudSummary struct {
	CrossCloudCorrelations int `json:"cross_cloud_correlations"`
	FederationRisks        int `json:"federation_risks"`
	PolicySimilarities     int `json:"policy_similarities"`
	CertificateIssues      int `json:"certificate_issues"`
	EscalationPaths        int `json:"escalation_paths"`
}

// Action represents a recommended action
type Action struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Priority     string   `json:"priority"`
	Effort       string   `json:"effort"`
	Timeline     string   `json:"timeline"`
	Stakeholders []string `json:"stakeholders"`
	Resources    []string `json:"resources"`
	Success      string   `json:"success_criteria"`
}

// ComplianceFramework represents a compliance framework assessment
type ComplianceFramework struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Score       float64   `json:"score"`
	Status      string    `json:"status"`
	LastUpdated time.Time `json:"last_updated"`
}

// ComplianceGap represents a compliance gap
type ComplianceGap struct {
	Framework   string `json:"framework"`
	ControlID   string `json:"control_id"`
	ControlName string `json:"control_name"`
	GapType     string `json:"gap_type"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Remediation string `json:"remediation"`
}

// AssessmentMetadata represents assessment metadata
type AssessmentMetadata struct {
	AssessmentMethod string   `json:"assessment_method"`
	Assessor         string   `json:"assessor"`
	ScopeAccounts    []string `json:"scope_accounts"`
	ScopeRegions     []string `json:"scope_regions"`
	ScopeServices    []string `json:"scope_services"`
	DataSources      []string `json:"data_sources"`
	AnalysisTools    []string `json:"analysis_tools"`
}

// PerformComprehensiveRiskAssessment performs a comprehensive security risk assessment
func (srs *SecurityRiskScorer) PerformComprehensiveRiskAssessment(ctx context.Context, assessmentType string, scope map[string]interface{}) (*RiskAssessment, error) {
	assessment := &RiskAssessment{
		ID:                  fmt.Sprintf("risk-assessment-%d", time.Now().Unix()),
		AssessmentType:      assessmentType,
		ScopeDefinition:     scope,
		AssessmentStartTime: time.Now(),
		Status:              "in_progress",
		AssessmentVersion:   "1.0",
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	srs.logger.Printf("Starting comprehensive risk assessment: %s", assessment.ID)

	// Collect risk data from all security components
	if err := srs.assessIdentityRisks(ctx, assessment); err != nil {
		return nil, fmt.Errorf("failed to assess identity risks: %w", err)
	}

	if err := srs.assessNetworkRisks(ctx, assessment); err != nil {
		return nil, fmt.Errorf("failed to assess network risks: %w", err)
	}

	if err := srs.assessDataRisks(ctx, assessment); err != nil {
		return nil, fmt.Errorf("failed to assess data risks: %w", err)
	}

	if err := srs.assessComplianceRisks(ctx, assessment); err != nil {
		return nil, fmt.Errorf("failed to assess compliance risks: %w", err)
	}

	if err := srs.assessOperationalRisks(ctx, assessment); err != nil {
		return nil, fmt.Errorf("failed to assess operational risks: %w", err)
	}

	// Calculate overall risk score
	assessment.OverallRiskScore = srs.calculateOverallRiskScore(assessment)
	assessment.RiskLevel = srs.determineRiskLevel(assessment.OverallRiskScore)

	// Generate findings summary
	assessment.FindingsSummary = srs.generateFindingsSummary(assessment)

	// Generate cross-cloud summary
	assessment.CrossCloudSummary = srs.generateCrossCloudSummary(ctx)

	// Generate recommendations
	assessment.ImmediateActions = srs.generateImmediateActions(assessment)
	assessment.ShortTermRecommendations = srs.generateShortTermRecommendations(assessment)
	assessment.LongTermRecommendations = srs.generateLongTermRecommendations(assessment)

	// Assess compliance
	assessment.ComplianceFrameworks = srs.assessComplianceFrameworks(ctx)
	assessment.ComplianceScores = srs.calculateComplianceScores(assessment.ComplianceFrameworks)
	assessment.ComplianceGaps = srs.identifyComplianceGaps(ctx)

	// Calculate improvement score if previous assessment exists
	if assessment.PreviousAssessmentID != "" {
		assessment.ImprovementScore = srs.calculateImprovementScore(ctx, assessment)
	}

	// Generate executive summary
	assessment.ExecutiveSummary = srs.generateExecutiveSummary(assessment)

	// Set assessment metadata
	assessment.AssessmentMetadata = srs.generateAssessmentMetadata(scope)

	assessment.AssessmentEndTime = time.Now()
	assessment.Status = "completed"
	assessment.UpdatedAt = time.Now()

	srs.logger.Printf("Completed risk assessment: %s (Overall Risk: %.2f - %s)",
		assessment.ID, assessment.OverallRiskScore, assessment.RiskLevel)

	return assessment, nil
}

// assessIdentityRisks assesses identity and access management risks
func (srs *SecurityRiskScorer) assessIdentityRisks(ctx context.Context, assessment *RiskAssessment) error {
	var riskFactors []RiskFactor

	// Assess identity federation risks
	federationRisks, err := srs.assessFederationRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing federation risks: %v", err)
	} else {
		riskFactors = append(riskFactors, federationRisks...)
	}

	// Assess role relationship risks
	roleRisks, err := srs.assessRoleRelationshipRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing role risks: %v", err)
	} else {
		riskFactors = append(riskFactors, roleRisks...)
	}

	// Assess privilege escalation risks
	escalationRisks, err := srs.assessPrivilegeEscalationRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing escalation risks: %v", err)
	} else {
		riskFactors = append(riskFactors, escalationRisks...)
	}

	// Calculate identity risk score
	assessment.IdentityRiskScore = srs.calculateCategoryRiskScore(riskFactors)

	// Categorize risk factors
	srs.categorizeRiskFactors(riskFactors, assessment)

	return nil
}

// assessFederationRisks assesses identity federation risks
func (srs *SecurityRiskScorer) assessFederationRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT COUNT(*) as count, AVG(security_risk_score) as avg_risk, security_risk_level
	FROM identity_federation_relationships
	WHERE status = 'active'
	GROUP BY security_risk_level
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var count int
		var avgRisk float64
		var riskLevel string

		if err := rows.Scan(&count, &avgRisk, &riskLevel); err != nil {
			continue
		}

		if count > 0 {
			riskFactor := RiskFactor{
				ID:           fmt.Sprintf("federation-%s", strings.ToLower(riskLevel)),
				Name:         fmt.Sprintf("Identity Federation - %s Risk", riskLevel),
				Description:  fmt.Sprintf("%d identity federation relationships with %s risk level", count, riskLevel),
				Category:     "identity",
				Severity:     riskLevel,
				Impact:       avgRisk,
				Likelihood:   srs.calculateFederationLikelihood(riskLevel, count),
				ResourceType: "federation",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessRoleRelationshipRisks assesses security role relationship risks
func (srs *SecurityRiskScorer) assessRoleRelationshipRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT COUNT(*) as count, AVG(risk_score) as avg_risk
	FROM security_role_relationships
	WHERE status = 'active' AND risk_score >= 0.6
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var count int
		var avgRisk float64

		if err := rows.Scan(&count, &avgRisk); err == nil && count > 0 {
			riskFactor := RiskFactor{
				ID:           "high-risk-roles",
				Name:         "High-Risk Security Role Relationships",
				Description:  fmt.Sprintf("%d security role relationships with high risk scores", count),
				Category:     "identity",
				Severity:     "HIGH",
				Impact:       avgRisk,
				Likelihood:   srs.calculateRoleLikelihood(count),
				ResourceType: "role",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessPrivilegeEscalationRisks assesses privilege escalation risks
func (srs *SecurityRiskScorer) assessPrivilegeEscalationRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT COUNT(*) as count, AVG(risk_score) as avg_risk, risk_level
	FROM privilege_escalation_paths
	WHERE status = 'open'
	GROUP BY risk_level
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var count int
		var avgRisk float64
		var riskLevel string

		if err := rows.Scan(&count, &avgRisk, &riskLevel); err != nil {
			continue
		}

		if count > 0 {
			riskFactor := RiskFactor{
				ID:           fmt.Sprintf("escalation-%s", strings.ToLower(riskLevel)),
				Name:         fmt.Sprintf("Privilege Escalation Paths - %s Risk", riskLevel),
				Description:  fmt.Sprintf("%d privilege escalation paths with %s risk level", count, riskLevel),
				Category:     "identity",
				Severity:     riskLevel,
				Impact:       avgRisk,
				Likelihood:   srs.calculateEscalationLikelihood(riskLevel, count),
				ResourceType: "escalation_path",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessNetworkRisks assesses network security risks
func (srs *SecurityRiskScorer) assessNetworkRisks(ctx context.Context, assessment *RiskAssessment) error {
	var riskFactors []RiskFactor

	// Assess cross-cloud network topology risks
	topologyRisks, err := srs.assessTopologyRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing topology risks: %v", err)
	} else {
		riskFactors = append(riskFactors, topologyRisks...)
	}

	// Calculate network risk score
	assessment.NetworkRiskScore = srs.calculateCategoryRiskScore(riskFactors)

	return nil
}

// assessTopologyRisks assesses network topology risks
func (srs *SecurityRiskScorer) assessTopologyRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	// Query for cross-cloud network connections
	query := `
	SELECT COUNT(*) as count
	FROM cross_cloud_network_topology
	WHERE status = 'active' AND encryption = false
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var count int
		if err := rows.Scan(&count); err == nil && count > 0 {
			riskFactor := RiskFactor{
				ID:           "unencrypted-connections",
				Name:         "Unencrypted Cross-Cloud Connections",
				Description:  fmt.Sprintf("%d cross-cloud network connections without encryption", count),
				Category:     "network",
				Severity:     "HIGH",
				Impact:       0.8,
				Likelihood:   0.6,
				ResourceType: "network_connection",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessDataRisks assesses data protection risks
func (srs *SecurityRiskScorer) assessDataRisks(ctx context.Context, assessment *RiskAssessment) error {
	var riskFactors []RiskFactor

	// Assess certificate and secret sharing risks
	certificateRisks, err := srs.assessCertificateRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing certificate risks: %v", err)
	} else {
		riskFactors = append(riskFactors, certificateRisks...)
	}

	// Calculate data risk score
	assessment.DataRiskScore = srs.calculateCategoryRiskScore(riskFactors)

	return nil
}

// assessCertificateRisks assesses certificate and secret risks
func (srs *SecurityRiskScorer) assessCertificateRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT COUNT(*) as count, AVG(security_risk_score) as avg_risk, security_risk_level
	FROM certificate_correlations
	WHERE status = 'active'
	GROUP BY security_risk_level
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var count int
		var avgRisk float64
		var riskLevel string

		if err := rows.Scan(&count, &avgRisk, &riskLevel); err != nil {
			continue
		}

		if count > 0 {
			riskFactor := RiskFactor{
				ID:           fmt.Sprintf("certificate-%s", strings.ToLower(riskLevel)),
				Name:         fmt.Sprintf("Certificate Correlations - %s Risk", riskLevel),
				Description:  fmt.Sprintf("%d certificate correlations with %s risk level", count, riskLevel),
				Category:     "data",
				Severity:     riskLevel,
				Impact:       avgRisk,
				Likelihood:   srs.calculateCertificateLikelihood(riskLevel, count),
				ResourceType: "certificate",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessComplianceRisks assesses compliance-related risks
func (srs *SecurityRiskScorer) assessComplianceRisks(ctx context.Context, assessment *RiskAssessment) error {
	var riskFactors []RiskFactor

	// Assess compliance violations
	complianceRisks, err := srs.assessComplianceViolationRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing compliance risks: %v", err)
	} else {
		riskFactors = append(riskFactors, complianceRisks...)
	}

	// Calculate compliance risk score
	assessment.ComplianceRiskScore = srs.calculateCategoryRiskScore(riskFactors)

	return nil
}

// assessComplianceViolationRisks assesses compliance violation risks
func (srs *SecurityRiskScorer) assessComplianceViolationRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT framework_name, COUNT(*) as violations
	FROM compliance_mappings
	WHERE compliance_status = 'non_compliant' AND remediation_required = true
	GROUP BY framework_name
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var framework string
		var violations int

		if err := rows.Scan(&framework, &violations); err != nil {
			continue
		}

		if violations > 0 {
			impact := math.Min(float64(violations)/10.0, 1.0) // Normalize by expected max violations
			riskFactor := RiskFactor{
				ID:           fmt.Sprintf("compliance-%s", strings.ToLower(framework)),
				Name:         fmt.Sprintf("%s Compliance Violations", framework),
				Description:  fmt.Sprintf("%d compliance violations for %s framework", violations, framework),
				Category:     "compliance",
				Severity:     srs.determineComplianceSeverity(violations),
				Impact:       impact,
				Likelihood:   0.9, // High likelihood as violations are already present
				ResourceType: "compliance",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// assessOperationalRisks assesses operational security risks
func (srs *SecurityRiskScorer) assessOperationalRisks(ctx context.Context, assessment *RiskAssessment) error {
	var riskFactors []RiskFactor

	// Assess policy similarity risks
	policyRisks, err := srs.assessPolicySimilarityRisks(ctx)
	if err != nil {
		srs.logger.Printf("Error assessing policy risks: %v", err)
	} else {
		riskFactors = append(riskFactors, policyRisks...)
	}

	// Calculate operational risk score
	assessment.OperationalRiskScore = srs.calculateCategoryRiskScore(riskFactors)

	return nil
}

// assessPolicySimilarityRisks assesses policy similarity risks
func (srs *SecurityRiskScorer) assessPolicySimilarityRisks(ctx context.Context) ([]RiskFactor, error) {
	var riskFactors []RiskFactor

	query := `
	SELECT COUNT(*) as count, AVG(risk_score) as avg_risk, risk_level
	FROM policy_similarity_analysis
	WHERE status = 'active'
	GROUP BY risk_level
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var count int
		var avgRisk float64
		var riskLevel string

		if err := rows.Scan(&count, &avgRisk, &riskLevel); err != nil {
			continue
		}

		if count > 0 {
			riskFactor := RiskFactor{
				ID:           fmt.Sprintf("policy-%s", strings.ToLower(riskLevel)),
				Name:         fmt.Sprintf("Policy Similarities - %s Risk", riskLevel),
				Description:  fmt.Sprintf("%d policy similarities with %s risk level", count, riskLevel),
				Category:     "operational",
				Severity:     riskLevel,
				Impact:       avgRisk,
				Likelihood:   srs.calculatePolicyLikelihood(riskLevel, count),
				ResourceType: "policy",
				Provider:     "cross-cloud",
			}
			riskFactor.RiskScore = (riskFactor.Impact + riskFactor.Likelihood) / 2
			riskFactors = append(riskFactors, riskFactor)
		}
	}

	return riskFactors, nil
}

// Helper methods for risk scoring calculations

func (srs *SecurityRiskScorer) calculateOverallRiskScore(assessment *RiskAssessment) float64 {
	// Weighted average of category scores
	weights := map[string]float64{
		"identity":    0.3,
		"network":     0.2,
		"data":        0.2,
		"compliance":  0.15,
		"operational": 0.15,
	}

	overallScore := assessment.IdentityRiskScore*weights["identity"] +
		assessment.NetworkRiskScore*weights["network"] +
		assessment.DataRiskScore*weights["data"] +
		assessment.ComplianceRiskScore*weights["compliance"] +
		assessment.OperationalRiskScore*weights["operational"]

	return math.Min(overallScore, 1.0)
}

func (srs *SecurityRiskScorer) calculateCategoryRiskScore(riskFactors []RiskFactor) float64 {
	if len(riskFactors) == 0 {
		return 0.0
	}

	var totalScore float64
	var weightedSum float64

	for _, factor := range riskFactors {
		weight := srs.getSeverityWeight(factor.Severity)
		totalScore += factor.RiskScore * weight
		weightedSum += weight
	}

	if weightedSum == 0 {
		return 0.0
	}

	return math.Min(totalScore/weightedSum, 1.0)
}

func (srs *SecurityRiskScorer) getSeverityWeight(severity string) float64 {
	switch strings.ToUpper(severity) {
	case "CRITICAL":
		return 1.0
	case "HIGH":
		return 0.8
	case "MEDIUM":
		return 0.6
	case "LOW":
		return 0.4
	default:
		return 0.5
	}
}

func (srs *SecurityRiskScorer) determineRiskLevel(score float64) string {
	if score >= 0.8 {
		return "CRITICAL"
	} else if score >= 0.6 {
		return "HIGH"
	} else if score >= 0.4 {
		return "MEDIUM"
	} else {
		return "LOW"
	}
}

func (srs *SecurityRiskScorer) categorizeRiskFactors(riskFactors []RiskFactor, assessment *RiskAssessment) {
	for _, factor := range riskFactors {
		switch strings.ToUpper(factor.Severity) {
		case "CRITICAL", "HIGH":
			assessment.HighRiskFactors = append(assessment.HighRiskFactors, factor)
		case "MEDIUM":
			assessment.MediumRiskFactors = append(assessment.MediumRiskFactors, factor)
		case "LOW":
			assessment.LowRiskFactors = append(assessment.LowRiskFactors, factor)
		}
	}
}

func (srs *SecurityRiskScorer) generateFindingsSummary(assessment *RiskAssessment) FindingsSummary {
	summary := FindingsSummary{
		CriticalFindings: len(assessment.HighRiskFactors), // Simplification
		HighFindings:     len(assessment.HighRiskFactors),
		MediumFindings:   len(assessment.MediumRiskFactors),
		LowFindings:      len(assessment.LowRiskFactors),
	}
	summary.TotalFindings = summary.CriticalFindings + summary.HighFindings + summary.MediumFindings + summary.LowFindings
	return summary
}

func (srs *SecurityRiskScorer) generateCrossCloudSummary(ctx context.Context) CrossCloudSummary {
	summary := CrossCloudSummary{}

	// Count cross-cloud correlations
	if count, err := srs.countCrossCloudCorrelations(ctx); err == nil {
		summary.CrossCloudCorrelations = count
	}

	// Count federation risks
	if count, err := srs.countFederationRisks(ctx); err == nil {
		summary.FederationRisks = count
	}

	// Count policy similarities
	if count, err := srs.countPolicySimilarities(ctx); err == nil {
		summary.PolicySimilarities = count
	}

	// Count certificate issues
	if count, err := srs.countCertificateIssues(ctx); err == nil {
		summary.CertificateIssues = count
	}

	// Count escalation paths
	if count, err := srs.countEscalationPaths(ctx); err == nil {
		summary.EscalationPaths = count
	}

	return summary
}

// Helper methods for counting different types of findings

func (srs *SecurityRiskScorer) countCrossCloudCorrelations(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM cross_cloud_correlations WHERE status = 'active'`
	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

func (srs *SecurityRiskScorer) countFederationRisks(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM identity_federation_relationships WHERE status = 'active' AND security_risk_score >= 0.6`
	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

func (srs *SecurityRiskScorer) countPolicySimilarities(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM policy_similarity_analysis WHERE status = 'active' AND risk_score >= 0.6`
	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

func (srs *SecurityRiskScorer) countCertificateIssues(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM certificate_correlations WHERE status = 'active' AND security_risk_score >= 0.6`
	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

func (srs *SecurityRiskScorer) countEscalationPaths(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM privilege_escalation_paths WHERE status = 'open' AND risk_score >= 0.6`
	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

// Likelihood calculation helpers

func (srs *SecurityRiskScorer) calculateFederationLikelihood(riskLevel string, count int) float64 {
	baseLikelihood := map[string]float64{
		"CRITICAL": 0.9,
		"HIGH":     0.7,
		"MEDIUM":   0.5,
		"LOW":      0.3,
	}

	likelihood := baseLikelihood[riskLevel]
	if likelihood == 0 {
		likelihood = 0.5
	}

	// Increase likelihood with more occurrences
	likelihood += math.Min(float64(count)/10.0, 0.3)
	return math.Min(likelihood, 1.0)
}

func (srs *SecurityRiskScorer) calculateRoleLikelihood(count int) float64 {
	return math.Min(0.5+float64(count)/20.0, 0.9)
}

func (srs *SecurityRiskScorer) calculateEscalationLikelihood(riskLevel string, count int) float64 {
	baseLikelihood := map[string]float64{
		"CRITICAL": 0.8,
		"HIGH":     0.6,
		"MEDIUM":   0.4,
		"LOW":      0.2,
	}

	likelihood := baseLikelihood[riskLevel]
	likelihood += math.Min(float64(count)/5.0, 0.2)
	return math.Min(likelihood, 1.0)
}

func (srs *SecurityRiskScorer) calculateCertificateLikelihood(riskLevel string, count int) float64 {
	baseLikelihood := map[string]float64{
		"CRITICAL": 0.7,
		"HIGH":     0.5,
		"MEDIUM":   0.3,
		"LOW":      0.1,
	}

	likelihood := baseLikelihood[riskLevel]
	likelihood += math.Min(float64(count)/15.0, 0.2)
	return math.Min(likelihood, 1.0)
}

func (srs *SecurityRiskScorer) calculatePolicyLikelihood(riskLevel string, count int) float64 {
	baseLikelihood := map[string]float64{
		"CRITICAL": 0.6,
		"HIGH":     0.4,
		"MEDIUM":   0.3,
		"LOW":      0.2,
	}

	likelihood := baseLikelihood[riskLevel]
	likelihood += math.Min(float64(count)/25.0, 0.2)
	return math.Min(likelihood, 1.0)
}

func (srs *SecurityRiskScorer) determineComplianceSeverity(violations int) string {
	if violations >= 10 {
		return "CRITICAL"
	} else if violations >= 5 {
		return "HIGH"
	} else if violations >= 2 {
		return "MEDIUM"
	} else {
		return "LOW"
	}
}

// Recommendation generation methods

func (srs *SecurityRiskScorer) generateImmediateActions(assessment *RiskAssessment) []Action {
	var actions []Action

	if assessment.OverallRiskScore >= 0.8 {
		actions = append(actions, Action{
			ID:           "immediate-001",
			Name:         "Emergency Security Review",
			Description:  "Conduct immediate review of critical security findings",
			Priority:     "CRITICAL",
			Effort:       "high",
			Timeline:     "24 hours",
			Stakeholders: []string{"CISO", "Security Team", "Cloud Architects"},
		})
	}

	if len(assessment.HighRiskFactors) > 0 {
		actions = append(actions, Action{
			ID:           "immediate-002",
			Name:         "Address High-Risk Factors",
			Description:  "Prioritize remediation of high-risk security factors",
			Priority:     "HIGH",
			Effort:       "medium",
			Timeline:     "72 hours",
			Stakeholders: []string{"Security Team", "DevOps"},
		})
	}

	return actions
}

func (srs *SecurityRiskScorer) generateShortTermRecommendations(assessment *RiskAssessment) []Action {
	var actions []Action

	actions = append(actions, Action{
		ID:           "short-001",
		Name:         "Implement MFA for Cross-Cloud Access",
		Description:  "Enable multi-factor authentication for all cross-cloud access scenarios",
		Priority:     "HIGH",
		Effort:       "medium",
		Timeline:     "2 weeks",
		Stakeholders: []string{"Security Team", "Cloud Teams"},
	})

	if assessment.IdentityRiskScore >= 0.6 {
		actions = append(actions, Action{
			ID:           "short-002",
			Name:         "Review Identity Federation Policies",
			Description:  "Comprehensive review and hardening of identity federation configurations",
			Priority:     "HIGH",
			Effort:       "high",
			Timeline:     "3 weeks",
			Stakeholders: []string{"Identity Team", "Security Architects"},
		})
	}

	return actions
}

func (srs *SecurityRiskScorer) generateLongTermRecommendations(assessment *RiskAssessment) []Action {
	var actions []Action

	actions = append(actions, Action{
		ID:           "long-001",
		Name:         "Implement Zero Trust Architecture",
		Description:  "Develop and implement comprehensive zero trust security model",
		Priority:     "MEDIUM",
		Effort:       "high",
		Timeline:     "6 months",
		Stakeholders: []string{"Security Team", "Cloud Architects", "Engineering"},
	})

	actions = append(actions, Action{
		ID:           "long-002",
		Name:         "Automated Security Monitoring",
		Description:  "Deploy automated monitoring and alerting for cross-cloud security events",
		Priority:     "MEDIUM",
		Effort:       "high",
		Timeline:     "4 months",
		Stakeholders: []string{"Security Team", "SRE", "DevOps"},
	})

	return actions
}

// Compliance assessment methods

func (srs *SecurityRiskScorer) assessComplianceFrameworks(ctx context.Context) []ComplianceFramework {
	frameworks := []ComplianceFramework{
		{Name: "CIS", Version: "1.4", Score: 0.75, Status: "partial", LastUpdated: time.Now()},
		{Name: "SOC2", Version: "2017", Score: 0.80, Status: "compliant", LastUpdated: time.Now()},
		{Name: "PCI-DSS", Version: "3.2", Score: 0.65, Status: "partial", LastUpdated: time.Now()},
		{Name: "NIST", Version: "1.1", Score: 0.70, Status: "partial", LastUpdated: time.Now()},
	}
	return frameworks
}

func (srs *SecurityRiskScorer) calculateComplianceScores(frameworks []ComplianceFramework) map[string]float64 {
	scores := make(map[string]float64)
	for _, framework := range frameworks {
		scores[framework.Name] = framework.Score
	}
	return scores
}

func (srs *SecurityRiskScorer) identifyComplianceGaps(ctx context.Context) []ComplianceGap {
	var gaps []ComplianceGap

	query := `
	SELECT framework_name, control_id, control_name, remediation_plan
	FROM compliance_mappings
	WHERE compliance_status = 'non_compliant'
	LIMIT 10
	`

	rows, err := srs.db.QueryContext(ctx, query)
	if err != nil {
		return gaps
	}
	defer rows.Close()

	for rows.Next() {
		var framework, controlID, controlName, remediation string
		if err := rows.Scan(&framework, &controlID, &controlName, &remediation); err == nil {
			gaps = append(gaps, ComplianceGap{
				Framework:   framework,
				ControlID:   controlID,
				ControlName: controlName,
				GapType:     "non_compliant",
				Description: fmt.Sprintf("Control %s is not compliant", controlID),
				Priority:    "HIGH",
				Remediation: remediation,
			})
		}
	}

	return gaps
}

func (srs *SecurityRiskScorer) calculateImprovementScore(ctx context.Context, assessment *RiskAssessment) float64 {
	// Placeholder for improvement calculation
	// Would compare with previous assessment scores
	return 0.0
}

func (srs *SecurityRiskScorer) generateExecutiveSummary(assessment *RiskAssessment) string {
	return fmt.Sprintf(
		"Security Risk Assessment Summary: Overall risk level is %s with a score of %.2f. "+
			"Key areas of concern include identity management (%.2f), network security (%.2f), "+
			"and data protection (%.2f). Immediate attention required for %d critical findings.",
		assessment.RiskLevel,
		assessment.OverallRiskScore,
		assessment.IdentityRiskScore,
		assessment.NetworkRiskScore,
		assessment.DataRiskScore,
		assessment.FindingsSummary.CriticalFindings,
	)
}

func (srs *SecurityRiskScorer) generateAssessmentMetadata(scope map[string]interface{}) AssessmentMetadata {
	return AssessmentMetadata{
		AssessmentMethod: "automated",
		Assessor:         "Corkscrew Security Analyzer",
		DataSources:      []string{"AWS", "Azure", "GCP"},
		AnalysisTools:    []string{"Identity Correlator", "Security Correlator", "Policy Analyzer", "Certificate Analyzer"},
	}
}

// PersistRiskAssessment saves risk assessment to database
func (srs *SecurityRiskScorer) PersistRiskAssessment(ctx context.Context, assessment *RiskAssessment) error {
	tx, err := srs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT OR REPLACE INTO security_risk_assessments (
		id, assessment_type, scope_definition, overall_risk_score, risk_level, risk_trend,
		identity_risk_score, network_risk_score, data_risk_score, compliance_risk_score, operational_risk_score,
		high_risk_factors, medium_risk_factors, low_risk_factors,
		critical_findings, high_findings, medium_findings, low_findings,
		cross_cloud_correlations, federation_risks, policy_similarities, certificate_issues, escalation_paths,
		immediate_actions, short_term_recommendations, long_term_recommendations,
		compliance_frameworks, compliance_scores, compliance_gaps,
		assessment_method, assessor, assessment_scope_accounts, assessment_scope_regions, assessment_scope_services,
		baseline_assessment_id, previous_assessment_id, improvement_score,
		status, assessment_version, assessment_start_time, assessment_end_time,
		created_at, updated_at, executive_summary, detailed_report_url, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Convert complex fields to JSON
	scopeJSON, _ := json.Marshal(assessment.ScopeDefinition)
	highRiskFactorsJSON, _ := json.Marshal(assessment.HighRiskFactors)
	mediumRiskFactorsJSON, _ := json.Marshal(assessment.MediumRiskFactors)
	lowRiskFactorsJSON, _ := json.Marshal(assessment.LowRiskFactors)
	immediateActionsJSON, _ := json.Marshal(assessment.ImmediateActions)
	shortTermRecommendationsJSON, _ := json.Marshal(assessment.ShortTermRecommendations)
	longTermRecommendationsJSON, _ := json.Marshal(assessment.LongTermRecommendations)
	complianceFrameworksJSON, _ := json.Marshal(assessment.ComplianceFrameworks)
	complianceScoresJSON, _ := json.Marshal(assessment.ComplianceScores)
	complianceGapsJSON, _ := json.Marshal(assessment.ComplianceGaps)
	metadataJSON, _ := json.Marshal(assessment.AssessmentMetadata)

	_, err = stmt.Exec(
		assessment.ID, assessment.AssessmentType, string(scopeJSON),
		assessment.OverallRiskScore, assessment.RiskLevel, assessment.RiskTrend,
		assessment.IdentityRiskScore, assessment.NetworkRiskScore, assessment.DataRiskScore,
		assessment.ComplianceRiskScore, assessment.OperationalRiskScore,
		string(highRiskFactorsJSON), string(mediumRiskFactorsJSON), string(lowRiskFactorsJSON),
		assessment.FindingsSummary.CriticalFindings, assessment.FindingsSummary.HighFindings,
		assessment.FindingsSummary.MediumFindings, assessment.FindingsSummary.LowFindings,
		assessment.CrossCloudSummary.CrossCloudCorrelations, assessment.CrossCloudSummary.FederationRisks,
		assessment.CrossCloudSummary.PolicySimilarities, assessment.CrossCloudSummary.CertificateIssues,
		assessment.CrossCloudSummary.EscalationPaths,
		string(immediateActionsJSON), string(shortTermRecommendationsJSON), string(longTermRecommendationsJSON),
		string(complianceFrameworksJSON), string(complianceScoresJSON), string(complianceGapsJSON),
		assessment.AssessmentMetadata.AssessmentMethod, assessment.AssessmentMetadata.Assessor,
		strings.Join(assessment.AssessmentMetadata.ScopeAccounts, ","),
		strings.Join(assessment.AssessmentMetadata.ScopeRegions, ","),
		strings.Join(assessment.AssessmentMetadata.ScopeServices, ","),
		assessment.BaselineAssessmentID, assessment.PreviousAssessmentID, assessment.ImprovementScore,
		assessment.Status, assessment.AssessmentVersion,
		assessment.AssessmentStartTime, assessment.AssessmentEndTime,
		assessment.CreatedAt, assessment.UpdatedAt,
		assessment.ExecutiveSummary, assessment.DetailedReportURL, string(metadataJSON),
	)

	if err != nil {
		return err
	}

	return tx.Commit()
}
