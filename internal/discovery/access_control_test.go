package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func TestAccessController_RateLimiting(t *testing.T) {
	ac := NewAccessController()
	userArn := "arn:aws:iam::123456789012:user/test-user"

	// Test initial tokens are available
	for i := 0; i < 10; i++ {
		if !ac.rateLimiter.AllowRequest(userArn) {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// After many requests, should be rate limited
	allowed := 0
	for i := 0; i < 200; i++ {
		if ac.rateLimiter.AllowRequest(userArn) {
			allowed++
		}
	}

	// Should have hit the rate limit
	if allowed >= 200 {
		t.Errorf("Expected rate limiting to kick in, but %d requests were allowed", allowed)
	}
}

func TestAccessController_RateLimitingPerUser(t *testing.T) {
	ac := NewAccessController()
	user1 := "arn:aws:iam::123456789012:user/user1"
	user2 := "arn:aws:iam::123456789012:user/user2"

	// Exhaust tokens for user1
	for i := 0; i < 150; i++ {
		ac.rateLimiter.AllowRequest(user1)
	}

	// user1 should be rate limited
	if ac.rateLimiter.AllowRequest(user1) {
		t.Error("user1 should be rate limited")
	}

	// user2 should still have tokens
	if !ac.rateLimiter.AllowRequest(user2) {
		t.Error("user2 should not be rate limited")
	}
}

func TestAccessController_PermissionCaching(t *testing.T) {
	ac := NewAccessController()
	cacheKey := "test-user:iam"

	// Initially no cached permission
	cached := ac.getCachedPermission(cacheKey)
	if cached != nil {
		t.Error("Should not have cached permission initially")
	}

	// Cache a permission
	ac.cachePermission(cacheKey, true, "test reason")

	// Should now be cached
	cached = ac.getCachedPermission(cacheKey)
	if cached == nil {
		t.Error("Permission should be cached")
	}
	if !cached.Allowed {
		t.Error("Cached permission should be allowed")
	}
	if cached.Reason != "test reason" {
		t.Errorf("Expected reason 'test reason', got '%s'", cached.Reason)
	}
}

func TestAccessController_PermissionCacheExpiry(t *testing.T) {
	ac := NewAccessController()
	ac.cacheTimeout = 100 * time.Millisecond // Short timeout for testing
	cacheKey := "test-user:iam"

	// Cache a permission
	ac.cachePermission(cacheKey, true, "test reason")

	// Should be cached immediately
	cached := ac.getCachedPermission(cacheKey)
	if cached == nil {
		t.Error("Permission should be cached")
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Should no longer be cached
	cached = ac.getCachedPermission(cacheKey)
	if cached != nil {
		t.Error("Permission should have expired")
	}
}

func TestAccessController_AuditLogging(t *testing.T) {
	ac := NewAccessController()

	// Test audit event creation and logging
	event := AuditEvent{
		Timestamp:   time.Now(),
		UserArn:     "arn:aws:iam::123456789012:user/test-user",
		Action:      "hierarchical_discovery_attempt",
		ServiceName: "iam",
		Success:     true,
		Reason:      "test audit",
	}

	// This should not panic
	ac.auditLogger.LogEvent(event)

	// Test with disabled logging
	ac.auditLogger.enabled = false
	ac.auditLogger.LogEvent(event) // Should be a no-op
}

func TestAccessController_ValidateServicePermissions(t *testing.T) {
	ac := NewAccessController()
	ctx := context.Background()
	cfg := aws.Config{} // Empty config for testing

	tests := []struct {
		name         string
		userArn      string
		serviceName  string
		expectAllow  bool
		expectReason string
	}{
		{
			name:         "root user denied",
			userArn:      "arn:aws:iam::123456789012:root",
			serviceName:  "iam",
			expectAllow:  false,
			expectReason: "root user access not allowed",
		},
		{
			name:         "service-linked role denied",
			userArn:      "arn:aws:iam::123456789012:role/aws-service-role/test-service-linked-role/TestRole",
			serviceName:  "iam",
			expectAllow:  false,
			expectReason: "service-linked roles not allowed",
		},
		{
			name:         "regular user without IAM permissions",
			userArn:      "arn:aws:iam::123456789012:user/regular-user",
			serviceName:  "iam",
			expectAllow:  false,
			expectReason: "insufficient IAM read permissions",
		},
		{
			name:         "admin user allowed",
			userArn:      "arn:aws:iam::123456789012:user/TestAdmin",
			serviceName:  "iam",
			expectAllow:  true,
			expectReason: "permissions validated",
		},
		{
			name:         "readonly user allowed",
			userArn:      "arn:aws:iam::123456789012:user/TestReadOnly",
			serviceName:  "iam",
			expectAllow:  true,
			expectReason: "permissions validated",
		},
		{
			name:         "non-IAM service allowed",
			userArn:      "arn:aws:iam::123456789012:user/regular-user",
			serviceName:  "s3",
			expectAllow:  true,
			expectReason: "permissions validated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, reason := ac.validateServicePermissions(ctx, cfg, tt.userArn, tt.serviceName)

			if allowed != tt.expectAllow {
				t.Errorf("Expected allowed=%v, got %v", tt.expectAllow, allowed)
			}

			if reason != tt.expectReason {
				t.Errorf("Expected reason='%s', got '%s'", tt.expectReason, reason)
			}
		})
	}
}

func TestAccessController_HelperFunctions(t *testing.T) {
	tests := []struct {
		name     string
		userArn  string
		function func(string) bool
		expected bool
	}{
		{
			name:     "root user detection",
			userArn:  "arn:aws:iam::123456789012:root",
			function: isRootUser,
			expected: true,
		},
		{
			name:     "regular user not root",
			userArn:  "arn:aws:iam::123456789012:user/test-user",
			function: isRootUser,
			expected: false,
		},
		{
			name:     "service-linked role detection",
			userArn:  "arn:aws:iam::123456789012:role/aws-service-role/test-service/TestRole",
			function: isServiceLinkedRole,
			expected: true,
		},
		{
			name:     "regular role not service-linked",
			userArn:  "arn:aws:iam::123456789012:role/TestRole",
			function: isServiceLinkedRole,
			expected: false,
		},
		{
			name:     "admin user has IAM permissions",
			userArn:  "arn:aws:iam::123456789012:user/TestAdmin",
			function: hasIAMReadPermissions,
			expected: true,
		},
		{
			name:     "readonly user has IAM permissions",
			userArn:  "arn:aws:iam::123456789012:user/TestReadOnly",
			function: hasIAMReadPermissions,
			expected: true,
		},
		{
			name:     "regular user lacks IAM permissions",
			userArn:  "arn:aws:iam::123456789012:user/regular-user",
			function: hasIAMReadPermissions,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.function(tt.userArn)
			if result != tt.expected {
				t.Errorf("Expected %v for ARN '%s', got %v", tt.expected, tt.userArn, result)
			}
		})
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	rl := &RateLimiter{
		tokens:     make(map[string]*TokenBucket),
		maxTokens:  10,
		refillRate: 100 * time.Millisecond, // Fast refill for testing
	}

	userArn := "arn:aws:iam::123456789012:user/test-user"

	// Exhaust all tokens
	for i := 0; i < 15; i++ {
		rl.AllowRequest(userArn)
	}

	// Should be rate limited
	if rl.AllowRequest(userArn) {
		t.Error("Should be rate limited after exhausting tokens")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Should have tokens again
	if !rl.AllowRequest(userArn) {
		t.Error("Should have tokens after refill period")
	}
}

func TestAccessController_Integration(t *testing.T) {
	// Skip this test if we don't have AWS credentials
	// This would be an integration test that requires real AWS access
	t.Skip("Integration test requires AWS credentials")

	ac := NewAccessController()
	ctx := context.Background()

	// Load real AWS config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	// Test access validation with real credentials
	err = ac.ValidateAccess(ctx, cfg, "iam")

	// The result depends on the actual AWS credentials and permissions
	// In a real test environment, we'd set up specific test credentials
	if err != nil {
		t.Logf("Access validation failed (expected in test environment): %v", err)
	} else {
		t.Log("Access validation succeeded")
	}
}
