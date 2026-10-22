package findings

import (
	"context"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
)

// Every failing row is either the first sighting of a new fingerprint (opened) or a repeat
// (recurred), so the counters must add up to the rows reported. A drift here would make the
// evaluation summary disagree with the findings it lists.
func TestAssertEvaluationTotalsCatchesCounterDrift(t *testing.T) {
	evaluation := Evaluation{Opened: 1, Recurred: 1, Findings: make([]Finding, 3)}
	asserttest.FailsWith(t, "Opened + Recurred", func() { assertEvaluationTotals(evaluation) })

	evaluation.Recurred = 2
	assertEvaluationTotals(evaluation)
}

func TestRecordResultAssertsSuccessfulControl(t *testing.T) {
	result := compliance.SimpleQueryResult{Error: context.Canceled}
	asserttest.FailsWith(t, "successful control", func() {
		_, _ = recordResult(context.Background(), nil, &Evaluation{}, dataaccess.ScanRecord{},
			result)
	})
}

func TestPlaceholdersAssertsPositiveArguments(t *testing.T) {
	asserttest.FailsWith(t, "count", func() { placeholders(0, 1) })
	asserttest.FailsWith(t, "width", func() { placeholders(1, 0) })
	if got := placeholders(2, 2); got != "(?, ?), (?, ?)" {
		t.Fatalf("placeholders(2, 2) = %q", got)
	}
}
