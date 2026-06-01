package verification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

// Verifier handles DuckDB verification operations
type Verifier struct {
	db     *sql.DB
	dbPath string
}

// VerificationResult contains the results of resource verification
type VerificationResult struct {
	BucketsFound   int
	ExpectedTestID string
	ActualTestIDs  []string
	BucketDetails  []BucketVerification
	RawDataSizes   []int
	AllPassed      bool
	Errors         []string
}

// BucketVerification contains details about a verified bucket
type BucketVerification struct {
	ID           string
	Name         string
	ARN          string
	TestID       string
	RawDataSize  int
	HasRawData   bool
	RawDataValid bool
}

// NewVerifier creates a new DuckDB verifier
func NewVerifier() (*Verifier, error) {
	// Find the DuckDB file (should be in the root corkscrew directory)
	dbPath, err := findDuckDBFile()
	if err != nil {
		return nil, fmt.Errorf("failed to find DuckDB file: %w", err)
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping DuckDB: %w", err)
	}

	return &Verifier{
		db:     db,
		dbPath: dbPath,
	}, nil
}

// Close closes the database connection
func (v *Verifier) Close() error {
	if v.db != nil {
		return v.db.Close()
	}
	return nil
}

// VerifyBucketExists verifies that a bucket with the given testID exists in the database
func (v *Verifier) VerifyBucketExists(ctx context.Context, expectedTestID, bucketName, bucketARN string) (*VerificationResult, error) {
	result := &VerificationResult{
		ExpectedTestID: expectedTestID,
		ActualTestIDs:  []string{},
		BucketDetails:  []BucketVerification{},
		RawDataSizes:   []int{},
		Errors:         []string{},
	}

	// Query for buckets with the testID in tags
	query := `
		SELECT 
			id, 
			name, 
			arn, 
			raw_data,
			CASE 
				WHEN raw_data IS NOT NULL AND raw_data != '' THEN length(raw_data)
				ELSE 0
			END as raw_data_size
		FROM aws_resources 
		WHERE service = 's3' 
		AND type = 'Bucket'
		AND (
			JSON_EXTRACT(tags, '$.TestID') = ? 
			OR name = ?
			OR arn = ?
		)
		ORDER BY created_at DESC
	`

	rows, err := v.db.QueryContext(ctx, query, expectedTestID, bucketName, bucketARN)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Query failed: %v", err))
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var bucket BucketVerification
		var rawData sql.NullString

		err := rows.Scan(&bucket.ID, &bucket.Name, &bucket.ARN, &rawData, &bucket.RawDataSize)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Row scan failed: %v", err))
			continue
		}

		// Check if raw data exists and is valid JSON
		bucket.HasRawData = rawData.Valid && rawData.String != ""
		if bucket.HasRawData {
			var jsonData interface{}
			err := json.Unmarshal([]byte(rawData.String), &jsonData)
			bucket.RawDataValid = err == nil

			// Extract TestID from tags if present
			if tags, err := v.extractTagsFromRawData(rawData.String); err == nil {
				if testID, exists := tags["TestID"]; exists {
					bucket.TestID = testID
					result.ActualTestIDs = append(result.ActualTestIDs, testID)
				}
			}
		}

		result.BucketDetails = append(result.BucketDetails, bucket)
		result.RawDataSizes = append(result.RawDataSizes, bucket.RawDataSize)
		result.BucketsFound++
	}

	if err := rows.Err(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Rows iteration failed: %v", err))
		return result, err
	}

	// Determine if verification passed
	result.AllPassed = result.BucketsFound > 0 &&
		len(result.Errors) == 0 &&
		v.allBucketsHaveValidRawData(result.BucketDetails)

	return result, nil
}

// GetDatabaseStats returns general statistics about the database
func (v *Verifier) GetDatabaseStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count total resources
	var totalResources int
	err := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM aws_resources").Scan(&totalResources)
	if err != nil {
		return nil, fmt.Errorf("failed to count total resources: %w", err)
	}
	stats["total_resources"] = totalResources

	// Count S3 buckets
	var s3Buckets int
	err = v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM aws_resources WHERE service = 's3' AND type = 'Bucket'").Scan(&s3Buckets)
	if err != nil {
		return nil, fmt.Errorf("failed to count S3 buckets: %w", err)
	}
	stats["s3_buckets"] = s3Buckets

	// Count test harness resources
	var testResources int
	err = v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM aws_resources WHERE JSON_EXTRACT(tags, '$.TestHarness') = 'true'").Scan(&testResources)
	if err != nil {
		// If this fails, it might be because the tags column doesn't exist or isn't JSON
		stats["test_resources"] = 0
	} else {
		stats["test_resources"] = testResources
	}

	return stats, nil
}

