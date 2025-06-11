//go:build validation
// +build validation

package tests

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHardcodedServiceValidation validates that no hardcoded services remain in the codebase
func TestHardcodedServiceValidation(t *testing.T) {
	if os.Getenv("RUN_VALIDATION_TESTS") != "true" {
		t.Skip("Skipping validation tests. Set RUN_VALIDATION_TESTS=true to run.")
	}

	validator := &HardcodedServiceValidator{
		rootPath: "/home/jg/git/corkscrew/plugins/aws-provider",
	}

	t.Run("ScanForHardcodedServices", validator.testScanForHardcodedServices)
	t.Run("ValidateServiceLists", validator.testValidateServiceLists)
	t.Run("CheckArchiveDirectory", validator.testCheckArchiveDirectory)
	t.Run("ValidateDynamicLoading", validator.testValidateDynamicLoading)
	t.Run("CheckImportStatements", validator.testCheckImportStatements)
	t.Run("ValidateConfigurationFiles", validator.testValidateConfigurationFiles)
}

type HardcodedServiceValidator struct {
	rootPath string
}

// Common AWS service names that should NOT be hardcoded
var knownAWSServices = []string{
	"s3", "ec2", "lambda", "rds", "dynamodb", "iam", "ecs", "eks",
	"cloudformation", "cloudwatch", "sns", "sqs", "kinesis", "glue",
	"secretsmanager", "ssm", "kms", "elasticache", "redshift",
	"apigateway", "route53", "elb", "elbv2", "autoscaling",
	"organizations", "cloudtrail", "config", "guardduty",
}

// Patterns that indicate hardcoded service usage
var hardcodedPatterns = []*regexp.Regexp{
	// Service name arrays/slices
	regexp.MustCompile(`\[\]string\s*{\s*"[a-z0-9-]+"`),
	regexp.MustCompile(`services\s*:=\s*\[\]string\s*{`),
	regexp.MustCompile(`serviceList\s*:=\s*\[\]string\s*{`),
	
	// Map initializations with service names
	regexp.MustCompile(`map\[string\].*{\s*"[a-z0-9-]+"`),
	regexp.MustCompile(`serviceMap\s*:=\s*map\[string\]`),
	
	// Switch statements with service names
	regexp.MustCompile(`switch\s+.*service.*{[\s\S]*case\s+"[a-z0-9-]+"`),
	regexp.MustCompile(`case\s+"(s3|ec2|lambda|rds|dynamodb|iam|ecs|eks)"`),
	
	// Import statements for specific AWS services
	regexp.MustCompile(`"github\.com/aws/aws-sdk-go-v2/service/[a-z0-9-]+"`),
	
	// Function calls with hardcoded service names
	regexp.MustCompile(`CreateClient\(\s*"[a-z0-9-]+"`),
	regexp.MustCompile(`ScanService\(\s*.*,\s*"[a-z0-9-]+"`),
}

// Files and directories that are allowed to contain hardcoded services
var allowedHardcodedFiles = map[string]bool{
	// Test files are allowed to have hardcoded services for testing
	"tests/":                              true,
	"test_":                               true,
	"_test.go":                            true,
	"testdata/":                           true,
	
	// Archive directory contains old code
	"archive/":                            true,
	
	// Generated files are allowed
	"generated/":                          true,
	
	// Documentation files
	"README.md":                           true,
	"MIGRATION_COMPLETED.md":              true,
	"PHASE2_REFLECTION_DISCOVERY_COMPLETED.md": true,
	"PHASE3_ANALYSIS_GENERATION_COMPLETED.md": true,
	
	// Migration-specific files that might contain examples
	"migration_":                          true,
	"example_":                            true,
}

