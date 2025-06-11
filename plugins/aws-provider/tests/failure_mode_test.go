//go:build failure
// +build failure

package tests

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFailureModes tests various failure scenarios and fail-fast behavior
func TestFailureModes(t *testing.T) {
	if os.Getenv("RUN_FAILURE_TESTS") != "true" {
		t.Skip("Skipping failure tests. Set RUN_FAILURE_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	suite := &FailureTestSuite{
		cfg: cfg,
		ctx: ctx,
	}

	t.Run("InvalidCredentials", suite.testInvalidCredentials)
	t.Run("ServiceUnavailable", suite.testServiceUnavailable)
	t.Run("NetworkTimeouts", suite.testNetworkTimeouts)
	t.Run("InvalidRegion", suite.testInvalidRegion)
	t.Run("AccessDenied", suite.testAccessDenied)
	t.Run("RateLimiting", suite.testRateLimiting)
	t.Run("MalformedResponse", suite.testMalformedResponse)
	t.Run("CircuitBreaker", suite.testCircuitBreaker)
	t.Run("ConcurrentFailures", suite.testConcurrentFailures)
	t.Run("PartialFailures", suite.testPartialFailures)
}

type FailureTestSuite struct {
	cfg aws.Config
	ctx context.Context
}

// testInvalidCredentials tests behavior with invalid AWS credentials
func (s *FailureTestSuite) testInvalidCredentials(t *testing.T) {
	// Create config with invalid credentials
	invalidCfg := s.cfg.Copy()
	invalidCfg.Credentials = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
		return aws.Credentials{}, errors.New("invalid credentials")
	})

	// Test runtime discovery failure
	t.Run("RuntimeDiscoveryFailure", func(t *testing.T) {
		discovery := discovery.NewRuntimeServiceDiscovery(invalidCfg)
		
		startTime := time.Now()
		services, err := discovery.DiscoverServices(s.ctx)
		duration := time.Since(startTime)
		
		// Should fail fast
		assert.Error(t, err, "Should fail with invalid credentials")
		assert.Nil(t, services, "Should not return services")
		assert.Less(t, duration, 10*time.Second, "Should fail fast (within 10s)")
		assert.Contains(t, err.Error(), "credentials", "Error should mention credentials")
		
		t.Logf("Failed fast in %v with error: %v", duration, err)
	})

	// Test pipeline failure
	t.Run("PipelineFailure", func(t *testing.T) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:   1,
			ScanTimeout:      5 * time.Second,
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(invalidCfg, pipelineConfig)
		require.NoError(t, err, "Pipeline creation should succeed")

		err = pipeline.Start()
		require.NoError(t, err, "Pipeline start should succeed")
		defer pipeline.Stop()

		startTime := time.Now()
		result, err := pipeline.ScanService(s.ctx, "s3", invalidCfg, "us-east-1")
		duration := time.Since(startTime)

		assert.Error(t, err, "Should fail with invalid credentials")
		assert.Nil(t, result, "Should not return result")
		assert.Less(t, duration, 10*time.Second, "Should fail fast")
		
		t.Logf("Pipeline failed fast in %v", duration)
	})
}

// testServiceUnavailable tests behavior when AWS services are unavailable
func (s *FailureTestSuite) testServiceUnavailable(t *testing.T) {
	// Test with nonexistent service
	t.Run("NonexistentService", func(t *testing.T) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:   1,
			ScanTimeout:      5 * time.Second,
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
		require.NoError(t, err, "Pipeline creation should succeed")

		err = pipeline.Start()
		require.NoError(t, err, "Pipeline start should succeed")
		defer pipeline.Stop()

		startTime := time.Now()
		result, err := pipeline.ScanService(s.ctx, "nonexistent-service", s.cfg, "us-east-1")
		duration := time.Since(startTime)

		assert.Error(t, err, "Should fail for nonexistent service")
		assert.Nil(t, result, "Should not return result")
		assert.Less(t, duration, 10*time.Second, "Should fail fast")
		
		t.Logf("Nonexistent service failed in %v", duration)
	})

	// Test with invalid region for service
	t.Run("ServiceNotInRegion", func(t *testing.T) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:   1,
			ScanTimeout:      5 * time.Second,
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
		require.NoError(t, err, "Pipeline creation should succeed")

		err = pipeline.Start()
		require.NoError(t, err, "Pipeline start should succeed")
		defer pipeline.Stop()

		// Some services might not be available in certain regions
		startTime := time.Now()
		result, err := pipeline.ScanService(s.ctx, "s3", s.cfg, "ap-south-2")
		duration := time.Since(startTime)

		// This might succeed or fail depending on service availability
		if err != nil {
			assert.Less(t, duration, 30*time.Second, "Should fail within reasonable time")
			t.Logf("Service unavailable in region, failed in %v", duration)
		} else {
			t.Logf("Service available in region, succeeded in %v", duration)
		}
	})
}

