package shared

import (
	"fmt"
	"strings"
)

// Entries in BatchScanResponse.Errors are either notices or errors.
//
// A notice is informational: the provider observed everything it was asked to, and is only
// reporting something about how (for example, a resource type the account cannot list). An error
// means the scope was not fully observed: a service, type, account, page, or describe failed and
// its resources may be missing. The core treats a scope with any error as incomplete, so the scan
// is partial and never resolves findings for resources it did not see.
//
// Providers build entries with the helpers below so the core can tell the two apart by prefix.
const (
	scanNoticeUnsupportedTypePrefix = "unsupported_type:"
	scanErrorServicePrefix          = "service:"
)

// UnsupportedTypeNotice reports a resource type the provider skipped because the target does not
// support listing it. It does not make the scope incomplete.
func UnsupportedTypeNotice(typeName, reason string) string {
	return fmt.Sprintf("%s%s: %s", scanNoticeUnsupportedTypePrefix, typeName, reason)
}

// ServiceScanError reports that service was not fully observed. Use it for every failure that
// may have dropped resources: a failed list, page, type, account, or describe.
func ServiceScanError(service string, err error) string {
	return fmt.Sprintf("%s%s: %v", scanErrorServicePrefix, service, err)
}

// IsScanNotice reports whether entry is informational. Every other entry, including ones that
// predate these helpers, is an error: an unknown entry must not let a scan claim completeness.
func IsScanNotice(entry string) bool {
	return strings.HasPrefix(entry, scanNoticeUnsupportedTypePrefix)
}

// SplitScanErrors separates a BatchScanResponse's Errors into errors and notices, keeping order.
func SplitScanErrors(entries []string) (errs, notices []string) {
	for _, entry := range entries {
		if IsScanNotice(entry) {
			notices = append(notices, entry)
		} else {
			errs = append(errs, entry)
		}
	}
	return errs, notices
}
