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

// --force reinstalls over an existing directory; it must never also accept a changed hash for a
// pinned version. Only --repin does that.
func TestParsePackInstallFlagsKeepsForceAndRepinApart(t *testing.T) {
	opts, err := parsePackInstallFlags([]string{"--force", "jlgore/cfi-ccc"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.force || opts.repin || opts.allowUnverified {
		t.Fatalf("--force parsed as %+v, want force only", opts)
	}
	if opts.packRef != "jlgore/cfi-ccc" || opts.version != "latest" {
		t.Fatalf("ref %q version %q", opts.packRef, opts.version)
	}

	opts, err = parsePackInstallFlags([]string{"--repin", "--allow-unverified", "jlgore/cfi-ccc"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.force || !opts.repin || !opts.allowUnverified {
		t.Fatalf("--repin --allow-unverified parsed as %+v", opts)
	}
}

func TestParsePackInstallFlagsRequiresRef(t *testing.T) {
	if _, err := parsePackInstallFlags([]string{"--force"}); err == nil {
		t.Fatal("missing pack ref accepted")
	}
}
