package assert_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert"
	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

func TestThatPassesOnTrue(t *testing.T) {
	assert.That(true, "never shown")
	assert.Thatf(true, "never shown %d", 1)
}

func TestThatPanicsWithFailureOnFalse(t *testing.T) {
	defer func() {
		recovered := recover()
		failure, ok := recovered.(assert.Failure)
		if !ok {
			t.Fatalf("panic value = %#v, want assert.Failure", recovered)
		}
		if failure.Message != "scope count matches" {
			t.Fatalf("Message = %q", failure.Message)
		}
		if failure.Error() != "assertion failed: scope count matches" {
			t.Fatalf("Error() = %q", failure.Error())
		}
	}()
	assert.That(false, "scope count matches")
	t.Fatal("That returned on a false condition")
}

func TestThatfFormatsOnlyOnFailure(t *testing.T) {
	failure := asserttest.Fails(t, func() { assert.Thatf(false, "got %d, want %d", 3, 4) })
	if failure.Message != "got 3, want 4" {
		t.Fatalf("Message = %q", failure.Message)
	}
	var asError error = failure
	if !strings.HasPrefix(asError.Error(), assert.Prefix) {
		t.Fatalf("Error() = %q, want prefix %q", asError.Error(), assert.Prefix)
	}
	if !errors.As(asError, new(assert.Failure)) {
		t.Fatal("Failure does not satisfy errors.As")
	}
}

func TestFailsReportsMessageSubstring(t *testing.T) {
	asserttest.FailsWith(t, "page 3", func() { assert.Thatf(false, "page %d", 3) })
}
