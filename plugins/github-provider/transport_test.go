package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRT is a scripted RoundTripper for transport tests. It pulls responses
// or errors from parallel queues, in order, and tracks how many times each
// canned response's Body was closed so tests can catch leaks on retry.
type fakeRT struct {
	steps []rtStep
	calls int32
}

type rtStep struct {
	resp *http.Response
	err  error
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	i := int(atomic.AddInt32(&f.calls, 1)) - 1
	if i >= len(f.steps) {
		return nil, fmt.Errorf("fakeRT: unexpected call #%d", i+1)
	}
	step := f.steps[i]
	return step.resp, step.err
}

// closeCounter wraps an io.Reader to count Close() invocations. Used to
// verify the transport drains+closes response bodies between retries.
type closeCounter struct {
	io.Reader
	closes int32
}

func (c *closeCounter) Close() error {
	atomic.AddInt32(&c.closes, 1)
	return nil
}

func newResp(status int, headers map[string]string, body string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       &closeCounter{Reader: strings.NewReader(body)},
	}
}

func newTestTransport(rt http.RoundTripper) *rateLimitTransport {
	return &rateLimitTransport{
		base:       rt,
		maxRetries: 3,
		maxWait:    time.Hour,
		backoffFn:  func(int) time.Duration { return 0 }, // zero-wait backoffs
		sleeper:    func(ctx context.Context, d time.Duration) error { return ctx.Err() },
		rng:        rand.New(rand.NewSource(1)),
	}
}

// roundTrip is a tiny helper that builds a GET request the transport will
// happily retry (no body, GetBody is nil but that's fine for nil-body GET).
func roundTrip(t *testing.T, tr http.RoundTripper) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return tr.RoundTrip(req)
}

func TestTransport_PassthroughOn200(t *testing.T) {
	rt := &fakeRT{steps: []rtStep{
		{resp: newResp(200, nil, "ok")},
	}}
	tr := newTestTransport(rt)
	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1", rt.calls)
	}
}

func TestTransport_RetryOnSecondaryRateLimit403WithRetryAfter(t *testing.T) {
	first := newResp(403, map[string]string{"Retry-After": "30"}, "")
	body1 := first.Body.(*closeCounter)
	rt := &fakeRT{steps: []rtStep{
		{resp: first},
		{resp: newResp(200, nil, "after retry")},
	}}
	tr := newTestTransport(rt)

	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if rt.calls != 2 {
		t.Errorf("calls = %d, want 2 (retried once)", rt.calls)
	}
	if atomic.LoadInt32(&body1.closes) == 0 {
		t.Errorf("first response body was not closed before retry")
	}
}

func TestTransport_RetryOn429(t *testing.T) {
	first := newResp(429, map[string]string{"Retry-After": "60"}, "")
	rt := &fakeRT{steps: []rtStep{
		{resp: first},
		{resp: newResp(200, nil, "ok")},
	}}
	tr := newTestTransport(rt)

	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if rt.calls != 2 {
		t.Errorf("calls = %d, want 2", rt.calls)
	}
}

func TestTransport_RetryOnPrimaryRateLimit(t *testing.T) {
	// 403 with X-RateLimit-Remaining:0 and a near-future reset → retry.
	resetUnix := time.Now().Add(30 * time.Second).Unix()
	first := newResp(403, map[string]string{
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Reset":     fmt.Sprintf("%d", resetUnix),
	}, "")
	rt := &fakeRT{steps: []rtStep{
		{resp: first},
		{resp: newResp(200, nil, "ok")},
	}}
	tr := newTestTransport(rt)

	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if rt.calls != 2 {
		t.Errorf("calls = %d, want 2", rt.calls)
	}
}

func TestTransport_RetryOn5xxThenSucceed(t *testing.T) {
	rt := &fakeRT{steps: []rtStep{
		{resp: newResp(500, nil, "err1")},
		{resp: newResp(502, nil, "err2")},
		{resp: newResp(200, nil, "ok")},
	}}
	tr := newTestTransport(rt) // backoffFn returns 0
	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if rt.calls != 3 {
		t.Errorf("calls = %d, want 3", rt.calls)
	}
}

func TestTransport_NoRetryOnNonTransientError(t *testing.T) {
	wantErr := errors.New("syntactic disaster")
	rt := &fakeRT{steps: []rtStep{
		{err: wantErr},
	}}
	tr := newTestTransport(rt)
	_, err := roundTrip(t, tr)
	if !errors.Is(err, wantErr) {
		t.Errorf("got err %v, want %v", err, wantErr)
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", rt.calls)
	}
}

