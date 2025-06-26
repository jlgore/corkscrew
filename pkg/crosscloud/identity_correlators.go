package crosscloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// IdentityCorrelator handles cross-cloud identity federation detection
type IdentityCorrelator struct {
	db               DatabaseInterface
	logger           *log.Logger
	confidenceThresh float64
}

// NewIdentityCorrelator creates a new identity correlator
func NewIdentityCorrelator(db DatabaseInterface, logger *log.Logger) *IdentityCorrelator {
	return &IdentityCorrelator{
		db:               db,
		logger:           logger,
		confidenceThresh: 0.85,
	}
}

// IdentityProvider represents a cross-cloud identity provider
type IdentityProvider struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // OIDC, SAML, OAuth2
	Provider    string                 `json:"provider"`
	Region      string                 `json:"region"`
	AccountID   string                 `json:"account_id"`
	Config      map[string]interface{} `json:"config"`
	Endpoints   []string               `json:"endpoints"`
	Thumbprints []string               `json:"thumbprints"`
	Audiences   []string               `json:"audiences"`
	Scopes      []string               `json:"scopes"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// FederationRelationship represents a detected federation relationship
type FederationRelationship struct {
	ID             string                 `json:"id"`
	SourceProvider IdentityProvider       `json:"source_provider"`
	TargetProvider IdentityProvider       `json:"target_provider"`
	FederationType string                 `json:"federation_type"` // trust_policy, oidc_federation, saml_federation
	TrustPolicy    map[string]interface{} `json:"trust_policy"`
	Conditions     []TrustCondition       `json:"conditions"`
	Confidence     float64                `json:"confidence"`
	Evidence       []string               `json:"evidence"`
	DetectedAt     time.Time              `json:"detected_at"`
}

// TrustCondition represents conditions for role assumption
type TrustCondition struct {
	Type     string      `json:"type"`
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

// CorrelateIdentityFederation finds identity federation relationships across clouds
func (ic *IdentityCorrelator) CorrelateIdentityFederation(ctx context.Context) ([]FederationRelationship, error) {
	var relationships []FederationRelationship

	// Get identity providers from all clouds
	providers, err := ic.extractIdentityProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract identity providers: %w", err)
	}

	ic.logger.Printf("Found %d identity providers across clouds", len(providers))

	// Correlate OIDC federation
	oidcRels, err := ic.correlateOIDCFederation(providers)
	if err != nil {
		ic.logger.Printf("Error correlating OIDC federation: %v", err)
	} else {
		relationships = append(relationships, oidcRels...)
	}

	// Correlate SAML federation
	samlRels, err := ic.correlateSAMLFederation(providers)
	if err != nil {
		ic.logger.Printf("Error correlating SAML federation: %v", err)
	} else {
		relationships = append(relationships, samlRels...)
	}

	// Correlate cross-account trust relationships
	trustRels, err := ic.correlateTrustRelationships(providers)
	if err != nil {
		ic.logger.Printf("Error correlating trust relationships: %v", err)
	} else {
		relationships = append(relationships, trustRels...)
	}

	// Correlate JWT issuer relationships
	jwtRels, err := ic.correlateJWTIssuers(providers)
	if err != nil {
		ic.logger.Printf("Error correlating JWT issuers: %v", err)
	} else {
		relationships = append(relationships, jwtRels...)
	}

	ic.logger.Printf("Found %d identity federation relationships", len(relationships))
	return relationships, nil
}

// extractIdentityProviders extracts identity providers from all cloud resources
func (ic *IdentityCorrelator) extractIdentityProviders(ctx context.Context) ([]IdentityProvider, error) {
	var providers []IdentityProvider

	// Extract AWS identity providers
	awsProviders, err := ic.extractAWSIdentityProviders(ctx)
	if err != nil {
		ic.logger.Printf("Error extracting AWS identity providers: %v", err)
	} else {
		providers = append(providers, awsProviders...)
	}

	// Extract Azure identity providers
	azureProviders, err := ic.extractAzureIdentityProviders(ctx)
	if err != nil {
		ic.logger.Printf("Error extracting Azure identity providers: %v", err)
	} else {
		providers = append(providers, azureProviders...)
	}

	return providers, nil
}

// extractAWSIdentityProviders extracts AWS IAM identity providers and roles
func (ic *IdentityCorrelator) extractAWSIdentityProviders(ctx context.Context) ([]IdentityProvider, error) {
	var providers []IdentityProvider

	query := `
	SELECT id, name, type, raw_data, region, account_id
	FROM aws_resources 
	WHERE type IN ('AWS::IAM::Role', 'AWS::IAM::OIDCIdentityProvider', 'AWS::IAM::SAMLIdentityProvider')
	`

	rows, err := ic.db.QueryContext(ctx, query)
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

		provider := ic.parseAWSIdentityProvider(id, name, resourceType, rawData, region, accountID)
		if provider != nil {
			providers = append(providers, *provider)
		}
	}

	return providers, nil
}

// parseAWSIdentityProvider parses AWS identity provider from resource data
func (ic *IdentityCorrelator) parseAWSIdentityProvider(id, name, resourceType string, rawData map[string]interface{}, region, accountID string) *IdentityProvider {
	provider := &IdentityProvider{
		ID:        id,
		Name:      name,
		Provider:  "aws",
		Region:    region,
		AccountID: accountID,
		Config:    make(map[string]interface{}),
		Metadata:  rawData,
	}

	switch resourceType {
	case "AWS::IAM::OIDCIdentityProvider":
		provider.Type = "OIDC"
		if url, ok := rawData["Url"].(string); ok {
			provider.Endpoints = []string{url}
		}
		if thumbprints, ok := rawData["ThumbprintList"].([]interface{}); ok {
			for _, tp := range thumbprints {
				if tpStr, ok := tp.(string); ok {
					provider.Thumbprints = append(provider.Thumbprints, tpStr)
				}
			}
		}
		if audiences, ok := rawData["ClientIDList"].([]interface{}); ok {
			for _, aud := range audiences {
				if audStr, ok := aud.(string); ok {
					provider.Audiences = append(provider.Audiences, audStr)
				}
			}
		}

	case "AWS::IAM::SAMLIdentityProvider":
		provider.Type = "SAML"
		if doc, ok := rawData["SAMLMetadataDocument"].(string); ok {
			provider.Config["metadata_document"] = doc
		}

	case "AWS::IAM::Role":
		provider.Type = "Role"
		if assumeRoleDoc, ok := rawData["AssumeRolePolicyDocument"].(string); ok {
			provider.Config["assume_role_policy"] = assumeRoleDoc
		}
	}

	return provider
}

// extractAzureIdentityProviders extracts Azure AD identity providers
func (ic *IdentityCorrelator) extractAzureIdentityProviders(ctx context.Context) ([]IdentityProvider, error) {
	var providers []IdentityProvider

	query := `
	SELECT id, name, type, raw_data, location, subscription_id
	FROM azure_resources 
	WHERE type IN ('Microsoft.ManagedIdentity/userAssignedIdentities', 'Microsoft.Authorization/roleAssignments')
	   OR (type = 'Microsoft.Web/sites' AND json_extract(properties, '$.kind') LIKE '%app%')
	`

	rows, err := ic.db.QueryContext(ctx, query)
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

		provider := ic.parseAzureIdentityProvider(id, name, resourceType, rawData, location, subscriptionID)
		if provider != nil {
			providers = append(providers, *provider)
		}
	}

	return providers, nil
}

// parseAzureIdentityProvider parses Azure identity provider from resource data
func (ic *IdentityCorrelator) parseAzureIdentityProvider(id, name, resourceType string, rawData map[string]interface{}, location, subscriptionID string) *IdentityProvider {
	provider := &IdentityProvider{
		ID:        id,
		Name:      name,
		Provider:  "azure",
		Region:    location,
		AccountID: subscriptionID,
		Config:    make(map[string]interface{}),
		Metadata:  rawData,
	}

	switch resourceType {
	case "Microsoft.ManagedIdentity/userAssignedIdentities":
		provider.Type = "ManagedIdentity"
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			if clientID, ok := props["clientId"].(string); ok {
				provider.Config["client_id"] = clientID
			}
			if principalID, ok := props["principalId"].(string); ok {
				provider.Config["principal_id"] = principalID
			}
		}

	case "Microsoft.Authorization/roleAssignments":
		provider.Type = "RoleAssignment"
		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			provider.Config["role_definition_id"] = props["roleDefinitionId"]
			provider.Config["principal_id"] = props["principalId"]
			provider.Config["scope"] = props["scope"]
		}
	}

	return provider
}

// correlateOIDCFederation finds OIDC federation relationships
func (ic *IdentityCorrelator) correlateOIDCFederation(providers []IdentityProvider) ([]FederationRelationship, error) {
	var relationships []FederationRelationship

	oidcProviders := make([]IdentityProvider, 0)
	for _, p := range providers {
		if p.Type == "OIDC" {
			oidcProviders = append(oidcProviders, p)
		}
	}

	// Find matching OIDC providers by endpoint and thumbprint
	for i, source := range oidcProviders {
		for j, target := range oidcProviders {
			if i >= j || source.Provider == target.Provider {
				continue
			}

			confidence := ic.calculateOIDCConfidence(source, target)
			if confidence >= ic.confidenceThresh {
				rel := FederationRelationship{
					ID:             fmt.Sprintf("oidc-%s-%s", source.ID, target.ID),
					SourceProvider: source,
					TargetProvider: target,
					FederationType: "oidc_federation",
					Confidence:     confidence,
					DetectedAt:     time.Now(),
				}

				rel.Evidence = ic.buildOIDCEvidence(source, target)
				relationships = append(relationships, rel)
			}
		}
	}

	return relationships, nil
}

// calculateOIDCConfidence calculates confidence score for OIDC federation
func (ic *IdentityCorrelator) calculateOIDCConfidence(source, target IdentityProvider) float64 {
	var score float64

	// Check for matching endpoints
	if len(source.Endpoints) > 0 && len(target.Endpoints) > 0 {
		for _, sEndpoint := range source.Endpoints {
			for _, tEndpoint := range target.Endpoints {
				if ic.compareOIDCEndpoints(sEndpoint, tEndpoint) {
					score += 0.5
				}
			}
		}
	}

	// Check for matching thumbprints
	for _, sThumb := range source.Thumbprints {
		for _, tThumb := range target.Thumbprints {
			if sThumb == tThumb {
				score += 0.3
			}
		}
	}

	// Check for matching audiences
	for _, sAud := range source.Audiences {
		for _, tAud := range target.Audiences {
			if sAud == tAud {
				score += 0.2
			}
		}
	}

	return score
}

// compareOIDCEndpoints compares OIDC endpoints for similarity
func (ic *IdentityCorrelator) compareOIDCEndpoints(endpoint1, endpoint2 string) bool {
	u1, err1 := url.Parse(endpoint1)
	u2, err2 := url.Parse(endpoint2)

	if err1 != nil || err2 != nil {
		return false
	}

	// Check if hosts match
	if u1.Host == u2.Host {
		return true
	}

	// Check for common OIDC patterns
	commonPatterns := []string{
		"accounts.google.com",
		"login.microsoftonline.com",
		"cognito-idp",
	}

	for _, pattern := range commonPatterns {
		if strings.Contains(u1.Host, pattern) && strings.Contains(u2.Host, pattern) {
			return true
		}
	}

	return false
}

// buildOIDCEvidence builds evidence list for OIDC federation
func (ic *IdentityCorrelator) buildOIDCEvidence(source, target IdentityProvider) []string {
	var evidence []string

	// Endpoint evidence
	if len(source.Endpoints) > 0 && len(target.Endpoints) > 0 {
		evidence = append(evidence, fmt.Sprintf("Matching OIDC endpoints: %v <-> %v", source.Endpoints, target.Endpoints))
	}

	// Thumbprint evidence
	for _, sThumb := range source.Thumbprints {
		for _, tThumb := range target.Thumbprints {
			if sThumb == tThumb {
				evidence = append(evidence, fmt.Sprintf("Matching certificate thumbprint: %s", sThumb))
			}
		}
	}

	// Audience evidence
	for _, sAud := range source.Audiences {
		for _, tAud := range target.Audiences {
			if sAud == tAud {
				evidence = append(evidence, fmt.Sprintf("Matching audience: %s", sAud))
			}
		}
	}

	return evidence
}

// correlateSAMLFederation finds SAML federation relationships
func (ic *IdentityCorrelator) correlateSAMLFederation(providers []IdentityProvider) ([]FederationRelationship, error) {
	var relationships []FederationRelationship

	samlProviders := make([]IdentityProvider, 0)
	for _, p := range providers {
		if p.Type == "SAML" {
			samlProviders = append(samlProviders, p)
		}
	}

	// Find matching SAML providers by metadata
	for i, source := range samlProviders {
		for j, target := range samlProviders {
			if i >= j || source.Provider == target.Provider {
				continue
			}

			confidence := ic.calculateSAMLConfidence(source, target)
			if confidence >= ic.confidenceThresh {
				rel := FederationRelationship{
					ID:             fmt.Sprintf("saml-%s-%s", source.ID, target.ID),
					SourceProvider: source,
					TargetProvider: target,
					FederationType: "saml_federation",
					Confidence:     confidence,
					DetectedAt:     time.Now(),
				}

				rel.Evidence = ic.buildSAMLEvidence(source, target)
				relationships = append(relationships, rel)
			}
		}
	}

	return relationships, nil
}

// calculateSAMLConfidence calculates confidence score for SAML federation
func (ic *IdentityCorrelator) calculateSAMLConfidence(source, target IdentityProvider) float64 {
	var score float64

	// Extract SAML metadata for comparison
	sourceMeta := ic.extractSAMLMetadata(source)
	targetMeta := ic.extractSAMLMetadata(target)

	// Check for matching entity IDs
	if sourceMeta["entity_id"] != "" && sourceMeta["entity_id"] == targetMeta["entity_id"] {
		score += 0.6
	}

	// Check for matching SSO endpoints
	if sourceMeta["sso_endpoint"] != "" && sourceMeta["sso_endpoint"] == targetMeta["sso_endpoint"] {
		score += 0.4
	}

	return score
}

// extractSAMLMetadata extracts SAML metadata from provider config
func (ic *IdentityCorrelator) extractSAMLMetadata(provider IdentityProvider) map[string]string {
	metadata := make(map[string]string)

	if metaDoc, ok := provider.Config["metadata_document"].(string); ok {
		// Parse SAML metadata document for entity ID and endpoints
		entityIDRegex := regexp.MustCompile(`entityID="([^"]+)"`)
		if matches := entityIDRegex.FindStringSubmatch(metaDoc); len(matches) > 1 {
			metadata["entity_id"] = matches[1]
		}

		ssoRegex := regexp.MustCompile(`Location="([^"]+)".*SingleSignOnService`)
		if matches := ssoRegex.FindStringSubmatch(metaDoc); len(matches) > 1 {
			metadata["sso_endpoint"] = matches[1]
		}
	}

	return metadata
}

