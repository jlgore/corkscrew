package githubauth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultManifestIsReadOnly(t *testing.T) {
	manifest := DefaultManifest("Corkscrew Scanner", "http://localhost:8947/github-app/callback")
	if manifest.Public {
		t.Fatal("manifest should not be public")
	}
	if manifest.RedirectURL != "http://localhost:8947/github-app/callback" {
		t.Fatalf("redirect url = %q", manifest.RedirectURL)
	}
	if len(manifest.Permissions) == 0 {
		t.Fatal("manifest has no permissions")
	}
	for scope, access := range manifest.Permissions {
		if access != "read" {
			t.Fatalf("permission %q = %q, want read", scope, access)
		}
	}
}

func TestStoreWritesCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Store("acme", &AppConversion{ID: 42, ClientID: "cid", PEM: "PEMDATA", HTMLURL: "https://github.com/apps/x"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	pem, err := os.ReadFile(filepath.Join(dir, "private-key.pem"))
	if err != nil || string(pem) != "PEMDATA" {
		t.Fatalf("private key = %q, %v", pem, err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app.json"))
	if err != nil {
		t.Fatalf("read app.json: %v", err)
	}
	var config map[string]string
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse app.json: %v", err)
	}
	if config["org"] != "acme" || config["app_id"] != "42" || config["client_id"] != "cid" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if config["private_key_path"] != filepath.Join(dir, "private-key.pem") {
		t.Fatalf("private_key_path = %q", config["private_key_path"])
	}
}

func TestManifestCallbackHandlerValidatesState(t *testing.T) {
	codeCh := make(chan string, 1)
	handler := manifestCallbackHandler("expected-state", codeCh)

	// Wrong state is rejected.
	rejected := httptest.NewRecorder()
	handler(rejected, httptest.NewRequest(http.MethodGet, "/github-app/callback?state=wrong&code=abc", nil))
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("wrong-state status = %d, want 400", rejected.Code)
	}

	// Correct state forwards the code.
	accepted := httptest.NewRecorder()
	handler(accepted, httptest.NewRequest(http.MethodGet, "/github-app/callback?state=expected-state&code=abc123", nil))
	if accepted.Code != http.StatusOK {
		t.Fatalf("valid-state status = %d, want 200", accepted.Code)
	}
	select {
	case code := <-codeCh:
		if code != "abc123" {
			t.Fatalf("code = %q, want abc123", code)
		}
	default:
		t.Fatal("expected code to be forwarded on the channel")
	}
}

func TestConvertManifestEscapesCode(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(AppConversion{ID: 7, Name: "app"})
	}))
	defer server.Close()

	conversion, err := convertManifest(context.Background(), server.Client(), server.URL,
		"../../user?x=1")
	if err != nil {
		t.Fatalf("convertManifest: %v", err)
	}
	if conversion.ID != 7 {
		t.Fatalf("conversion id = %d, want 7", conversion.ID)
	}
	want := "/app-manifests/..%2F..%2Fuser%3Fx=1/conversions"
	if gotPath != want {
		t.Fatalf("request path = %q, want %q", gotPath, want)
	}
}

func TestConvertManifestRejectsOversizeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxManifestConversionBytes+1))
	}))
	defer server.Close()

	_, err := convertManifest(context.Background(), server.Client(), server.URL, "code")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize body error = %v, want size-limit error", err)
	}
}

func TestManifestClientHasTimeout(t *testing.T) {
	if manifestClient.Timeout != manifestConversionTimeout || manifestClient.Timeout <= 0 {
		t.Fatalf("manifest client timeout = %v", manifestClient.Timeout)
	}
}
