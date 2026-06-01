package verification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// EdgeCaseVerifier provides specialized verification for edge cases
type EdgeCaseVerifier struct {
	*Verifier
}

// NewEdgeCaseVerifier creates a new edge case verifier
func NewEdgeCaseVerifier(dbPath string) (*EdgeCaseVerifier, error) {
	baseVerifier, err := NewVerifierWithPath(dbPath)
	if err != nil {
		return nil, err
	}

	return &EdgeCaseVerifier{
		Verifier: baseVerifier,
	}, nil
}

// EdgeCaseVerificationResult contains detailed edge case verification results
type EdgeCaseVerificationResult struct {
	UnicodeSupport       UnicodeVerification       `json:"unicode_support"`
	TagLimits            TagLimitVerification      `json:"tag_limits"`
	LongNames            LongNameVerification      `json:"long_names"`
	GlobalServices       GlobalServiceVerification `json:"global_services"`
	CircularDependencies CircularDepVerification   `json:"circular_dependencies"`
	SpecialStates        SpecialStateVerification  `json:"special_states"`
	RawDataIntegrity     RawDataVerification       `json:"raw_data_integrity"`
	CompressionAnalysis  CompressionAnalysis       `json:"compression_analysis"`
}

// UnicodeVerification verifies Unicode character handling
type UnicodeVerification struct {
	UnicodeTagsFound      int      `json:"unicode_tags_found"`
	EmojiTagsFound        int      `json:"emoji_tags_found"`
	UnicodeInDescriptions int      `json:"unicode_in_descriptions"`
	UnicodeCharsets       []string `json:"unicode_charsets"`
	EncodingIssues        []string `json:"encoding_issues"`
}

// TagLimitVerification verifies tag limit handling
type TagLimitVerification struct {
	ResourcesWithMaxTags int            `json:"resources_with_max_tags"`
	MaxTagsPerResource   int            `json:"max_tags_per_resource"`
	TagValueLengthStats  map[string]int `json:"tag_value_length_stats"`
	TagKeyPatterns       map[string]int `json:"tag_key_patterns"`
	TagLimitCompliance   bool           `json:"tag_limit_compliance"`
}

// LongNameVerification verifies long name handling
type LongNameVerification struct {
	LongestResourceName    int      `json:"longest_resource_name"`
	ResourcesWithLongNames int      `json:"resources_with_long_names"`
	NameTruncationIssues   []string `json:"name_truncation_issues"`
	SpecialCharacterNames  int      `json:"special_character_names"`
}

// GlobalServiceVerification verifies global vs regional service handling
type GlobalServiceVerification struct {
	GlobalServices           []string          `json:"global_services"`
	RegionalServices         []string          `json:"regional_services"`
	GlobalResourcesInRegions map[string]int    `json:"global_resources_in_regions"`
	ServiceClassification    map[string]string `json:"service_classification"`
}

// CircularDepVerification verifies circular dependency detection
type CircularDepVerification struct {
	CircularDependenciesFound int             `json:"circular_dependencies_found"`
	CircularChains            []CircularChain `json:"circular_chains"`
	DependencyGraphHealth     bool            `json:"dependency_graph_health"`
}

// CircularChain represents a circular dependency chain
type CircularChain struct {
	Resources    []string `json:"resources"`
	ChainLength  int      `json:"chain_length"`
	RelationType string   `json:"relation_type"`
}

// SpecialStateVerification verifies handling of resources in special states
type SpecialStateVerification struct {
	StoppedInstances      int      `json:"stopped_instances"`
	TerminatedInstances   int      `json:"terminated_instances"`
	FailedResources       int      `json:"failed_resources"`
	PendingResources      int      `json:"pending_resources"`
	StateTransitionIssues []string `json:"state_transition_issues"`
}

// RawDataVerification verifies raw data integrity and completeness
type RawDataVerification struct {
	ResourcesWithRawData  int              `json:"resources_with_raw_data"`
	RawDataValidJSON      int              `json:"raw_data_valid_json"`
	RawDataSizeStats      map[string]int64 `json:"raw_data_size_stats"`
	MissingFields         []string         `json:"missing_fields"`
	DataConsistencyIssues []string         `json:"data_consistency_issues"`
	UnicodeInRawData      int              `json:"unicode_in_raw_data"`
}

