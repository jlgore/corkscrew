# Advanced Testing Framework for Corkscrew

This document describes the advanced testing capabilities that extend the basic integration testing framework with edge cases, cross-region testing, and performance benchmarking.

## Overview

The advanced testing framework provides:

- **Edge Case Testing**: Unicode, maximum tags, long names, circular dependencies
- **Cross-Region Testing**: Multi-region deployments, global services, cross-region relationships
- **Performance Benchmarking**: Scalability testing, memory profiling, database optimization
- **Regression Analysis**: Performance trend tracking, baseline comparison, automated alerts

## 🔥 Edge Case Testing

### Features

Edge case testing validates Corkscrew's ability to handle boundary conditions and unusual resource configurations:

#### 1. Unicode and Special Characters
- **Unicode tags and descriptions**: Tests UTF-8 character handling including Chinese characters (测试), emojis (🔥🚀), and special symbols
- **Encoding validation**: Ensures proper UTF-8 encoding in database storage
- **Character set detection**: Identifies various Unicode character sets in use

#### 2. Tag Limits and Compliance
- **Maximum tags**: Tests AWS limit of 50 tags per resource
- **Tag value lengths**: Tests short, medium, and long tag values
- **Special character tags**: Validates handling of special characters in tag keys and values
- **Tag pattern analysis**: Tracks common tag naming patterns

#### 3. Long Resource Names
- **Name length testing**: Tests resources with very long names (up to 255 characters)
- **Truncation detection**: Identifies cases where names are truncated during processing
- **Special character names**: Tests names with Unicode and special characters

#### 4. Global vs Regional Services
- **Service classification**: Verifies correct identification of global (IAM, CloudFront) vs regional (EC2, S3) services
- **Multi-region appearance**: Tests how global resources appear across regions
- **Service distribution**: Analyzes resource distribution patterns

#### 5. Circular Dependencies
- **Dependency detection**: Tests resources with circular references (e.g., security groups referencing each other)
- **Graph analysis**: Validates dependency graph health
- **Chain identification**: Identifies circular dependency chains and their lengths

#### 6. Special Resource States
- **Stopped instances**: Tests EC2 instances in stopped state
- **Failed resources**: Tests handling of resources in error states
- **State transitions**: Validates state change detection

### Usage

```bash
# Run edge case testing
cd test/harness
go test -v -edge-cases -timeout=25m

# Run with specific test ID
go test -v -edge-cases -testid=my-edge-test

# Keep resources for debugging
go test -v -edge-cases -keepOnFail=true
```

### Edge Case Verification

The framework includes specialized verification for edge cases:

```go
// Create edge case verifier
edgeVerifier, err := verification.NewEdgeCaseVerifier(dbPath)
defer edgeVerifier.Close()

// Run comprehensive edge case verification
results, err := edgeVerifier.VerifyEdgeCases(ctx, testID)

// Check specific edge case results
fmt.Printf("Unicode tags found: %d\n", results.UnicodeSupport.UnicodeTagsFound)
fmt.Printf("Max tags per resource: %d\n", results.TagLimits.MaxTagsPerResource)
fmt.Printf("Circular dependencies: %d\n", results.CircularDependencies.CircularDependenciesFound)
```

## 🌍 Cross-Region Testing

### Features

Cross-region testing validates Corkscrew's ability to scan and relate resources across multiple AWS regions:

#### 1. Multi-Region Deployment
- **3-region deployment**: Deploys resources in us-east-1, us-west-2, and eu-west-1
- **Regional AMIs**: Uses different AMIs appropriate for each region
- **Regional configuration**: Tests region-specific settings and availability zones

#### 2. Cross-Region Relationships
- **VPC peering**: Creates VPC peering connections between regions
- **S3 replication**: Sets up cross-region S3 bucket replication
- **Global service integration**: Tests how CloudFront integrates with regional S3 buckets

