package main

import (
	"errors"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/jlgore/corkscrew/internal/shared/paging"
)

// guardNextLink checks an ARM page's nextLink before the pager follows it.
//
// runtime.Pager.More() only asks whether nextLink is non-empty, so a server that keeps
// returning the same link would loop until the scope deadline. Call this after every page;
// a nil or empty link ends the listing and needs no check.
func guardNextLink(guard *paging.Guard, nextLink *string) error {
	if nextLink == nil || *nextLink == "" {
		return nil
	}
	return guard.Next(*nextLink)
}

// isForbidden reports whether err is an ARM 403: the credential cannot read the object at all.
// That answer is stable for a given credential, so the object is outside the scan's scope rather
// than missing from it.
func isForbidden(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden
}