// extractTagsFromRawData attempts to extract tags from the raw JSON data
func (v *Verifier) extractTagsFromRawData(rawData string) (map[string]string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(rawData), &data); err != nil {
		return nil, err
	}

	// Look for tags in various possible locations
	if tags, ok := data["Tags"].([]interface{}); ok {
		// AWS API format: Tags array with Key/Value objects
		result := make(map[string]string)
		for _, tag := range tags {
			if tagMap, ok := tag.(map[string]interface{}); ok {
				if key, keyOk := tagMap["Key"].(string); keyOk {
					if value, valueOk := tagMap["Value"].(string); valueOk {
						result[key] = value
					}
				}
			}
		}
		return result, nil
	}

	if tags, ok := data["tags"].(map[string]interface{}); ok {
		// Alternative format: tags as key-value map
		result := make(map[string]string)
		for key, value := range tags {
			if strValue, ok := value.(string); ok {
				result[key] = strValue
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("no tags found in raw data")
}

// allBucketsHaveValidRawData checks if all buckets have valid raw data
func (v *Verifier) allBucketsHaveValidRawData(buckets []BucketVerification) bool {
	for _, bucket := range buckets {
		if !bucket.HasRawData || !bucket.RawDataValid {
			return false
		}
	}
	return true
}

// NewVerifierWithPath creates a verifier with a specific database path
func NewVerifierWithPath(dbPath string) (*Verifier, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB at %s: %w", dbPath, err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping DuckDB: %w", err)
	}

	return &Verifier{
		db:     db,
		dbPath: dbPath,
	}, nil
}

// Enhanced verification methods for the new framework

// VerifyResources verifies all expected resources exist in the database
func (v *Verifier) VerifyResources(ctx context.Context, expected map[string]interface{}) (*EnhancedVerificationResult, error) {
	result := &EnhancedVerificationResult{
		StartTime:          time.Now(),
		Matches:            []ResourceMatch{},
		Missing:            []ExpectedResource{},
		AttributeChecks:    []AttributeCheck{},
		RelationshipChecks: []RelationshipCheck{},
	}

	expectedResources := extractExpectedResources(expected)
	result.TotalExpected = len(expectedResources)

	for _, exp := range expectedResources {
		match, err := v.findResource(ctx, exp)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error finding %s: %v", exp.ID, err))
			continue
		}

		if match == nil {
			result.Missing = append(result.Missing, exp)
			continue
		}

		result.TotalFound++

		// Verify attributes
		attrChecks := v.verifyAttributes(exp, match)
		result.AttributeChecks = append(result.AttributeChecks, attrChecks...)

		matchScore := calculateAttributeScore(attrChecks)
		result.Matches = append(result.Matches, ResourceMatch{
			Expected:       exp,
			Actual:         match,
			Match:          matchScore > 0.8, // 80% threshold
			AttributeScore: matchScore,
		})
	}

	result.TotalMissing = len(result.Missing)
	result.Success = result.TotalMissing == 0 && len(result.Errors) == 0
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// VerifyRelationships verifies expected relationships between resources
func (v *Verifier) VerifyRelationships(ctx context.Context, expected map[string]interface{}) ([]RelationshipCheck, error) {
	checks := []RelationshipCheck{}

	if relationships, ok := expected["relationships"].(map[string]interface{}); ok {
		for name, rel := range relationships {
			if relMap, ok := rel.(map[string]interface{}); ok {
				check := v.verifyRelationship(ctx, name, relMap)
				checks = append(checks, check)
			}
		}
	}

	return checks, nil
}