#### 3. Global Services Testing
- **IAM roles**: Tests global IAM resources with cross-region policies
- **CloudFront distributions**: Tests global distributions with multi-region origins
- **Service discovery**: Validates proper detection of global vs regional resources

#### 4. Multi-Region Cleanup
- **Parallel cleanup**: Cleans up resources across regions concurrently
- **Dependency-aware**: Handles cleanup order for resources with dependencies
- **Global resource cleanup**: Manages IAM and CloudFront resource cleanup

### Usage

```bash
# Run cross-region testing
cd test/harness
go test -v -cross-region -timeout=35m

# Test specific regions
go test -v -cross-region -region=us-east-1

# Keep resources for analysis
go test -v -cross-region -keepOnFail=true
```

### Cross-Region Cleanup

```go
// Create multi-region cleanup manager
regions := []string{"us-east-1", "us-west-2", "eu-west-1"}
multiCleanup, err := cleanup.NewMultiRegionCleanup(testID, regions, 3)

// Execute parallel cleanup across regions
cleanupResult, err := multiCleanup.ExecuteCleanup(ctx)

// Check cleanup results
fmt.Printf("Successful regions: %d/%d\n", 
    cleanupResult.Summary.SuccessfulRegions, 
    cleanupResult.Summary.TotalRegions)
```

## 📊 Performance Benchmarking

### Features

Performance benchmarking provides comprehensive analysis of Corkscrew's scalability and performance characteristics:

#### 1. Scalability Testing
- **Resource count scaling**: Tests with 1, 10, 100, 1000 resources
- **Concurrency testing**: Tests different levels of concurrent scanning
- **Memory profiling**: Tracks memory usage patterns and peak consumption
- **Database size analysis**: Measures database growth and compression ratios

#### 2. Performance Metrics
- **Scan time measurement**: Precise timing of scan operations
- **Insert rate calculation**: Database insert performance (records/second)
- **Memory efficiency**: Memory usage per resource scanned
- **Compression analysis**: Raw data compression potential

#### 3. Baseline Comparison
- **Performance baselines**: Establishes performance baselines for regression detection
- **Threshold monitoring**: Configurable thresholds for performance degradation
- **Trend analysis**: Historical performance trend tracking

#### 4. Scalability Analysis
- **Linear scaling verification**: Tests if performance scales linearly with resource count
- **Optimal concurrency detection**: Identifies best concurrency level for performance
- **Resource efficiency**: Analyzes resource usage efficiency

### Usage

```bash
# Run performance benchmarks
cd test/harness
go test -v -performance -timeout=30m

# Test with specific resource count
go test -v -performance -perf-scale=100

# Run with custom configuration
go test -v -performance -perf-scale=50 -timeout=45m

# Run benchmarks only
go test -bench=. -benchmem -performance
```

### Performance Configuration

```go
config := performance.BenchmarkConfig{
    ResourceCounts:    []int{1, 10, 100, 1000},
    Scenarios:         []string{"performance-scaled"},
    Iterations:        5,
    WarmupIterations:  2,
    ConcurrencyLevels: []int{1, 2, 4, 8},
    BaselineFile:      "baseline.json",
    ResultsDir:        "results/performance",
}

benchmark := performance.NewPerformanceBenchmark(ctx, baselineFile, resultsDir)
report, err := benchmark.RunBenchmarkSuite(config)
```

## 📈 Regression Analysis

### Features

Regression analysis provides automated detection of performance regressions and trend tracking:

#### 1. Performance Regression Detection
- **Configurable thresholds**: Set acceptable performance degradation limits
- **Multi-metric analysis**: Analyzes scan time, memory usage, database size, and success rates
- **Severity classification**: Categorizes regressions as minor, major, or critical
- **Baseline comparison**: Compares current performance with established baselines

#### 2. Trend Analysis
- **Historical tracking**: Maintains performance history across test runs
- **Trend direction**: Identifies improving, degrading, or stable performance trends
- **Correlation analysis**: Statistical analysis of performance trends over time
- **Performance scoring**: Overall performance, scalability, and reliability scores

