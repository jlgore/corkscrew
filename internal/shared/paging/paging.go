// Package paging bounds provider pagination loops.
//
// A page loop that trusts the server's continuation token runs until the scope deadline when a
// server or proxy returns the same token again, appending duplicate resources the whole time.
// Guard gives every loop a page limit and rejects a token that does not advance.
package paging

import (
	"errors"
	"fmt"
)

// PagesMax is the default page limit. At typical page sizes of 100-1000 items it allows
// 1-10 million resources per listing, far beyond any real account, so hitting it signals a
// looping server rather than a large one.
const PagesMax = 10_000

var (
	// ErrPageLimit means the loop reached its page limit; results so far are incomplete.
	ErrPageLimit = errors.New("pagination exceeded page limit")
	// ErrTokenRepeated means the server returned a continuation token it had already returned.
	ErrTokenRepeated = errors.New("pagination token did not advance")
)

// Guard tracks one pagination loop. Create one per listing with New, and call Next with each
// page's continuation token before requesting the following page.
type Guard struct {
	label         string
	pagesMax      int
	pages         int
	tokenPrevious string
}

// New returns a Guard for the listing named label (used in errors), allowing pagesMax pages.
func New(label string, pagesMax int) *Guard {
	if pagesMax <= 0 {
		panic(fmt.Sprintf("paging: pagesMax must be positive, got %d", pagesMax))
	}
	return &Guard{label: label, pagesMax: pagesMax}
}

// Next records that a page was received whose continuation token is token. Call it only when
// another page will be requested, i.e. token is non-empty. It returns ErrPageLimit once
// pagesMax pages have been received, and ErrTokenRepeated if token equals the previous one.
func (g *Guard) Next(token string) error {
	if token == "" {
		panic("paging: Next called with an empty token; the loop should have stopped")
	}
	g.pages++
	if g.pages >= g.pagesMax {
		return fmt.Errorf("%s: %w (%d pages)", g.label, ErrPageLimit, g.pagesMax)
	}
	if token == g.tokenPrevious {
		return fmt.Errorf("%s: %w after %d pages", g.label, ErrTokenRepeated, g.pages)
	}
	g.tokenPrevious = token
	return nil
}

// Pages returns how many pages have been recorded.
func (g *Guard) Pages() int {
	return g.pages
}