// buildSAMLEvidence builds evidence list for SAML federation
func (ic *IdentityCorrelator) buildSAMLEvidence(source, target IdentityProvider) []string {
	var evidence []string

	sourceMeta := ic.extractSAMLMetadata(source)
	targetMeta := ic.extractSAMLMetadata(target)

	if sourceMeta["entity_id"] != "" && sourceMeta["entity_id"] == targetMeta["entity_id"] {
		evidence = append(evidence, fmt.Sprintf("Matching SAML entity ID: %s", sourceMeta["entity_id"]))
	}

	if sourceMeta["sso_endpoint"] != "" && sourceMeta["sso_endpoint"] == targetMeta["sso_endpoint"] {
		evidence = append(evidence, fmt.Sprintf("Matching SSO endpoint: %s", sourceMeta["sso_endpoint"]))
	}

	return evidence
}

// correlateTrustRelationships finds cross-account trust relationships
func (ic *IdentityCorrelator) correlateTrustRelationships(providers []IdentityProvider) ([]FederationRelationship, error) {
	var relationships []FederationRelationship

	roles := make([]IdentityProvider, 0)
	for _, p := range providers {
		if p.Type == "Role" || p.Type == "RoleAssignment" {
			roles = append(roles, p)
		}
	}

	// Analyze trust policies in roles
	for _, role := range roles {
		trustRels := ic.analyzeTrustPolicy(role, providers)
		relationships = append(relationships, trustRels...)
	}

	return relationships, nil
}

