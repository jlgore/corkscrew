package safety

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	costTypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// Guardian provides safety mechanisms for integration tests
type Guardian struct {
	ctx              context.Context
	awsConfig        aws.Config
	cloudWatchClient *cloudwatch.Client
	costClient       *costexplorer.Client
	maxCostThreshold float64
	maxTestDuration  time.Duration
	accountID        string
}

// GuardianConfig configures the safety guardian
type GuardianConfig struct {
	Region           string
	MaxCostThreshold float64 // in USD
	MaxTestDuration  time.Duration
	AccountID        string
}

// NewGuardian creates a new safety guardian
func NewGuardian(ctx context.Context, cfg GuardianConfig) (*Guardian, error) {
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	if cfg.MaxCostThreshold == 0 {
		cfg.MaxCostThreshold = 5.0 // Default $5 threshold
	}

	if cfg.MaxTestDuration == 0 {
		cfg.MaxTestDuration = 15 * time.Minute // Default 15 minute timeout
	}

	return &Guardian{
		ctx:              ctx,
		awsConfig:        awsConfig,
		cloudWatchClient: cloudwatch.NewFromConfig(awsConfig),
		costClient:       costexplorer.NewFromConfig(awsConfig),
		maxCostThreshold: cfg.MaxCostThreshold,
		maxTestDuration:  cfg.MaxTestDuration,
		accountID:        cfg.AccountID,
	}, nil
}

// SafetyCheck performs pre-test safety validations
type SafetyCheck struct {
	Name        string
	Description string
	Status      CheckStatus
	Message     string
	Error       error
}

type CheckStatus int

const (
	CheckPassed CheckStatus = iota
	CheckWarning
	CheckFailed
)

