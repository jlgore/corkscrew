package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/jlgore/corkscrew/test/harness/aws"
)

// TestResult captures all metrics and validation results from a test run
type TestResult struct {
	TestID       string        `json:"test_id"`
	ScenarioName string        `json:"scenario_name"`
	Provider     string        `json:"provider"`
	Region       string        `json:"region"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	Phase        string        `json:"phase"`
	Success      bool          `json:"success"`
	Error        error         `json:"error,omitempty"`

	// Deployment results
	DeploymentOutputs  map[string]interface{} `json:"deployment_outputs"`
	DeploymentDuration time.Duration          `json:"deployment_duration"`

	// Scan results
	ScanResult *automation.ScanResult `json:"scan_result"`

	// Verification results
	VerificationResult *VerificationResult `json:"verification_result"`

	// Cleanup results
	CleanupResult *aws.CleanupResult `json:"cleanup_result"`

	// Performance metrics
	Metrics TestMetrics `json:"metrics"`
}

// TestMetrics contains performance and resource metrics
type TestMetrics struct {
	ResourcesDeployed    int           `json:"resources_deployed"`
	ResourcesScanned     int           `json:"resources_scanned"`
	ResourcesVerified    int           `json:"resources_verified"`
	ScanDuration         time.Duration `json:"scan_duration"`
	VerificationDuration time.Duration `json:"verification_duration"`
	DatabaseSize         int64         `json:"database_size"`
	MemoryUsed           int64         `json:"memory_used"`
}

// VerificationResult contains detailed verification outcomes
type VerificationResult struct {
	TotalExpected      int                 `json:"total_expected"`
	TotalFound         int                 `json:"total_found"`
	TotalMissing       int                 `json:"total_missing"`
	Matches            []ResourceMatch     `json:"matches"`
	Missing            []ExpectedResource  `json:"missing"`
	AttributeChecks    []AttributeCheck    `json:"attribute_checks"`
	RelationshipChecks []RelationshipCheck `json:"relationship_checks"`
	Success            bool                `json:"success"`
	Details            string              `json:"details"`
}

// ResourceMatch represents a successful match between expected and actual resources
type ResourceMatch struct {
	Expected       ExpectedResource       `json:"expected"`
	Actual         map[string]interface{} `json:"actual"`
	Match          bool                   `json:"match"`
	AttributeScore float64                `json:"attribute_score"`
}

// ExpectedResource defines what we expect to find in the database
type ExpectedResource struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	ARN        string                 `json:"arn,omitempty"`
	ID         string                 `json:"id,omitempty"`
	Region     string                 `json:"region"`
	Attributes map[string]interface{} `json:"attributes"`
	Tags       map[string]string      `json:"tags,omitempty"`
}

// AttributeCheck represents verification of a specific attribute
type AttributeCheck struct {
	ResourceID  string      `json:"resource_id"`
	Attribute   string      `json:"attribute"`
	Expected    interface{} `json:"expected"`
	Actual      interface{} `json:"actual"`
	Match       bool        `json:"match"`
	Description string      `json:"description"`
}

// RelationshipCheck represents verification of relationships between resources
type RelationshipCheck struct {
	FromResource     string `json:"from_resource"`
	ToResource       string `json:"to_resource"`
	RelationshipType string `json:"relationship_type"`
	Expected         bool   `json:"expected"`
	Found            bool   `json:"found"`
	Description      string `json:"description"`
}

// AllPassed returns true if all verification checks passed
func (vr *VerificationResult) AllPassed() bool {
	return vr.Success && vr.TotalMissing == 0
}

// GetSuccessRate returns the percentage of successful verifications
func (vr *VerificationResult) GetSuccessRate() float64 {
	if vr.TotalExpected == 0 {
		return 0.0
	}
	return float64(vr.TotalFound) / float64(vr.TotalExpected) * 100.0
}

// Summary returns a human-readable summary of the test result
func (tr *TestResult) Summary() string {
	if tr.Error != nil {
		return fmt.Sprintf("FAILED in %s phase: %v", tr.Phase, tr.Error)
	}

	if tr.VerificationResult == nil {
		return "Test completed but no verification results available"
	}

	vr := tr.VerificationResult
	successRate := vr.GetSuccessRate()

	return fmt.Sprintf("SUCCESS: %d/%d resources found (%.1f%%), %d attribute checks, %d relationship checks",
		vr.TotalFound, vr.TotalExpected, successRate,
		len(vr.AttributeChecks), len(vr.RelationshipChecks))
}

// SaveToFile saves the test result to a JSON file
func (tr *TestResult) SaveToFile(filename string) error {
	data, err := json.Marshal(tr)
	if err != nil {
		return fmt.Errorf("failed to marshal test result: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write test result file: %w", err)
	}

	return nil
}

// LoadFromFile loads the test result from a JSON file
func (tr *TestResult) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read test result file: %w", err)
	}

	if err := json.Unmarshal(data, tr); err != nil {
		return fmt.Errorf("failed to unmarshal test result: %w", err)
	}

	return nil
}
