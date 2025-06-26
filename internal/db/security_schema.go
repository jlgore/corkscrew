package db

import (
	"fmt"
)

// createSecurityTables creates tables for security relationships and analysis
func (c *UnifiedDatabaseConfig) createSecurityTables() error {
	// Create identity federation table
	if err := c.createIdentityFederationTable(); err != nil {
		return fmt.Errorf("failed to create identity federation table: %w", err)
	}

	// Create security role relationships table
	if err := c.createSecurityRoleRelationshipsTable(); err != nil {
		return fmt.Errorf("failed to create security role relationships table: %w", err)
	}

	// Create policy similarity table
	if err := c.createPolicySimilarityTable(); err != nil {
		return fmt.Errorf("failed to create policy similarity table: %w", err)
	}

	// Create certificate correlation table
	if err := c.createCertificateCorrelationTable(); err != nil {
		return fmt.Errorf("failed to create certificate correlation table: %w", err)
	}

	// Create privilege escalation table
	if err := c.createPrivilegeEscalationTable(); err != nil {
		return fmt.Errorf("failed to create privilege escalation table: %w", err)
	}

	// Create security risk assessment table
	if err := c.createSecurityRiskAssessmentTable(); err != nil {
		return fmt.Errorf("failed to create security risk assessment table: %w", err)
	}

	// Create compliance mapping table
	if err := c.createComplianceMappingTable(); err != nil {
		return fmt.Errorf("failed to create compliance mapping table: %w", err)
	}

	// Create shared secrets table
	if err := c.createSharedSecretsTable(); err != nil {
		return fmt.Errorf("failed to create shared secrets table: %w", err)
	}

	return nil
}

