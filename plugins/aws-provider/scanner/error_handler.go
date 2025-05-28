package scanner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
)

// ServiceError represents an error from an AWS service call
type ServiceError struct {
	Service   string
	Operation string
	Code      string
	Message   string
	Retryable bool
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("%s.%s: %s (%s)", e.Service, e.Operation, e.Message, e.Code)
}

// ErrorHandler handles and categorizes AWS service errors
type ErrorHandler struct {
	debug         bool
	errorSummary  map[string][]ServiceError
	circuitStates map[string]string
	mu            sync.RWMutex
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(debug bool) *ErrorHandler {
	return &ErrorHandler{
		debug:         debug,
		errorSummary:  make(map[string][]ServiceError),
		circuitStates: make(map[string]string),
	}
}

// HandleError processes an error and determines if it's retryable
func (eh *ErrorHandler) HandleError(ctx context.Context, service, operation string, err error) *ServiceError {
	if err == nil {
		return nil
	}

	serviceErr := &ServiceError{
		Service:   service,
		Operation: operation,
		Message:   err.Error(),
		Retryable: false,
	}

	// Categorize common AWS errors
	errMsg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errMsg, "access denied") || strings.Contains(errMsg, "forbidden"):
		serviceErr.Code = "AccessDenied"
		serviceErr.Retryable = false
	case strings.Contains(errMsg, "throttling") || strings.Contains(errMsg, "rate exceeded"):
		serviceErr.Code = "ThrottlingException"
		serviceErr.Retryable = true
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded"):
		serviceErr.Code = "TimeoutException"
		serviceErr.Retryable = true
	case strings.Contains(errMsg, "service unavailable") || strings.Contains(errMsg, "internal error"):
		serviceErr.Code = "ServiceUnavailable"
		serviceErr.Retryable = true
	case strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "bad request"):
		serviceErr.Code = "InvalidRequest"
		serviceErr.Retryable = false
	default:
		serviceErr.Code = "UnknownError"
		serviceErr.Retryable = false
	}

	if eh.debug {
		log.Printf("Error in %s.%s: %s (Code: %s, Retryable: %v)",
			service, operation, serviceErr.Message, serviceErr.Code, serviceErr.Retryable)
	}

	return serviceErr
}

// ShouldRetry determines if an operation should be retried based on the error
func (eh *ErrorHandler) ShouldRetry(serviceErr *ServiceError, attemptCount int) bool {
	if serviceErr == nil {
		return false
	}

	// Don't retry more than 3 times
	if attemptCount >= 3 {
		return false
	}

	return serviceErr.Retryable
}

// GetRetryDelay returns the delay before retrying an operation
func (eh *ErrorHandler) GetRetryDelay(attemptCount int) int {
	// Exponential backoff: 1s, 2s, 4s
	delay := 1 << attemptCount
	if delay > 8 {
		delay = 8
	}
	return delay
}

// ShouldProceed determines if an operation should proceed for a service (circuit breaker)
func (eh *ErrorHandler) ShouldProceed(serviceName string) bool {
	eh.mu.RLock()
	defer eh.mu.RUnlock()

	// Check circuit breaker state
	state, exists := eh.circuitStates[serviceName]
	if !exists {
		eh.circuitStates[serviceName] = "closed"
		return true
	}

	// If circuit is open, don't proceed
	return state != "open"
}

// GetErrorSummary returns a summary of errors
func (eh *ErrorHandler) GetErrorSummary() map[string][]ServiceError {
	return eh.errorSummary
}

// GetCircuitStates returns the current circuit states
func (eh *ErrorHandler) GetCircuitStates() map[string]string {
	return eh.circuitStates
}
