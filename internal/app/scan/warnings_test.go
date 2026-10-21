package scan

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jlgore/corkscrew/internal/shared"
)

func TestScopeWarningsSummarizeCoverageAndErrors(t *testing.T) {
	var denied []string
	for i := 0; i < 5; i++ {
		denied = append(denied, shared.AccessDeniedNotice(fmt.Sprintf("AWS::T::%d", i), errors.New("denied")))
	}
	scopes := []ScopeResult{
		{Scope: "us-east-1", Notices: append(denied, shared.UnsupportedTypeNotice("AWS::X::Y", "n/a"))},
		{Scope: "eu-west-1", Errors: []string{shared.ServiceScanError("ec2", errors.New("throttled"))}},
	}
	warnings := scopeWarnings(scopes)
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want one coverage and one incomplete", warnings)
	}
	coverage, incomplete := warnings[0], warnings[1]
	// Unsupported types are not something the user can fix, so they are not counted.
	if coverage.Kind != WarningCoverage || !strings.HasPrefix(coverage.Message, "5 item(s)") {
		t.Fatalf("coverage = %#v", coverage)
	}
	if !strings.Contains(coverage.Message, "and 2 more") {
		t.Fatalf("coverage = %q, want examples capped at %d", coverage.Message, warningExamplesMax)
	}
	if incomplete.Kind != WarningIncomplete || !strings.Contains(incomplete.Message, "eu-west-1: service:ec2") {
		t.Fatalf("incomplete = %#v", incomplete)
	}
}

func TestScopeWarningsSilentForCleanScan(t *testing.T) {
	scopes := []ScopeResult{{Scope: "us-east-1", Notices: []string{shared.UnsupportedTypeNotice("AWS::X::Y", "n/a")}}}
	if warnings := scopeWarnings(scopes); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}
