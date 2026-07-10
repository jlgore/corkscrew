//go:build integration

package performance

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RegressionAnalyzer analyzes performance trends and detects regressions
type RegressionAnalyzer struct {
	baselineDir string
	historyDir  string
	outputDir   string
	thresholds  RegressionThresholds
}

// RegressionThresholds defines acceptable performance degradation limits
type RegressionThresholds struct {
	ScanTimeRatio        float64 `json:"scan_time_ratio"`        // 1.5 = 50% slower is regression
	MemoryUsageRatio     float64 `json:"memory_usage_ratio"`     // 2.0 = 100% more memory is regression
	DatabaseSizeRatio    float64 `json:"database_size_ratio"`    // 2.0 = 100% larger database is regression
	SuccessRateThreshold float64 `json:"success_rate_threshold"` // 95% = below 95% success is regression
}

// RegressionReport contains comprehensive regression analysis
type RegressionReport struct {
	GeneratedAt      time.Time                    `json:"generated_at"`
	TestID           string                       `json:"test_id"`
	BaselineVersion  string                       `json:"baseline_version"`
	CurrentVersion   string                       `json:"current_version"`
	Thresholds       RegressionThresholds         `json:"thresholds"`
	OverallStatus    RegressionStatus             `json:"overall_status"`
	ScenarioAnalysis []ScenarioRegressionAnalysis `json:"scenario_analysis"`
	TrendAnalysis    TrendAnalysis                `json:"trend_analysis"`
	Recommendations  []string                     `json:"recommendations"`
	Summary          RegressionSummary            `json:"summary"`
}

// RegressionStatus represents the overall regression status
type RegressionStatus string

const (
	StatusNoRegression    RegressionStatus = "no_regression"
	StatusMinorRegression RegressionStatus = "minor_regression"
	StatusMajorRegression RegressionStatus = "major_regression"
	StatusImprovement     RegressionStatus = "improvement"
	StatusInconclusive    RegressionStatus = "inconclusive"
)

// ScenarioRegressionAnalysis analyzes regression for a specific scenario
type ScenarioRegressionAnalysis struct {
	Scenario          string             `json:"scenario"`
	ResourceCount     int                `json:"resource_count"`
	Status            RegressionStatus   `json:"status"`
	Metrics           MetricComparison   `json:"metrics"`
	HistoricalTrend   HistoricalTrend    `json:"historical_trend"`
	RegressionDetails []RegressionDetail `json:"regression_details"`
}

// MetricComparison compares current metrics with baseline
type MetricComparison struct {
	ScanTime     MetricDelta `json:"scan_time"`
	MemoryUsage  MetricDelta `json:"memory_usage"`
	DatabaseSize MetricDelta `json:"database_size"`
	SuccessRate  MetricDelta `json:"success_rate"`
}

// MetricDelta represents change in a specific metric
type MetricDelta struct {
	Baseline      float64 `json:"baseline"`
	Current       float64 `json:"current"`
	Delta         float64 `json:"delta"`          // Absolute change
	PercentChange float64 `json:"percent_change"` // Percentage change
	IsRegression  bool    `json:"is_regression"`
	Severity      string  `json:"severity"` // "minor", "major", "critical"
}

// HistoricalTrend analyzes performance trends over time
type HistoricalTrend struct {
	DataPoints     []TrendDataPoint `json:"data_points"`
	TrendDirection string           `json:"trend_direction"` // "improving", "degrading", "stable"
	ChangeRate     float64          `json:"change_rate"`     // Rate of change per day
	Correlation    float64          `json:"correlation"`     // R-squared for trend line
}

// TrendDataPoint represents a single point in the performance timeline
type TrendDataPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	ScanTime     float64   `json:"scan_time"`
	MemoryUsage  float64   `json:"memory_usage"`
	DatabaseSize float64   `json:"database_size"`
	SuccessRate  float64   `json:"success_rate"`
}