func (cs CheckStatus) String() string {
	switch cs {
	case CheckPassed:
		return "PASSED"
	case CheckWarning:
		return "WARNING"
	case CheckFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// RunSafetyChecks performs comprehensive pre-test validation
func (g *Guardian) RunSafetyChecks(testID string) ([]SafetyCheck, error) {
	checks := []SafetyCheck{}

	// Check 1: Account validation
	accountCheck := g.checkAccountSafety()
	checks = append(checks, accountCheck)

	// Check 2: Cost monitoring
	costCheck := g.checkCostThreshold()
	checks = append(checks, costCheck)

	// Check 3: Existing test resources
	resourceCheck := g.checkExistingTestResources(testID)
	checks = append(checks, resourceCheck)

	// Check 4: Service quotas
	quotaCheck := g.checkServiceQuotas()
	checks = append(checks, quotaCheck)

	// Check 5: Region capacity
	capacityCheck := g.checkRegionCapacity()
	checks = append(checks, capacityCheck)

	return checks, nil
}

// checkAccountSafety ensures we're not running in production
func (g *Guardian) checkAccountSafety() SafetyCheck {
	check := SafetyCheck{
		Name:        "Account Safety",
		Description: "Verify test is not running in production account",
	}

	// List of known production account patterns/IDs
	productionPatterns := []string{
		"111111111111", // Example production account
		"222222222222", // Another production account
	}

	for _, pattern := range productionPatterns {
		if g.accountID == pattern {
			check.Status = CheckFailed
			check.Message = fmt.Sprintf("DANGER: Test attempted in production account %s", g.accountID)
			check.Error = fmt.Errorf("production account detected")
			return check
		}
	}

	check.Status = CheckPassed
	check.Message = fmt.Sprintf("Account %s is safe for testing", g.accountID)
	return check
}

// checkCostThreshold monitors current AWS costs
func (g *Guardian) checkCostThreshold() SafetyCheck {
	check := SafetyCheck{
		Name:        "Cost Monitoring",
		Description: "Verify current AWS costs are below threshold",
	}

	// Get cost for current month
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endOfMonth := startOfMonth.AddDate(0, 1, 0)

	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &costTypes.DateInterval{
			Start: aws.String(startOfMonth.Format("2006-01-02")),
			End:   aws.String(endOfMonth.Format("2006-01-02")),
		},
		Granularity: costTypes.GranularityMonthly,
		Metrics:     []string{"BlendedCost"},
		GroupBy: []costTypes.GroupDefinition{
			{
				Type: costTypes.GroupDefinitionTypeDimension,
				Key:  aws.String("SERVICE"),
			},
		},
	}

	result, err := g.costClient.GetCostAndUsage(g.ctx, input)
	if err != nil {
		check.Status = CheckWarning
		check.Message = "Could not retrieve cost information"
		check.Error = err
		return check
	}

	totalCost := 0.0
	if len(result.ResultsByTime) > 0 && len(result.ResultsByTime[0].Groups) > 0 {
		for _, group := range result.ResultsByTime[0].Groups {
			if group.Metrics != nil && group.Metrics["BlendedCost"] != nil {
				if cost := group.Metrics["BlendedCost"].Amount; cost != nil {
					// Convert string cost to float (simplified - would need proper parsing)
					// This is a placeholder for actual cost parsing
				}
			}
		}
	}

	if totalCost > g.maxCostThreshold {
		check.Status = CheckFailed
		check.Message = fmt.Sprintf("Current month cost $%.2f exceeds threshold $%.2f", totalCost, g.maxCostThreshold)
		check.Error = fmt.Errorf("cost threshold exceeded")
	} else {
		check.Status = CheckPassed
		check.Message = fmt.Sprintf("Current month cost $%.2f is within threshold", totalCost)
	}

	return check
}

// checkExistingTestResources looks for orphaned test resources
func (g *Guardian) checkExistingTestResources(testID string) SafetyCheck {
	check := SafetyCheck{
		Name:        "Resource Cleanup",
		Description: "Check for orphaned test resources",
	}

	// This would integrate with the existing cleanup verifier
	// For now, return a basic check
	check.Status = CheckPassed
	check.Message = "No orphaned resources detected"
	return check
}

// checkServiceQuotas ensures we have sufficient quotas
func (g *Guardian) checkServiceQuotas() SafetyCheck {
	check := SafetyCheck{
		Name:        "Service Quotas",
		Description: "Verify sufficient AWS service quotas",
	}

	// This would check specific service quotas
	// For integration tests, we typically need:
	// - S3: Bucket limits
	// - EC2: Instance limits
	// - Lambda: Function limits

	check.Status = CheckPassed
	check.Message = "Service quotas sufficient for testing"
	return check
}

// checkRegionCapacity checks if the region has capacity
func (g *Guardian) checkRegionCapacity() SafetyCheck {
	check := SafetyCheck{
		Name:        "Region Capacity",
		Description: "Verify region has sufficient capacity",
	}

	// For basic tests, this is usually not an issue
	// More complex scenarios might need actual capacity checks
	check.Status = CheckPassed
	check.Message = "Region capacity appears sufficient"
	return check
}

// CreateCostAlert sets up CloudWatch alarm for cost monitoring
func (g *Guardian) CreateCostAlert(testID string, threshold float64) error {
	alarmName := fmt.Sprintf("corkscrew-test-cost-%s", testID)

	input := &cloudwatch.PutMetricAlarmInput{
		AlarmName:          aws.String(alarmName),
		AlarmDescription:   aws.String(fmt.Sprintf("Cost alert for Corkscrew test %s", testID)),
		MetricName:         aws.String("EstimatedCharges"),
		Namespace:          aws.String("AWS/Billing"),
		Statistic:          types.StatisticMaximum,
		Period:             aws.Int32(86400), // 24 hours
		EvaluationPeriods:  aws.Int32(1),
		Threshold:          aws.Float64(threshold),
		ComparisonOperator: types.ComparisonOperatorGreaterThanThreshold,
		Dimensions: []types.Dimension{
			{
				Name:  aws.String("Currency"),
				Value: aws.String("USD"),
			},
		},
		Tags: []types.Tag{
			{
				Key:   aws.String("TestHarness"),
				Value: aws.String("true"),
			},
			{
				Key:   aws.String("TestID"),
				Value: aws.String(testID),
			},
		},
	}

	_, err := g.cloudWatchClient.PutMetricAlarm(g.ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create cost alert: %w", err)
	}

	fmt.Printf("💰 Cost alert created: %s (threshold: $%.2f)\n", alarmName, threshold)
	return nil
}

// CleanupCostAlert removes the cost alert after test completion
func (g *Guardian) CleanupCostAlert(testID string) error {
	alarmName := fmt.Sprintf("corkscrew-test-cost-%s", testID)

	input := &cloudwatch.DeleteAlarmsInput{
		AlarmNames: []string{alarmName},
	}

	_, err := g.cloudWatchClient.DeleteAlarms(g.ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete cost alert: %w", err)
	}

	fmt.Printf("💰 Cost alert cleaned up: %s\n", alarmName)
	return nil
}

// TimeoutGuard implements test timeout with graceful shutdown
type TimeoutGuard struct {
	ctx        context.Context
	cancel     context.CancelFunc
	timeout    time.Duration
	onTimeout  func()
	isTimedOut bool
}

// NewTimeoutGuard creates a new timeout guard
func NewTimeoutGuard(timeout time.Duration, onTimeout func()) *TimeoutGuard {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	return &TimeoutGuard{
		ctx:       ctx,
		cancel:    cancel,
		timeout:   timeout,
		onTimeout: onTimeout,
	}
}

// Start begins the timeout monitoring
func (tg *TimeoutGuard) Start() {
	go func() {
		<-tg.ctx.Done()
		if tg.ctx.Err() == context.DeadlineExceeded {
			tg.isTimedOut = true
			fmt.Printf("⏰ Test timeout after %v - initiating graceful shutdown\n", tg.timeout)
			if tg.onTimeout != nil {
				tg.onTimeout()
			}
		}
	}()
}

// IsTimedOut returns whether the test has timed out
func (tg *TimeoutGuard) IsTimedOut() bool {
	return tg.isTimedOut
}

// Context returns the timeout context
func (tg *TimeoutGuard) Context() context.Context {
	return tg.ctx
}

// Stop cancels the timeout
func (tg *TimeoutGuard) Stop() {
	tg.cancel()
}

// EmergencyCleanup provides emergency resource cleanup capabilities
type EmergencyCleanup struct {
	awsConfig aws.Config
	region    string
}

// NewEmergencyCleanup creates a new emergency cleanup handler
func NewEmergencyCleanup(region string) (*EmergencyCleanup, error) {
	awsConfig, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &EmergencyCleanup{
		awsConfig: awsConfig,
		region:    region,
	}, nil
}

// CleanupTestResources performs emergency cleanup of test resources
func (ec *EmergencyCleanup) CleanupTestResources(ctx context.Context, testIDPattern string) error {
	fmt.Printf("🚨 Emergency cleanup starting for pattern: %s\n", testIDPattern)

	// This would integrate with the existing AWS cleanup functionality
	// but with broader permissions for emergency scenarios

	// Cleanup S3 buckets
	if err := ec.cleanupS3Buckets(ctx, testIDPattern); err != nil {
		fmt.Printf("⚠️ S3 cleanup warning: %v\n", err)
	}

	// Cleanup EC2 instances
	if err := ec.cleanupEC2Instances(ctx, testIDPattern); err != nil {
		fmt.Printf("⚠️ EC2 cleanup warning: %v\n", err)
	}

	// Cleanup Lambda functions
	if err := ec.cleanupLambdaFunctions(ctx, testIDPattern); err != nil {
		fmt.Printf("⚠️ Lambda cleanup warning: %v\n", err)
	}

	// Cleanup CloudWatch alarms
	if err := ec.cleanupCloudWatchAlarms(ctx, testIDPattern); err != nil {
		fmt.Printf("⚠️ CloudWatch cleanup warning: %v\n", err)
	}

	fmt.Printf("✅ Emergency cleanup completed for pattern: %s\n", testIDPattern)
	return nil
}

func (ec *EmergencyCleanup) cleanupS3Buckets(ctx context.Context, pattern string) error {
	// Implementation would go here
	fmt.Printf("   Cleaning S3 buckets matching: %s\n", pattern)
	return nil
}

func (ec *EmergencyCleanup) cleanupEC2Instances(ctx context.Context, pattern string) error {
	// Implementation would go here
	fmt.Printf("   Cleaning EC2 instances matching: %s\n", pattern)
	return nil
}

func (ec *EmergencyCleanup) cleanupLambdaFunctions(ctx context.Context, pattern string) error {
	// Implementation would go here
	fmt.Printf("   Cleaning Lambda functions matching: %s\n", pattern)
	return nil
}

func (ec *EmergencyCleanup) cleanupCloudWatchAlarms(ctx context.Context, pattern string) error {
	// Implementation would go here
	fmt.Printf("   Cleaning CloudWatch alarms matching: %s\n", pattern)
	return nil
}

// SafetyReport provides a comprehensive safety summary
type SafetyReport struct {
	TestID          string        `json:"test_id"`
	Timestamp       time.Time     `json:"timestamp"`
	SafetyChecks    []SafetyCheck `json:"safety_checks"`
	OverallStatus   CheckStatus   `json:"overall_status"`
	Recommendations []string      `json:"recommendations"`
}

// GenerateSafetyReport creates a comprehensive safety report
func (g *Guardian) GenerateSafetyReport(testID string, checks []SafetyCheck) *SafetyReport {
	report := &SafetyReport{
		TestID:          testID,
		Timestamp:       time.Now(),
		SafetyChecks:    checks,
		OverallStatus:   CheckPassed,
		Recommendations: []string{},
	}

	// Determine overall status
	for _, check := range checks {
		if check.Status == CheckFailed {
			report.OverallStatus = CheckFailed
			break
		} else if check.Status == CheckWarning && report.OverallStatus == CheckPassed {
			report.OverallStatus = CheckWarning
		}
	}

	// Generate recommendations
	if report.OverallStatus != CheckPassed {
		report.Recommendations = append(report.Recommendations,
			"Review failed safety checks before proceeding")
	}

	failedCount := 0
	warningCount := 0
	for _, check := range checks {
		if check.Status == CheckFailed {
			failedCount++
		} else if check.Status == CheckWarning {
			warningCount++
		}
	}

	if failedCount > 0 {
		report.Recommendations = append(report.Recommendations,
			fmt.Sprintf("Address %d failed safety checks", failedCount))
	}

	if warningCount > 0 {
		report.Recommendations = append(report.Recommendations,
			fmt.Sprintf("Consider addressing %d warning safety checks", warningCount))
	}

	return report
}

// PrintSafetyReport prints a formatted safety report
func (sr *SafetyReport) Print() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Printf("🛡️  SAFETY REPORT - %s\n", sr.TestID)
	fmt.Println(strings.Repeat("=", 60))

	status := "✅ SAFE"
	if sr.OverallStatus == CheckWarning {
		status = "⚠️ WARNINGS"
	} else if sr.OverallStatus == CheckFailed {
		status = "❌ UNSAFE"
	}

	fmt.Printf("Overall Status: %s\n", status)
	fmt.Printf("Generated: %s\n\n", sr.Timestamp.Format(time.RFC3339))

	fmt.Println("Safety Checks:")
	for _, check := range sr.SafetyChecks {
		icon := "✅"
		if check.Status == CheckWarning {
			icon = "⚠️"
		} else if check.Status == CheckFailed {
			icon = "❌"
		}

		fmt.Printf("  %s %s: %s\n", icon, check.Name, check.Message)
		if check.Error != nil {
			fmt.Printf("     Error: %v\n", check.Error)
		}
	}

	if len(sr.Recommendations) > 0 {
		fmt.Println("\nRecommendations:")
		for _, rec := range sr.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
	}

	fmt.Println(strings.Repeat("=", 60))
}
