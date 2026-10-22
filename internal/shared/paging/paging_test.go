package paging

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

func TestGuardAllowsAdvancingTokens(t *testing.T) {
	guard := New("list widgets", 5)
	for page := 1; page < 5; page++ {
		if err := guard.Next(fmt.Sprintf("token-%d", page)); err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	if guard.Pages() != 4 {
		t.Fatalf("pages = %d, want 4", guard.Pages())
	}
}

func TestGuardStopsAtPageLimit(t *testing.T) {
	guard := New("list widgets", 3)
	var err error
	for page := 1; page <= 3 && err == nil; page++ {
		err = guard.Next(fmt.Sprintf("token-%d", page))
	}
	if !errors.Is(err, ErrPageLimit) {
		t.Fatalf("err = %v, want ErrPageLimit", err)
	}
}

func TestGuardRejectsRepeatedToken(t *testing.T) {
	guard := New("list widgets", PagesMax)
	if err := guard.Next("same"); err != nil {
		t.Fatal(err)
	}
	if err := guard.Next("same"); !errors.Is(err, ErrTokenRepeated) {
		t.Fatalf("err = %v, want ErrTokenRepeated", err)
	}
}

// A loop that keeps paging after ErrPageLimit has ignored the guard's error; the page count
// must never pass pagesMax, so that is a programmer error.
func TestGuardAssertsNoPageAfterLimit(t *testing.T) {
	guard := New("list widgets", 2)
	if err := guard.Next("a"); err != nil {
		t.Fatal(err)
	}
	if err := guard.Next("b"); !errors.Is(err, ErrPageLimit) {
		t.Fatalf("err = %v, want ErrPageLimit", err)
	}
	asserttest.FailsWith(t, "pagesMax", func() { _ = guard.Next("c") })
}

func TestGuardAssertsArguments(t *testing.T) {
	asserttest.FailsWith(t, "pagesMax is positive", func() { New("list widgets", 0) })
	asserttest.FailsWith(t, "non-empty token", func() { _ = New("list widgets", 2).Next("") })
}
