package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Limits on how much of a binding value the classifier reads. A Worker's settings can carry
// large JSON blobs; a credential is short, so the first part of each string is enough.
const (
	classifyValueBytesMax = 64 * 1024
	classifyJSONDepthMax  = 16
	classifyJSONLeavesMax = 1024
)

// Entropy fallback: a token-shaped string of at least this many characters, with this much
// Shannon entropy per character, looks randomly generated. Hex tops out at 4.0 bits, so IDs and
// hashes stay below; random base64 runs around 4.5 to 5.5.
const (
	entropyLengthMin  = 20
	entropyBitsMin    = 4.0
	entropyLengthRead = 4096
)

// credentialPatterns are the shapes of well-known secrets, checked in order. The name is what a
// finding reports, never the matched text.
var credentialPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"github_token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"github_pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{"stripe_live_key", regexp.MustCompile(`\b[sr]k_live_[A-Za-z0-9]{16,}\b`)},
	{"slack_token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"gitlab_token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"private_key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"url_credentials", regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)},
}

// tokenAlphabet is the character set of generated tokens: base64, base64url, and hex. Strings
// with dots, colons, or spaces (hostnames, URLs, dates, prose) aren't entropy-checked.
var tokenAlphabet = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)

// classifyBindingValue reports whether value looks like a credential and why, as a reason that
// names the pattern ("prefix:github_token" or "entropy") and never repeats the value.
func classifyBindingValue(value string) (bool, string) {
	if len(value) > classifyValueBytesMax {
		value = value[:classifyValueBytesMax]
	}
	for _, p := range credentialPatterns {
		if p.pattern.MatchString(value) {
			return true, "prefix:" + p.name
		}
	}
	if looksRandom(value) {
		return true, "entropy"
	}
	return false, ""
}

// looksRandom reports whether value is a long, mixed-case, token-shaped string with high
// entropy: what a generated API key looks like, and what words, paths, IDs, and hashes don't.
func looksRandom(value string) bool {
	if len(value) < entropyLengthMin || !tokenAlphabet.MatchString(value) {
		return false
	}
	if len(value) > entropyLengthRead {
		value = value[:entropyLengthRead]
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range value {
		hasUpper = hasUpper || unicode.IsUpper(r)
		hasLower = hasLower || unicode.IsLower(r)
		hasDigit = hasDigit || unicode.IsDigit(r)
	}
	return hasUpper && hasLower && hasDigit && shannonBits(value) >= entropyBitsMin
}

// shannonBits is the Shannon entropy of value's bytes, in bits per byte.
func shannonBits(value string) float64 {
	var counts [256]int
	for i := 0; i < len(value); i++ {
		counts[value[i]]++
	}
	bits := 0.0
	n := float64(len(value))
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			bits -= p * math.Log2(p)
		}
	}
	return bits
}

// classifyJSONValue walks the string leaves of a decoded JSON value (keys sorted, so the result
// is deterministic) and reports the first that looks like a credential, as
// "json:<path>:<reason>". The path is safe to record; the value is not.
func classifyJSONValue(value any) (bool, string) {
	leaves := 0
	var walk func(v any, path string, depth int) (bool, string)
	walk = func(v any, path string, depth int) (bool, string) {
		if depth > classifyJSONDepthMax || leaves >= classifyJSONLeavesMax {
			return false, ""
		}
		switch v := v.(type) {
		case string:
			leaves++
			if ok, reason := classifyBindingValue(v); ok {
				return true, fmt.Sprintf("json:%s:%s", path, reason)
			}
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if ok, reason := walk(v[k], path+"."+jsonPathKey(k), depth+1); ok {
					return ok, reason
				}
			}
		case []any:
			for i, item := range v {
				if ok, reason := walk(item, fmt.Sprintf("%s[%d]", path, i), depth+1); ok {
					return ok, reason
				}
			}
		}
		return false, ""
	}
	return walk(value, "$", 0)
}

var identifierKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// jsonPathKey writes a key for a path: bare when it is an identifier, quoted otherwise. A key
// that itself looks like a credential is redacted, and keys are capped, so the path can't
// carry a secret into the reason.
func jsonPathKey(key string) string {
	const keyMax = 64
	if ok, _ := classifyBindingValue(key); ok {
		return "[redacted]"
	}
	if len(key) > keyMax {
		key = key[:keyMax] + "…"
	}
	if identifierKey.MatchString(key) {
		return key
	}
	return fmt.Sprintf("[%q]", strings.ReplaceAll(key, `"`, `'`))
}