// analyzeTrustPolicy analyzes role trust policies for federation relationships
func (ic *IdentityCorrelator) analyzeTrustPolicy(role IdentityProvider, allProviders []IdentityProvider) []FederationRelationship {
	var relationships []FederationRelationship

	if role.Provider == "aws" {
		if policyStr, ok := role.Config["assume_role_policy"].(string); ok {
			var policy map[string]interface{}
			if err := json.Unmarshal([]byte(policyStr), &policy); err != nil {
				return relationships
			}

			// Extract trusted principals and conditions
			if statement, ok := policy["Statement"].([]interface{}); ok {
				for _, stmt := range statement {
					if stmtMap, ok := stmt.(map[string]interface{}); ok {
						rels := ic.analyzeTrustStatement(role, stmtMap, allProviders)
						relationships = append(relationships, rels...)
					}
				}
			}
		}
	}

	return relationships
}

// analyzeTrustStatement analyzes individual trust policy statements
func (ic *IdentityCorrelator) analyzeTrustStatement(role IdentityProvider, statement map[string]interface{}, allProviders []IdentityProvider) []FederationRelationship {
	var relationships []FederationRelationship

	principal, ok := statement["Principal"]
	if !ok {
		return relationships
	}

	// Handle different principal formats
	principals := ic.extractPrincipals(principal)
	for _, p := range principals {
		if strings.Contains(p, "oidc") || strings.Contains(p, "saml") {
			// Find matching identity provider
			for _, provider := range allProviders {
				if ic.matchesPrincipal(provider, p) {
					rel := FederationRelationship{
						ID:             fmt.Sprintf("trust-%s-%s", provider.ID, role.ID),
						SourceProvider: provider,
						TargetProvider: role,
						FederationType: "trust_policy",
						Confidence:     0.9,
						DetectedAt:     time.Now(),
						Evidence:       []string{fmt.Sprintf("Trust policy allows assumption from principal: %s", p)},
					}

					// Extract conditions
					if conditions, ok := statement["Condition"].(map[string]interface{}); ok {
						rel.Conditions = ic.extractTrustConditions(conditions)
					}

					relationships = append(relationships, rel)
				}
			}
		}
	}

	return relationships
}