#### 3. Automated Reporting
- **HTML reports**: Rich visual reports with charts and trend analysis
- **JSON data**: Machine-readable reports for CI/CD integration
- **Recommendations**: Automated suggestions for performance improvements
- **Alert generation**: Configurable alerts for performance regressions

### Usage

```bash
# Run regression analysis
cd test/harness
go test -v -regression

# Generate regression report from existing data
go test -v -regression -baseline=baseline.json

# Update performance baselines
go test -v -regression -update-baseline
```

### Regression Configuration

```go
analyzer := performance.NewRegressionAnalyzer(
    "results/baseline",    // Baseline directory
    "results/history",     // Historical data directory
    "results/regression",  // Output directory
)

// Set custom thresholds
analyzer.SetThresholds(performance.RegressionThresholds{
    ScanTimeRatio:        1.3,  // 30% slower is regression
    MemoryUsageRatio:     1.5,  // 50% more memory is regression
    DatabaseSizeRatio:    2.0,  // 100% larger database is regression
    SuccessRateThreshold: 98.0, // Below 98% success is regression
})

report, err := analyzer.GenerateRegressionReport(benchmarkReport)
```

## 🧪 Advanced Test Suite

### Running All Advanced Tests

```bash
# Run complete advanced test suite
cd test/harness
go test -v -edge-cases -cross-region -performance -regression -timeout=60m

# Run specific components
go test -v -edge-cases -performance

# Run with specific scale
go test -v -performance -perf-scale=500 -regression
```

### Example Test Output

```
🔥 Starting edge case testing: edge-cases-1640995200
📦 Phase 1: Deploying test infrastructure...
✅ Deployment complete in 2m 15s
🔍 Phase 2: Running Corkscrew scan...
✅ Scan complete in 18s
✅ Phase 3: Verifying results in DuckDB...
✅ Verification complete in 3s

🔍 Running edge case verification
Unicode Support:
  Unicode tags found: 15
  Emoji tags found: 8
  Encoding issues: 0
Tag Limits:
  Resources with max tags: 3
  Max tags per resource: 50
  Tag limit compliance: true
Long Names:
  Longest resource name: 247 chars
  Resources with long names: 2
  Name truncation issues: 0
...

🌍 Starting cross-region testing: cross-region-1640995500
📦 Deploying resources in 3 regions...
✅ All regions deployed successfully
🔍 Scanning cross-region resources...
✅ Cross-region scan completed
🧹 Testing multi-region cleanup...
✅ Multi-region cleanup completed

📊 Starting performance benchmark: perf-1640995800
Benchmark Results:
  Total tests: 9
  Successful tests: 9
  Failed tests: 0
  Average scan time: 12.5s
  Peak memory usage: 256.00 MB
  Optimal concurrency: 4
Scalability Metrics:
  simple-s3-1_scaling: 0.95
  simple-s3-4_scaling: 1.08
  performance-scaled-1_scaling: 1.02

📈 Starting regression analysis
Regression Analysis Results:
  Overall status: no_regression
  Total scenarios: 3
  No regression: 3
  Performance score: 87.5
  Scalability score: 92.1
  Reliability score: 100.0
```

## Performance Baselines

### Establishing Baselines

Performance baselines should be established after multiple successful test runs:

```bash
# Run multiple benchmark iterations to establish baseline
for i in {1..5}; do
    go test -v -performance -perf-scale=10
done

# Analyze results and set baseline
go test -v -regression -update-baseline
```

### Baseline Metrics

Typical performance baselines for reference:

| Scenario | Resource Count | Scan Time | Memory Usage | Database Size |
|----------|----------------|-----------|--------------|---------------|
| simple-s3 | 1 | 5-8s | 64MB | 512KB |
| simple-s3 | 10 | 8-15s | 128MB | 2MB |
| complex | 5 | 15-25s | 256MB | 4MB |
| edge-cases | 8 | 20-35s | 512MB | 8MB |
| cross-region | 15 | 30-60s | 1GB | 12MB |