// findResource searches for a resource in the database
func (v *Verifier) findResource(ctx context.Context, expected ExpectedResource) (map[string]interface{}, error) {
	query := `
		SELECT id, name, arn, type, service, region, tags, raw_data
		FROM aws_resources 
		WHERE (name = ? OR arn = ? OR id = ?)
		AND service = ?
		AND type = ?
		LIMIT 1
	`

	service := strings.ToLower(getServiceFromType(expected.Type))

	row := v.db.QueryRowContext(ctx, query,
		expected.Name, expected.ARN, expected.ID,
		service, expected.Type)

	var id, name, arn, resourceType, resourceService, region sql.NullString
	var tags, rawData sql.NullString

	err := row.Scan(&id, &name, &arn, &resourceType, &resourceService, &region, &tags, &rawData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Resource not found
		}
		return nil, err
	}

	result := map[string]interface{}{
		"id":      id.String,
		"name":    name.String,
		"arn":     arn.String,
		"type":    resourceType.String,
		"service": resourceService.String,
		"region":  region.String,
	}

	// Parse tags if available
	if tags.Valid && tags.String != "" {
		if tagMap, err := parseTagsFromJSON(tags.String); err == nil {
			result["tags"] = tagMap
		}
	}

	// Parse raw data if available
	if rawData.Valid && rawData.String != "" {
		var rawDataMap map[string]interface{}
		if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
			result["raw_data"] = rawDataMap
		}
	}

	return result, nil
}

// verifyAttributes checks if resource attributes match expectations
func (v *Verifier) verifyAttributes(expected ExpectedResource, actual map[string]interface{}) []AttributeCheck {
	checks := []AttributeCheck{}

	for attr, expectedValue := range expected.Attributes {
		check := AttributeCheck{
			ResourceID:  actual["id"].(string),
			Attribute:   attr,
			Expected:    expectedValue,
			Description: fmt.Sprintf("Verify %s attribute for %s", attr, expected.Type),
		}

		if actualValue, exists := actual[attr]; exists {
			check.Actual = actualValue
			check.Match = compareValues(expectedValue, actualValue)
		} else if rawData, exists := actual["raw_data"].(map[string]interface{}); exists {
			if rawValue, exists := rawData[attr]; exists {
				check.Actual = rawValue
				check.Match = compareValues(expectedValue, rawValue)
			} else {
				check.Match = false
			}
		} else {
			check.Match = false
		}

		checks = append(checks, check)
	}

	return checks
}

// verifyRelationship checks if a relationship exists between resources
func (v *Verifier) verifyRelationship(ctx context.Context, name string, relMap map[string]interface{}) RelationshipCheck {
	check := RelationshipCheck{
		Description: name,
		Expected:    true,
	}

	fromResource, _ := relMap["from"].(string)
	toResource, _ := relMap["to"].(string)
	relType, _ := relMap["type"].(string)

	check.FromResource = fromResource
	check.ToResource = toResource
	check.RelationshipType = relType

	// Query for relationship in database
	query := `
		SELECT COUNT(*) 
		FROM aws_relationships 
		WHERE from_id = ? AND to_id = ? AND relationship_type = ?
	`

	var count int
	err := v.db.QueryRowContext(ctx, query, fromResource, toResource, relType).Scan(&count)
	if err != nil {
		check.Found = false
	} else {
		check.Found = count > 0
	}

	return check
}

// Helper functions

func extractExpectedResources(expected map[string]interface{}) []ExpectedResource {
	resources := []ExpectedResource{}

	if expectedMap, ok := expected["expectedResources"].(map[string]interface{}); ok {
		for _, serviceResources := range expectedMap {
			if resourceArray, ok := serviceResources.([]interface{}); ok {
				for _, resource := range resourceArray {
					if resourceMap, ok := resource.(map[string]interface{}); ok {
						res := ExpectedResource{
							Type:       getStringValue(resourceMap, "type"),
							Name:       getStringValue(resourceMap, "name"),
							ARN:        getStringValue(resourceMap, "arn"),
							ID:         getStringValue(resourceMap, "id"),
							Region:     getStringValue(resourceMap, "region"),
							Attributes: make(map[string]interface{}),
						}

						if attrs, ok := resourceMap["attributes"].(map[string]interface{}); ok {
							res.Attributes = attrs
						}

						if tags, ok := resourceMap["tags"].(map[string]string); ok {
							res.Tags = tags
						}

						resources = append(resources, res)
					}
				}
			}
		}
	}

	return resources
}

func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getServiceFromType(resourceType string) string {
	switch resourceType {
	case "Bucket", "BucketVersioning":
		return "s3"
	case "Instance", "VPC", "SecurityGroup", "Subnet":
		return "ec2"
	case "Function":
		return "lambda"
	case "Role", "Policy":
		return "iam"
	default:
		return strings.ToLower(resourceType)
	}
}