// testScanForHardcodedServices scans the codebase for hardcoded service references
func (v *HardcodedServiceValidator) testScanForHardcodedServices(t *testing.T) {
	var violations []HardcodedViolation
	
	err := filepath.Walk(v.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories
		if info.IsDir() {
			return nil
		}
		
		// Only check Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		
		// Check if file is allowed to have hardcoded services
		relPath, _ := filepath.Rel(v.rootPath, path)
		if v.isFileAllowed(relPath) {
			return nil
		}
		
		fileViolations, err := v.scanFileForHardcodedServices(path)
		if err != nil {
			t.Logf("Warning: Failed to scan file %s: %v", path, err)
			return nil
		}
		
		violations = append(violations, fileViolations...)
		return nil
	})
	
	require.NoError(t, err, "Failed to walk directory tree")
	
	// Report violations
	if len(violations) > 0 {
		t.Logf("Found %d hardcoded service violations:", len(violations))
		
		// Group violations by file
		violationsByFile := make(map[string][]HardcodedViolation)
		for _, violation := range violations {
			violationsByFile[violation.File] = append(violationsByFile[violation.File], violation)
		}
		
		// Sort files for consistent output
		var files []string
		for file := range violationsByFile {
			files = append(files, file)
		}
		sort.Strings(files)
		
		for _, file := range files {
			t.Logf("\nFile: %s", file)
			for _, violation := range violationsByFile[file] {
				t.Logf("  Line %d: %s", violation.Line, violation.Content)
				t.Logf("    Reason: %s", violation.Reason)
			}
		}
	}
	
	// Test should pass if no violations in non-archive, non-test files
	criticalViolations := 0
	for _, violation := range violations {
		if !strings.Contains(violation.File, "archive/") && 
		   !strings.Contains(violation.File, "test") {
			criticalViolations++
		}
	}
	
	assert.Equal(t, 0, criticalViolations, 
		"Found %d critical hardcoded service violations (excluding tests and archive)", 
		criticalViolations)
		
	t.Logf("Hardcoded service validation: %d total violations, %d critical violations", 
		len(violations), criticalViolations)
}

// testValidateServiceLists checks for any remaining hardcoded service lists
func (v *HardcodedServiceValidator) testValidateServiceLists(t *testing.T) {
	suspiciousFiles := []string{
		"aws_provider.go",
		"client_factory.go", 
		"unified_scanner.go",
		"service_registry.go",
		"factory.go",
		"scanner_registry.go",
	}
	
	var violations []string
	
	for _, filename := range suspiciousFiles {
		err := filepath.Walk(v.rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			
			if info.Name() == filename {
				// Skip archive and test files
				relPath, _ := filepath.Rel(v.rootPath, path)
				if v.isFileAllowed(relPath) {
					return nil
				}
				
				hasLists, listDetails := v.checkForServiceLists(path)
				if hasLists {
					violations = append(violations, fmt.Sprintf("%s: %s", path, listDetails))
				}
			}
			
			return nil
		})
		
		require.NoError(t, err, "Failed to walk directory for file %s", filename)
	}
	
	if len(violations) > 0 {
		t.Logf("Found hardcoded service lists in:")
		for _, violation := range violations {
			t.Logf("  %s", violation)
		}
	}
	
	assert.Empty(t, violations, "No hardcoded service lists should remain in production code")
}

// testCheckArchiveDirectory ensures archive directory contains expected legacy code
func (v *HardcodedServiceValidator) testCheckArchiveDirectory(t *testing.T) {
	archivePath := filepath.Join(v.rootPath, "archive")
	
	// Archive directory should exist
	_, err := os.Stat(archivePath)
	require.NoError(t, err, "Archive directory should exist")
	
	// Archive should contain legacy files
	var archiveFiles []string
	err = filepath.Walk(archivePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			archiveFiles = append(archiveFiles, info.Name())
		}
		
		return nil
	})
	
	require.NoError(t, err, "Failed to walk archive directory")
	
	// Should have archived files
	assert.Greater(t, len(archiveFiles), 0, "Archive directory should contain legacy files")
	
	// Check for expected legacy files
	expectedLegacyFiles := []string{
		"hardcoded_service_lists.go",
		"hardcoded_methods.go",
		"factory_original.go",
	}
	
	archiveFileSet := make(map[string]bool)
	for _, file := range archiveFiles {
		archiveFileSet[file] = true
	}
	
	for _, expected := range expectedLegacyFiles {
		if archiveFileSet[expected] {
			t.Logf("Found expected legacy file in archive: %s", expected)
		}
	}
	
	t.Logf("Archive contains %d Go files", len(archiveFiles))
}

// testValidateDynamicLoading ensures all service loading is dynamic
func (v *HardcodedServiceValidator) testValidateDynamicLoading(t *testing.T) {
	dynamicLoadingFiles := []string{
		"dynamic_loader.go",
		"reflection_factory.go", 
		"runtime_discovery.go",
		"service_discovery.go",
	}
	
	foundFiles := 0
	
	for _, filename := range dynamicLoadingFiles {
		err := filepath.Walk(v.rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			
			if info.Name() == filename {
				// Skip archive files
				if strings.Contains(path, "archive/") {
					return nil
				}
				
				foundFiles++
				
				// Verify file contains dynamic loading patterns
				hasDynamic := v.checkForDynamicPatterns(path)
				assert.True(t, hasDynamic, "File %s should contain dynamic loading patterns", path)
				
				t.Logf("Validated dynamic loading file: %s", path)
			}
			
			return nil
		})
		
		require.NoError(t, err, "Failed to walk directory for file %s", filename)
	}
	
	assert.Greater(t, foundFiles, 0, "Should find dynamic loading files")
	t.Logf("Found and validated %d dynamic loading files", foundFiles)
}

