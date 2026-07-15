// Package githubauth owns the GitHub App manifest bootstrap flow: it serves the
// manifest-creation form, receives GitHub's callback, converts the returned code
// into app credentials, and persists them under the corkscrew provider config
// directory. CLI adapters supply the browser opener and user messaging.
package githubauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// AppManifest is the GitHub App manifest submitted to create a new app.
type AppManifest struct {
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	RedirectURL string            `json:"redirect_url"`
	Public      bool              `json:"public"`
	Description string            `json:"description"`
	Permissions map[string]string `json:"default_permissions"`
	Events      []string          `json:"default_events"`
	HookAttrs   map[string]string `json:"hook_attributes,omitempty"`
}

// AppConversion is the credential set GitHub returns for a created app.
type AppConversion struct {
	ID            int64  `json:"id"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
	HTMLURL       string `json:"html_url"`
	Name          string `json:"name"`
}

// DefaultManifest builds the read-only scanner manifest for the given app name
// and OAuth callback URL.
func DefaultManifest(name, callbackURL string) AppManifest {
	return AppManifest{
		Name:        name,
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
}

// BootstrapRequest configures a GitHub App manifest bootstrap.
type BootstrapRequest struct {
	Org     string
	AppName string
	Port    int
	Timeout time.Duration
	// OpenURL opens the manifest-creation URL in a browser. If it returns an
	// error, the URL is surfaced through Notify instead.
	OpenURL func(url string) error
	// Notify reports progress and fallback instructions to the user.
	Notify func(message string)
}

func (r BootstrapRequest) notify(message string) {
	if r.Notify != nil {
		r.Notify(message)
	}
}

// BootstrapApp runs the GitHub App manifest OAuth flow and returns the created
// app's credentials.
func BootstrapApp(ctx context.Context, request BootstrapRequest) (*AppConversion, error) {
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	callbackURL := fmt.Sprintf("http://localhost:%d/github-app/callback", request.Port)
	manifest := DefaultManifest(request.AppName, callbackURL)

	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/github-app/start", manifestStartHandler(request.Org, state, manifest))
	mux.HandleFunc("/github-app/callback", manifestCallbackHandler(state, codeCh))
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", request.Port), Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			request.notify(fmt.Sprintf("local callback server failed: %v", err))
		}
	}()
	defer server.Shutdown(context.Background())

	startURL := fmt.Sprintf("http://localhost:%d/github-app/start", request.Port)
	if request.OpenURL != nil {
		if err := request.OpenURL(startURL); err != nil {
			request.notify(fmt.Sprintf("Open this URL to create the GitHub App:\n%s", startURL))
		}
	}
	request.notify("Waiting for GitHub App manifest callback...")

	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	select {
	case code := <-codeCh:
		return convertManifest(code)
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for GitHub callback")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func manifestStartHandler(org, state string, manifest AppManifest) http.HandlerFunc {
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

func convertManifest(code string) (*AppConversion, error) {
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
	var conversion AppConversion
	if err := json.Unmarshal(body, &conversion); err != nil {
		return nil, err
	}
	return &conversion, nil
}

// Store persists the created GitHub App credentials under
// ~/.corkscrew/providers/github and returns that directory.
func Store(org string, conversion *AppConversion) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".corkscrew", "providers", "github")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	keyPath := filepath.Join(dir, "private-key.pem")
	if err := os.WriteFile(keyPath, []byte(conversion.PEM), 0600); err != nil {
		return "", err
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
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), data, 0600); err != nil {
		return "", err
	}
	return dir, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
