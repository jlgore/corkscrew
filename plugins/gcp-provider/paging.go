package main

import (
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"google.golang.org/api/iterator"
)

// listPages drains a Google Cloud iterator in pages of pageSize and passes every item to visit.
//
// Ranging over it.Next() hides the continuation tokens, so a server that keeps returning the
// same token would loop until the scope deadline. Driving the iterator through a Pager exposes
// each token to a paging.Guard instead. Items from pages already received are visited even when
// a later page fails, so callers keep what they observed and report the returned error.
func listPages[T any](label string, it iterator.Pageable, pageSize int, visit func(T)) error {
	pager := iterator.NewPager(it, pageSize, "")
	guard := paging.New(label, paging.PagesMax)
	for {
		var page []T
		token, err := pager.NextPage(&page)
		for _, item := range page {
			visit(item)
		}
		if err != nil {
			return err
		}
		if token == "" {
			return nil
		}
		if err := guard.Next(token); err != nil {
			return err
		}
	}
}