// RegressionDetail provides specific details about detected regressions
type RegressionDetail struct {
	Metric      string `json:"metric"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Impact      string `json:"impact"`
	Suggestion  string `json:"suggestion"`
}

// TrendAnalysis provides overall trend analysis across all scenarios
type TrendAnalysis struct {
	OverallTrend     string            `json:"overall_trend"`
	ScenarioTrends   map[string]string `json:"scenario_trends"`
	PerformanceScore float64           `json:"performance_score"` // 0-100
	ScalabilityScore float64           `json:"scalability_score"` // 0-100
	ReliabilityScore float64           `json:"reliability_score"` // 0-100
}

// RegressionSummary provides high-level summary of regression analysis
type RegressionSummary struct {
	TotalScenarios       int      `json:"total_scenarios"`
	NoRegressionCount    int      `json:"no_regression_count"`
	MinorRegressionCount int      `json:"minor_regression_count"`
	MajorRegressionCount int      `json:"major_regression_count"`
	ImprovementCount     int      `json:"improvement_count"`
	CriticalIssues       []string `json:"critical_issues"`
	ActionRequired       bool     `json:"action_required"`
}

// NewRegressionAnalyzer creates a new regression analyzer
func NewRegressionAnalyzer(baselineDir, historyDir, outputDir string) *RegressionAnalyzer {
	return &RegressionAnalyzer{
		baselineDir: baselineDir,
		historyDir:  historyDir,
		outputDir:   outputDir,
		thresholds: RegressionThresholds{
			ScanTimeRatio:        1.5,  // 50% slower
			MemoryUsageRatio:     2.0,  // 100% more memory
			DatabaseSizeRatio:    2.0,  // 100% larger database
			SuccessRateThreshold: 95.0, // Below 95% success
		},
	}
}

// GenerateRegressionReport creates a comprehensive regression analysis report
func (ra *RegressionAnalyzer) GenerateRegressionReport(currentReport *BenchmarkReport) (*RegressionReport, error) {
	report := &RegressionReport{
		GeneratedAt:     time.Now(),
		TestID:          currentReport.TestID,
		CurrentVersion:  getCurrentVersion(),
		Thresholds:      ra.thresholds,
		Recommendations: []string{},
	}

	// Load baseline data
	baseline, err := ra.loadBaseline()
	if err != nil {
		return nil, fmt.Errorf("failed to load baseline: %w", err)
	}
	report.BaselineVersion = baseline.version

	// Load historical data for trend analysis
	historicalData, err := ra.loadHistoricalData()
	if err != nil {
		fmt.Printf("Warning: Could not load historical data: %v\n", err)
		historicalData = []BenchmarkReport{}
	}

	// Analyze each scenario
	for _, result := range currentReport.Results {
		analysis := ra.analyzeScenarioRegression(result, baseline.data, historicalData)
		report.ScenarioAnalysis = append(report.ScenarioAnalysis, analysis)
	}

	// Perform trend analysis
	report.TrendAnalysis = ra.performTrendAnalysis(historicalData, currentReport)

	// Determine overall status
	report.OverallStatus = ra.determineOverallStatus(report.ScenarioAnalysis)

	// Generate summary
	report.Summary = ra.generateRegressionSummary(report.ScenarioAnalysis)

	// Generate recommendations
	report.Recommendations = ra.generateRecommendations(report)

	// Save the report
	if err := ra.saveRegressionReport(report); err != nil {
		return nil, fmt.Errorf("failed to save regression report: %w", err)
	}

	// Generate HTML report
	if err := ra.generateHTMLRegressionReport(report); err != nil {
		fmt.Printf("Warning: Failed to generate HTML report: %v\n", err)
	}

	// Update historical data
	if err := ra.updateHistoricalData(currentReport); err != nil {
		fmt.Printf("Warning: Failed to update historical data: %v\n", err)
	}

	return report, nil
}

// analyzeScenarioRegression analyzes regression for a specific scenario
func (ra *RegressionAnalyzer) analyzeScenarioRegression(current BenchmarkResult, baseline map[string]BenchmarkResult, history []BenchmarkReport) ScenarioRegressionAnalysis {
	analysis := ScenarioRegressionAnalysis{
		Scenario:          current.Scenario,
		ResourceCount:     current.ResourceCount,
		RegressionDetails: []RegressionDetail{},
	}

	// Find baseline for this scenario
	baselineKey := fmt.Sprintf("%s-%d-%d", current.Scenario, current.ResourceCount, current.ConcurrencyLevel)
	baselineResult, hasBaseline := baseline[baselineKey]

	if !hasBaseline {
		analysis.Status = StatusInconclusive
		analysis.RegressionDetails = append(analysis.RegressionDetails, RegressionDetail{
			Metric:      "baseline",
			Severity:    "info",
			Description: "No baseline data available for comparison",
			Impact:      "Cannot detect regressions",
			Suggestion:  "Establish baseline by running multiple successful tests",
		})
		return analysis
	}

	// Compare metrics
	analysis.Metrics = ra.compareMetrics(current, baselineResult)

	// Analyze historical trend
	analysis.HistoricalTrend = ra.analyzeHistoricalTrend(current.Scenario, current.ResourceCount, history)

	// Detect regressions
	regressions := ra.detectRegressions(analysis.Metrics)
	analysis.RegressionDetails = append(analysis.RegressionDetails, regressions...)

	// Determine scenario status
	analysis.Status = ra.determineScenarioStatus(analysis.Metrics, regressions)

	return analysis
}

// compareMetrics compares current metrics with baseline
func (ra *RegressionAnalyzer) compareMetrics(current, baseline BenchmarkResult) MetricComparison {
	comparison := MetricComparison{}

	// Scan time comparison
	comparison.ScanTime = ra.calculateMetricDelta(
		float64(baseline.ScanTime.Nanoseconds()),
		float64(current.ScanTime.Nanoseconds()),
		ra.thresholds.ScanTimeRatio,
		"scan_time",
	)

	// Memory usage comparison
	comparison.MemoryUsage = ra.calculateMetricDelta(
		float64(baseline.MemoryUsage.PeakMemory),
		float64(current.MemoryUsage.PeakMemory),
		ra.thresholds.MemoryUsageRatio,
		"memory_usage",
	)

	// Database size comparison
	comparison.DatabaseSize = ra.calculateMetricDelta(
		float64(baseline.DatabaseSize),
		float64(current.DatabaseSize),
		ra.thresholds.DatabaseSizeRatio,
		"database_size",
	)

	// Success rate comparison (reverse logic - lower is worse)
	comparison.SuccessRate = ra.calculateMetricDelta(
		baseline.ScanSuccessRate,
		current.ScanSuccessRate,
		1.0/ra.thresholds.SuccessRateThreshold*100, // Convert to ratio
		"success_rate",
	)
	// For success rate, regression is when current is lower than baseline
	comparison.SuccessRate.IsRegression = current.ScanSuccessRate < ra.thresholds.SuccessRateThreshold

	return comparison
}

// calculateMetricDelta calculates the delta and determines if it's a regression
func (ra *RegressionAnalyzer) calculateMetricDelta(baseline, current, threshold float64, metricType string) MetricDelta {
	delta := MetricDelta{
		Baseline: baseline,
		Current:  current,
		Delta:    current - baseline,
	}

	if baseline > 0 {
		delta.PercentChange = (current - baseline) / baseline * 100
	}

	// Determine if this is a regression
	ratio := current / baseline
	delta.IsRegression = ratio > threshold

	// Determine severity
	if delta.IsRegression {
		if ratio > threshold*2 {
			delta.Severity = "critical"
		} else if ratio > threshold*1.5 {
			delta.Severity = "major"
		} else {
			delta.Severity = "minor"
		}
	} else if ratio < 0.8 { // Significant improvement
		delta.Severity = "improvement"
	} else {
		delta.Severity = "normal"
	}

	return delta
}

// detectRegressions identifies specific regression issues
func (ra *RegressionAnalyzer) detectRegressions(metrics MetricComparison) []RegressionDetail {
	var regressions []RegressionDetail

	// Check scan time regression
	if metrics.ScanTime.IsRegression {
		regressions = append(regressions, RegressionDetail{
			Metric:   "scan_time",
			Severity: metrics.ScanTime.Severity,
			Description: fmt.Sprintf("Scan time increased by %.1f%% (%v to %v)",
				metrics.ScanTime.PercentChange,
				time.Duration(metrics.ScanTime.Baseline),
				time.Duration(metrics.ScanTime.Current)),
			Impact:     "Longer scan times affect user experience and CI/CD pipeline duration",
			Suggestion: "Profile scan operations, check for inefficient queries, review recent code changes",
		})
	}

	// Check memory usage regression
	if metrics.MemoryUsage.IsRegression {
		regressions = append(regressions, RegressionDetail{
			Metric:   "memory_usage",
			Severity: metrics.MemoryUsage.Severity,
			Description: fmt.Sprintf("Memory usage increased by %.1f%% (%.1f MB to %.1f MB)",
				metrics.MemoryUsage.PercentChange,
				metrics.MemoryUsage.Baseline/1024/1024,
				metrics.MemoryUsage.Current/1024/1024),
			Impact:     "Higher memory usage may cause OOM errors and affect system stability",
			Suggestion: "Check for memory leaks, review data structures, consider streaming/pagination",
		})
	}

	// Check database size regression
	if metrics.DatabaseSize.IsRegression {
		regressions = append(regressions, RegressionDetail{
			Metric:   "database_size",
			Severity: metrics.DatabaseSize.Severity,
			Description: fmt.Sprintf("Database size increased by %.1f%% (%.1f KB to %.1f KB)",
				metrics.DatabaseSize.PercentChange,
				metrics.DatabaseSize.Baseline/1024,
				metrics.DatabaseSize.Current/1024),
			Impact:     "Larger database files affect storage costs and scan startup time",
			Suggestion: "Review raw_data compression, check for duplicate data, optimize schema",
		})
	}

	// Check success rate regression
	if metrics.SuccessRate.IsRegression {
		regressions = append(regressions, RegressionDetail{
			Metric:   "success_rate",
			Severity: "critical", // Always critical
			Description: fmt.Sprintf("Success rate dropped from %.1f%% to %.1f%%",
				metrics.SuccessRate.Baseline, metrics.SuccessRate.Current),
			Impact:     "Lower success rates indicate reliability issues that affect data completeness",
			Suggestion: "Review error logs, check API rate limits, validate credentials and permissions",
		})
	}

	return regressions
}

// analyzeHistoricalTrend analyzes performance trends over time
func (ra *RegressionAnalyzer) analyzeHistoricalTrend(scenario string, resourceCount int, history []BenchmarkReport) HistoricalTrend {
	trend := HistoricalTrend{
		DataPoints: []TrendDataPoint{},
	}

	// Extract relevant data points from history
	for _, report := range history {
		for _, result := range report.Results {
			if result.Scenario == scenario && result.ResourceCount == resourceCount {
				dataPoint := TrendDataPoint{
					Timestamp:    result.Timestamp,
					ScanTime:     float64(result.ScanTime.Nanoseconds()),
					MemoryUsage:  float64(result.MemoryUsage.PeakMemory),
					DatabaseSize: float64(result.DatabaseSize),
					SuccessRate:  result.ScanSuccessRate,
				}
				trend.DataPoints = append(trend.DataPoints, dataPoint)
			}
		}
	}

	if len(trend.DataPoints) < 2 {
		trend.TrendDirection = "insufficient_data"
		return trend
	}

	// Sort by timestamp
	sort.Slice(trend.DataPoints, func(i, j int) bool {
		return trend.DataPoints[i].Timestamp.Before(trend.DataPoints[j].Timestamp)
	})

	// Calculate trend for scan time (primary metric)
	scanTimes := make([]float64, len(trend.DataPoints))
	timestamps := make([]float64, len(trend.DataPoints))

	baseTime := trend.DataPoints[0].Timestamp
	for i, point := range trend.DataPoints {
		scanTimes[i] = point.ScanTime
		timestamps[i] = point.Timestamp.Sub(baseTime).Hours() / 24 // Days since first measurement
	}

	// Calculate linear regression
	trend.ChangeRate, trend.Correlation = ra.calculateLinearRegression(timestamps, scanTimes)

	// Determine trend direction
	if math.Abs(trend.ChangeRate) < 0.01 { // Less than 1% change per day
		trend.TrendDirection = "stable"
	} else if trend.ChangeRate > 0 {
		trend.TrendDirection = "degrading"
	} else {
		trend.TrendDirection = "improving"
	}

	return trend
}

// calculateLinearRegression calculates slope and R-squared for trend analysis
func (ra *RegressionAnalyzer) calculateLinearRegression(x, y []float64) (slope, rSquared float64) {
	if len(x) != len(y) || len(x) < 2 {
		return 0, 0
	}

	n := float64(len(x))

	// Calculate means
	var sumX, sumY float64
	for i := 0; i < len(x); i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / n
	meanY := sumY / n

	// Calculate slope and correlation
	var sumXY, sumXX, sumYY float64
	for i := 0; i < len(x); i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		sumXY += dx * dy
		sumXX += dx * dx
		sumYY += dy * dy
	}

	if sumXX == 0 {
		return 0, 0
	}

	slope = sumXY / sumXX

	// Calculate R-squared
	if sumYY == 0 {
		rSquared = 1.0 // Perfect fit
	} else {
		rSquared = (sumXY * sumXY) / (sumXX * sumYY)
	}

	return slope, rSquared
}

// performTrendAnalysis performs overall trend analysis
func (ra *RegressionAnalyzer) performTrendAnalysis(history []BenchmarkReport, current *BenchmarkReport) TrendAnalysis {
	analysis := TrendAnalysis{
		ScenarioTrends: make(map[string]string),
	}

	// Analyze trends for each scenario
	scenarioTrends := make(map[string]string)
	improvingCount := 0
	degradingCount := 0
	stableCount := 0

	for _, result := range current.Results {
		trend := ra.analyzeHistoricalTrend(result.Scenario, result.ResourceCount, history)
		scenarioTrends[result.Scenario] = trend.TrendDirection

		switch trend.TrendDirection {
		case "improving":
			improvingCount++
		case "degrading":
			degradingCount++
		case "stable":
			stableCount++
		}
	}

	analysis.ScenarioTrends = scenarioTrends

	// Determine overall trend
	if degradingCount > improvingCount+stableCount {
		analysis.OverallTrend = "degrading"
	} else if improvingCount > degradingCount+stableCount {
		analysis.OverallTrend = "improving"
	} else {
		analysis.OverallTrend = "stable"
	}

	// Calculate performance scores (0-100)
	analysis.PerformanceScore = ra.calculatePerformanceScore(current)
	analysis.ScalabilityScore = ra.calculateScalabilityScore(current)
	analysis.ReliabilityScore = ra.calculateReliabilityScore(current)

	return analysis
}

// calculatePerformanceScore calculates overall performance score
func (ra *RegressionAnalyzer) calculatePerformanceScore(report *BenchmarkReport) float64 {
	if len(report.Results) == 0 {
		return 0
	}

	totalScore := 0.0
	for _, result := range report.Results {
		// Score based on scan time (faster is better)
		scanScore := 100.0
		if result.ScanTime > 60*time.Second {
			scanScore = 50.0
		} else if result.ScanTime > 30*time.Second {
			scanScore = 75.0
		}

		// Score based on memory efficiency
		memoryScore := 100.0
		memoryMB := result.MemoryUsage.PeakMemory / 1024 / 1024
		if memoryMB > 1000 {
			memoryScore = 50.0
		} else if memoryMB > 500 {
			memoryScore = 75.0
		}

		// Average the scores
		resultScore := (scanScore + memoryScore) / 2
		totalScore += resultScore
	}

	return totalScore / float64(len(report.Results))
}

// calculateScalabilityScore calculates scalability score
func (ra *RegressionAnalyzer) calculateScalabilityScore(report *BenchmarkReport) float64 {
	// Analyze how performance scales with resource count
	// This is a simplified implementation
	scalabilityFactors := report.Summary.ScalabilityMetrics

	score := 100.0
	for _, factor := range scalabilityFactors {
		// Good scalability: factor close to 1.0 (linear scaling)
		deviation := math.Abs(factor - 1.0)
		if deviation > 2.0 {
			score -= 30.0
		} else if deviation > 1.0 {
			score -= 15.0
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

// calculateReliabilityScore calculates reliability score based on success rates
func (ra *RegressionAnalyzer) calculateReliabilityScore(report *BenchmarkReport) float64 {
	if len(report.Results) == 0 {
		return 0
	}

	totalSuccessRate := 0.0
	for _, result := range report.Results {
		totalSuccessRate += result.ScanSuccessRate
	}

	return totalSuccessRate / float64(len(report.Results))
}

// determineOverallStatus determines the overall regression status
func (ra *RegressionAnalyzer) determineOverallStatus(scenarios []ScenarioRegressionAnalysis) RegressionStatus {
	majorCount := 0
	minorCount := 0
	improvementCount := 0
	totalCount := len(scenarios)

	for _, scenario := range scenarios {
		switch scenario.Status {
		case StatusMajorRegression:
			majorCount++
		case StatusMinorRegression:
			minorCount++
		case StatusImprovement:
			improvementCount++
		}
	}

	// Determine overall status
	if majorCount > 0 {
		return StatusMajorRegression
	} else if minorCount > totalCount/2 {
		return StatusMinorRegression
	} else if improvementCount > minorCount {
		return StatusImprovement
	} else if minorCount == 0 && majorCount == 0 {
		return StatusNoRegression
	} else {
		return StatusMinorRegression
	}
}

// determineScenarioStatus determines regression status for a scenario
func (ra *RegressionAnalyzer) determineScenarioStatus(metrics MetricComparison, regressions []RegressionDetail) RegressionStatus {
	criticalCount := 0
	majorCount := 0
	minorCount := 0
	improvementCount := 0

	for _, regression := range regressions {
		switch regression.Severity {
		case "critical":
			criticalCount++
		case "major":
			majorCount++
		case "minor":
			minorCount++
		case "improvement":
			improvementCount++
		}
	}

	if criticalCount > 0 {
		return StatusMajorRegression
	} else if majorCount > 0 {
		return StatusMajorRegression
	} else if minorCount > 0 {
		return StatusMinorRegression
	} else if improvementCount > 0 {
		return StatusImprovement
	} else {
		return StatusNoRegression
	}
}

// generateRegressionSummary generates a summary of regression analysis
func (ra *RegressionAnalyzer) generateRegressionSummary(scenarios []ScenarioRegressionAnalysis) RegressionSummary {
	summary := RegressionSummary{
		TotalScenarios: len(scenarios),
		CriticalIssues: []string{},
	}

	for _, scenario := range scenarios {
		switch scenario.Status {
		case StatusNoRegression:
			summary.NoRegressionCount++
		case StatusMinorRegression:
			summary.MinorRegressionCount++
		case StatusMajorRegression:
			summary.MajorRegressionCount++
		case StatusImprovement:
			summary.ImprovementCount++
		}

		// Collect critical issues
		for _, detail := range scenario.RegressionDetails {
			if detail.Severity == "critical" {
				summary.CriticalIssues = append(summary.CriticalIssues,
					fmt.Sprintf("%s: %s", scenario.Scenario, detail.Description))
			}
		}
	}

	summary.ActionRequired = summary.MajorRegressionCount > 0 || len(summary.CriticalIssues) > 0

	return summary
}

// generateRecommendations generates actionable recommendations
func (ra *RegressionAnalyzer) generateRecommendations(report *RegressionReport) []string {
	recommendations := []string{}

	// Overall recommendations based on status
	switch report.OverallStatus {
	case StatusMajorRegression:
		recommendations = append(recommendations,
			"🚨 CRITICAL: Major performance regressions detected. Immediate action required.",
			"Consider reverting recent changes until issues are resolved.",
			"Schedule performance optimization sprint to address regressions.")

	case StatusMinorRegression:
		recommendations = append(recommendations,
			"⚠️ Minor performance regressions detected. Monitor closely.",
			"Review recent code changes for performance impact.",
			"Consider performance optimization in next development cycle.")

	case StatusImprovement:
		recommendations = append(recommendations,
			"✅ Performance improvements detected. Great work!",
			"Document changes that led to improvements for future reference.",
			"Consider updating performance baselines to reflect improvements.")

	case StatusNoRegression:
		recommendations = append(recommendations,
			"✅ No significant regressions detected. Performance is stable.",
			"Continue current development practices.",
			"Consider expanding test coverage or resource scales.")
	}

	// Specific recommendations based on trends
	if report.TrendAnalysis.PerformanceScore < 70 {
		recommendations = append(recommendations,
			"Performance score is below threshold. Focus on optimization.",
			"Profile critical paths and optimize database queries.",
			"Consider caching strategies and parallel processing.")
	}

	if report.TrendAnalysis.ReliabilityScore < 95 {
		recommendations = append(recommendations,
			"Reliability score needs improvement. Investigate scan failures.",
			"Review error handling and retry mechanisms.",
			"Check API rate limits and authentication issues.")
	}

	// Add scenario-specific recommendations
	for _, scenario := range report.ScenarioAnalysis {
		if scenario.Status == StatusMajorRegression {
			recommendations = append(recommendations,
				fmt.Sprintf("Focus on %s scenario - major regressions detected", scenario.Scenario))
		}
	}

	return recommendations
}

// Baseline management and historical data functions

type baselineData struct {
	version string
	data    map[string]BenchmarkResult
}

func (ra *RegressionAnalyzer) loadBaseline() (*baselineData, error) {
	baselineFile := filepath.Join(ra.baselineDir, "baseline.json")
	data, err := os.ReadFile(baselineFile)
	if err != nil {
		return nil, err
	}

	var baseline baselineData
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, err
	}

	return &baseline, nil
}

func (ra *RegressionAnalyzer) loadHistoricalData() ([]BenchmarkReport, error) {
	var reports []BenchmarkReport

	entries, err := os.ReadDir(ra.historyDir)
	if err != nil {
		return reports, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			filePath := filepath.Join(ra.historyDir, entry.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			var report BenchmarkReport
			if err := json.Unmarshal(data, &report); err != nil {
				continue
			}

			reports = append(reports, report)
		}
	}

	// Sort by timestamp
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Timestamp.Before(reports[j].Timestamp)
	})

	return reports, nil
}

func (ra *RegressionAnalyzer) saveRegressionReport(report *RegressionReport) error {
	if err := os.MkdirAll(ra.outputDir, 0755); err != nil {
		return err
	}

	filename := fmt.Sprintf("regression_report_%s.json", report.TestID)
	filePath := filepath.Join(ra.outputDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func (ra *RegressionAnalyzer) updateHistoricalData(report *BenchmarkReport) error {
	if err := os.MkdirAll(ra.historyDir, 0755); err != nil {
		return err
	}

	filename := fmt.Sprintf("benchmark_%s.json", report.TestID)
	filePath := filepath.Join(ra.historyDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func getCurrentVersion() string {
	// This would typically get the version from git or build info
	return "current"
}

// generateHTMLRegressionReport creates an HTML visualization of the regression report
func (ra *RegressionAnalyzer) generateHTMLRegressionReport(report *RegressionReport) error {
	templateStr := `
<!DOCTYPE html>
<html>
<head>
    <title>Performance Regression Report</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #f5f5f5; padding: 20px; border-radius: 5px; }
        .status-no-regression { color: #28a745; }
        .status-minor-regression { color: #ffc107; }
        .status-major-regression { color: #dc3545; }
        .status-improvement { color: #17a2b8; }
        .metric { margin: 10px 0; padding: 10px; border-left: 4px solid #ccc; }
        .metric.regression { border-left-color: #dc3545; }
        .metric.improvement { border-left-color: #28a745; }
        .recommendations { background: #e7f3ff; padding: 15px; border-radius: 5px; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Performance Regression Report</h1>
        <p><strong>Test ID:</strong> {{.TestID}}</p>
        <p><strong>Generated:</strong> {{.GeneratedAt.Format "2006-01-02 15:04:05"}}</p>
        <p><strong>Status:</strong> <span class="status-{{.OverallStatus}}">{{.OverallStatus}}</span></p>
    </div>

    <h2>Summary</h2>
    <ul>
        <li>Total Scenarios: {{.Summary.TotalScenarios}}</li>
        <li>No Regression: {{.Summary.NoRegressionCount}}</li>
        <li>Minor Regression: {{.Summary.MinorRegressionCount}}</li>
        <li>Major Regression: {{.Summary.MajorRegressionCount}}</li>
        <li>Improvements: {{.Summary.ImprovementCount}}</li>
    </ul>

    {{if .Summary.CriticalIssues}}
    <h2>Critical Issues</h2>
    <ul>
        {{range .Summary.CriticalIssues}}
        <li style="color: #dc3545;">{{.}}</li>
        {{end}}
    </ul>
    {{end}}

    <h2>Scenario Analysis</h2>
    {{range .ScenarioAnalysis}}
    <div class="scenario">
        <h3>{{.Scenario}} ({{.ResourceCount}} resources)</h3>
        <p><strong>Status:</strong> <span class="status-{{.Status}}">{{.Status}}</span></p>
        
        {{range .RegressionDetails}}
        <div class="metric {{if .IsRegression}}regression{{else}}improvement{{end}}">
            <strong>{{.Metric}}:</strong> {{.Description}}<br>
            <em>{{.Suggestion}}</em>
        </div>
        {{end}}
    </div>
    {{end}}

    <h2>Recommendations</h2>
    <div class="recommendations">
        <ul>
            {{range .Recommendations}}
            <li>{{.}}</li>
            {{end}}
        </ul>
    </div>
</body>
</html>`

	tmpl, err := template.New("report").Parse(templateStr)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("regression_report_%s.html", report.TestID)
	filePath := filepath.Join(ra.outputDir, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, report)
}
