package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// The auth commands check the credential a scan would use: providers.cloudflare.config from the
// config file, with only explicitly given flags on top.
func TestCloudflareAuthOverrideReadsConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corkscrew.yaml")
	config := `version: "1.0"
providers:
  cloudflare:
    enabled: true
    config:
      auth.method: api_token
      auth.token_env: MY_CF_TOKEN
      auth.profile: work
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORKSCREW_CONFIG_FILE", path)

	parse := func(args ...string) (*flag.FlagSet, *string, *string) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		profile := fs.String("profile", "default", "")
		method := fs.String("method", "", "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return fs, profile, method
	}

	fs, profile, method := parse()
	override, err := cloudflareAuthOverride(fs, *profile, *method)
	if err != nil {
		t.Fatal(err)
	}
	if override["auth.token_env"] != "MY_CF_TOKEN" || override["auth.profile"] != "work" {
		t.Errorf("no flags: override = %v, want the file's token_env and profile", override)
	}

	fs, profile, method = parse("--profile", "other", "--method", "api_key")
	override, err = cloudflareAuthOverride(fs, *profile, *method)
	if err != nil {
		t.Fatal(err)
	}
	if override["auth.profile"] != "other" || override["auth.method"] != "api_key" {
		t.Errorf("with flags: override = %v, want the flags to win", override)
	}
}