// extractPrincipals extracts principals from various formats
func (ic *IdentityCorrelator) extractPrincipals(principal interface{}) []string {
	var principals []string

	switch p := principal.(type) {
	case string:
		principals = append(principals, p)
	case []interface{}:
		for _, item := range p {
			if str, ok := item.(string); ok {
				principals = append(principals, str)
			}
		}
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

	return principals
}

// matchesPrincipal checks if a provider matches a trust policy principal
func (ic *IdentityCorrelator) matchesPrincipal(provider IdentityProvider, principal string) bool {
	// Check OIDC endpoints
	for _, endpoint := range provider.Endpoints {
		if strings.Contains(principal, endpoint) {
			return true
		}
	}

	// Check for SAML provider ARNs
	if provider.Type == "SAML" && strings.Contains(principal, provider.Name) {
		return true
	}

	return false
}

// extractTrustConditions extracts trust conditions from policy
func (ic *IdentityCorrelator) extractTrustConditions(conditions map[string]interface{}) []TrustCondition {
	var trustConditions []TrustCondition

	for operator, condition := range conditions {
		if condMap, ok := condition.(map[string]interface{}); ok {
			for field, value := range condMap {
				trustConditions = append(trustConditions, TrustCondition{
					Type:     "trust_policy",
					Field:    field,
					Operator: operator,
					Value:    value,
				})
			}
		}
	}

	return trustConditions
}

// correlateJWTIssuers finds JWT issuer correlation across clouds
func (ic *IdentityCorrelator) correlateJWTIssuers(providers []IdentityProvider) ([]FederationRelationship, error) {
	var relationships []FederationRelationship

	// Extract JWT issuers from OIDC providers
	issuerMap := make(map[string][]IdentityProvider)

	for _, provider := range providers {
		if provider.Type == "OIDC" {
			for _, endpoint := range provider.Endpoints {
				issuer := ic.extractJWTIssuer(endpoint)
				if issuer != "" {
					issuerMap[issuer] = append(issuerMap[issuer], provider)
				}
			}
		}
	}

	// Create relationships for providers with same issuer
	for issuer, providerList := range issuerMap {
		if len(providerList) > 1 {
			for i, source := range providerList {
				for j, target := range providerList {
					if i >= j || source.Provider == target.Provider {
						continue
					}

					rel := FederationRelationship{
						ID:             fmt.Sprintf("jwt-%s-%s", source.ID, target.ID),
						SourceProvider: source,
						TargetProvider: target,
						FederationType: "jwt_issuer_correlation",
						Confidence:     0.95,
						DetectedAt:     time.Now(),
						Evidence:       []string{fmt.Sprintf("Shared JWT issuer: %s", issuer)},
					}

					relationships = append(relationships, rel)
				}
			}
		}
	}

	return relationships, nil
}

// extractJWTIssuer extracts JWT issuer from OIDC endpoint
func (ic *IdentityCorrelator) extractJWTIssuer(endpoint string) string {
	// OIDC issuer is typically the base URL
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}

	// Remove well-known paths
	path := strings.TrimSuffix(u.Path, "/.well-known/openid_configuration")
	path = strings.TrimSuffix(path, "/.well-known/jwks.json")

	u.Path = path
	return u.String()
}

