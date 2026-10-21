package shared

import (
	"fmt"
	"strings"
)

// Entries in BatchScanResponse.Errors are either notices or errors.
//
// A notice is informational: the scan is as complete as the target allows, and the provider is
// reporting something about its coverage. There are three kinds:
//   - unsupported_type: the target has nothing of that type to enumerate here.
//   - access_denied: the credential cannot read something. This is the credential's real scope,
//     not a broken scan; the fix is a permission change, so it is surfaced as a coverage warning.
//   - not_enabled: a feature that would hold resources is turned off for the target.
//
// An error means the scope was not fully observed: a service, type, account, page, or describe
// failed and its resources may be missing. The core treats a scope with any error as incomplete,
// so the scan is partial and never resolves findings for resources it did not see.
//
// Providers build entries with the helpers below so the core can tell the two apart by prefix.
const (
	scanNoticeUnsupportedTypePrefix = "unsupported_type:"
	scanNoticeAccessDeniedPrefix    = "access_denied:"
	scanNoticeNotEnabledPrefix      = "not_enabled:"
	scanErrorServicePrefix          = "service:"
)

// UnsupportedTypeNotice reports a resource type the provider skipped because the target does not
// support listing it. It does not make the scope incomplete.
func UnsupportedTypeNotice(typeName, reason string) string {
	return fmt.Sprintf("%s%s: %s", scanNoticeUnsupportedTypePrefix, typeName, reason)
}

// AccessDeniedNotice reports that the credential is not permitted to read target (a resource
// type, endpoint, or repository feature). Resources behind it are outside the scan's coverage.
func AccessDeniedNotice(target string, err error) string {
	return fmt.Sprintf("%s%s: %v", scanNoticeAccessDeniedPrefix, target, err)
}

// NotEnabledNotice reports that a feature holding resources is disabled for target.
func NotEnabledNotice(target, detail string) string {
	return fmt.Sprintf("%s%s: %s", scanNoticeNotEnabledPrefix, target, detail)
}

// ServiceScanError reports that service was not fully observed. Use it for every failure that
// may have dropped resources: a failed list, page, type, account, or describe.
func ServiceScanError(service string, err error) string {
	return fmt.Sprintf("%s%s: %v", scanErrorServicePrefix, service, err)
}

// IsScanNotice reports whether entry is informational. Every other entry, including ones that
// predate these helpers, is an error: an unknown entry must not let a scan claim completeness.
func IsScanNotice(entry string) bool {
	return strings.HasPrefix(entry, scanNoticeUnsupportedTypePrefix) || IsCoverageNotice(entry)
}

// IsCoverageNotice reports whether entry is a notice the user can act on to widen coverage
// (a permission or a disabled feature), as opposed to an unsupported type, which they cannot.
func IsCoverageNotice(entry string) bool {
	return strings.HasPrefix(entry, scanNoticeAccessDeniedPrefix) ||
		strings.HasPrefix(entry, scanNoticeNotEnabledPrefix)
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