// testNetworkTimeouts tests behavior with network timeouts
func (s *FailureTestSuite) testNetworkTimeouts(t *testing.T) {
	// Test with very short timeout
	t.Run("ShortTimeout", func(t *testing.T) {
		shortCtx, cancel := context.WithTimeout(s.ctx, 1*time.Millisecond)
		defer cancel()

		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:   1,
			ScanTimeout:      1 * time.Millisecond,
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
		require.NoError(t, err, "Pipeline creation should succeed")

		err = pipeline.Start()
		require.NoError(t, err, "Pipeline start should succeed")
		defer pipeline.Stop()

		startTime := time.Now()
		result, err := pipeline.ScanService(shortCtx, "s3", s.cfg, "us-east-1")
		duration := time.Since(startTime)

		assert.Error(t, err, "Should timeout")
		assert.Nil(t, result, "Should not return result")
		assert.Less(t, duration, 5*time.Second, "Should timeout quickly")
		
		// Check if error is timeout-related
		if errors.Is(err, context.DeadlineExceeded) || 
		   errors.Is(err, context.Canceled) {
			t.Logf("Correctly timed out in %v", duration)
		} else {
			t.Logf("Failed for other reason in %v: %v", duration, err)
		}
	})

	// Test progressive timeout handling
	t.Run("ProgressiveTimeouts", func(t *testing.T) {
		timeouts := []time.Duration{
			100 * time.Millisecond,
			500 * time.Millisecond,
			1 * time.Second,
			5 * time.Second,
		}

		for _, timeout := range timeouts {
			t.Run(fmt.Sprintf("Timeout_%v", timeout), func(t *testing.T) {
				timeoutCtx, cancel := context.WithTimeout(s.ctx, timeout)
				defer cancel()

				pipelineConfig := &runtime.PipelineConfig{
					MaxConcurrency:   1,
					ScanTimeout:      timeout,
					EnableAutoDiscovery: false,
				}

				pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
				require.NoError(t, err, "Pipeline creation should succeed")

				err = pipeline.Start()
				require.NoError(t, err, "Pipeline start should succeed")
				defer pipeline.Stop()

				startTime := time.Now()
				result, err := pipeline.ScanService(timeoutCtx, "s3", s.cfg, "us-east-1")
				duration := time.Since(startTime)

				if err != nil {
					assert.LessOrEqual(t, duration, timeout+1*time.Second, 
						"Should timeout within expected time")
					t.Logf("Timeout %v: Failed in %v", timeout, duration)
				} else {
					t.Logf("Timeout %v: Succeeded in %v", timeout, duration)
				}
			})
		}
	})
}

// testInvalidRegion tests behavior with invalid AWS regions
func (s *FailureTestSuite) testInvalidRegion(t *testing.T) {
	invalidRegions := []string{
		"invalid-region",
		"us-invalid-1",
		"",
		"fake-region-1",
	}

	for _, region := range invalidRegions {
		t.Run(fmt.Sprintf("Region_%s", region), func(t *testing.T) {
			pipelineConfig := &runtime.PipelineConfig{
				MaxConcurrency:   1,
				ScanTimeout:      10 * time.Second,
				EnableAutoDiscovery: false,
			}

			pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
			require.NoError(t, err, "Pipeline creation should succeed")

			err = pipeline.Start()
			require.NoError(t, err, "Pipeline start should succeed")
			defer pipeline.Stop()

			startTime := time.Now()
			result, err := pipeline.ScanService(s.ctx, "s3", s.cfg, region)
			duration := time.Since(startTime)

			assert.Error(t, err, "Should fail with invalid region")
			assert.Nil(t, result, "Should not return result")
			assert.Less(t, duration, 30*time.Second, "Should fail within reasonable time")
			
			t.Logf("Invalid region %s failed in %v", region, duration)
		})
	}
}