// CompressionAnalysis analyzes raw data compression potential
type CompressionAnalysis struct {
	TotalRawDataSize      int64          `json:"total_raw_data_size"`
	EstimatedUncompressed int64          `json:"estimated_uncompressed"`
	CompressionRatio      float64        `json:"compression_ratio"`
	RepetitiveFields      map[string]int `json:"repetitive_fields"`
	CompressionPotential  float64        `json:"compression_potential"`
}

// VerifyEdgeCases performs comprehensive edge case verification
func (ecv *EdgeCaseVerifier) VerifyEdgeCases(ctx context.Context, testID string) (*EdgeCaseVerificationResult, error) {
	result := &EdgeCaseVerificationResult{}

	// Verify Unicode support
	unicodeResult, err := ecv.verifyUnicodeSupport(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("unicode verification failed: %w", err)
	}
	result.UnicodeSupport = *unicodeResult

	// Verify tag limits
	tagResult, err := ecv.verifyTagLimits(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("tag limit verification failed: %w", err)
	}
	result.TagLimits = *tagResult

	// Verify long names
	nameResult, err := ecv.verifyLongNames(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("long name verification failed: %w", err)
	}
	result.LongNames = *nameResult

	// Verify global services
	globalResult, err := ecv.verifyGlobalServices(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("global service verification failed: %w", err)
	}
	result.GlobalServices = *globalResult

	// Verify circular dependencies
	circularResult, err := ecv.verifyCircularDependencies(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("circular dependency verification failed: %w", err)
	}
	result.CircularDependencies = *circularResult

	// Verify special states
	stateResult, err := ecv.verifySpecialStates(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("special state verification failed: %w", err)
	}
	result.SpecialStates = *stateResult

	// Verify raw data integrity
	rawDataResult, err := ecv.verifyRawDataIntegrity(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("raw data verification failed: %w", err)
	}
	result.RawDataIntegrity = *rawDataResult

	// Analyze compression
	compressionResult, err := ecv.analyzeCompression(ctx, testID)
	if err != nil {
		return nil, fmt.Errorf("compression analysis failed: %w", err)
	}
	result.CompressionAnalysis = *compressionResult

	return result, nil
}

