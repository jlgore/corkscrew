package main

import (
	"os"
	"path/filepath"
	"testing"
)

// `pack install jlgore/cfi-ccc` is the help text's own example, so a bare
// owner/name must reach the registry rather than the GitHub stub.
func TestIsGitHubPackRef(t *testing.T) {
	cases := map[string]bool{
		"jlgore/cfi-ccc":                    false,
		"cfi-ccc":                           false,
		"jlgore/cfi-ccc@v1.0.0":             false,
		"github.com/company/security-packs": true,
		"https://github.com/company/packs":  true,
		"http://github.com/company/packs":   true,
		"example.com/github.com/x":          false,
	}
	for ref, want := range cases {
		if got := isGitHubPackRef(ref); got != want {
			t.Errorf("isGitHubPackRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestCheckExistingPack(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "jlgore", "cfi-ccc")
	if err := checkExistingPack(missing, false); err != nil {
		t.Fatalf("missing pack: %v", err)
	}
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkExistingPack(missing, false); err == nil {
		t.Fatal("existing pack without --force: want error")
	}
	if err := checkExistingPack(missing, true); err != nil {
		t.Fatalf("existing pack with --force: %v", err)
	}
}
