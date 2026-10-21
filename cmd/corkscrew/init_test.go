package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookupDependencyPin(t *testing.T) {
	got, err := lookupDependencyPin("protoc", "25.3", "linux-amd64")
	if err != nil {
		t.Fatalf("pinned tuple: %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("pin %q is not a hex SHA-256", got)
	}

	misses := []struct{ tool, version, platform string }{
		{"protoc", "99.0", "linux-amd64"},
		{"duckdb", "1.5.2", "freebsd-amd64"},
		{"curl", "8.0", "linux-amd64"},
	}
	for _, m := range misses {
		_, err := lookupDependencyPin(m.tool, m.version, m.platform)
		if err == nil || !strings.Contains(err.Error(), "no pinned SHA-256") {
			t.Fatalf("%v: want fail-closed error, got %v", m, err)
		}
	}
	_, err = lookupDependencyPin("protoc", "99.0", "linux-amd64")
	if !strings.Contains(err.Error(), "25.3/linux-amd64") {
		t.Fatalf("error should list pinned versions: %v", err)
	}
}

// Every platform the URL builders map must have a pin, or init fails closed on a platform it
// claims to support.
func TestEverySupportedPlatformIsPinned(t *testing.T) {
	protoc := []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64",
		"windows-amd64", "windows-386"}
	duckdb := []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64",
		"windows-amd64"}
	for _, p := range protoc {
		if _, err := lookupDependencyPin("protoc", "25.3", p); err != nil {
			t.Errorf("protoc %s: %v", p, err)
		}
	}
	for _, p := range duckdb {
		if _, err := lookupDependencyPin("duckdb", "1.5.2", p); err != nil {
			t.Errorf("duckdb %s: %v", p, err)
		}
	}
	if got := getDuckDBURL("1.5.2", "linux-arm64"); !strings.HasSuffix(got,
		"/v1.5.2/duckdb_cli-linux-arm64.zip") {
		t.Errorf("duckdb linux-arm64 URL = %s", got)
	}
	if got := getProtocURL("25.3", "darwin-arm64"); !strings.HasSuffix(got,
		"/v25.3/protoc-25.3-osx-aarch_64.zip") {
		t.Errorf("protoc darwin-arm64 URL = %s", got)
	}
}

func tinyZip(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("bin/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testInitConfig(t *testing.T) *InitConfig {
	t.Helper()
	root := t.TempDir()
	config := &InitConfig{BinDir: filepath.Join(root, "bin"), DepsDir: filepath.Join(root, "deps")}
	for _, dir := range []string{config.BinDir, config.DepsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return config
}

func TestDownloadRejectsChecksumMismatchBeforeExtraction(t *testing.T) {
	archive := tinyZip(t, "protoc", "#!/bin/sh\necho tampered\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	config := testInitConfig(t)
	dep := DependencyInfo{Name: "protoc", Version: "25.3", Platform: "linux-amd64",
		URL: srv.URL, ArchiveName: "protoc-test.zip", BinaryName: "protoc"}
	err := downloadAndInstallDependency(context.Background(), config, dep)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	sum := sha256.Sum256(archive)
	want := dependencyPinsSHA256["protoc@25.3"]["linux-amd64"]
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(),
		hex.EncodeToString(sum[:])) {
		t.Fatalf("error must name expected and actual digests: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.BinDir, "protoc")); !os.IsNotExist(err) {
		t.Fatalf("binary must not be extracted after a mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.DepsDir, dep.ArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("rejected archive must be removed: %v", err)
	}
}

func TestDownloadUnpinnedMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	dep := DependencyInfo{Name: "protoc", Version: "99.0", Platform: "linux-amd64",
		URL: srv.URL, ArchiveName: "protoc.zip", BinaryName: "protoc"}
	err := downloadAndInstallDependency(context.Background(), testInitConfig(t), dep)
	if err == nil || !strings.Contains(err.Error(), "no pinned SHA-256") {
		t.Fatalf("want fail-closed error, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("unpinned dependency triggered %d requests", hits.Load())
	}
}

// A chunked response has ContentLength -1; the download must still succeed and install.
func TestDownloadUnknownContentLength(t *testing.T) {
	archive := tinyZip(t, "testtool", "ok")
	sum := sha256.Sum256(archive)
	dependencyPinsSHA256["testtool@1.0"] = map[string]string{
		"linux-amd64": hex.EncodeToString(sum[:])}
	defer delete(dependencyPinsSHA256, "testtool@1.0")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		half := len(archive) / 2
		_, _ = w.Write(archive[:half])
		w.(http.Flusher).Flush() // forces chunked encoding: no Content-Length
		_, _ = w.Write(archive[half:])
	}))
	defer srv.Close()

	config := testInitConfig(t)
	dep := DependencyInfo{Name: "testtool", Version: "1.0", Platform: "linux-amd64",
		URL: srv.URL, ArchiveName: "testtool.zip", BinaryName: "testtool"}
	n, err := downloadToFile(context.Background(), dep, filepath.Join(t.TempDir(), "a.zip"))
	if err != nil || n != int64(len(archive)) {
		t.Fatalf("downloadToFile = %d, %v; want %d bytes", n, err, len(archive))
	}
	if err := downloadAndInstallDependency(context.Background(), config, dep); err != nil {
		t.Fatalf("install: %v", err)
	}
	binPath := filepath.Join(config.BinDir, "testtool")
	if strings.HasPrefix(dep.Platform, "windows") {
		binPath += ".exe"
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
}

func TestDownloadHonoursContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dep := DependencyInfo{Name: "protoc", URL: srv.URL}
	if _, err := downloadToFile(ctx, dep, filepath.Join(t.TempDir(), "a.zip")); err == nil {
		t.Fatal("cancelled context must abort the download")
	}
}

func TestRunBoundedTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	start := time.Now()
	_, err := runBounded(context.Background(), 50*time.Millisecond, t.TempDir(), "sleep", "10")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > subprocessWaitDelay+2*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestFindPluginSourceRoot(t *testing.T) {
	makeRepo := func(module string, plugins bool) string {
		dir := t.TempDir()
		gomod := []byte("module " + module + "\n\ngo 1.26\n")
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), gomod, 0o644); err != nil {
			t.Fatal(err)
		}
		if plugins {
			if err := os.Mkdir(filepath.Join(dir, "plugins"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	good := makeRepo(corkscrewModulePath, true)
	if got, err := findPluginSourceRoot(good); err != nil || got != good {
		t.Fatalf("corkscrew repo: got %q, %v", got, err)
	}
	if _, err := findPluginSourceRoot(makeRepo("example.com/evil", true)); err == nil {
		t.Fatal("foreign module must be rejected")
	}
	if _, err := findPluginSourceRoot(makeRepo(corkscrewModulePath, false)); err == nil {
		t.Fatal("missing plugins/ must be rejected")
	}
	if _, err := findPluginSourceRoot(t.TempDir()); err == nil {
		t.Fatal("directory without go.mod must be rejected")
	}
	// The package test runs in cmd/corkscrew; the repository root is two levels up.
	if _, err := findPluginSourceRoot(filepath.Join("..", "..")); err != nil {
		t.Fatalf("real repository root: %v", err)
	}
}
