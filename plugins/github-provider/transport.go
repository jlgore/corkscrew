package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rateLimitTransport is an http.RoundTripper that handles GitHub's three rate
// limit modes plus transient 5xx/network errors:
//
//   - Secondary rate limit / abuse detection: 403 or 429 with Retry-After.
//     Sleep for the indicated duration, then retry.
//   - Primary rate limit: 403 with X-RateLimit-Remaining: 0. Sleep until
//     X-RateLimit-Reset (capped) and retry.
//   - 5xx and transient network errors: bounded exponential backoff with
//     jitter.
//
// It also emits a stderr warning when the primary budget gets low, so users
// see scans approaching the cliff before they fall off it.
//
// Body handling: only requests whose body is nil or whose GetBody is set are
// retryable. The github-provider only issues GETs, so this is a non-issue in
// practice.
type rateLimitTransport struct {
	base       http.RoundTripper
	maxRetries int
	maxWait    time.Duration

	// backoffFn lets tests substitute a zero-wait backoff so retry-path
	// tests don't spend real wall time. Nil means use the default
	// exponential-backoff-with-jitter.
	backoffFn func(attempt int) time.Duration

	// sleeper is the same hook for the rate-limit-driven waits (Retry-After,
	// X-RateLimit-Reset) where jitter or header values would otherwise force
	// real wall time in tests. Nil means use sleepCtx.
	sleeper func(ctx context.Context, d time.Duration) error

	rngMu sync.Mutex
	rng   *rand.Rand
}

func (t *rateLimitTransport) sleep(ctx context.Context, d time.Duration) error {
	if t.sleeper != nil {
		return t.sleeper(ctx, d)
	}
	return sleepCtx(ctx, d)
}

const (
	defaultMaxRetries          = 3
	defaultMaxRateLimitWait    = 15 * time.Minute
	rateLimitWarnRemaining     = 50
	rateLimitWarnRepeatEveryMs = 60_000
)

var (
	lastWarnMu sync.Mutex
	lastWarn   time.Time
)

func newRateLimitTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &rateLimitTransport{
		base:       base,
		maxRetries: defaultMaxRetries,
		maxWait:    defaultMaxRateLimitWait,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Non-retryable bodies (POST/PATCH without GetBody) bypass the loop.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return t.base.RoundTrip(req)
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("rewind request body for retry: %w", err)
			}
			req.Body = body
		}

		resp, err := t.base.RoundTrip(req)
		lastResp, lastErr = resp, err

		wait, retry := t.classify(resp, err, attempt)
		if !retry {
			if resp != nil {
				t.checkBudget(resp)
			}
			return resp, err
		}

		// Drain & close so the connection can be reused.
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err := t.sleep(req.Context(), wait); err != nil {
			return nil, err
		}
	}
	return lastResp, lastErr
}

// classify decides whether to retry and how long to wait. Returns (wait, true)
// to retry, (_, false) to stop.
func (t *rateLimitTransport) classify(resp *http.Response, err error, attempt int) (time.Duration, bool) {
	if attempt >= t.maxRetries {
		return 0, false
	}
	if err != nil {
		if isTransientErr(err) {
			return t.backoff(attempt), true
		}
		return 0, false
	}
	if resp == nil {
		return 0, false
	}

	// Secondary rate limit (abuse detection) returns 403 or 429 with Retry-After.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if wait := retryAfter(resp); wait > 0 {
			if wait > t.maxWait {
				wait = t.maxWait
			}
			return wait, true
		}
		// Primary rate limit: 403 with X-RateLimit-Remaining: 0.
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			if wait := untilReset(resp); wait > 0 && wait <= t.maxWait {
				return wait + jitter(t, time.Second), true
			}
		}
	}

	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return t.backoff(attempt), true
	}
	return 0, false
}

func (t *rateLimitTransport) backoff(attempt int) time.Duration {
	if t.backoffFn != nil {
		return t.backoffFn(attempt)
	}
	base := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	return base + jitter(t, time.Second)
}

// checkBudget warns when the primary rate-limit budget gets low. Throttled to
// at most once per minute so it doesn't spam during a noisy scan.
func (t *rateLimitTransport) checkBudget(resp *http.Response) {
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "" {
		return
	}
	n, err := strconv.Atoi(remaining)
	if err != nil || n > rateLimitWarnRemaining {
		return
	}

	lastWarnMu.Lock()
	defer lastWarnMu.Unlock()
	if time.Since(lastWarn).Milliseconds() < rateLimitWarnRepeatEveryMs {
		return
	}
	lastWarn = time.Now()

	resetMsg := ""
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil {
			resetMsg = fmt.Sprintf(" (resets in %s)", time.Until(time.Unix(epoch, 0)).Round(time.Second))
		}
	}
	fmt.Fprintf(os.Stderr, "[github-provider] rate limit low: %s remaining%s\n", remaining, resetMsg)
}

// retryAfter parses the Retry-After header (integer seconds or HTTP-date).
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func untilReset(resp *http.Response) time.Duration {
	v := resp.Header.Get("X-RateLimit-Reset")
	if v == "" {
		return 0
	}
	epoch, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return time.Until(time.Unix(epoch, 0))
}

// isTransientErr matches network/IO errors that are worth retrying. Errors
// from canceled contexts pass through unchanged so callers see ctx.Err().
func isTransientErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := err.Error()
	for _, marker := range []string{
		"connection reset",
		"connection refused",
		"EOF",
		"timeout",
		"temporary failure",
		"i/o timeout",
		"broken pipe",
		"no such host",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func jitter(t *rateLimitTransport, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	t.rngMu.Lock()
	defer t.rngMu.Unlock()
	return time.Duration(t.rng.Int63n(int64(max)))
}