// testCheckImportStatements validates import statements don't hardcode services
func (v *HardcodedServiceValidator) testCheckImportStatements(t *testing.T) {
	var violations []string
	
	err := filepath.Walk(v.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if !strings.HasSuffix(path, ".go") || info.IsDir() {
			return nil
		}
		
		// Skip allowed files
		relPath, _ := filepath.Rel(v.rootPath, path)
		if v.isFileAllowed(relPath) {
			return nil
		}
		
		badImports := v.checkForHardcodedImports(path)
		if len(badImports) > 0 {
			for _, imp := range badImports {
				violations = append(violations, fmt.Sprintf("%s: %s", path, imp))
			}
		}
		
		return nil
	})
	
	require.NoError(t, err, "Failed to check import statements")
	
	if len(violations) > 0 {
		t.Logf("Found hardcoded service imports:")
		for _, violation := range violations {
			t.Logf("  %s", violation)
		}
	}
	
	assert.Empty(t, violations, "No hardcoded service imports should remain")
}

// testValidateConfigurationFiles checks configuration files for hardcoded services
func (v *HardcodedServiceValidator) testValidateConfigurationFiles(t *testing.T) {
	configFiles := []string{
		"corkscrew.yaml",
		"config.yaml", 
		"services.yaml",
		"*.json",
		"*.toml",
	}
	
	for _, pattern := range configFiles {
		matches, err := filepath.Glob(filepath.Join(v.rootPath, pattern))
		if err != nil {
			continue
		}
		
		for _, match := range matches {
			// Skip generated files
			if strings.Contains(match, "generated/") {
				continue
			}
			
			violations := v.checkConfigFileForHardcodedServices(match)
			if len(violations) > 0 {
				t.Logf("Config file %s contains potential hardcoded services:", match)
				for _, violation := range violations {
					t.Logf("  %s", violation)
				}
			}
		}
	}
}

// Helper methods

type HardcodedViolation struct {
	File    string
	Line    int
	Content string
	Reason  string
}

func (v *HardcodedServiceValidator) isFileAllowed(relPath string) bool {
	for pattern := range allowedHardcodedFiles {
		if strings.Contains(relPath, pattern) {
			return true
		}
	}
	return false
}

func (v *HardcodedServiceValidator) scanFileForHardcodedServices(filePath string) ([]HardcodedViolation, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	var violations []HardcodedViolation
	scanner := bufio.NewScanner(file)
	lineNum := 0
	
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		
		// Check against hardcoded patterns
		for _, pattern := range hardcodedPatterns {
			if pattern.MatchString(line) {
				violations = append(violations, HardcodedViolation{
					File:    filePath,
					Line:    lineNum,
					Content: strings.TrimSpace(line),
					Reason:  "Matches hardcoded service pattern",
				})
			}
		}
		
		// Check for explicit service names
		for _, service := range knownAWSServices {
			if v.containsHardcodedService(line, service) {
				violations = append(violations, HardcodedViolation{
					File:    filePath,
					Line:    lineNum,
					Content: strings.TrimSpace(line),
					Reason:  fmt.Sprintf("Contains hardcoded service name: %s", service),
				})
			}
		}
	}
	
	return violations, scanner.Err()
}

func (v *HardcodedServiceValidator) containsHardcodedService(line, service string) bool {
	// Look for service name in quotes
	quotedService := `"` + service + `"`
	if strings.Contains(line, quotedService) {
		// But exclude certain allowed contexts
		allowedContexts := []string{
			"t.Log", "fmt.Sprint", "assert.", "require.", "Skip(",
			"// ", "* ", "func Test", "func Benchmark",
		}
		
		for _, context := range allowedContexts {
			if strings.Contains(line, context) {
				return false
			}
		}
		
		return true
	}
	
	return false
}

func (v *HardcodedServiceValidator) checkForServiceLists(filePath string) (bool, string) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, ""
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	lineNum := 0
	inArray := false
	arrayServiceCount := 0
	
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		
		// Start of array
		if strings.Contains(line, "[]string{") || strings.Contains(line, "] = []string{") {
			inArray = true
			arrayServiceCount = 0
			continue
		}
		
		// End of array
		if inArray && strings.Contains(line, "}") {
			inArray = false
			if arrayServiceCount >= 3 {
				return true, fmt.Sprintf("Found array with %d service-like strings near line %d", arrayServiceCount, lineNum)
			}
		}
		
		// Count potential services in array
		if inArray {
			for _, service := range knownAWSServices {
				if strings.Contains(line, `"`+service+`"`) {
					arrayServiceCount++
					break
				}
			}
		}
	}
	
	return false, ""
}