// PersistFederationRelationships saves federation relationships to database
func (ic *IdentityCorrelator) PersistFederationRelationships(ctx context.Context, relationships []FederationRelationship) error {
	if len(relationships) == 0 {
		return nil
	}

	tx, err := ic.db.BeginTx(ctx, nil)
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
		attributesJSON, _ := json.Marshal(rel.TrustPolicy)

		_, err := stmt.Exec(
			rel.ID,
			rel.SourceProvider.ID, rel.SourceProvider.Provider, rel.SourceProvider.Region, rel.SourceProvider.AccountID, rel.SourceProvider.Type,
			rel.TargetProvider.ID, rel.TargetProvider.Provider, rel.TargetProvider.Region, rel.TargetProvider.AccountID, rel.TargetProvider.Type,
			"identity_federation", rel.FederationType, "automated_analysis", rel.Confidence,
			string(evidenceJSON), string(attributesJSON),
			fmt.Sprintf("Identity federation between %s and %s", rel.SourceProvider.Provider, rel.TargetProvider.Provider),
			"active", false,
			rel.DetectedAt, time.Now(), time.Now(),
		)
		if err != nil {
			ic.logger.Printf("Error persisting federation relationship %s: %v", rel.ID, err)
			continue
		}
	}

	return tx.Commit()
}