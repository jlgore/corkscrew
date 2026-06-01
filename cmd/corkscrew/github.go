package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type githubAppManifest struct {
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	RedirectURL string            `json:"redirect_url"`
	Public      bool              `json:"public"`
	Description string            `json:"description"`
	Permissions map[string]string `json:"default_permissions"`
	Events      []string          `json:"default_events"`
	HookAttrs   map[string]string `json:"hook_attributes,omitempty"`
}

type githubAppConversion struct {
	ID            int64  `json:"id"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
	HTMLURL       string `json:"html_url"`
	Name          string `json:"name"`
}

func runGitHub(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew github <command>")
		fmt.Println("Commands: bootstrap-app")
		return
	}

	switch args[0] {
	case "bootstrap-app":
		if err := runGitHubBootstrapApp(args[1:]); err != nil {
			fmt.Printf("GitHub App bootstrap failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown github command: %s\n", args[0])
		fmt.Println("Available commands: bootstrap-app")
	}
}

func runGitHubBootstrapApp(args []string) error {
	fs := flag.NewFlagSet("github bootstrap-app", flag.ContinueOnError)
	org := fs.String("org", os.Getenv("GITHUB_ORG"), "GitHub organization slug")
	name := fs.String("name", "Corkscrew Scanner", "GitHub App name")
	callbackPort := fs.Int("port", 8947, "Local callback port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *org == "" {
		return fmt.Errorf("--org or GITHUB_ORG is required")
	}

	if err := checkGHAuth(); err != nil {
		return err
	}

	state, err := randomState()
	if err != nil {
		return err
	}
	callbackURL := fmt.Sprintf("http://localhost:%d/github-app/callback", *callbackPort)
	manifest := githubAppManifest{
		Name:        *name,
		URL:         "https://github.com/jlgore/corkscrew",
		RedirectURL: callbackURL,
		Public:      false,
		Description: "Read-only GitHub organization scanner for Corkscrew.",
		Permissions: map[string]string{
			"metadata":                    "read",
			"contents":                    "read",
			"administration":              "read",
			"members":                     "read",
			"organization_administration": "read",
			"security_events":             "read",
			"dependabot_alerts":           "read",
			"secret_scanning_alerts":      "read",
			"actions":                     "read",
			"checks":                      "read",
			"pull_requests":               "read",
			"issues":                      "read",
		},
		Events: []string{},
	}

	codeCh := make(chan string, 1)
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *callbackPort)}
	http.HandleFunc("/github-app/start", manifestStartHandler(*org, state, manifest))
	http.HandleFunc("/github-app/callback", manifestCallbackHandler(state, codeCh))

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Local callback server failed: %v\n", err)
		}
	}()
	defer server.Shutdown(context.Background())

	startURL := fmt.Sprintf("http://localhost:%d/github-app/start", *callbackPort)
	if err := openURL(startURL); err != nil {
		fmt.Printf("Open this URL to create the GitHub App:\n%s\n", startURL)
	}

	fmt.Println("Waiting for GitHub App manifest callback...")
	var code string
	select {
	case code = <-codeCh:
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("timed out waiting for GitHub callback")
	}

	conversion, err := convertGitHubAppManifest(code)
	if err != nil {
		return err
	}
	if err := writeGitHubAppConfig(*org, conversion); err != nil {
		return err
	}

	fmt.Println("GitHub App created and stored locally.")
	fmt.Printf("Install the app for %s: %s/installations/new\n", *org, conversion.HTMLURL)
	fmt.Println("After installation, Corkscrew can resolve the installation automatically; set installation_id only for locked-down environments.")
	return nil
}

func manifestStartHandler(org, state string, manifest githubAppManifest) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		manifestJSON, _ := json.Marshal(manifest)
		githubURL := fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?state=%s", org, state)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><body><p>Redirecting to GitHub App creation...</p><form id="f" method="post" action="%s"><input type="hidden" name="manifest" value='%s'></form><script>document.getElementById("f").submit();</script></body></html>`, githubURL, string(manifestJSON))
	}
}

func manifestCallbackHandler(state string, codeCh chan<- string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		codeCh <- code
		fmt.Fprintln(w, "GitHub App manifest received. You can close this tab and return to Corkscrew.")
	}
}

func convertGitHubAppManifest(code string) (*githubAppConversion, error) {
	url := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("manifest conversion failed: %s: %s", resp.Status, string(body))
	}
	var conversion githubAppConversion
	if err := json.Unmarshal(body, &conversion); err != nil {
		return nil, err
	}
	return &conversion, nil
}

func writeGitHubAppConfig(org string, conversion *githubAppConversion) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".corkscrew", "providers", "github")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "private-key.pem")
	if err := os.WriteFile(keyPath, []byte(conversion.PEM), 0600); err != nil {
		return err
	}
	config := map[string]string{
		"org":              org,
		"app_id":           strconv.FormatInt(conversion.ID, 10),
		"client_id":        conversion.ClientID,
		"client_secret":    conversion.ClientSecret,
		"webhook_secret":   conversion.WebhookSecret,
		"private_key_path": keyPath,
		"installation_id":  "",
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "app.json"), data, 0600)
}

func checkGHAuth() error {
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth status failed; run gh auth login first")
	}
	return nil
}

func openURL(url string) error {
	if _, err := exec.LookPath("gh"); err == nil {
		return exec.Command("gh", "browse", url).Run()
	}
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", url).Run()
	}
	return fmt.Errorf("no browser opener found")
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
