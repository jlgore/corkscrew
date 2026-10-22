package compliance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func packTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	// Sorted, so the same files always produce the same bytes and the same hash.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)),
			Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func registryWithPack(serverURL, checksum string) *RegistryClient {
	client := NewRegistryClient()
	client.registryCache = &RegistryCache{Packs: map[string]PackInfo{
		"acme": {Namespace: "acme", Versions: []RegistryPackVersion{{
			Version: "1.0.0", DownloadURL: serverURL, Checksum: checksum,
		}}},
	}}
	return client
}

func TestDownloadPackRejectsChecksumMismatch(t *testing.T) {
	tarball := packTarball(t, map[string]string{"root/manifest.yaml": "name: acme"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	}))
	defer server.Close()

	client := registryWithPack(server.URL, strings.Repeat("0", 64))
	_, err := client.DownloadPack(context.Background(), "acme", "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("err = %v, want checksum verification failure", err)
	}
}

func TestDownloadPackRejectsSiblingEscape(t *testing.T) {
	tarball := packTarball(t, map[string]string{
		"root/../acme-prod/manifest.yaml": "name: hijacked",
	})
	sum := sha256.Sum256(tarball)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	}))
	defer server.Close()

	destDir := t.TempDir()
	client := registryWithPack(server.URL, hex.EncodeToString(sum[:]))
	_, err := client.DownloadPack(context.Background(), "acme", "", destDir)
	if err == nil || !strings.Contains(err.Error(), "extract") {
		t.Fatalf("err = %v, want extract error", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "acme-prod")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sibling pack directory exists: %v", err)
	}
}

func TestRetryWaitHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewRegistryClient().WithRetryConfig(RetryConfig{
		MaxRetries: 3, RetryDelay: time.Hour, Backoff: 2,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = client.doRequestWithRetry(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("retry wait ignored cancellation for %v", elapsed)
	}
}

func TestNextRetryDelayIsCapped(t *testing.T) {
	tests := []struct {
		name    string
		delay   time.Duration
		backoff float64
		want    time.Duration
	}{
		{"doubles", time.Second, 2, 2 * time.Second},
		{"caps", 20 * time.Second, 2, retryDelayMax},
		{"overflow", retryDelayMax, math.MaxFloat64, retryDelayMax},
		{"negative backoff", time.Second, -1, 0},
		{"nan backoff", time.Second, math.NaN(), 0},
	}
	for _, tc := range tests {
		if got := nextRetryDelay(tc.delay, tc.backoff); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPackExtractDirRejectsEscapingNamespace(t *testing.T) {
	root := t.TempDir()
	for _, namespace := range []string{"..", "../acme", "", "org/../../x"} {
		if dir, err := packExtractDir(root, namespace); err == nil {
			t.Errorf("namespace %q accepted as %s", namespace, dir)
		}
	}
	if _, err := packExtractDir(root, "org/aws-security"); err != nil {
		t.Errorf("valid namespace rejected: %v", err)
	}
}
