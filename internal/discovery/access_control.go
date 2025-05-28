package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AccessController manages access control for hierarchical discovery
type AccessController struct {
	rateLimiter     *RateLimiter
	auditLogger     *AuditLogger
	permissionCache map[string]*PermissionCacheEntry
	cacheMutex      sync.RWMutex
	cacheTimeout    time.Duration
}

// PermissionCacheEntry caches permission validation results
type PermissionCacheEntry struct {
	Allowed   bool
	ExpiresAt time.Time
	Reason    string
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens     map[string]*TokenBucket
	mutex      sync.RWMutex
	maxTokens  int
	refillRate time.Duration
}

// TokenBucket represents a token bucket for rate limiting
type TokenBucket struct {
	tokens     int
	lastRefill time.Time
	maxTokens  int
}

// AuditLogger logs security-relevant events
type AuditLogger struct {
	enabled bool
	mutex   sync.Mutex
}

// AuditEvent represents a security audit event
type AuditEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	UserArn     string    `json:"user_arn"`
	Action      string    `json:"action"`
	ServiceName string    `json:"service_name"`
	Success     bool      `json:"success"`
	Reason      string    `json:"reason"`
	ClientIP    string    `json:"client_ip,omitempty"`
}

// NewAccessController creates a new access controller
func NewAccessController() *AccessController {
	return &AccessController{
		rateLimiter: &RateLimiter{
			tokens:     make(map[string]*TokenBucket),
			maxTokens:  100,         // 100 operations per window
			refillRate: time.Minute, // Refill every minute
		},
		auditLogger: &AuditLogger{
			enabled: true,
		},
		permissionCache: make(map[string]*PermissionCacheEntry),
		cacheTimeout:    5 * time.Minute, // Cache permissions for 5 minutes
	}
}

// ValidateAccess validates if the user has permission to perform hierarchical discovery
func (ac *AccessController) ValidateAccess(ctx context.Context, cfg aws.Config, serviceName string) error {
	// Get caller identity
	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		ac.auditLogger.LogEvent(AuditEvent{
			Timestamp:   time.Now(),
			Action:      "hierarchical_discovery_attempt",
			ServiceName: serviceName,
			Success:     false,
			Reason:      "failed to get caller identity: " + err.Error(),
		})
		return fmt.Errorf("failed to get caller identity: %w", err)
	}

	userArn := aws.ToString(identity.Arn)

	// Check rate limiting
	if !ac.rateLimiter.AllowRequest(userArn) {
		ac.auditLogger.LogEvent(AuditEvent{
			Timestamp:   time.Now(),
			UserArn:     userArn,
			Action:      "hierarchical_discovery_attempt",
			ServiceName: serviceName,
			Success:     false,
			Reason:      "rate limit exceeded",
		})
		return fmt.Errorf("rate limit exceeded for user %s", userArn)
	}

	// Check cached permissions
	cacheKey := fmt.Sprintf("%s:%s", userArn, serviceName)
	if cached := ac.getCachedPermission(cacheKey); cached != nil {
		if !cached.Allowed {
			ac.auditLogger.LogEvent(AuditEvent{
				Timestamp:   time.Now(),
				UserArn:     userArn,
				Action:      "hierarchical_discovery_attempt",
				ServiceName: serviceName,
				Success:     false,
				Reason:      "cached permission denied: " + cached.Reason,
			})
			return fmt.Errorf("access denied: %s", cached.Reason)
		}
		// Permission allowed and cached
		return nil
	}

	// Validate permissions for the service
	allowed, reason := ac.validateServicePermissions(ctx, cfg, userArn, serviceName)

	// Cache the result
	ac.cachePermission(cacheKey, allowed, reason)

	if !allowed {
		ac.auditLogger.LogEvent(AuditEvent{
			Timestamp:   time.Now(),
			UserArn:     userArn,
			Action:      "hierarchical_discovery_attempt",
			ServiceName: serviceName,
			Success:     false,
			Reason:      reason,
		})
		return fmt.Errorf("access denied: %s", reason)
	}

	// Log successful access
	ac.auditLogger.LogEvent(AuditEvent{
		Timestamp:   time.Now(),
		UserArn:     userArn,
		Action:      "hierarchical_discovery_granted",
		ServiceName: serviceName,
		Success:     true,
		Reason:      "permission validated",
	})

	return nil
}