// verifyUnicodeSupport checks Unicode character handling
func (ecv *EdgeCaseVerifier) verifyUnicodeSupport(ctx context.Context, testID string) (*UnicodeVerification, error) {
	result := &UnicodeVerification{
		UnicodeCharsets: []string{},
		EncodingIssues:  []string{},
	}

	// Query for resources with Unicode in tags or names
	query := `
		SELECT id, name, tags, raw_data
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	unicodePattern := regexp.MustCompile(`[^\x00-\x7F]`)
	emojiPattern := regexp.MustCompile(`[\x{1F600}-\x{1F64F}]|[\x{1F300}-\x{1F5FF}]|[\x{1F680}-\x{1F6FF}]|[\x{1F1E0}-\x{1F1FF}]`)

	for rows.Next() {
		var id, name sql.NullString
		var tags, rawData sql.NullString

		if err := rows.Scan(&id, &name, &tags, &rawData); err != nil {
			continue
		}

		// Check name for Unicode
		if name.Valid {
			if unicodePattern.MatchString(name.String) {
				result.UnicodeInDescriptions++
			}
			if emojiPattern.MatchString(name.String) {
				result.EmojiTagsFound++
			}
		}

		// Check tags for Unicode
		if tags.Valid {
			if unicodePattern.MatchString(tags.String) {
				result.UnicodeTagsFound++
			}
			if emojiPattern.MatchString(tags.String) {
				result.EmojiTagsFound++
			}

			// Validate UTF-8 encoding
			if !utf8.ValidString(tags.String) {
				result.EncodingIssues = append(result.EncodingIssues,
					fmt.Sprintf("Invalid UTF-8 in tags for resource %s", id.String))
			}
		}

		// Check raw data for Unicode
		if rawData.Valid {
			if unicodePattern.MatchString(rawData.String) {
				result.UnicodeInDescriptions++
			}

			// Validate JSON with Unicode
			var jsonData interface{}
			if err := json.Unmarshal([]byte(rawData.String), &jsonData); err != nil {
				if strings.Contains(err.Error(), "unicode") || strings.Contains(err.Error(), "UTF") {
					result.EncodingIssues = append(result.EncodingIssues,
						fmt.Sprintf("Unicode JSON parsing issue for resource %s: %v", id.String, err))
				}
			}
		}
	}

	// Detect character sets used
	if result.UnicodeTagsFound > 0 {
		result.UnicodeCharsets = append(result.UnicodeCharsets, "UTF-8")
	}
	if result.EmojiTagsFound > 0 {
		result.UnicodeCharsets = append(result.UnicodeCharsets, "Emoji")
	}

	return result, nil
}

// verifyTagLimits checks tag limit compliance and handling
func (ecv *EdgeCaseVerifier) verifyTagLimits(ctx context.Context, testID string) (*TagLimitVerification, error) {
	result := &TagLimitVerification{
		TagValueLengthStats: make(map[string]int),
		TagKeyPatterns:      make(map[string]int),
	}

	query := `
		SELECT id, type, tags, raw_data
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	maxTagsFound := 0

	for rows.Next() {
		var id, resourceType sql.NullString
		var tags, rawData sql.NullString

		if err := rows.Scan(&id, &resourceType, &tags, &rawData); err != nil {
			continue
		}

		// Parse tags from database
		var tagMap map[string]interface{}
		if tags.Valid && tags.String != "" {
			if err := json.Unmarshal([]byte(tags.String), &tagMap); err == nil {
				tagCount := len(tagMap)
				if tagCount > maxTagsFound {
					maxTagsFound = tagCount
				}

				// Check for maximum tag compliance (AWS limit is 50)
				if tagCount >= 50 {
					result.ResourcesWithMaxTags++
				}

				// Analyze tag patterns
				for key, value := range tagMap {
					// Track key patterns
					if strings.HasPrefix(key, "EdgeTag") {
						result.TagKeyPatterns["EdgeTag"]++
					} else {
						result.TagKeyPatterns["Standard"]++
					}

					// Track value lengths
					if valueStr, ok := value.(string); ok {
						length := len(valueStr)
						if length > 100 {
							result.TagValueLengthStats["Long"]++
						} else if length > 50 {
							result.TagValueLengthStats["Medium"]++
						} else {
							result.TagValueLengthStats["Short"]++
						}
					}
				}
			}
		}

		// Also check raw data for tag information
		if rawData.Valid {
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				if rawTags, exists := rawDataMap["Tags"]; exists {
					if tagArray, ok := rawTags.([]interface{}); ok {
						tagCount := len(tagArray)
						if tagCount > maxTagsFound {
							maxTagsFound = tagCount
						}
						if tagCount >= 50 {
							result.ResourcesWithMaxTags++
						}
					}
				}
			}
		}
	}

	result.MaxTagsPerResource = maxTagsFound
	result.TagLimitCompliance = maxTagsFound <= 50 // AWS limit

	return result, nil
}

