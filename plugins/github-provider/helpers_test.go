package main

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestNormalizeSeverity(t *testing.T) {
	cases := map[string]string{
		"critical":  "critical",
		"Critical":  "critical",
		"CRITICAL":  "critical",
		"  HIGH  ":  "high",
		"medium":    "medium",
		"moderate":  "medium", // GitHub alias must collapse to medium
		"Moderate":  "medium",
		"low":       "low",
		"":          "",
		"info":      "",
		"unknown":   "",
		"important": "",
	}
	for in, want := range cases {
		if got := normalizeSeverity(in); got != want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDependabotSeverity(t *testing.T) {
	cases := []struct {
		name  string
		alert map[string]interface{}
		want  string
	}{
		{
			name:  "advisory severity wins",
			alert: map[string]interface{}{"security_advisory": map[string]interface{}{"severity": "critical"}},
			want:  "critical",
		},
		{
			name:  "advisory moderate normalizes to medium",
			alert: map[string]interface{}{"security_advisory": map[string]interface{}{"severity": "moderate"}},
			want:  "medium",
		},
		{
			name: "advisory present but invalid falls through to vulnerability",
			alert: map[string]interface{}{
				"security_advisory":      map[string]interface{}{"severity": "garbage"},
				"security_vulnerability": map[string]interface{}{"severity": "high"},
			},
			want: "high",
		},
		{
			name:  "vulnerability fallback",
			alert: map[string]interface{}{"security_vulnerability": map[string]interface{}{"severity": "low"}},
			want:  "low",
		},
		{
			name:  "no severity → default medium",
			alert: map[string]interface{}{},
			want:  "medium",
		},
		{
			name:  "advisory wrong type → default medium",
			alert: map[string]interface{}{"security_advisory": "not a map"},
			want:  "medium",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dependabotSeverity(c.alert); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestCodeScanningSeverity(t *testing.T) {
	cases := []struct {
		name  string
		alert map[string]interface{}
		want  string
	}{
		{
			name:  "security_severity_level preferred",
			alert: map[string]interface{}{"rule": map[string]interface{}{"security_severity_level": "high", "severity": "warning"}},
			want:  "high",
		},
		{
			name:  "fallback severity error→high",
			alert: map[string]interface{}{"rule": map[string]interface{}{"severity": "error"}},
			want:  "high",
		},
		{
			name:  "fallback severity warning→medium",
			alert: map[string]interface{}{"rule": map[string]interface{}{"severity": "warning"}},
			want:  "medium",
		},
		{
			name:  "fallback severity note→low",
			alert: map[string]interface{}{"rule": map[string]interface{}{"severity": "note"}},
			want:  "low",
		},
		{
			name:  "missing rule → medium default",
			alert: map[string]interface{}{},
			want:  "medium",
		},
		{
			name:  "rule wrong type → medium default",
			alert: map[string]interface{}{"rule": "scalar"},
			want:  "medium",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codeScanningSeverity(c.alert); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSecretScanningSeverityAlwaysCritical(t *testing.T) {
	if got := secretScanningSeverity(nil); got != "critical" {
		t.Errorf("got %q, want critical", got)
	}
}

func TestParseRepoFromID(t *testing.T) {
	cases := []struct {
		id           string
		wantOwner    string
		wantName     string
		wantOK       bool
	}{
		{"acme/web/branches/main/protection", "acme", "web", true},
		{"acme/web/rulesets/42", "acme", "web", true},
		{"acme/web/dependabot_alert/3", "acme", "web", true},
		{"acme/web", "acme", "web", true},
		{"single", "", "", false},
		{"", "", "", false},
		{"/foo", "", "", false},
		{"foo/", "", "", false},
		{"R_kgDOAbc123", "", "", false}, // node ID, no slashes
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			owner, name, ok := parseRepoFromID(c.id)
			if owner != c.wantOwner || name != c.wantName || ok != c.wantOK {
				t.Errorf("parseRepoFromID(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.id, owner, name, ok, c.wantOwner, c.wantName, c.wantOK)
			}
		})
	}
}

func TestBranchFromBranchProtectionID(t *testing.T) {
	cases := map[string]string{
		"acme/web/branches/main/protection":             "main",
		"acme/web/branches/release/2026/protection":     "release/2026", // slash-bearing branch
		"acme/web/branches/feature/foo/bar/protection":  "feature/foo/bar",
		"acme/web/branches/main":                        "main", // no /protection suffix
		"unrelated":                                     "",
		"":                                              "",
	}
	for in, want := range cases {
		if got := branchFromBranchProtectionID(in); got != want {
			t.Errorf("branchFromBranchProtectionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastSegment(t *testing.T) {
	cases := map[string]string{
		"a/b/c":     "c",
		"single":    "single",
		"a/":        "",
		"":          "",
		"trailing/": "",
	}
	for in, want := range cases {
		if got := lastSegment(in); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoCoordsFromRef(t *testing.T) {
	cases := []struct {
		name      string
		ref       *pb.ResourceRef
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{
			name:      "Name has owner/repo",
			ref:       &pb.ResourceRef{Name: "acme/web"},
			wantOwner: "acme", wantName: "web", wantOK: true,
		},
		{
			name:      "fallback to ID",
			ref:       &pb.ResourceRef{Id: "acme/web/branches/main/protection"},
			wantOwner: "acme", wantName: "web", wantOK: true,
		},
		{
			name:      "fallback to BasicAttributes.repository",
			ref:       &pb.ResourceRef{BasicAttributes: map[string]string{"repository": "acme/web"}},
			wantOwner: "acme", wantName: "web", wantOK: true,
		},
		{
			name:   "name with single segment is rejected",
			ref:    &pb.ResourceRef{Name: "single"},
			wantOK: false,
		},
		{
			name:   "empty ref",
			ref:    &pb.ResourceRef{},
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owner, name, ok := repoCoordsFromRef(c.ref)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if owner != c.wantOwner || name != c.wantName {
				t.Errorf("got (%q, %q), want (%q, %q)", owner, name, c.wantOwner, c.wantName)
			}
		})
	}
}

func TestListCacheKeyDeterministic(t *testing.T) {
	a := listCacheKey("repos", map[string]string{"foo": "1", "bar": "2"})
	b := listCacheKey("repos", map[string]string{"bar": "2", "foo": "1"})
	if a != b {
		t.Errorf("listCacheKey not order-stable: %q vs %q", a, b)
	}
	c := listCacheKey("repos", map[string]string{"foo": "1"})
	if a == c {
		t.Errorf("listCacheKey collision across different filter sets")
	}
	d := listCacheKey("orgs", map[string]string{"foo": "1", "bar": "2"})
	if a == d {
		t.Errorf("listCacheKey collision across different services")
	}
}

func TestParseStringMap(t *testing.T) {
	cases := []struct {
		in      string
		want    map[string]string
		wantErr bool
	}{
		{`{"a":"1","b":"2"}`, map[string]string{"a": "1", "b": "2"}, false},
		{``, map[string]string{}, false},
		{`null`, map[string]string{}, false},
		{`{`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseStringMap(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestRepoOwnerName(t *testing.T) {
	cases := []struct {
		name      string
		repo      *github.Repository
		wantOwner string
		wantName  string
	}{
		{
			name: "owner login present",
			repo: &github.Repository{
				Owner: &github.User{Login: github.Ptr("acme")},
				Name:  github.Ptr("web"),
			},
			wantOwner: "acme",
			wantName:  "web",
		},
		{
			name: "fall back to FullName",
			repo: &github.Repository{
				FullName: github.Ptr("acme/web"),
				Name:     github.Ptr("web"),
			},
			wantOwner: "acme",
			wantName:  "web",
		},
		{
			name: "no owner anywhere",
			repo: &github.Repository{
				Name: github.Ptr("web"),
			},
			wantOwner: "",
			wantName:  "web",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, n := repoOwnerName(c.repo)
			if o != c.wantOwner || n != c.wantName {
				t.Errorf("got (%q, %q), want (%q, %q)", o, n, c.wantOwner, c.wantName)
			}
		})
	}
}

func TestCompactResources(t *testing.T) {
	in := []*pb.Resource{
		{Id: "a"},
		nil,
		{Id: "b"},
		nil,
		{Id: "c"},
	}
	out := compactResources(in)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	want := []string{"a", "b", "c"}
	for i, r := range out {
		if r.Id != want[i] {
			t.Errorf("out[%d].Id = %q, want %q", i, r.Id, want[i])
		}
	}
}

func TestAppendAPIError(t *testing.T) {
	t.Run("403 marked skipped", func(t *testing.T) {
		var errs []string
		appendAPIError(&errs, "op", "target", &github.ErrorResponse{
			Response: stubHTTPResponse(403, "403 Forbidden"),
		})
		if len(errs) != 1 || !contains(errs[0], "skipped") {
			t.Errorf("got %v", errs)
		}
	})
	t.Run("404 marked skipped", func(t *testing.T) {
		var errs []string
		appendAPIError(&errs, "op", "target", &github.ErrorResponse{
			Response: stubHTTPResponse(404, "404 Not Found"),
		})
		if len(errs) != 1 || !contains(errs[0], "skipped") {
			t.Errorf("got %v", errs)
		}
	})
	t.Run("500 marked failed", func(t *testing.T) {
		var errs []string
		appendAPIError(&errs, "op", "target", &github.ErrorResponse{
			Response: stubHTTPResponse(500, "500 Internal Server Error"),
		})
		if len(errs) != 1 || !contains(errs[0], "failed") {
			t.Errorf("got %v", errs)
		}
	})
	t.Run("nil err is no-op", func(t *testing.T) {
		var errs []string
		appendAPIError(&errs, "op", "target", nil)
		if len(errs) != 0 {
			t.Errorf("got %v", errs)
		}
	})
}

func TestCollaboratorPermission(t *testing.T) {
	cases := []struct {
		name string
		perm map[string]bool
		want string
	}{
		{"admin wins", map[string]bool{"admin": true, "push": true, "pull": true}, "admin"},
		{"maintain over push", map[string]bool{"maintain": true, "push": true}, "maintain"},
		{"push only", map[string]bool{"push": true, "pull": true}, "push"},
		{"pull only", map[string]bool{"pull": true}, "pull"},
		{"empty", map[string]bool{}, ""},
		{"all false", map[string]bool{"admin": false, "push": false}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := &github.User{Permissions: c.perm}
			if got := collaboratorPermission(u); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestHookConfigGetters(t *testing.T) {
	t.Run("nil config returns empty strings", func(t *testing.T) {
		h := &github.Hook{}
		if hookConfigURL(h) != "" || hookContentType(h) != "" || hookInsecureSSL(h) != "" {
			t.Errorf("expected all empty for nil config")
		}
	})
	t.Run("populated config returns values", func(t *testing.T) {
		h := &github.Hook{Config: &github.HookConfig{
			URL:         github.Ptr("https://example.com/hook"),
			ContentType: github.Ptr("json"),
			InsecureSSL: github.Ptr("0"),
		}}
		if hookConfigURL(h) != "https://example.com/hook" {
			t.Error("URL mismatch")
		}
		if hookContentType(h) != "json" {
			t.Error("ContentType mismatch")
		}
		if hookInsecureSSL(h) != "0" {
			t.Error("InsecureSSL mismatch")
		}
	})
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "a", "b"); got != "a" {
		t.Errorf("got %q, want a", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := firstNonEmpty("  trim  "); got != "trim" {
		t.Errorf("got %q, want trim", got)
	}
}

func TestMergeConfig(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2", "c": "3"}
	override := map[string]string{"b": "X", "d": "4", "a": ""} // empty overrides skipped
	got := mergeConfig(base, override)
	want := map[string]string{"a": "1", "b": "X", "c": "3", "d": "4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ---- helpers for tests above ----

func stubHTTPResponse(status int, statusText string) *http.Response {
	return &http.Response{StatusCode: status, Status: statusText, Header: map[string][]string{}}
}

func contains(haystack, needle string) bool {
	return stringIndex(haystack, needle) >= 0
}

// stringIndex avoids importing strings just for the helper above.
func stringIndex(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