// validateServicePermissions validates if the user has necessary permissions for a service
func (ac *AccessController) validateServicePermissions(ctx context.Context, cfg aws.Config, userArn, serviceName string) (bool, string) {
	// For now, implement basic validation based on user type
	// In production, this should integrate with AWS IAM policy evaluation

	// Check if it's a root user (not recommended for discovery)
	if isRootUser(userArn) {
		return false, "root user access not allowed"
	}

	// Check if it's a service-linked role (might have limited permissions)
	if isServiceLinkedRole(userArn) {
		return false, "service-linked roles not allowed"
	}

	// For IAM service, require specific permissions
	if serviceName == "iam" {
		if !hasIAMReadPermissions(userArn) {
			return false, "insufficient IAM read permissions"
		}
	}

	// Additional service-specific checks can be added here
	return true, "permissions validated"
}

// getCachedPermission retrieves cached permission result
func (ac *AccessController) getCachedPermission(cacheKey string) *PermissionCacheEntry {
	ac.cacheMutex.RLock()
	defer ac.cacheMutex.RUnlock()

	entry, exists := ac.permissionCache[cacheKey]
	if !exists || time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return entry
}

// cachePermission caches a permission validation result
func (ac *AccessController) cachePermission(cacheKey string, allowed bool, reason string) {
	ac.cacheMutex.Lock()
	defer ac.cacheMutex.Unlock()

	ac.permissionCache[cacheKey] = &PermissionCacheEntry{
		Allowed:   allowed,
		ExpiresAt: time.Now().Add(ac.cacheTimeout),
		Reason:    reason,
	}
}

// AllowRequest checks if a request is allowed under rate limiting
func (rl *RateLimiter) AllowRequest(userArn string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	bucket, exists := rl.tokens[userArn]
	if !exists {
		bucket = &TokenBucket{
			tokens:     rl.maxTokens,
			lastRefill: time.Now(),
			maxTokens:  rl.maxTokens,
		}
		rl.tokens[userArn] = bucket
	}

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	if elapsed >= rl.refillRate {
		tokensToAdd := int(elapsed / rl.refillRate)
		bucket.tokens = min(bucket.maxTokens, bucket.tokens+tokensToAdd)
		bucket.lastRefill = now
	}

	// Check if we have tokens available
	if bucket.tokens > 0 {
		bucket.tokens--
		return true
	}

	return false
}

// LogEvent logs an audit event
func (al *AuditLogger) LogEvent(event AuditEvent) {
	if !al.enabled {
		return
	}

	al.mutex.Lock()
	defer al.mutex.Unlock()

	// In production, this should write to a secure audit log
	// For now, we'll use structured logging
	fmt.Printf("[AUDIT] %s | User: %s | Action: %s | Service: %s | Success: %t | Reason: %s\n",
		event.Timestamp.Format(time.RFC3339),
		event.UserArn,
		event.Action,
		event.ServiceName,
		event.Success,
		event.Reason,
	)
}

// Helper functions for permission validation
func isRootUser(userArn string) bool {
	return userArn == "arn:aws:iam::root" ||
		(len(userArn) > 0 && userArn[len(userArn)-5:] == ":root")
}

func isServiceLinkedRole(userArn string) bool {
	return len(userArn) > 0 && strings.Contains(userArn, "/aws-service-role/")
}

func hasIAMReadPermissions(userArn string) bool {
	// In production, this should check actual IAM policies
	// For now, assume users with "ReadOnly" or "Admin" in their name have permissions
	return strings.Contains(userArn, "Admin") || strings.Contains(userArn, "ReadOnly")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
