package tui

import (
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

// Completions are filtered before they are counted (duplicates and stale messages are
// ignored), so a count past the provider total, or a scan with no providers, is a bug.
func TestQuickScanProgressAssertsBounds(t *testing.T) {
	asserttest.FailsWith(t, "providers", func() { quickScanProgress(0, 0) })
	asserttest.FailsWith(t, "at most", func() { quickScanProgress(3, 2) })
	asserttest.FailsWith(t, "not negative", func() { quickScanProgress(-1, 2) })
	if got := quickScanProgress(1, 4); got != 0.25 {
		t.Fatalf("quickScanProgress(1, 4) = %v, want 0.25", got)
	}
}

// pushHistory drops the oldest entry once history is full, so it can only grow past the cap
// if something else appended to it.
func TestPushHistoryAssertsCap(t *testing.T) {
	router := NewViewRouter()
	router.history = make([]ViewType, maxRouterHistory+1)
	asserttest.FailsWith(t, "maxRouterHistory", func() { router.pushHistory(ViewScan) })
}

func TestRouterAssertsCurrentViewExists(t *testing.T) {
	router := NewViewRouter()
	router.currentView = ViewType(999)
	asserttest.FailsWith(t, "current view", func() { router.mustCurrentView() })
}