## Troubleshooting Advanced Tests

### Common Issues

#### Edge Case Testing
- **Unicode encoding errors**: Check database character set and Go string handling
- **Tag limit failures**: Verify AWS tag limits and resource type restrictions
- **Circular dependency detection**: Ensure relationship scanning captures all dependency types

#### Cross-Region Testing
- **Region capacity issues**: Some regions may have capacity limitations
- **VPC peering failures**: Cross-region peering requires manual acceptance
- **Global resource cleanup**: IAM and CloudFront resources may take time to delete

#### Performance Testing
- **Memory leaks**: Monitor for increasing memory usage across iterations
- **Database locking**: Ensure proper connection management in concurrent tests
- **Timeout issues**: Increase timeouts for large-scale tests

#### Regression Analysis
- **Missing baseline**: Establish baseline data before running regression analysis
- **False positives**: Adjust thresholds based on acceptable performance variance
- **Historical data corruption**: Validate historical data integrity

### Debug Commands

```bash
# Enable verbose logging
go test -v -edge-cases -debug

# Keep all resources for investigation
go test -v -cross-region -keepOnFail=true -debug

# Profile memory usage
go test -v -performance -cpuprofile=cpu.prof -memprofile=mem.prof

# Generate detailed reports
go test -v -regression -report-level=detailed
```

## Integration with CI/CD

### GitHub Actions Integration

The advanced tests can be integrated into CI/CD pipelines with appropriate triggers:

```yaml
# .github/workflows/advanced-tests.yml
name: Advanced Integration Tests

on:
  schedule:
    - cron: '0 2 * * *'  # Daily at 2 AM
  workflow_dispatch:
    inputs:
      test_type:
        description: 'Test type to run'
        required: true
        default: 'all'
        type: choice
        options:
        - edge-cases
        - cross-region
        - performance
        - regression
        - all

jobs:
  advanced-tests:
    runs-on: ubuntu-latest
    steps:
      - name: Run Advanced Tests
        run: |
          cd test/harness
          case "${{ github.event.inputs.test_type }}" in
            "edge-cases")
              go test -v -edge-cases -timeout=30m
              ;;
            "cross-region")
              go test -v -cross-region -timeout=45m
              ;;
            "performance")
              go test -v -performance -timeout=30m
              ;;
            "regression")
              go test -v -regression
              ;;
            "all")
              go test -v -edge-cases -cross-region -performance -regression -timeout=90m
              ;;
          esac
```

### Performance Monitoring

Set up automated performance monitoring:

```bash
# Weekly performance benchmarks
0 2 * * 1 cd /path/to/corkscrew/test/harness && go test -performance -perf-scale=100

# Daily regression checks
0 3 * * * cd /path/to/corkscrew/test/harness && go test -regression

# Alert on regressions
*/30 * * * * cd /path/to/corkscrew/test/harness && ./check-regressions.sh
```

## Best Practices

### Test Design
- **Isolation**: Ensure tests can run independently
- **Cleanup**: Always clean up resources, even on failure
- **Timeouts**: Set appropriate timeouts for complex scenarios
- **Randomization**: Use unique test IDs to prevent conflicts

### Performance Testing
- **Consistent environment**: Run performance tests in consistent environments
- **Multiple iterations**: Use multiple iterations to account for variance
- **Baseline maintenance**: Regularly update baselines as the system evolves
- **Trend monitoring**: Monitor long-term performance trends

### Regression Analysis
- **Threshold tuning**: Adjust regression thresholds based on system characteristics
- **Historical data**: Maintain sufficient historical data for trend analysis
- **Automated alerts**: Set up automated alerts for critical regressions
- **Root cause analysis**: Investigate regressions promptly to identify causes

This advanced testing framework ensures Corkscrew can handle real-world complexity and edge cases while maintaining optimal performance across different scales and configurations.