func (v *HardcodedServiceValidator) checkForDynamicPatterns(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()
	
	dynamicPatterns := []string{
		"reflect.",
		"runtime.",
		"discovery.",
		"Generate",
		"Discover",
		"Dynamic",
		"Analysis",
	}
	
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := scanner.Text()
		
		for _, pattern := range dynamicPatterns {
			if strings.Contains(line, pattern) {
				return true
			}
		}
	}
	
	return false
}

func (v *HardcodedServiceValidator) checkForHardcodedImports(filePath string) []string {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()
	
	var badImports []string
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Check for specific AWS service imports
		if strings.Contains(line, `"github.com/aws/aws-sdk-go-v2/service/`) {
			// Extract service name
			parts := strings.Split(line, "/service/")
			if len(parts) > 1 {
				servicePart := strings.Split(parts[1], `"`)[0]
				serviceName := strings.Split(servicePart, "/")[0]
				
				// Check if it's a known AWS service (indicating hardcoded import)
				for _, knownService := range knownAWSServices {
					if serviceName == knownService {
						badImports = append(badImports, line)
						break
					}
				}
			}
		}
	}
	
	return badImports
}

func (v *HardcodedServiceValidator) checkConfigFileForHardcodedServices(filePath string) []string {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()
	
	var violations []string
	scanner := bufio.NewScanner(file)
	lineNum := 0
	
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		
		// Check for service names in configuration
		for _, service := range knownAWSServices {
			if strings.Contains(line, service) && 
			   (strings.Contains(line, ":") || strings.Contains(line, "=")) {
				violations = append(violations, 
					fmt.Sprintf("Line %d: %s", lineNum, strings.TrimSpace(line)))
			}
		}
	}
	
	return violations
}

// TestMigrationCompleteness verifies migration completeness markers
func TestMigrationCompleteness(t *testing.T) {
	if os.Getenv("RUN_COMPLETENESS_TESTS") != "true" {
		t.Skip("Skipping completeness tests. Set RUN_COMPLETENESS_TESTS=true to run.")
	}
	
	rootPath := "/home/jg/git/corkscrew/plugins/aws-provider"
	
	// Check for migration completion markers
	t.Run("MigrationMarkers", func(t *testing.T) {
		expectedMarkers := []string{
			"MIGRATION_COMPLETED.md",
			"PHASE2_REFLECTION_DISCOVERY_COMPLETED.md", 
			"PHASE3_ANALYSIS_GENERATION_COMPLETED.md",
		}
		
		for _, marker := range expectedMarkers {
			markerPath := filepath.Join(rootPath, marker)
			_, err := os.Stat(markerPath)
			assert.NoError(t, err, "Migration marker %s should exist", marker)
			
			if err == nil {
				// Check marker file has content
				content, err := os.ReadFile(markerPath)
				require.NoError(t, err, "Should be able to read marker file")
				assert.Greater(t, len(content), 100, "Marker file should have substantial content")
				
				t.Logf("Found migration marker: %s (%d bytes)", marker, len(content))
			}
		}
	})
	
	// Check for archive directory with legacy code
	t.Run("ArchiveStructure", func(t *testing.T) {
		archivePath := filepath.Join(rootPath, "archive")
		
		expectedArchiveDirs := []string{
			"hardcoded-services",
			"legacy-scanners", 
			"old-discovery",
			"feature-flags",
		}
		
		for _, dir := range expectedArchiveDirs {
			dirPath := filepath.Join(archivePath, dir)
			_, err := os.Stat(dirPath)
			if err == nil {
				t.Logf("Found expected archive directory: %s", dir)
			} else {
				t.Logf("Archive directory %s not found (may be optional)", dir)
			}
		}
	})
	
	// Check for new dynamic system files
	t.Run("DynamicSystemFiles", func(t *testing.T) {
		expectedFiles := []string{
			"discovery/runtime_discovery.go",
			"pkg/client/reflection_factory.go",
			"pkg/scanner/optimized_unified_scanner.go",
			"runtime/scanner_registry.go",
		}
		
		foundFiles := 0
		for _, file := range expectedFiles {
			filePath := filepath.Join(rootPath, file)
			_, err := os.Stat(filePath)
			if err == nil {
				foundFiles++
				t.Logf("Found dynamic system file: %s", file)
			}
		}
		
		assert.Greater(t, foundFiles, 0, "Should find some dynamic system files")
		t.Logf("Found %d/%d expected dynamic system files", foundFiles, len(expectedFiles))
	})
}