// testAccessDenied tests behavior with access denied errors
func (s *FailureTestSuite) testAccessDenied(t *testing.T) {
	// Create config with minimal permissions (simulate restricted access)
	restrictedCfg := s.cfg.Copy()

	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   1,
		ScanTimeout:      10 * time.Second,
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(restrictedCfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Test access to service that might require special permissions
	t.Run("RestrictedService", func(t *testing.T) {
		restrictedServices := []string{
			"organizations",
			"cloudtrail",
			"config",
			"guardduty",
		}

		for _, service := range restrictedServices {
			t.Run(fmt.Sprintf("Service_%s", service), func(t *testing.T) {
				startTime := time.Now()
				result, err := pipeline.ScanService(s.ctx, service, restrictedCfg, "us-east-1")
				duration := time.Since(startTime)

				if err != nil {
					// Should fail fast on access denied
					assert.Less(t, duration, 30*time.Second, "Should fail fast on access denied")
					t.Logf("Service %s access denied in %v", service, duration)
				} else {
					t.Logf("Service %s accessible in %v", service, duration)
				}
			})
		}
	})
}

// testRateLimiting tests behavior under rate limiting
func (s *FailureTestSuite) testRateLimiting(t *testing.T) {
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   10, // High concurrency to trigger rate limits
		ScanTimeout:      30 * time.Second,
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Rapidly scan the same service multiple times
	t.Run("RapidRequests", func(t *testing.T) {
		numRequests := 20
		results := make(chan error, numRequests)

		startTime := time.Now()
		
		for i := 0; i < numRequests; i++ {
			go func(index int) {
				_, err := pipeline.ScanService(s.ctx, "s3", s.cfg, "us-east-1")
				results <- err
			}(i)
		}

		// Collect results
		var errors []error
		var successes int

		for i := 0; i < numRequests; i++ {
			err := <-results
			if err != nil {
				errors = append(errors, err)
			} else {
				successes++
			}
		}

		duration := time.Since(startTime)

		t.Logf("Rapid requests completed in %v: %d successes, %d errors", 
			duration, successes, len(errors))

		// At least some requests should succeed
		assert.Greater(t, successes, 0, "At least some requests should succeed")

		// Check for rate limiting errors
		rateLimitErrors := 0
		for _, err := range errors {
			if containsRateLimitError(err) {
				rateLimitErrors++
			}
		}

		if rateLimitErrors > 0 {
			t.Logf("Encountered %d rate limit errors (expected under high load)", rateLimitErrors)
		}
	})
}

// testMalformedResponse tests handling of malformed responses
func (s *FailureTestSuite) testMalformedResponse(t *testing.T) {
	// This test would typically use mocked clients that return malformed data
	// For now, we test the pipeline's robustness with edge cases

	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   1,
		ScanTimeout:      10 * time.Second,
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Test with services that might return edge case responses
	edgeCaseServices := []string{
		"s3",      // Usually reliable
		"lambda",  // Might have empty function lists
		"ec2",     // Might have no instances
	}

	for _, service := range edgeCaseServices {
		t.Run(fmt.Sprintf("EdgeCase_%s", service), func(t *testing.T) {
			startTime := time.Now()
			result, err := pipeline.ScanService(s.ctx, service, s.cfg, "us-east-1")
			duration := time.Since(startTime)

			// Should handle edge cases gracefully
			if err != nil {
				assert.Less(t, duration, 30*time.Second, "Should fail within reasonable time")
				t.Logf("Service %s edge case failed in %v: %v", service, duration, err)
			} else {
				assert.NotNil(t, result, "Should return valid result")
				t.Logf("Service %s edge case handled in %v", service, duration)
			}
		})
	}
}

// testCircuitBreaker tests circuit breaker behavior
func (s *FailureTestSuite) testCircuitBreaker(t *testing.T) {
	// Simulate circuit breaker by repeatedly calling a failing operation
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   1,
		ScanTimeout:      2 * time.Second, // Short timeout to trigger failures
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Try to scan an invalid service multiple times
	t.Run("RepeatedFailures", func(t *testing.T) {
		failures := 0
		maxAttempts := 10

		var firstFailureTime, lastFailureTime time.Time

		for i := 0; i < maxAttempts; i++ {
			startTime := time.Now()
			_, err := pipeline.ScanService(s.ctx, "invalid-service", s.cfg, "us-east-1")
			duration := time.Since(startTime)

			if err != nil {
				failures++
				if firstFailureTime.IsZero() {
					firstFailureTime = startTime
				}
				lastFailureTime = time.Now()

				t.Logf("Attempt %d failed in %v", i+1, duration)

				// After several failures, subsequent calls should fail faster (circuit breaker)
				if failures > 3 {
					assert.Less(t, duration, 5*time.Second, 
						"Circuit breaker should make subsequent failures faster")
				}
			}

			// Small delay between attempts
			time.Sleep(100 * time.Millisecond)
		}

		assert.Equal(t, maxAttempts, failures, "All attempts should fail")

		if !firstFailureTime.IsZero() && !lastFailureTime.IsZero() {
			t.Logf("Failure pattern: first at %v, last at %v", 
				firstFailureTime, lastFailureTime)
		}
	})
}

// testConcurrentFailures tests behavior under concurrent failure scenarios
func (s *FailureTestSuite) testConcurrentFailures(t *testing.T) {
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   5,
		ScanTimeout:      10 * time.Second,
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Mix of valid and invalid services
	services := []string{
		"s3",               // Should succeed
		"invalid-service1", // Should fail
		"lambda",           // Should succeed
		"invalid-service2", // Should fail
		"ec2",              // Should succeed
	}

	t.Run("MixedServices", func(t *testing.T) {
		results := make(chan ScanResult, len(services))
		
		startTime := time.Now()
		
		for _, service := range services {
			go func(svc string) {
				scanStart := time.Now()
				result, err := pipeline.ScanService(s.ctx, svc, s.cfg, "us-east-1")
				scanDuration := time.Since(scanStart)
				
				results <- ScanResult{
					Service:  svc,
					Success:  err == nil,
					Duration: scanDuration,
					Error:    err,
					Result:   result,
				}
			}(service)
		}

		// Collect results
		var successCount, failureCount int
		var totalDuration time.Duration

		for i := 0; i < len(services); i++ {
			result := <-results
			totalDuration += result.Duration

			if result.Success {
				successCount++
				t.Logf("Service %s: SUCCESS in %v", result.Service, result.Duration)
			} else {
				failureCount++
				t.Logf("Service %s: FAILED in %v - %v", result.Service, result.Duration, result.Error)
			}
		}

		overallDuration := time.Since(startTime)
		avgDuration := totalDuration / time.Duration(len(services))

		t.Logf("Concurrent scan results:")
		t.Logf("  Overall duration: %v", overallDuration)
		t.Logf("  Average per service: %v", avgDuration)
		t.Logf("  Successes: %d", successCount)
		t.Logf("  Failures: %d", failureCount)

		// Should have some successes and some failures
		assert.Greater(t, successCount, 0, "Should have some successful scans")
		assert.Greater(t, failureCount, 0, "Should have some failed scans")

		// Concurrent execution should be faster than sequential
		assert.Less(t, overallDuration, totalDuration, 
			"Concurrent execution should be faster than sequential")
	})
}

// testPartialFailures tests handling of partial failures in batch operations
func (s *FailureTestSuite) testPartialFailures(t *testing.T) {
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:   3,
		ScanTimeout:      15 * time.Second,
		EnableAutoDiscovery: false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Pipeline creation should succeed")

	err = pipeline.Start()
	require.NoError(t, err, "Pipeline start should succeed")
	defer pipeline.Stop()

	// Batch scan with mixed valid/invalid services
	t.Run("BatchPartialFailure", func(t *testing.T) {
		services := []string{
			"s3",
			"invalid-service",
			"lambda",
			"another-invalid-service",
			"ec2",
		}

		startTime := time.Now()
		batchResult, err := pipeline.ScanServices(s.ctx, services, s.cfg, "us-east-1")
		duration := time.Since(startTime)

		// Batch operation should handle partial failures gracefully
		if err != nil {
			t.Logf("Batch scan failed in %v: %v", duration, err)
			
			// Should fail fast for critical errors
			assert.Less(t, duration, 60*time.Second, "Should fail within reasonable time")
		} else {
			t.Logf("Batch scan completed in %v", duration)
			assert.NotNil(t, batchResult, "Should return batch result")
			
			// Should have results for valid services
			assert.Greater(t, batchResult.ServicesScanned, 0, "Should scan some services")
			
			t.Logf("Batch results: %d services scanned, %d resources found", 
				batchResult.ServicesScanned, batchResult.TotalResources)
		}
	})
}

// Helper types and functions

type ScanResult struct {
	Service  string
	Success  bool
	Duration time.Duration
	Error    error
	Result   interface{}
}

func containsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	
	errorStr := err.Error()
	rateLimitKeywords := []string{
		"throttled",
		"rate limit",
		"too many requests",
		"request rate exceeded",
		"slow down",
	}
	
	for _, keyword := range rateLimitKeywords {
		if len(errorStr) >= len(keyword) {
			for i := 0; i <= len(errorStr)-len(keyword); i++ {
				if errorStr[i:i+len(keyword)] == keyword {
					return true
				}
			}
		}
	}
	
	return false
}

