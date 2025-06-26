package security

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CertificateAnalyzer handles SSL/TLS certificate and CA chain analysis
type CertificateAnalyzer struct {
	db               DatabaseInterface
	logger           *log.Logger
	correlationThresh float64
}

// NewCertificateAnalyzer creates a new certificate analyzer
func NewCertificateAnalyzer(db DatabaseInterface, logger *log.Logger) *CertificateAnalyzer {
	return &CertificateAnalyzer{
		db:               db,
		logger:           logger,
		correlationThresh: 0.80,
	}
}

// Certificate represents a parsed certificate
type Certificate struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Provider       string                 `json:"provider"`
	Region         string                 `json:"region"`
	AccountID      string                 `json:"account_id"`
	ResourceID     string                 `json:"resource_id"`
	CertificateData *x509.Certificate     `json:"-"`
	SerialNumber   string                 `json:"serial_number"`
	Subject        string                 `json:"subject"`
	Issuer         string                 `json:"issuer"`
	CommonName     string                 `json:"common_name"`
	SANs           []string               `json:"sans"`
	Thumbprint     string                 `json:"thumbprint"`
	ThumbprintSHA1 string                 `json:"thumbprint_sha1"`
	NotBefore      time.Time              `json:"not_before"`
	NotAfter       time.Time              `json:"not_after"`
	KeyAlgorithm   string                 `json:"key_algorithm"`
	KeySize        int                    `json:"key_size"`
	SignatureAlgo  string                 `json:"signature_algorithm"`
	IsCA           bool                   `json:"is_ca"`
	IsSelfSigned   bool                   `json:"is_self_signed"`
	Usage          []string               `json:"usage"`
	RawPEM         string                 `json:"raw_pem"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// CertificateChain represents a certificate chain
type CertificateChain struct {
	ID          string        `json:"id"`
	RootCA      *Certificate  `json:"root_ca"`
	Intermediate []Certificate `json:"intermediate"`
	LeafCert    *Certificate  `json:"leaf_cert"`
	ChainDepth  int           `json:"chain_depth"`
	IsValid     bool          `json:"is_valid"`
	Issues      []string      `json:"issues"`
}

// CertificateCorrelation represents correlation between certificates
type CertificateCorrelation struct {
	ID                  string                 `json:"id"`
	SourceCertificate   Certificate            `json:"source_certificate"`
	TargetCertificate   Certificate            `json:"target_certificate"`
	CorrelationType     string                 `json:"correlation_type"`
	ConfidenceScore     float64                `json:"confidence_score"`
	MatchingAttributes  []string               `json:"matching_attributes"`
	SharedSecrets       []SharedSecret         `json:"shared_secrets"`
	ChainRelationship   string                 `json:"chain_relationship"`
	SecurityAssessment  SecurityAssessment     `json:"security_assessment"`
	Metadata            map[string]interface{} `json:"metadata"`
	DetectedAt          time.Time              `json:"detected_at"`
}

// SharedSecret represents a shared secret or credential
type SharedSecret struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"` // certificate, key, ca_bundle
	Provider     string                 `json:"provider"`
	Region       string                 `json:"region"`
	AccountID    string                 `json:"account_id"`
	ResourceID   string                 `json:"resource_id"`
	SecretName   string                 `json:"secret_name"`
	Hash         string                 `json:"hash"`
	References   []string               `json:"references"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// SecurityAssessment represents security assessment for certificate correlation
type SecurityAssessment struct {
	RiskLevel       string   `json:"risk_level"`
	RiskScore       float64  `json:"risk_score"`
	SecurityIssues  []string `json:"security_issues"`
	Recommendations []string `json:"recommendations"`
	ComplianceFlags []string `json:"compliance_flags"`
}

// AnalyzeCertificateCorrelation analyzes certificate correlations across clouds
func (ca *CertificateAnalyzer) AnalyzeCertificateCorrelation(ctx context.Context) ([]CertificateCorrelation, error) {
	var correlations []CertificateCorrelation

	// Extract certificates from all clouds
	certificates, err := ca.extractCertificates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract certificates: %w", err)
	}

	ca.logger.Printf("Found %d certificates across clouds", len(certificates))

	// Extract shared secrets
	secrets, err := ca.extractSharedSecrets(ctx)
	if err != nil {
		ca.logger.Printf("Error extracting shared secrets: %v", err)
	}

	// Correlate certificates by various attributes
	correlations = append(correlations, ca.correlateByThumbprint(certificates)...)
	correlations = append(correlations, ca.correlateByIssuer(certificates)...)
	correlations = append(correlations, ca.correlateBySubject(certificates)...)
	correlations = append(correlations, ca.correlateBySAN(certificates)...)
	correlations = append(correlations, ca.correlateByChain(certificates)...)

	// Add shared secret correlations
	for i := range correlations {
		correlations[i].SharedSecrets = ca.findSharedSecrets(correlations[i], secrets)
		correlations[i].SecurityAssessment = ca.assessCertificateSecurity(correlations[i])
	}

	ca.logger.Printf("Found %d certificate correlations", len(correlations))
	return correlations, nil
}

// extractCertificates extracts certificates from all cloud resources
func (ca *CertificateAnalyzer) extractCertificates(ctx context.Context) ([]Certificate, error) {
	var certificates []Certificate

	// Extract AWS certificates
	awsCerts, err := ca.extractAWSCertificates(ctx)
	if err != nil {
		ca.logger.Printf("Error extracting AWS certificates: %v", err)
	} else {
		certificates = append(certificates, awsCerts...)
	}

	// Extract Azure certificates
	azureCerts, err := ca.extractAzureCertificates(ctx)
	if err != nil {
		ca.logger.Printf("Error extracting Azure certificates: %v", err)
	} else {
		certificates = append(certificates, azureCerts...)
	}

	return certificates, nil
}

// extractAWSCertificates extracts AWS certificates from ACM and other services
func (ca *CertificateAnalyzer) extractAWSCertificates(ctx context.Context) ([]Certificate, error) {
	var certificates []Certificate

	query := `
	SELECT id, name, type, raw_data, region, account_id
	FROM aws_resources 
	WHERE type IN ('AWS::CertificateManager::Certificate', 'AWS::IAM::ServerCertificate')
	  OR (type = 'AWS::S3::Bucket' AND json_extract(raw_data, '$.ServerSideEncryptionConfiguration') IS NOT NULL)
	  OR (type = 'AWS::ElasticLoadBalancingV2::LoadBalancer' AND json_extract(raw_data, '$.Listeners') IS NOT NULL)
	`

	rows, err := ca.db.QueryContext(ctx, query)
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

		extractedCerts := ca.parseAWSCertificates(id, name, resourceType, rawData, region, accountID)
		certificates = append(certificates, extractedCerts...)
	}

	return certificates, nil
}

// parseAWSCertificates parses AWS certificates from resource data
func (ca *CertificateAnalyzer) parseAWSCertificates(id, name, resourceType string, rawData map[string]interface{}, region, accountID string) []Certificate {
	var certificates []Certificate

	base := Certificate{
		Provider:   "aws",
		Region:     region,
		AccountID:  accountID,
		ResourceID: id,
		Metadata:   rawData,
	}

	switch resourceType {
	case "AWS::CertificateManager::Certificate":
		cert := base
		cert.ID = fmt.Sprintf("aws-acm-%s", id)
		cert.Name = name

		// Extract certificate details from ACM
		if domainName, ok := rawData["DomainName"].(string); ok {
			cert.CommonName = domainName
		}

		if sans, ok := rawData["SubjectAlternativeNames"].([]interface{}); ok {
			for _, san := range sans {
				if sanStr, ok := san.(string); ok {
					cert.SANs = append(cert.SANs, sanStr)
				}
			}
		}

		if certStr, ok := rawData["Certificate"].(string); ok {
			if parsedCert := ca.parsePEMCertificate(certStr); parsedCert != nil {
				cert = ca.fillCertificateDetails(cert, parsedCert, certStr)
			}
		}

		certificates = append(certificates, cert)

	case "AWS::IAM::ServerCertificate":
		cert := base
		cert.ID = fmt.Sprintf("aws-iam-%s", id)
		cert.Name = name

		if certBody, ok := rawData["CertificateBody"].(string); ok {
			if parsedCert := ca.parsePEMCertificate(certBody); parsedCert != nil {
				cert = ca.fillCertificateDetails(cert, parsedCert, certBody)
			}
		}

		certificates = append(certificates, cert)
	}

	return certificates
}

// extractAzureCertificates extracts Azure certificates from Key Vault and other services
func (ca *CertificateAnalyzer) extractAzureCertificates(ctx context.Context) ([]Certificate, error) {
	var certificates []Certificate

	query := `
	SELECT id, name, type, raw_data, location, subscription_id
	FROM azure_resources 
	WHERE type IN ('Microsoft.KeyVault/vaults/certificates', 'Microsoft.Web/certificates')
	  OR (type = 'Microsoft.Network/applicationGateways' AND json_extract(raw_data, '$.properties.sslCertificates') IS NOT NULL)
	`

	rows, err := ca.db.QueryContext(ctx, query)
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

		extractedCerts := ca.parseAzureCertificates(id, name, resourceType, rawData, location, subscriptionID)
		certificates = append(certificates, extractedCerts...)
	}

	return certificates, nil
}

// parseAzureCertificates parses Azure certificates from resource data
func (ca *CertificateAnalyzer) parseAzureCertificates(id, name, resourceType string, rawData map[string]interface{}, location, subscriptionID string) []Certificate {
	var certificates []Certificate

	base := Certificate{
		Provider:   "azure",
		Region:     location,
		AccountID:  subscriptionID,
		ResourceID: id,
		Metadata:   rawData,
	}

	switch resourceType {
	case "Microsoft.KeyVault/vaults/certificates":
		cert := base
		cert.ID = fmt.Sprintf("azure-kv-%s", id)
		cert.Name = name

		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			if certData, ok := props["certificateData"].(string); ok {
				if parsedCert := ca.parsePEMCertificate(certData); parsedCert != nil {
					cert = ca.fillCertificateDetails(cert, parsedCert, certData)
				}
			}
		}

		certificates = append(certificates, cert)

	case "Microsoft.Web/certificates":
		cert := base
		cert.ID = fmt.Sprintf("azure-web-%s", id)
		cert.Name = name

		if props, ok := rawData["properties"].(map[string]interface{}); ok {
			if thumbprint, ok := props["thumbprint"].(string); ok {
				cert.Thumbprint = thumbprint
			}
			if subject, ok := props["subjectName"].(string); ok {
				cert.Subject = subject
			}
		}

		certificates = append(certificates, cert)
	}

	return certificates
}

// parsePEMCertificate parses a PEM-encoded certificate
func (ca *CertificateAnalyzer) parsePEMCertificate(pemData string) *x509.Certificate {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	return cert
}

// fillCertificateDetails fills certificate details from parsed x509 certificate
func (ca *CertificateAnalyzer) fillCertificateDetails(cert Certificate, parsedCert *x509.Certificate, rawPEM string) Certificate {
	cert.CertificateData = parsedCert
	cert.SerialNumber = parsedCert.SerialNumber.String()
	cert.Subject = parsedCert.Subject.String()
	cert.Issuer = parsedCert.Issuer.String()
	cert.CommonName = parsedCert.Subject.CommonName
	cert.SANs = parsedCert.DNSNames
	cert.NotBefore = parsedCert.NotBefore
	cert.NotAfter = parsedCert.NotAfter
	cert.KeyAlgorithm = parsedCert.PublicKeyAlgorithm.String()
	cert.SignatureAlgo = parsedCert.SignatureAlgorithm.String()
	cert.IsCA = parsedCert.IsCA
	cert.IsSelfSigned = parsedCert.Subject.String() == parsedCert.Issuer.String()
	cert.RawPEM = rawPEM

	// Calculate thumbprints
	cert.Thumbprint = fmt.Sprintf("%x", sha256.Sum256(parsedCert.Raw))
	cert.ThumbprintSHA1 = fmt.Sprintf("%x", sha1.Sum(parsedCert.Raw))

	// Extract key usage
	if parsedCert.KeyUsage&x509.KeyUsageDigitalSignature != 0 {
		cert.Usage = append(cert.Usage, "digital_signature")
	}
	if parsedCert.KeyUsage&x509.KeyUsageKeyEncipherment != 0 {
		cert.Usage = append(cert.Usage, "key_encipherment")
	}
	if parsedCert.KeyUsage&x509.KeyUsageCertSign != 0 {
		cert.Usage = append(cert.Usage, "cert_sign")
	}

	return cert
}

// extractSharedSecrets extracts shared secrets from cloud resources
func (ca *CertificateAnalyzer) extractSharedSecrets(ctx context.Context) ([]SharedSecret, error) {
	var secrets []SharedSecret

	// Extract AWS secrets
	awsSecrets, err := ca.extractAWSSecrets(ctx)
	if err != nil {
		ca.logger.Printf("Error extracting AWS secrets: %v", err)
	} else {
		secrets = append(secrets, awsSecrets...)
	}

	// Extract Azure secrets
	azureSecrets, err := ca.extractAzureSecrets(ctx)
	if err != nil {
		ca.logger.Printf("Error extracting Azure secrets: %v", err)
	} else {
		secrets = append(secrets, azureSecrets...)
	}

	return secrets, nil
}

// extractAWSSecrets extracts AWS secrets from Secrets Manager and Parameter Store
func (ca *CertificateAnalyzer) extractAWSSecrets(ctx context.Context) ([]SharedSecret, error) {
	var secrets []SharedSecret

	query := `
	SELECT id, name, type, raw_data, region, account_id
	FROM aws_resources 
	WHERE type IN ('AWS::SecretsManager::Secret', 'AWS::SSM::Parameter')
	  AND (name LIKE '%cert%' OR name LIKE '%key%' OR name LIKE '%ssl%' OR name LIKE '%tls%')
	`

	rows, err := ca.db.QueryContext(ctx, query)
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

		secret := SharedSecret{
			ID:         fmt.Sprintf("aws-secret-%s", id),
			Provider:   "aws",
			Region:     region,
			AccountID:  accountID,
			ResourceID: id,
			SecretName: name,
			Metadata:   rawData,
		}

		// Determine secret type
		if strings.Contains(strings.ToLower(name), "cert") {
			secret.Type = "certificate"
		} else if strings.Contains(strings.ToLower(name), "key") {
			secret.Type = "key"
		} else if strings.Contains(strings.ToLower(name), "ca") {
			secret.Type = "ca_bundle"
		}

		// Generate hash from secret name and metadata (not actual secret value)
		hashData := fmt.Sprintf("%s:%s", name, resourceType)
		secret.Hash = fmt.Sprintf("%x", sha256.Sum256([]byte(hashData)))

		secrets = append(secrets, secret)
	}

	return secrets, nil
}

// extractAzureSecrets extracts Azure secrets from Key Vault
func (ca *CertificateAnalyzer) extractAzureSecrets(ctx context.Context) ([]SharedSecret, error) {
	var secrets []SharedSecret

	query := `
	SELECT id, name, type, raw_data, location, subscription_id
	FROM azure_resources 
	WHERE type IN ('Microsoft.KeyVault/vaults/secrets', 'Microsoft.KeyVault/vaults/keys')
	  AND (name LIKE '%cert%' OR name LIKE '%key%' OR name LIKE '%ssl%' OR name LIKE '%tls%')
	`

	rows, err := ca.db.QueryContext(ctx, query)
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

		secret := SharedSecret{
			ID:         fmt.Sprintf("azure-secret-%s", id),
			Provider:   "azure",
			Region:     location,
			AccountID:  subscriptionID,
			ResourceID: id,
			SecretName: name,
			Metadata:   rawData,
		}

		// Determine secret type
		if strings.Contains(resourceType, "secrets") && strings.Contains(strings.ToLower(name), "cert") {
			secret.Type = "certificate"
		} else if strings.Contains(resourceType, "keys") {
			secret.Type = "key"
		}

		// Generate hash
		hashData := fmt.Sprintf("%s:%s", name, resourceType)
		secret.Hash = fmt.Sprintf("%x", sha256.Sum256([]byte(hashData)))

		secrets = append(secrets, secret)
	}

	return secrets, nil
}

// correlateByThumbprint correlates certificates by thumbprint
func (ca *CertificateAnalyzer) correlateByThumbprint(certificates []Certificate) []CertificateCorrelation {
	var correlations []CertificateCorrelation

	// Group by thumbprint
	thumbprintMap := make(map[string][]Certificate)
	for _, cert := range certificates {
		if cert.Thumbprint != "" {
			thumbprintMap[cert.Thumbprint] = append(thumbprintMap[cert.Thumbprint], cert)
		}
		if cert.ThumbprintSHA1 != "" {
			thumbprintMap[cert.ThumbprintSHA1] = append(thumbprintMap[cert.ThumbprintSHA1], cert)
		}
	}

	// Create correlations for matching thumbprints
	for thumbprint, certs := range thumbprintMap {
		if len(certs) > 1 {
			for i, source := range certs {
				for j, target := range certs {
					if i >= j || source.Provider == target.Provider {
						continue
					}

					correlation := CertificateCorrelation{
						ID:                fmt.Sprintf("cert-thumb-%s-%s", generateCertHash(source.ID), generateCertHash(target.ID)),
						SourceCertificate: source,
						TargetCertificate: target,
						CorrelationType:   "thumbprint_match",
						ConfidenceScore:   1.0,
						MatchingAttributes: []string{fmt.Sprintf("Matching thumbprint: %s", thumbprint)},
						DetectedAt:        time.Now(),
					}

					correlations = append(correlations, correlation)
				}
			}
		}
	}

	return correlations
}

// correlateByIssuer correlates certificates by issuer
func (ca *CertificateAnalyzer) correlateByIssuer(certificates []Certificate) []CertificateCorrelation {
	var correlations []CertificateCorrelation

	// Group by issuer
	issuerMap := make(map[string][]Certificate)
	for _, cert := range certificates {
		if cert.Issuer != "" {
			issuerMap[cert.Issuer] = append(issuerMap[cert.Issuer], cert)
		}
	}

	// Create correlations for certificates with same issuer across different providers
	for issuer, certs := range issuerMap {
		if len(certs) > 1 {
			for i, source := range certs {
				for j, target := range certs {
					if i >= j || source.Provider == target.Provider {
						continue
					}

					confidence := ca.calculateIssuerConfidence(source, target)
					if confidence >= ca.correlationThresh {
						correlation := CertificateCorrelation{
							ID:                fmt.Sprintf("cert-issuer-%s-%s", generateCertHash(source.ID), generateCertHash(target.ID)),
							SourceCertificate: source,
							TargetCertificate: target,
							CorrelationType:   "issuer_match",
							ConfidenceScore:   confidence,
							MatchingAttributes: []string{fmt.Sprintf("Matching issuer: %s", issuer)},
							DetectedAt:        time.Now(),
						}

						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}

	return correlations
}

// calculateIssuerConfidence calculates confidence for issuer-based correlation
func (ca *CertificateAnalyzer) calculateIssuerConfidence(source, target Certificate) float64 {
	confidence := 0.6 // Base confidence for same issuer

	// Boost confidence for same organization
	if ca.extractOrganization(source.Issuer) == ca.extractOrganization(target.Issuer) && ca.extractOrganization(source.Issuer) != "" {
		confidence += 0.2
	}

	// Boost confidence for same validity period
	if source.NotBefore.Equal(target.NotBefore) && source.NotAfter.Equal(target.NotAfter) {
		confidence += 0.1
	}

	// Boost confidence for same key algorithm
	if source.KeyAlgorithm == target.KeyAlgorithm {
		confidence += 0.1
	}

	return confidence
}

// extractOrganization extracts organization from certificate subject/issuer
func (ca *CertificateAnalyzer) extractOrganization(dn string) string {
	orgRegex := regexp.MustCompile(`O=([^,]+)`)
	matches := orgRegex.FindStringSubmatch(dn)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// correlateBySubject correlates certificates by subject
func (ca *CertificateAnalyzer) correlateBySubject(certificates []Certificate) []CertificateCorrelation {
	var correlations []CertificateCorrelation

	// Group by common name
	cnMap := make(map[string][]Certificate)
	for _, cert := range certificates {
		if cert.CommonName != "" {
			cnMap[cert.CommonName] = append(cnMap[cert.CommonName], cert)
		}
	}

	// Create correlations for matching common names
	for cn, certs := range cnMap {
		if len(certs) > 1 {
			for i, source := range certs {
				for j, target := range certs {
					if i >= j || source.Provider == target.Provider {
						continue
					}

					correlation := CertificateCorrelation{
						ID:                fmt.Sprintf("cert-subject-%s-%s", generateCertHash(source.ID), generateCertHash(target.ID)),
						SourceCertificate: source,
						TargetCertificate: target,
						CorrelationType:   "subject_match",
						ConfidenceScore:   0.85,
						MatchingAttributes: []string{fmt.Sprintf("Matching common name: %s", cn)},
						DetectedAt:        time.Now(),
					}

					correlations = append(correlations, correlation)
				}
			}
		}
	}

	return correlations
}

// correlateBySAN correlates certificates by Subject Alternative Names
func (ca *CertificateAnalyzer) correlateBySAN(certificates []Certificate) []CertificateCorrelation {
	var correlations []CertificateCorrelation

	// Create SAN-based correlations
	for i, source := range certificates {
		for j, target := range certificates {
			if i >= j || source.Provider == target.Provider {
				continue
			}

			matchingSANs := ca.findMatchingSANs(source.SANs, target.SANs)
			if len(matchingSANs) > 0 {
				confidence := float64(len(matchingSANs)) / float64(len(source.SANs)+len(target.SANs)-len(matchingSANs))
				if confidence >= ca.correlationThresh {
					correlation := CertificateCorrelation{
						ID:                fmt.Sprintf("cert-san-%s-%s", generateCertHash(source.ID), generateCertHash(target.ID)),
						SourceCertificate: source,
						TargetCertificate: target,
						CorrelationType:   "san_match",
						ConfidenceScore:   confidence,
						MatchingAttributes: []string{fmt.Sprintf("Matching SANs: %v", matchingSANs)},
						DetectedAt:        time.Now(),
					}

					correlations = append(correlations, correlation)
				}
			}
		}
	}

	return correlations
}

// findMatchingSANs finds matching SANs between two certificate SAN lists
func (ca *CertificateAnalyzer) findMatchingSANs(sans1, sans2 []string) []string {
	var matching []string
	sanSet := make(map[string]bool)

	for _, san := range sans1 {
		sanSet[san] = true
	}

	for _, san := range sans2 {
		if sanSet[san] {
			matching = append(matching, san)
		}
	}

	return matching
}

// correlateByChain correlates certificates by certificate chain relationships
func (ca *CertificateAnalyzer) correlateByChain(certificates []Certificate) []CertificateCorrelation {
	var correlations []CertificateCorrelation

	// Find potential chain relationships
	for i, source := range certificates {
		for j, target := range certificates {
			if i == j || source.Provider == target.Provider {
				continue
			}

			chainRelationship := ca.analyzeChainRelationship(source, target)
			if chainRelationship != "" {
				correlation := CertificateCorrelation{
					ID:                fmt.Sprintf("cert-chain-%s-%s", generateCertHash(source.ID), generateCertHash(target.ID)),
					SourceCertificate: source,
					TargetCertificate: target,
					CorrelationType:   "chain_relationship",
					ChainRelationship: chainRelationship,
					ConfidenceScore:   0.9,
					MatchingAttributes: []string{fmt.Sprintf("Chain relationship: %s", chainRelationship)},
					DetectedAt:        time.Now(),
				}

				correlations = append(correlations, correlation)
			}
		}
	}

	return correlations
}

// analyzeChainRelationship analyzes chain relationship between two certificates
func (ca *CertificateAnalyzer) analyzeChainRelationship(cert1, cert2 Certificate) string {
	// Check if cert1 issued cert2
	if cert1.Subject == cert2.Issuer && cert1.IsCA {
		return "issuer_to_leaf"
	}

	// Check if cert2 issued cert1
	if cert2.Subject == cert1.Issuer && cert2.IsCA {
		return "leaf_to_issuer"
	}

	// Check if they have the same issuer (siblings)
	if cert1.Issuer == cert2.Issuer && cert1.Issuer != "" && !cert1.IsCA && !cert2.IsCA {
		return "sibling_certificates"
	}

	return ""
}

// findSharedSecrets finds shared secrets related to certificate correlation
func (ca *CertificateAnalyzer) findSharedSecrets(correlation CertificateCorrelation, secrets []SharedSecret) []SharedSecret {
	var relatedSecrets []SharedSecret

	for _, secret := range secrets {
		// Check if secret name matches certificate common name or SANs
		if ca.secretMatchesCertificate(secret, correlation.SourceCertificate) ||
			ca.secretMatchesCertificate(secret, correlation.TargetCertificate) {
			relatedSecrets = append(relatedSecrets, secret)
		}
	}

	return relatedSecrets
}

// secretMatchesCertificate checks if a secret is related to a certificate
func (ca *CertificateAnalyzer) secretMatchesCertificate(secret SharedSecret, cert Certificate) bool {
	secretName := strings.ToLower(secret.SecretName)
	
	// Check common name
	if cert.CommonName != "" && strings.Contains(secretName, strings.ToLower(cert.CommonName)) {
		return true
	}

	// Check SANs
	for _, san := range cert.SANs {
		if strings.Contains(secretName, strings.ToLower(san)) {
			return true
		}
	}

	// Check if secret name contains domain from certificate
	if cert.CommonName != "" {
		if u, err := url.Parse("https://" + cert.CommonName); err == nil {
			domain := u.Hostname()
			if domain != "" && strings.Contains(secretName, strings.ToLower(domain)) {
				return true
			}
		}
	}

	return false
}

// assessCertificateSecurity assesses security for certificate correlation
func (ca *CertificateAnalyzer) assessCertificateSecurity(correlation CertificateCorrelation) SecurityAssessment {
	assessment := SecurityAssessment{
		RiskScore: 0.0,
	}

	// Cross-cloud certificate correlations are higher risk
	if correlation.SourceCertificate.Provider != correlation.TargetCertificate.Provider {
		assessment.RiskScore += 0.3
		assessment.SecurityIssues = append(assessment.SecurityIssues, "Cross-cloud certificate correlation")
		assessment.Recommendations = append(assessment.Recommendations, "Review cross-cloud certificate usage")
	}

	// Check for expired certificates
	now := time.Now()
	if correlation.SourceCertificate.NotAfter.Before(now) || correlation.TargetCertificate.NotAfter.Before(now) {
		assessment.RiskScore += 0.4
		assessment.SecurityIssues = append(assessment.SecurityIssues, "Expired certificate detected")
		assessment.Recommendations = append(assessment.Recommendations, "Renew expired certificates")
	}

	// Check for soon-to-expire certificates (within 30 days)
	thirtyDaysFromNow := now.AddDate(0, 0, 30)
	if correlation.SourceCertificate.NotAfter.Before(thirtyDaysFromNow) || correlation.TargetCertificate.NotAfter.Before(thirtyDaysFromNow) {
		assessment.RiskScore += 0.2
		assessment.SecurityIssues = append(assessment.SecurityIssues, "Certificate expiring soon")
		assessment.Recommendations = append(assessment.Recommendations, "Plan certificate renewal")
	}

	// Check for self-signed certificates
	if correlation.SourceCertificate.IsSelfSigned || correlation.TargetCertificate.IsSelfSigned {
		assessment.RiskScore += 0.3
		assessment.SecurityIssues = append(assessment.SecurityIssues, "Self-signed certificate detected")
		assessment.Recommendations = append(assessment.Recommendations, "Use CA-signed certificates")
	}

	// Check for weak key algorithms or sizes
	if ca.isWeakKeyAlgorithm(correlation.SourceCertificate) || ca.isWeakKeyAlgorithm(correlation.TargetCertificate) {
		assessment.RiskScore += 0.3
		assessment.SecurityIssues = append(assessment.SecurityIssues, "Weak cryptographic algorithm detected")
		assessment.Recommendations = append(assessment.Recommendations, "Upgrade to stronger algorithms")
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

	// Add compliance flags
	assessment.ComplianceFlags = []string{"TLS", "PKI", "Certificate_Management"}

	return assessment
}

// isWeakKeyAlgorithm checks if certificate uses weak cryptographic algorithms
func (ca *CertificateAnalyzer) isWeakKeyAlgorithm(cert Certificate) bool {
	// Check for weak signature algorithms
	weakSigAlgos := []string{"SHA1", "MD5", "MD2"}
	for _, weak := range weakSigAlgos {
		if strings.Contains(cert.SignatureAlgo, weak) {
			return true
		}
	}

	// Check for weak key sizes (assuming RSA)
	if cert.KeySize < 2048 {
		return true
	}

	return false
}

// generateCertHash generates a hash for certificate IDs
func generateCertHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)[:8]
}

// PersistCertificateCorrelations saves certificate correlations to database
func (ca *CertificateAnalyzer) PersistCertificateCorrelations(ctx context.Context, correlations []CertificateCorrelation) error {
	if len(correlations) == 0 {
		return nil
	}

	tx, err := ca.db.BeginTx(ctx, nil)
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

	for _, corr := range correlations {
		evidenceJSON, _ := json.Marshal(corr.MatchingAttributes)
		metadataJSON, _ := json.Marshal(map[string]interface{}{
			"chain_relationship":   corr.ChainRelationship,
			"shared_secrets":       corr.SharedSecrets,
			"security_assessment":  corr.SecurityAssessment,
		})

		_, err := stmt.Exec(
			corr.ID,
			corr.SourceCertificate.ID, corr.SourceCertificate.Provider, corr.SourceCertificate.Region, corr.SourceCertificate.AccountID, "certificate",
			corr.TargetCertificate.ID, corr.TargetCertificate.Provider, corr.TargetCertificate.Region, corr.TargetCertificate.AccountID, "certificate",
			"certificate_correlation", corr.CorrelationType, "automated_analysis", corr.ConfidenceScore,
			string(evidenceJSON), string(metadataJSON),
			fmt.Sprintf("Certificate correlation between %s (%s) and %s (%s)", corr.SourceCertificate.Name, corr.SourceCertificate.Provider, corr.TargetCertificate.Name, corr.TargetCertificate.Provider),
			"active", false,
			corr.DetectedAt, time.Now(), time.Now(),
		)
		if err != nil {
			ca.logger.Printf("Error persisting certificate correlation %s: %v", corr.ID, err)
			continue
		}
	}

	return tx.Commit()
}