func TestTransport_RetryOnTransientNetworkError(t *testing.T) {
	rt := &fakeRT{steps: []rtStep{
		{err: errors.New("read: connection reset by peer")},
		{resp: newResp(200, nil, "ok")},
	}}
	tr := newTestTransport(rt)
	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || rt.calls != 2 {
		t.Errorf("status=%d calls=%d, want 200/2", resp.StatusCode, rt.calls)
	}
}

func TestTransport_GivesUpAfterMaxRetries(t *testing.T) {
	rt := &fakeRT{steps: []rtStep{
		{resp: newResp(500, nil, "")},
		{resp: newResp(500, nil, "")},
		{resp: newResp(500, nil, "")},
		{resp: newResp(500, nil, "")}, // attempt 4 (final)
	}}
	tr := newTestTransport(rt)
	resp, err := roundTrip(t, tr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	// maxRetries=3 means attempts 0..3 inclusive = 4 total
	if rt.calls != 4 {
		t.Errorf("calls = %d, want 4", rt.calls)
	}
}

func TestTransport_ContextCancelDuringSleep(t *testing.T) {
	rt := &fakeRT{steps: []rtStep{
		{resp: newResp(429, map[string]string{"Retry-After": "30"}, "")},
	}}
	tr := newTestTransport(rt)
	tr.sleeper = nil // use the real sleepCtx so cancellation can interrupt it
	tr.maxWait = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.test/", nil)
	_, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	t.Run("seconds", func(t *testing.T) {
		resp := newResp(429, map[string]string{"Retry-After": "5"}, "")
		if d := retryAfter(resp); d != 5*time.Second {
			t.Errorf("got %v, want 5s", d)
		}
	})
	t.Run("HTTP date", func(t *testing.T) {
		future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		resp := newResp(429, map[string]string{"Retry-After": future}, "")
		d := retryAfter(resp)
		if d <= 0 || d > 3*time.Second {
			t.Errorf("got %v, want ~2s", d)
		}
	})
	t.Run("missing", func(t *testing.T) {
		resp := newResp(429, nil, "")
		if d := retryAfter(resp); d != 0 {
			t.Errorf("got %v, want 0", d)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		resp := newResp(429, map[string]string{"Retry-After": "not-a-time"}, "")
		if d := retryAfter(resp); d != 0 {
			t.Errorf("got %v, want 0", d)
		}
	})
}

func TestUntilReset(t *testing.T) {
	t.Run("future", func(t *testing.T) {
		future := time.Now().Add(time.Minute).Unix()
		resp := newResp(403, map[string]string{"X-RateLimit-Reset": fmt.Sprintf("%d", future)}, "")
		d := untilReset(resp)
		if d <= 0 || d > time.Minute+time.Second {
			t.Errorf("got %v, want ~1m", d)
		}
	})
	t.Run("past", func(t *testing.T) {
		past := time.Now().Add(-time.Minute).Unix()
		resp := newResp(403, map[string]string{"X-RateLimit-Reset": fmt.Sprintf("%d", past)}, "")
		d := untilReset(resp)
		if d > 0 {
			t.Errorf("got %v, want <= 0", d)
		}
	})
	t.Run("missing", func(t *testing.T) {
		resp := newResp(403, nil, "")
		if d := untilReset(resp); d != 0 {
			t.Errorf("got %v, want 0", d)
		}
	})
}

func TestIsTransientErr(t *testing.T) {
	cases := map[string]bool{
		"connection reset by peer":  true,
		"i/o timeout":               true,
		"broken pipe":               true,
		"unexpected EOF":            true,
		"no such host":              true,
		"definitely fatal":          false,
		"":                          false,
	}
	for msg, want := range cases {
		if got := isTransientErr(errors.New(msg)); got != want {
			t.Errorf("isTransientErr(%q) = %v, want %v", msg, got, want)
		}
	}
	// nil and ctx errors do not retry
	if isTransientErr(nil) {
		t.Errorf("nil err must not be transient")
	}
	if isTransientErr(context.Canceled) {
		t.Errorf("context.Canceled must not be transient")
	}
	if isTransientErr(context.DeadlineExceeded) {
		t.Errorf("context.DeadlineExceeded must not be transient")
	}
}