func parseTagsFromJSON(tagsJSON string) (map[string]string, error) {
	var tags interface{}
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return nil, err
	}

	result := make(map[string]string)

	// Handle AWS format: array of {Key, Value} objects
	if tagArray, ok := tags.([]interface{}); ok {
		for _, tag := range tagArray {
			if tagMap, ok := tag.(map[string]interface{}); ok {
				if key, keyOk := tagMap["Key"].(string); keyOk {
					if value, valueOk := tagMap["Value"].(string); valueOk {
						result[key] = value
					}
				}
			}
		}
		return result, nil
	}

	// Handle simple key-value format
	if tagMap, ok := tags.(map[string]interface{}); ok {
		for key, value := range tagMap {
			if strValue, ok := value.(string); ok {
				result[key] = strValue
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("unsupported tags format")
}

func compareValues(expected, actual interface{}) bool {
	// Handle nil cases
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}

	// Use reflection for deep comparison
	return reflect.DeepEqual(expected, actual)
}

func calculateAttributeScore(checks []AttributeCheck) float64 {
	if len(checks) == 0 {
		return 1.0
	}

	passed := 0
	for _, check := range checks {
		if check.Match {
			passed++
		}
	}

	return float64(passed) / float64(len(checks))
}

// Enhanced verification result types

type EnhancedVerificationResult struct {
	TotalExpected      int                 `json:"total_expected"`
	TotalFound         int                 `json:"total_found"`
	TotalMissing       int                 `json:"total_missing"`
	Matches            []ResourceMatch     `json:"matches"`
	Missing            []ExpectedResource  `json:"missing"`
	AttributeChecks    []AttributeCheck    `json:"attribute_checks"`
	RelationshipChecks []RelationshipCheck `json:"relationship_checks"`
	Success            bool                `json:"success"`
	StartTime          time.Time           `json:"start_time"`
	EndTime            time.Time           `json:"end_time"`
	Duration           time.Duration       `json:"duration"`
	Errors             []string            `json:"errors"`
}

type ResourceMatch struct {
	Expected       ExpectedResource       `json:"expected"`
	Actual         map[string]interface{} `json:"actual"`
	Match          bool                   `json:"match"`
	AttributeScore float64                `json:"attribute_score"`
}

type ExpectedResource struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	ARN        string                 `json:"arn,omitempty"`
	ID         string                 `json:"id,omitempty"`
	Region     string                 `json:"region"`
	Attributes map[string]interface{} `json:"attributes"`
	Tags       map[string]string      `json:"tags,omitempty"`
}

type AttributeCheck struct {
	ResourceID  string      `json:"resource_id"`
	Attribute   string      `json:"attribute"`
	Expected    interface{} `json:"expected"`
	Actual      interface{} `json:"actual"`
	Match       bool        `json:"match"`
	Description string      `json:"description"`
}

type RelationshipCheck struct {
	FromResource     string `json:"from_resource"`
	ToResource       string `json:"to_resource"`
	RelationshipType string `json:"relationship_type"`
	Expected         bool   `json:"expected"`
	Found            bool   `json:"found"`
	Description      string `json:"description"`
}

func (vr *EnhancedVerificationResult) AllPassed() bool {
	return vr.Success && vr.TotalMissing == 0 && len(vr.Errors) == 0
}

func (vr *EnhancedVerificationResult) GetSuccessRate() float64 {
	if vr.TotalExpected == 0 {
		return 0.0
	}
	return float64(vr.TotalFound) / float64(vr.TotalExpected) * 100.0
}

// findDuckDBFile locates the DuckDB database file
func findDuckDBFile() (string, error) {
	// Common DuckDB file names used by Corkscrew
	possibleNames := []string{
		"corkscrew.db",
		"data.db",
		"resources.db",
		"aws_resources.db",
	}

	// Start from current directory and work up
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check current directory first (test/harness), then parent directories
	searchDirs := []string{
		currentDir,                                    // test/harness (should have local corkscrew.db)
		filepath.Join(currentDir, ".."),               // test
		filepath.Join(currentDir, "..", ".."),         // root
		filepath.Join(currentDir, "..", "..", "data"), // root/data
	}

	for _, dir := range searchDirs {
		for _, dbName := range possibleNames {
			dbPath := filepath.Join(dir, dbName)
			if _, err := os.Stat(dbPath); err == nil {
				return dbPath, nil
			}
		}
	}

	// If not found, return a path in the current directory (test/harness)
	// This is where corkscrew init should have created it
	return filepath.Join(currentDir, "corkscrew.db"), nil
}