// createIdentityFederationTable creates table for identity federation relationships
func (c *UnifiedDatabaseConfig) createIdentityFederationTable() error {
	federationSQL := `
CREATE TABLE IF NOT EXISTS identity_federation_relationships (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique relationship ID
    
    -- Source identity provider
    source_provider_id VARCHAR NOT NULL,      -- Source provider resource ID
    source_provider_type VARCHAR NOT NULL,    -- OIDC, SAML, OAuth2, Role
    source_provider_name VARCHAR,             -- Provider name
    source_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    source_region VARCHAR,                     -- Region
    source_account_id VARCHAR,                -- Account/subscription ID
    
    -- Target identity provider
    target_provider_id VARCHAR NOT NULL,      -- Target provider resource ID
    target_provider_type VARCHAR NOT NULL,    -- OIDC, SAML, OAuth2, Role
    target_provider_name VARCHAR,             -- Provider name
    target_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    target_region VARCHAR,                     -- Region
    target_account_id VARCHAR,                -- Account/subscription ID
    
    -- Federation details
    federation_type VARCHAR NOT NULL,         -- trust_policy, oidc_federation, saml_federation, jwt_issuer_correlation
    federation_method VARCHAR,                -- How federation was established
    trust_policy JSON,                        -- Trust policy document
    trust_conditions JSON,                    -- Trust conditions array
    
    -- OIDC/OAuth specific
    oidc_issuer VARCHAR,                      -- OIDC issuer URL
    oidc_endpoints JSON,                      -- OIDC endpoints array
    client_ids JSON,                          -- Client IDs/audiences
    scopes JSON,                              -- OAuth scopes
    
    -- SAML specific
    saml_entity_id VARCHAR,                   -- SAML entity ID
    saml_sso_endpoint VARCHAR,                -- SAML SSO endpoint
    saml_metadata_document TEXT,              -- SAML metadata document
    
    -- Certificate details
    certificate_thumbprints JSON,             -- Certificate thumbprints
    signing_certificates JSON,                -- Signing certificates
    
    -- Confidence and evidence
    confidence_score DOUBLE NOT NULL,         -- Confidence in relationship (0-1)
    evidence JSON,                           -- Evidence supporting relationship
    matching_attributes JSON,                -- Attributes that match
    
    -- Security assessment
    security_risk_level VARCHAR,             -- LOW, MEDIUM, HIGH, CRITICAL
    security_risk_score DOUBLE,              -- Security risk score (0-1)
    security_issues JSON,                    -- Array of security issues
    recommendations JSON,                    -- Security recommendations
    
    -- Status and verification
    status VARCHAR DEFAULT 'active',          -- active, inactive, pending_verification
    verified BOOLEAN DEFAULT FALSE,           -- Whether relationship is verified
    verification_method VARCHAR,              -- How it was verified
    last_verified_at TIMESTAMP,              -- Last verification time
    
    -- Timestamps
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON,                           -- Additional metadata
    tags JSON                                -- Tags for categorization
);`

	if _, err := c.DB.Exec(federationSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_fed_source_provider ON identity_federation_relationships(source_provider_id)",
		"CREATE INDEX IF NOT EXISTS idx_fed_target_provider ON identity_federation_relationships(target_provider_id)",
		"CREATE INDEX IF NOT EXISTS idx_fed_type ON identity_federation_relationships(federation_type)",
		"CREATE INDEX IF NOT EXISTS idx_fed_source_cloud ON identity_federation_relationships(source_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_fed_target_cloud ON identity_federation_relationships(target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_fed_confidence ON identity_federation_relationships(confidence_score)",
		"CREATE INDEX IF NOT EXISTS idx_fed_risk_level ON identity_federation_relationships(security_risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_fed_status ON identity_federation_relationships(status)",
		"CREATE INDEX IF NOT EXISTS idx_fed_cross_cloud ON identity_federation_relationships(source_cloud_provider, target_cloud_provider)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createSecurityRoleRelationshipsTable creates table for cross-account security roles
func (c *UnifiedDatabaseConfig) createSecurityRoleRelationshipsTable() error {
	roleSQL := `
CREATE TABLE IF NOT EXISTS security_role_relationships (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique relationship ID
    
    -- Source role
    source_role_id VARCHAR NOT NULL,          -- Source role resource ID
    source_role_arn VARCHAR,                  -- Role ARN
    source_role_name VARCHAR NOT NULL,        -- Role name
    source_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    source_region VARCHAR,                     -- Region
    source_account_id VARCHAR,                -- Account/subscription ID
    
    -- Target role
    target_role_id VARCHAR NOT NULL,          -- Target role resource ID
    target_role_arn VARCHAR,                  -- Role ARN
    target_role_name VARCHAR NOT NULL,        -- Role name
    target_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    target_region VARCHAR,                     -- Region
    target_account_id VARCHAR,                -- Account/subscription ID
    
    -- Relationship details
    relationship_type VARCHAR NOT NULL,       -- cross_account_trust, service_principal_access, role_assumption_chain
    assumption_chain JSON,                    -- Role assumption chain
    trusted_principals JSON,                  -- Trusted principals array
    trust_conditions JSON,                    -- Trust conditions array
    
    -- Permissions
    source_permissions JSON,                  -- Source role permissions
    target_permissions JSON,                  -- Target role permissions
    effective_permissions JSON,               -- Effective permissions when assumed
    
    -- Security analysis
    confidence_score DOUBLE NOT NULL,         -- Confidence in relationship (0-1)
    risk_score DOUBLE,                        -- Risk score (0-1)
    escalation_paths JSON,                    -- Privilege escalation paths
    security_issues JSON,                     -- Security issues detected
    recommendations JSON,                     -- Security recommendations
    
    -- Evidence
    evidence JSON,                           -- Evidence supporting relationship
    detection_method VARCHAR,                 -- How relationship was detected
    
    -- Status
    status VARCHAR DEFAULT 'active',          -- active, inactive, remediated
    verified BOOLEAN DEFAULT FALSE,           -- Whether relationship is verified
    remediation_status VARCHAR,               -- open, in_progress, resolved
    
    -- Timestamps
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON,                           -- Additional metadata
    compliance_tags JSON                     -- Compliance framework tags
);`

	if _, err := c.DB.Exec(roleSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_role_source_id ON security_role_relationships(source_role_id)",
		"CREATE INDEX IF NOT EXISTS idx_role_target_id ON security_role_relationships(target_role_id)",
		"CREATE INDEX IF NOT EXISTS idx_role_type ON security_role_relationships(relationship_type)",
		"CREATE INDEX IF NOT EXISTS idx_role_source_cloud ON security_role_relationships(source_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_role_target_cloud ON security_role_relationships(target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_role_risk_score ON security_role_relationships(risk_score)",
		"CREATE INDEX IF NOT EXISTS idx_role_status ON security_role_relationships(status)",
		"CREATE INDEX IF NOT EXISTS idx_role_cross_cloud ON security_role_relationships(source_cloud_provider, target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_role_cross_account ON security_role_relationships(source_account_id, target_account_id)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createPolicySimilarityTable creates table for policy similarity analysis
func (c *UnifiedDatabaseConfig) createPolicySimilarityTable() error {
	policySQL := `
CREATE TABLE IF NOT EXISTS policy_similarity_analysis (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique similarity ID
    
    -- Source policy
    source_policy_id VARCHAR NOT NULL,        -- Source policy resource ID
    source_policy_name VARCHAR NOT NULL,      -- Policy name
    source_policy_type VARCHAR NOT NULL,      -- inline, managed, resource, trust, role
    source_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    source_region VARCHAR,                     -- Region
    source_account_id VARCHAR,                -- Account/subscription ID
    source_resource_id VARCHAR,               -- Associated resource ID
    
    -- Target policy
    target_policy_id VARCHAR NOT NULL,        -- Target policy resource ID
    target_policy_name VARCHAR NOT NULL,      -- Policy name
    target_policy_type VARCHAR NOT NULL,      -- inline, managed, resource, trust, role
    target_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    target_region VARCHAR,                     -- Region
    target_account_id VARCHAR,                -- Account/subscription ID
    target_resource_id VARCHAR,               -- Associated resource ID
    
    -- Similarity analysis
    similarity_score DOUBLE NOT NULL,         -- Similarity score (0-1)
    similarity_type VARCHAR NOT NULL,         -- identical, nearly_identical, highly_similar, moderately_similar, low_similarity
    matching_elements JSON,                   -- Elements that match
    differences JSON,                         -- Differences found
    normalized_permissions JSON,              -- Normalized permissions for comparison
    
    -- Policy details
    source_policy_hash VARCHAR,               -- Hash of source policy
    target_policy_hash VARCHAR,               -- Hash of target policy
    source_statements JSON,                   -- Source policy statements
    target_statements JSON,                   -- Target policy statements
    
    -- Risk assessment
    risk_level VARCHAR,                       -- LOW, MEDIUM, HIGH, CRITICAL
    risk_score DOUBLE,                        -- Risk score (0-1)
    risk_factors JSON,                        -- Risk factors array
    security_issues JSON,                     -- Security issues detected
    recommendations JSON,                     -- Security recommendations
    compliance_tags JSON,                     -- Compliance framework tags
    
    -- Analysis metadata
    analysis_method VARCHAR,                  -- automated_analysis, manual_review
    confidence_score DOUBLE,                  -- Confidence in analysis
    false_positive_likelihood DOUBLE,         -- Likelihood of false positive
    
    -- Status
    status VARCHAR DEFAULT 'active',          -- active, dismissed, under_review
    reviewed BOOLEAN DEFAULT FALSE,           -- Whether similarity has been reviewed
    reviewer VARCHAR,                         -- Who reviewed it
    review_notes TEXT,                        -- Review notes
    
    -- Timestamps
    detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON                            -- Additional metadata
);`

	if _, err := c.DB.Exec(policySQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_pol_source_id ON policy_similarity_analysis(source_policy_id)",
		"CREATE INDEX IF NOT EXISTS idx_pol_target_id ON policy_similarity_analysis(target_policy_id)",
		"CREATE INDEX IF NOT EXISTS idx_pol_similarity_score ON policy_similarity_analysis(similarity_score)",
		"CREATE INDEX IF NOT EXISTS idx_pol_similarity_type ON policy_similarity_analysis(similarity_type)",
		"CREATE INDEX IF NOT EXISTS idx_pol_source_cloud ON policy_similarity_analysis(source_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_pol_target_cloud ON policy_similarity_analysis(target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_pol_risk_level ON policy_similarity_analysis(risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_pol_status ON policy_similarity_analysis(status)",
		"CREATE INDEX IF NOT EXISTS idx_pol_cross_cloud ON policy_similarity_analysis(source_cloud_provider, target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_pol_cross_account ON policy_similarity_analysis(source_account_id, target_account_id)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createCertificateCorrelationTable creates table for certificate correlations
func (c *UnifiedDatabaseConfig) createCertificateCorrelationTable() error {
	certSQL := `
CREATE TABLE IF NOT EXISTS certificate_correlations (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique correlation ID
    
    -- Source certificate
    source_cert_id VARCHAR NOT NULL,          -- Source certificate resource ID
    source_cert_name VARCHAR,                 -- Certificate name
    source_cert_thumbprint VARCHAR,           -- Certificate thumbprint
    source_cert_serial_number VARCHAR,        -- Serial number
    source_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    source_region VARCHAR,                     -- Region
    source_account_id VARCHAR,                -- Account/subscription ID
    source_resource_id VARCHAR,               -- Associated resource ID
    
    -- Target certificate
    target_cert_id VARCHAR NOT NULL,          -- Target certificate resource ID
    target_cert_name VARCHAR,                 -- Certificate name
    target_cert_thumbprint VARCHAR,           -- Certificate thumbprint
    target_cert_serial_number VARCHAR,        -- Serial number
    target_cloud_provider VARCHAR NOT NULL,   -- aws, azure, gcp
    target_region VARCHAR,                     -- Region
    target_account_id VARCHAR,                -- Account/subscription ID
    target_resource_id VARCHAR,               -- Associated resource ID
    
    -- Correlation details
    correlation_type VARCHAR NOT NULL,        -- thumbprint_match, issuer_match, subject_match, san_match, chain_relationship
    chain_relationship VARCHAR,               -- issuer_to_leaf, leaf_to_issuer, sibling_certificates
    confidence_score DOUBLE NOT NULL,         -- Confidence in correlation (0-1)
    matching_attributes JSON,                 -- Attributes that match
    
    -- Certificate details
    source_cert_details JSON,                -- Source certificate details
    target_cert_details JSON,                -- Target certificate details
    shared_attributes JSON,                   -- Shared attributes
    
    -- Subject and issuer information
    source_subject VARCHAR,                   -- Source certificate subject
    source_issuer VARCHAR,                    -- Source certificate issuer
    source_common_name VARCHAR,               -- Source common name
    source_sans JSON,                         -- Source SANs
    target_subject VARCHAR,                   -- Target certificate subject
    target_issuer VARCHAR,                    -- Target certificate issuer
    target_common_name VARCHAR,               -- Target common name
    target_sans JSON,                         -- Target SANs
    
    -- Validity information
    source_not_before TIMESTAMP,             -- Source certificate valid from
    source_not_after TIMESTAMP,              -- Source certificate valid until
    target_not_before TIMESTAMP,             -- Target certificate valid from
    target_not_after TIMESTAMP,              -- Target certificate valid until
    
    -- Security assessment
    security_risk_level VARCHAR,             -- LOW, MEDIUM, HIGH, CRITICAL
    security_risk_score DOUBLE,              -- Security risk score (0-1)
    security_issues JSON,                    -- Security issues detected
    recommendations JSON,                    -- Security recommendations
    compliance_flags JSON,                  -- Compliance flags
    
    -- Shared secrets
    shared_secrets JSON,                     -- Related shared secrets
    secret_correlations JSON,               -- Secret correlation details
    
    -- Status
    status VARCHAR DEFAULT 'active',         -- active, expired, revoked, dismissed
    verified BOOLEAN DEFAULT FALSE,          -- Whether correlation is verified
    
    -- Timestamps
    detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_verified_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON                           -- Additional metadata
);`

	if _, err := c.DB.Exec(certSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_cert_source_id ON certificate_correlations(source_cert_id)",
		"CREATE INDEX IF NOT EXISTS idx_cert_target_id ON certificate_correlations(target_cert_id)",
		"CREATE INDEX IF NOT EXISTS idx_cert_source_thumbprint ON certificate_correlations(source_cert_thumbprint)",
		"CREATE INDEX IF NOT EXISTS idx_cert_target_thumbprint ON certificate_correlations(target_cert_thumbprint)",
		"CREATE INDEX IF NOT EXISTS idx_cert_correlation_type ON certificate_correlations(correlation_type)",
		"CREATE INDEX IF NOT EXISTS idx_cert_source_cloud ON certificate_correlations(source_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_cert_target_cloud ON certificate_correlations(target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_cert_risk_level ON certificate_correlations(security_risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_cert_status ON certificate_correlations(status)",
		"CREATE INDEX IF NOT EXISTS idx_cert_cross_cloud ON certificate_correlations(source_cloud_provider, target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_cert_expiry ON certificate_correlations(source_not_after, target_not_after)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createPrivilegeEscalationTable creates table for privilege escalation analysis
func (c *UnifiedDatabaseConfig) createPrivilegeEscalationTable() error {
	escalationSQL := `
CREATE TABLE IF NOT EXISTS privilege_escalation_paths (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique escalation path ID
    
    -- Associated relationship
    relationship_id VARCHAR NOT NULL,         -- Related relationship ID
    relationship_type VARCHAR NOT NULL,       -- identity_federation, security_role, policy_similarity, etc.
    
    -- Escalation path details
    path_type VARCHAR NOT NULL,               -- role_assumption, policy_exploitation, credential_misuse, etc.
    escalation_steps JSON NOT NULL,          -- Array of escalation steps
    entry_point VARCHAR NOT NULL,            -- Starting point of escalation
    target_privilege VARCHAR NOT NULL,       -- Target privilege/permission
    
    -- Path analysis
    step_count INTEGER NOT NULL,             -- Number of steps in path
    complexity_score DOUBLE,                 -- Path complexity (0-1)
    feasibility_score DOUBLE,               -- Feasibility of exploitation (0-1)
    
    -- Risk assessment
    risk_level VARCHAR NOT NULL,             -- LOW, MEDIUM, HIGH, CRITICAL
    risk_score DOUBLE NOT NULL,              -- Risk score (0-1)
    impact_score DOUBLE,                     -- Impact if exploited (0-1)
    likelihood_score DOUBLE,                -- Likelihood of exploitation (0-1)
    
    -- Attack vectors
    attack_vectors JSON,                     -- Possible attack vectors
    prerequisites JSON,                      -- Prerequisites for exploitation
    indicators JSON,                         -- Indicators of compromise
    
    -- Affected resources
    source_cloud_provider VARCHAR NOT NULL,  -- Source cloud provider
    target_cloud_provider VARCHAR NOT NULL,  -- Target cloud provider
    source_account_id VARCHAR,               -- Source account
    target_account_id VARCHAR,               -- Target account
    affected_resources JSON,                 -- Resources affected by escalation
    
    -- Mitigation
    mitigations JSON,                        -- Recommended mitigations
    controls JSON,                           -- Security controls to implement
    detection_methods JSON,                  -- Detection methods
    response_procedures JSON,                -- Incident response procedures
    
    -- Status and tracking
    status VARCHAR DEFAULT 'open',           -- open, investigating, mitigated, false_positive
    assigned_to VARCHAR,                     -- Assigned security analyst
    priority VARCHAR,                        -- low, medium, high, critical
    remediation_deadline TIMESTAMP,          -- Deadline for remediation
    
    -- Analysis metadata
    detection_method VARCHAR,                -- How escalation was detected
    confidence_score DOUBLE,                 -- Confidence in analysis (0-1)
    false_positive_likelihood DOUBLE,        -- Likelihood of false positive (0-1)
    
    -- Compliance and frameworks
    compliance_violations JSON,              -- Compliance violations
    framework_mappings JSON,                -- Security framework mappings (MITRE ATT&CK, etc.)
    
    -- Timestamps
    detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON,                          -- Additional metadata
    analyst_notes TEXT                      -- Analyst notes
);`

	if _, err := c.DB.Exec(escalationSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_esc_relationship_id ON privilege_escalation_paths(relationship_id)",
		"CREATE INDEX IF NOT EXISTS idx_esc_path_type ON privilege_escalation_paths(path_type)",
		"CREATE INDEX IF NOT EXISTS idx_esc_risk_level ON privilege_escalation_paths(risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_esc_risk_score ON privilege_escalation_paths(risk_score)",
		"CREATE INDEX IF NOT EXISTS idx_esc_status ON privilege_escalation_paths(status)",
		"CREATE INDEX IF NOT EXISTS idx_esc_priority ON privilege_escalation_paths(priority)",
		"CREATE INDEX IF NOT EXISTS idx_esc_source_cloud ON privilege_escalation_paths(source_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_esc_target_cloud ON privilege_escalation_paths(target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_esc_cross_cloud ON privilege_escalation_paths(source_cloud_provider, target_cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_esc_deadline ON privilege_escalation_paths(remediation_deadline)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createSecurityRiskAssessmentTable creates table for security risk assessments
func (c *UnifiedDatabaseConfig) createSecurityRiskAssessmentTable() error {
	riskSQL := `
CREATE TABLE IF NOT EXISTS security_risk_assessments (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique assessment ID
    
    -- Assessment scope
    assessment_type VARCHAR NOT NULL,         -- cross_cloud, single_cloud, account, resource
    scope_definition JSON,                    -- Definition of assessment scope
    
    -- Risk scoring
    overall_risk_score DOUBLE NOT NULL,      -- Overall risk score (0-1)
    risk_level VARCHAR NOT NULL,             -- LOW, MEDIUM, HIGH, CRITICAL
    risk_trend VARCHAR,                      -- increasing, stable, decreasing
    
    -- Risk categories
    identity_risk_score DOUBLE,              -- Identity and access risk
    network_risk_score DOUBLE,               -- Network security risk
    data_risk_score DOUBLE,                  -- Data protection risk
    compliance_risk_score DOUBLE,            -- Compliance risk
    operational_risk_score DOUBLE,           -- Operational security risk
    
    -- Risk factors
    high_risk_factors JSON,                  -- High risk factors
    medium_risk_factors JSON,                -- Medium risk factors
    low_risk_factors JSON,                   -- Low risk factors
    
    -- Findings summary
    critical_findings INTEGER DEFAULT 0,     -- Number of critical findings
    high_findings INTEGER DEFAULT 0,         -- Number of high findings
    medium_findings INTEGER DEFAULT 0,       -- Number of medium findings
    low_findings INTEGER DEFAULT 0,          -- Number of low findings
    
    -- Cross-cloud specific risks
    cross_cloud_correlations INTEGER DEFAULT 0,  -- Number of cross-cloud correlations
    federation_risks INTEGER DEFAULT 0,      -- Number of federation risks
    policy_similarities INTEGER DEFAULT 0,   -- Number of policy similarities
    certificate_issues INTEGER DEFAULT 0,    -- Number of certificate issues
    escalation_paths INTEGER DEFAULT 0,      -- Number of escalation paths
    
    -- Recommendations
    immediate_actions JSON,                  -- Immediate actions required
    short_term_recommendations JSON,         -- Short-term recommendations
    long_term_recommendations JSON,          -- Long-term recommendations
    
    -- Compliance status
    compliance_frameworks JSON,             -- Assessed compliance frameworks
    compliance_scores JSON,                 -- Compliance scores by framework
    compliance_gaps JSON,                   -- Compliance gaps identified
    
    -- Assessment metadata
    assessment_method VARCHAR,              -- automated, manual, hybrid
    assessor VARCHAR,                       -- Who performed assessment
    assessment_scope_accounts JSON,         -- Accounts included in scope
    assessment_scope_regions JSON,          -- Regions included in scope
    assessment_scope_services JSON,         -- Services included in scope
    
    -- Baseline and comparison
    baseline_assessment_id VARCHAR,         -- Baseline assessment for comparison
    previous_assessment_id VARCHAR,         -- Previous assessment
    improvement_score DOUBLE,               -- Improvement since last assessment
    
    -- Status and tracking
    status VARCHAR DEFAULT 'completed',     -- draft, in_progress, completed, archived
    assessment_version VARCHAR,             -- Assessment version
    
    -- Timestamps
    assessment_start_time TIMESTAMP NOT NULL,   -- When assessment started
    assessment_end_time TIMESTAMP,          -- When assessment completed
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON,                          -- Additional metadata
    executive_summary TEXT,                 -- Executive summary
    detailed_report_url VARCHAR             -- URL to detailed report
);`

	if _, err := c.DB.Exec(riskSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_risk_assessment_type ON security_risk_assessments(assessment_type)",
		"CREATE INDEX IF NOT EXISTS idx_risk_overall_score ON security_risk_assessments(overall_risk_score)",
		"CREATE INDEX IF NOT EXISTS idx_risk_level ON security_risk_assessments(risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_risk_status ON security_risk_assessments(status)",
		"CREATE INDEX IF NOT EXISTS idx_risk_start_time ON security_risk_assessments(assessment_start_time)",
		"CREATE INDEX IF NOT EXISTS idx_risk_assessor ON security_risk_assessments(assessor)",
		"CREATE INDEX IF NOT EXISTS idx_risk_baseline ON security_risk_assessments(baseline_assessment_id)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createComplianceMappingTable creates table for compliance framework mappings
func (c *UnifiedDatabaseConfig) createComplianceMappingTable() error {
	complianceSQL := `
CREATE TABLE IF NOT EXISTS compliance_mappings (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                   -- Unique mapping ID
    
    -- Framework details
    framework_name VARCHAR NOT NULL,         -- CIS, SOC2, PCI-DSS, NIST, ISO27001, etc.
    framework_version VARCHAR,               -- Framework version
    control_id VARCHAR NOT NULL,             -- Control identifier
    control_name VARCHAR NOT NULL,           -- Control name
    control_description TEXT,                -- Control description
    
    -- Resource mapping
    resource_type VARCHAR NOT NULL,          -- Type of resource (correlation, relationship, etc.)
    resource_id VARCHAR NOT NULL,            -- Resource ID
    cloud_provider VARCHAR,                  -- Cloud provider
    account_id VARCHAR,                      -- Account/subscription ID
    
    -- Compliance status
    compliance_status VARCHAR NOT NULL,      -- compliant, non_compliant, partial, not_applicable
    compliance_score DOUBLE,                 -- Compliance score for this control (0-1)
    last_assessment_date TIMESTAMP,          -- Last assessment date
    
    -- Assessment details
    assessment_method VARCHAR,               -- automated, manual, hybrid
    evidence JSON,                          -- Evidence for compliance
    findings JSON,                          -- Assessment findings
    exceptions JSON,                        -- Approved exceptions
    
    -- Remediation
    remediation_required BOOLEAN DEFAULT FALSE,  -- Whether remediation is required
    remediation_priority VARCHAR,           -- low, medium, high, critical
    remediation_status VARCHAR,             -- open, in_progress, completed
    remediation_deadline TIMESTAMP,         -- Deadline for remediation
    remediation_owner VARCHAR,              -- Owner of remediation
    remediation_plan TEXT,                  -- Remediation plan
    
    -- Risk assessment
    risk_if_non_compliant VARCHAR,          -- Risk level if non-compliant
    business_impact TEXT,                   -- Business impact of non-compliance
    
    -- Audit trail
    auditor VARCHAR,                        -- Auditor who assessed
    audit_notes TEXT,                       -- Audit notes
    next_review_date TIMESTAMP,             -- Next review date
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON                           -- Additional metadata
);`

	if _, err := c.DB.Exec(complianceSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_comp_framework ON compliance_mappings(framework_name)",
		"CREATE INDEX IF NOT EXISTS idx_comp_control_id ON compliance_mappings(control_id)",
		"CREATE INDEX IF NOT EXISTS idx_comp_resource_id ON compliance_mappings(resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_comp_status ON compliance_mappings(compliance_status)",
		"CREATE INDEX IF NOT EXISTS idx_comp_cloud_provider ON compliance_mappings(cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_comp_remediation_status ON compliance_mappings(remediation_status)",
		"CREATE INDEX IF NOT EXISTS idx_comp_deadline ON compliance_mappings(remediation_deadline)",
		"CREATE INDEX IF NOT EXISTS idx_comp_next_review ON compliance_mappings(next_review_date)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createSharedSecretsTable creates table for shared secrets across clouds
func (c *UnifiedDatabaseConfig) createSharedSecretsTable() error {
	secretsSQL := `
CREATE TABLE IF NOT EXISTS shared_secrets_correlation (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                   -- Unique correlation ID
    
    -- Secret details
    secret_type VARCHAR NOT NULL,            -- certificate, key, ca_bundle, api_key, password
    secret_name VARCHAR NOT NULL,            -- Secret name/identifier
    secret_hash VARCHAR NOT NULL,            -- Hash of secret (not actual value)
    
    -- Provider details
    cloud_provider VARCHAR NOT NULL,         -- aws, azure, gcp
    region VARCHAR,                          -- Region
    account_id VARCHAR,                      -- Account/subscription ID
    resource_id VARCHAR NOT NULL,            -- Resource ID where secret is stored
    service_name VARCHAR,                    -- Service name (Secrets Manager, Key Vault, etc.)
    
    -- Secret metadata
    secret_description TEXT,                 -- Secret description
    secret_tags JSON,                        -- Secret tags
    secret_version VARCHAR,                  -- Secret version
    
    -- Usage correlation
    referenced_by JSON,                      -- Resources that reference this secret
    cross_cloud_references JSON,             -- Cross-cloud references
    usage_patterns JSON,                     -- Usage patterns
    
    -- Security assessment
    security_risk_level VARCHAR,             -- LOW, MEDIUM, HIGH, CRITICAL
    security_issues JSON,                    -- Security issues
    recommendations JSON,                    -- Security recommendations
    
    -- Lifecycle information
    created_date TIMESTAMP,                 -- When secret was created
    last_modified_date TIMESTAMP,           -- Last modification date
    expiry_date TIMESTAMP,                  -- Expiry date (if applicable)
    rotation_frequency VARCHAR,             -- Rotation frequency
    last_rotated_date TIMESTAMP,            -- Last rotation date
    
    -- Compliance
    compliance_requirements JSON,           -- Compliance requirements
    encryption_status VARCHAR,              -- Encryption status
    access_control_status VARCHAR,          -- Access control status
    
    -- Correlation details
    correlation_confidence DOUBLE,          -- Confidence in correlation (0-1)
    correlation_method VARCHAR,             -- How correlation was detected
    correlation_evidence JSON,              -- Evidence for correlation
    
    -- Status
    status VARCHAR DEFAULT 'active',        -- active, inactive, compromised, rotated
    monitoring_enabled BOOLEAN DEFAULT FALSE,   -- Whether monitoring is enabled
    
    -- Timestamps
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Additional metadata
    metadata JSON                           -- Additional metadata
);`

	if _, err := c.DB.Exec(secretsSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_secrets_type ON shared_secrets_correlation(secret_type)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_name ON shared_secrets_correlation(secret_name)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_hash ON shared_secrets_correlation(secret_hash)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_cloud_provider ON shared_secrets_correlation(cloud_provider)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_resource_id ON shared_secrets_correlation(resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_risk_level ON shared_secrets_correlation(security_risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_status ON shared_secrets_correlation(status)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_expiry ON shared_secrets_correlation(expiry_date)",
		"CREATE INDEX IF NOT EXISTS idx_secrets_account_id ON shared_secrets_correlation(account_id)",
	}

	for _, idx := range indexes {
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// CreateSecurityViews creates views for security analysis
func (c *UnifiedDatabaseConfig) CreateSecurityViews() error {
	// Create view for cross-cloud security summary
	securitySummaryView := `
CREATE OR REPLACE VIEW cross_cloud_security_summary AS
SELECT 
    'identity_federation' as correlation_type,
    source_cloud_provider,
    target_cloud_provider,
    COUNT(*) as correlation_count,
    AVG(confidence_score) as avg_confidence,
    COUNT(CASE WHEN security_risk_level = 'CRITICAL' THEN 1 END) as critical_risks,
    COUNT(CASE WHEN security_risk_level = 'HIGH' THEN 1 END) as high_risks,
    COUNT(CASE WHEN security_risk_level = 'MEDIUM' THEN 1 END) as medium_risks,
    COUNT(CASE WHEN security_risk_level = 'LOW' THEN 1 END) as low_risks
FROM identity_federation_relationships
WHERE status = 'active'
GROUP BY source_cloud_provider, target_cloud_provider

UNION ALL

SELECT 
    'security_roles' as correlation_type,
    source_cloud_provider,
    target_cloud_provider,
    COUNT(*) as correlation_count,
    AVG(confidence_score) as avg_confidence,
    COUNT(CASE WHEN risk_score >= 0.8 THEN 1 END) as critical_risks,
    COUNT(CASE WHEN risk_score >= 0.6 AND risk_score < 0.8 THEN 1 END) as high_risks,
    COUNT(CASE WHEN risk_score >= 0.4 AND risk_score < 0.6 THEN 1 END) as medium_risks,
    COUNT(CASE WHEN risk_score < 0.4 THEN 1 END) as low_risks
FROM security_role_relationships
WHERE status = 'active'
GROUP BY source_cloud_provider, target_cloud_provider

UNION ALL

SELECT 
    'policy_similarities' as correlation_type,
    source_cloud_provider,
    target_cloud_provider,
    COUNT(*) as correlation_count,
    AVG(similarity_score) as avg_confidence,
    COUNT(CASE WHEN risk_level = 'CRITICAL' THEN 1 END) as critical_risks,
    COUNT(CASE WHEN risk_level = 'HIGH' THEN 1 END) as high_risks,
    COUNT(CASE WHEN risk_level = 'MEDIUM' THEN 1 END) as medium_risks,
    COUNT(CASE WHEN risk_level = 'LOW' THEN 1 END) as low_risks
FROM policy_similarity_analysis
WHERE status = 'active'
GROUP BY source_cloud_provider, target_cloud_provider

UNION ALL

SELECT 
    'certificates' as correlation_type,
    source_cloud_provider,
    target_cloud_provider,
    COUNT(*) as correlation_count,
    AVG(confidence_score) as avg_confidence,
    COUNT(CASE WHEN security_risk_level = 'CRITICAL' THEN 1 END) as critical_risks,
    COUNT(CASE WHEN security_risk_level = 'HIGH' THEN 1 END) as high_risks,
    COUNT(CASE WHEN security_risk_level = 'MEDIUM' THEN 1 END) as medium_risks,
    COUNT(CASE WHEN security_risk_level = 'LOW' THEN 1 END) as low_risks
FROM certificate_correlations
WHERE status = 'active'
GROUP BY source_cloud_provider, target_cloud_provider;`

	if _, err := c.DB.Exec(securitySummaryView); err != nil {
		return fmt.Errorf("failed to create security summary view: %w", err)
	}

	// Create view for privilege escalation summary
	escalationSummaryView := `
CREATE OR REPLACE VIEW privilege_escalation_summary AS
SELECT 
    source_cloud_provider,
    target_cloud_provider,
    path_type,
    risk_level,
    COUNT(*) as path_count,
    AVG(risk_score) as avg_risk_score,
    AVG(step_count) as avg_steps,
    COUNT(CASE WHEN status = 'open' THEN 1 END) as open_paths,
    COUNT(CASE WHEN status = 'mitigated' THEN 1 END) as mitigated_paths
FROM privilege_escalation_paths
GROUP BY source_cloud_provider, target_cloud_provider, path_type, risk_level;`

	if _, err := c.DB.Exec(escalationSummaryView); err != nil {
		return fmt.Errorf("failed to create escalation summary view: %w", err)
	}

	return nil
}

// UpdateUnifiedTablesForSecurity updates the main createUnifiedTables function to include security tables
func (c *UnifiedDatabaseConfig) createUnifiedTablesWithSecurity() error {
	// Call original method first
	if err := c.createUnifiedTables(); err != nil {
		return err
	}

	// Add security tables
	if err := c.createSecurityTables(); err != nil {
		return fmt.Errorf("failed to create security tables: %w", err)
	}

	// Create security views
	if err := c.CreateSecurityViews(); err != nil {
		return fmt.Errorf("failed to create security views: %w", err)
	}

	return nil
}