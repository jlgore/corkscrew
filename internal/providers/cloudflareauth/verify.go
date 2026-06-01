package cloudflareauth

import "sort"

func VerifyResolvedAuth(plan *PermissionPlan, auth *ResolvedAuth) *VerifyResult {
	result := &VerifyResult{
		Method:          auth.Method,
		Source:          auth.Source,
		RequiredBundles: append([]PermissionBundle(nil), plan.Bundles...),
		RequiredScopes:  append([]string(nil), plan.Scopes...),
		GrantedScopes:   append([]string(nil), auth.Scopes...),
	}

	sort.Strings(result.GrantedScopes)

	if auth.Method != AuthMethodOAuth {
		result.ScopeCheckStrict = false
		return result
	}

	granted := make(map[string]struct{}, len(auth.Scopes))
	for _, scope := range auth.Scopes {
		granted[scope] = struct{}{}
	}
	for _, scope := range plan.Scopes {
		if _, ok := granted[scope]; !ok {
			result.MissingScopes = append(result.MissingScopes, scope)
		}
	}
	sort.Strings(result.MissingScopes)
	result.ScopeCheckStrict = true
	return result
}
