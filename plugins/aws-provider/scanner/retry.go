package scanner

import (
	"context"
	"time"
)

// RetryWithBackoff executes a function with exponential backoff retry logic
func RetryWithBackoff(ctx context.Context, fn func() error, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			// Exponential backoff: 1s, 2s, 4s, etc.
			delay := time.Duration(1<<attempt) * time.Second
			if delay > 8*time.Second {
				delay = 8 * time.Second
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue to next attempt
			}
		}
	}

	return lastErr
}