// verifyLongNames checks handling of very long resource names
func (ecv *EdgeCaseVerifier) verifyLongNames(ctx context.Context, testID string) (*LongNameVerification, error) {
	result := &LongNameVerification{
		NameTruncationIssues: []string{},
	}

	query := `
		SELECT id, name, type, tags, raw_data
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	specialCharPattern := regexp.MustCompile(`[^a-zA-Z0-9\-_\.]`)

	for rows.Next() {
		var id, name, resourceType sql.NullString
		var tags, rawData sql.NullString

		if err := rows.Scan(&id, &name, &resourceType, &tags, &rawData); err != nil {
			continue
		}

		// Check name length
		if name.Valid {
			nameLength := len(name.String)
			if nameLength > result.LongestResourceName {
				result.LongestResourceName = nameLength
			}

			// Consider names > 100 characters as "long"
			if nameLength > 100 {
				result.ResourcesWithLongNames++
			}

			// Check for special characters
			if specialCharPattern.MatchString(name.String) {
				result.SpecialCharacterNames++
			}
		}

		// Check for name truncation by comparing with raw data
		if rawData.Valid {
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				if rawName, exists := rawDataMap["Name"]; exists {
					if rawNameStr, ok := rawName.(string); ok && name.Valid {
						if len(rawNameStr) != len(name.String) {
							result.NameTruncationIssues = append(result.NameTruncationIssues,
								fmt.Sprintf("Name truncation detected for %s: raw=%d, stored=%d",
									id.String, len(rawNameStr), len(name.String)))
						}
					}
				}
			}
		}
	}

	return result, nil
}

// verifyGlobalServices checks global vs regional service classification
func (ecv *EdgeCaseVerifier) verifyGlobalServices(ctx context.Context, testID string) (*GlobalServiceVerification, error) {
	result := &GlobalServiceVerification{
		GlobalServices:           []string{},
		RegionalServices:         []string{},
		GlobalResourcesInRegions: make(map[string]int),
		ServiceClassification:    make(map[string]string),
	}

	// Known global services
	globalServices := map[string]bool{
		"iam":        true,
		"cloudfront": true,
		"route53":    true,
		"waf":        true,
	}

	query := `
		SELECT DISTINCT service, region, COUNT(*) as count
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
		GROUP BY service, region
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	serviceRegions := make(map[string]map[string]int)

	for rows.Next() {
		var service, region sql.NullString
		var count int

		if err := rows.Scan(&service, &region, &count); err != nil {
			continue
		}

		if !service.Valid {
			continue
		}

		serviceName := service.String
		regionName := "unknown"
		if region.Valid {
			regionName = region.String
		}

		if serviceRegions[serviceName] == nil {
			serviceRegions[serviceName] = make(map[string]int)
		}
		serviceRegions[serviceName][regionName] = count
	}

	// Classify services based on their regional distribution
	for serviceName, regions := range serviceRegions {
		isGlobal := globalServices[serviceName]

		if isGlobal {
			result.GlobalServices = append(result.GlobalServices, serviceName)
			result.ServiceClassification[serviceName] = "global"

			// Count global resources appearing in each region
			for regionName, count := range regions {
				result.GlobalResourcesInRegions[regionName] += count
			}
		} else {
			result.RegionalServices = append(result.RegionalServices, serviceName)
			result.ServiceClassification[serviceName] = "regional"
		}
	}

	return result, nil
}