// TestFailFastBehavior tests that the system fails fast under various conditions
func TestFailFastBehavior(t *testing.T) {
	if os.Getenv("RUN_FAIL_FAST_TESTS") != "true" {
		t.Skip("Skipping fail-fast tests. Set RUN_FAIL_FAST_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	failFastScenarios := []struct {
		name           string
		maxDuration    time.Duration
		setupFailure   func() aws.Config
		expectedError  string
	}{
		{
			name:        "InvalidCredentials",
			maxDuration: 5 * time.Second,
			setupFailure: func() aws.Config {
				badCfg := cfg.Copy()
				badCfg.Credentials = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
					return aws.Credentials{}, errors.New("invalid credentials")
				})
				return badCfg
			},
			expectedError: "credentials",
		},
		{
			name:        "NetworkUnavailable", 
			maxDuration: 10 * time.Second,
			setupFailure: func() aws.Config {
				badCfg := cfg.Copy()
				badCfg.BaseEndpoint = aws.String("https://invalid-endpoint.amazonaws.com")
				return badCfg
			},
			expectedError: "network",
		},
	}

	for _, scenario := range failFastScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			failureCfg := scenario.setupFailure()
			
			pipelineConfig := &runtime.PipelineConfig{
				MaxConcurrency:   1,
				ScanTimeout:      scenario.maxDuration,
				EnableAutoDiscovery: false,
			}

			pipeline, err := runtime.NewRuntimePipeline(failureCfg, pipelineConfig)
			require.NoError(t, err, "Pipeline creation should succeed")

			err = pipeline.Start()
			require.NoError(t, err, "Pipeline start should succeed")
			defer pipeline.Stop()

			startTime := time.Now()
			_, err = pipeline.ScanService(ctx, "s3", failureCfg, "us-east-1")
			duration := time.Since(startTime)

			assert.Error(t, err, "Should fail for scenario %s", scenario.name)
			assert.Less(t, duration, scenario.maxDuration, 
				"Should fail fast within %v for scenario %s", scenario.maxDuration, scenario.name)
			
			t.Logf("Scenario %s: Failed fast in %v with error: %v", 
				scenario.name, duration, err)
		})
	}
}