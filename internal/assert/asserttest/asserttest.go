// Package asserttest helps tests check that an invariant violation trips an assertion.
package asserttest

import (
	"strings"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert"
)

// Fails runs fn and returns the assert.Failure it panics with. The test fails if fn returns
// normally or panics with anything else, so an ordinary nil dereference cannot pass for a
// deliberate assertion.
func Fails(t testing.TB, fn func()) (failure assert.Failure) {
	t.Helper()
	panicked := true
	func() {
		defer func() {
			recovered := recover()
			if !panicked {
				return
			}
			value, ok := recovered.(assert.Failure)
			if !ok {
				t.Fatalf("panic value = %#v, want assert.Failure", recovered)
			}
			failure = value
		}()
		fn()
		panicked = false
	}()
	if !panicked {
		t.Fatal("expected an assertion failure, but the function returned")
	}
	return failure
}

// FailsWith is Fails that also requires the failure message to contain substring.
func FailsWith(t testing.TB, substring string, fn func()) assert.Failure {
	t.Helper()
	failure := Fails(t, fn)
	if !strings.Contains(failure.Message, substring) {
		t.Fatalf("assertion message = %q, want it to contain %q", failure.Message, substring)
	}
	return failure
}