// verifyCircularDependencies checks for circular dependency detection
func (ecv *EdgeCaseVerifier) verifyCircularDependencies(ctx context.Context, testID string) (*CircularDepVerification, error) {
	result := &CircularDepVerification{
		CircularChains: []CircularChain{},
	}

	// Query for relationships that might form circular dependencies
	query := `
		SELECT r.from_id, r.to_id, r.relationship_type, 
		       res_from.type as from_type, res_to.type as to_type
		FROM aws_relationships r
		JOIN aws_resources res_from ON r.from_id = res_from.id
		JOIN aws_resources res_to ON r.to_id = res_to.id
		WHERE (JSON_EXTRACT(res_from.tags, '$.TestID') = ? 
		       OR JSON_EXTRACT(res_to.tags, '$.TestID') = ?)
		  AND res_from.type = 'SecurityGroup' 
		  AND res_to.type = 'SecurityGroup'
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := make(map[string][]string)

	for rows.Next() {
		var fromID, toID, relType, fromType, toType sql.NullString

		if err := rows.Scan(&fromID, &toID, &relType, &fromType, &toType); err != nil {
			continue
		}

		if fromID.Valid && toID.Valid {
			relationships[fromID.String] = append(relationships[fromID.String], toID.String)
		}
	}

	// Detect circular dependencies using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var detectCycle func(string, []string) bool
	detectCycle = func(node string, path []string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range relationships[node] {
			if !visited[neighbor] {
				if detectCycle(neighbor, path) {
					return true
				}
			} else if recStack[neighbor] {
				// Found a cycle
				cycleStart := -1
				for i, n := range path {
					if n == neighbor {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cycle := append(path[cycleStart:], neighbor)
					result.CircularChains = append(result.CircularChains, CircularChain{
						Resources:    cycle,
						ChainLength:  len(cycle) - 1,
						RelationType: "security_group_reference",
					})
					result.CircularDependenciesFound++
				}
				return true
			}
		}

		recStack[node] = false
		return false
	}

	// Check all nodes for cycles
	for node := range relationships {
		if !visited[node] {
			detectCycle(node, []string{})
		}
	}

	result.DependencyGraphHealth = result.CircularDependenciesFound == 0

	return result, nil
}

// verifySpecialStates checks handling of resources in special states
func (ecv *EdgeCaseVerifier) verifySpecialStates(ctx context.Context, testID string) (*SpecialStateVerification, error) {
	result := &SpecialStateVerification{
		StateTransitionIssues: []string{},
	}

	query := `
		SELECT id, type, name, raw_data
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
		  AND type = 'Instance'
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, resourceType, name sql.NullString
		var rawData sql.NullString

		if err := rows.Scan(&id, &resourceType, &name, &rawData); err != nil {
			continue
		}

		if rawData.Valid {
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				if state, exists := rawDataMap["State"]; exists {
					if stateMap, ok := state.(map[string]interface{}); ok {
						if stateName, exists := stateMap["Name"]; exists {
							stateStr, _ := stateName.(string)
							switch stateStr {
							case "stopped":
								result.StoppedInstances++
							case "terminated":
								result.TerminatedInstances++
							case "pending":
								result.PendingResources++
							case "failed":
								result.FailedResources++
							}
						}
					}
				}

				// Check for state transition issues
				if stateReason, exists := rawDataMap["StateReason"]; exists {
					if reasonMap, ok := stateReason.(map[string]interface{}); ok {
						if code, exists := reasonMap["Code"]; exists {
							if codeStr, ok := code.(string); ok && strings.Contains(codeStr, "failed") {
								result.StateTransitionIssues = append(result.StateTransitionIssues,
									fmt.Sprintf("State transition issue for %s: %s", id.String, codeStr))
							}
						}
					}
				}
			}
		}
	}

	return result, nil
}

// verifyRawDataIntegrity checks raw data completeness and integrity
func (ecv *EdgeCaseVerifier) verifyRawDataIntegrity(ctx context.Context, testID string) (*RawDataVerification, error) {
	result := &RawDataVerification{
		RawDataSizeStats:      make(map[string]int64),
		MissingFields:         []string{},
		DataConsistencyIssues: []string{},
	}

	query := `
		SELECT id, type, name, arn, raw_data,
		       CASE 
		           WHEN raw_data IS NOT NULL AND raw_data != '' THEN length(raw_data)
		           ELSE 0
		       END as raw_data_size
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	unicodePattern := regexp.MustCompile(`[^\x00-\x7F]`)

	for rows.Next() {
		var id, resourceType, name, arn sql.NullString
		var rawData sql.NullString
		var rawDataSize int64

		if err := rows.Scan(&id, &resourceType, &name, &arn, &rawData, &rawDataSize); err != nil {
			continue
		}

		// Count resources with raw data
		if rawData.Valid && rawData.String != "" {
			result.ResourcesWithRawData++

			// Track size statistics
			sizeCategory := "small"
			if rawDataSize > 10000 {
				sizeCategory = "large"
			} else if rawDataSize > 1000 {
				sizeCategory = "medium"
			}
			result.RawDataSizeStats[sizeCategory] += rawDataSize

			// Check for Unicode in raw data
			if unicodePattern.MatchString(rawData.String) {
				result.UnicodeInRawData++
			}

			// Validate JSON structure
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				result.RawDataValidJSON++

				// Check for essential fields
				essentialFields := []string{"Name", "Type", "State"}
				for _, field := range essentialFields {
					if _, exists := rawDataMap[field]; !exists {
						if resourceType.Valid && resourceType.String == "Instance" {
							result.MissingFields = append(result.MissingFields,
								fmt.Sprintf("Missing %s field in raw data for %s", field, id.String))
						}
					}
				}

				// Check data consistency between structured and raw data
				if name.Valid && name.String != "" {
					if rawName, exists := rawDataMap["Name"]; exists {
						if rawNameStr, ok := rawName.(string); ok {
							if rawNameStr != name.String && !strings.Contains(rawNameStr, name.String) {
								result.DataConsistencyIssues = append(result.DataConsistencyIssues,
									fmt.Sprintf("Name inconsistency for %s: structured='%s', raw='%s'",
										id.String, name.String, rawNameStr))
							}
						}
					}
				}

				if arn.Valid && arn.String != "" {
					if rawArn, exists := rawDataMap["Arn"]; exists {
						if rawArnStr, ok := rawArn.(string); ok {
							if rawArnStr != arn.String {
								result.DataConsistencyIssues = append(result.DataConsistencyIssues,
									fmt.Sprintf("ARN inconsistency for %s", id.String))
							}
						}
					}
				}
			} else {
				// JSON parsing failed
				result.DataConsistencyIssues = append(result.DataConsistencyIssues,
					fmt.Sprintf("Invalid JSON in raw data for %s: %v", id.String, err))
			}
		}
	}

	return result, nil
}

// analyzeCompression analyzes raw data compression potential
func (ecv *EdgeCaseVerifier) analyzeCompression(ctx context.Context, testID string) (*CompressionAnalysis, error) {
	result := &CompressionAnalysis{
		RepetitiveFields: make(map[string]int),
	}

	query := `
		SELECT raw_data,
		       CASE 
		           WHEN raw_data IS NOT NULL AND raw_data != '' THEN length(raw_data)
		           ELSE 0
		       END as raw_data_size
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, '$.TestID') = ?
		  AND raw_data IS NOT NULL 
		  AND raw_data != ''
	`

	rows, err := ecv.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fieldFrequency := make(map[string]int)
	valueFrequency := make(map[string]int)

	for rows.Next() {
		var rawData sql.NullString
		var rawDataSize int64

		if err := rows.Scan(&rawData, &rawDataSize); err != nil {
			continue
		}

		result.TotalRawDataSize += rawDataSize

		if rawData.Valid {
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				// Analyze field repetition
				ecv.analyzeMapRepetition(rawDataMap, "", fieldFrequency, valueFrequency)
			}
		}
	}

	// Calculate compression potential
	if result.TotalRawDataSize > 0 {
		// Estimate uncompressed size (rough estimate)
		result.EstimatedUncompressed = result.TotalRawDataSize

		// Simple compression ratio estimation based on repetitive content
		totalRepetitions := 0
		for field, count := range fieldFrequency {
			if count > 1 {
				result.RepetitiveFields[field] = count
				totalRepetitions += count
			}
		}

		// Estimate compression potential (simplified)
		if totalRepetitions > 0 {
			repetitionFactor := float64(totalRepetitions) / float64(len(fieldFrequency))
			result.CompressionPotential = 1.0 - (1.0 / (1.0 + repetitionFactor))
			result.CompressionRatio = 1.0 + repetitionFactor
		} else {
			result.CompressionPotential = 0.1 // Minimal compression for JSON structure
			result.CompressionRatio = 1.1
		}
	}

	return result, nil
}

// analyzeMapRepetition recursively analyzes field repetition in JSON data
func (ecv *EdgeCaseVerifier) analyzeMapRepetition(data map[string]interface{}, prefix string, fieldFreq, valueFreq map[string]int) {
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		fieldFreq[fullKey]++

		switch v := value.(type) {
		case map[string]interface{}:
			ecv.analyzeMapRepetition(v, fullKey, fieldFreq, valueFreq)
		case []interface{}:
			for i, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					ecv.analyzeMapRepetition(itemMap, fmt.Sprintf("%s[%d]", fullKey, i), fieldFreq, valueFreq)
				} else {
					valueFreq[fmt.Sprintf("%v", item)]++
				}
			}
		default:
			valueFreq[fmt.Sprintf("%v", value)]++
		}
	}
}
