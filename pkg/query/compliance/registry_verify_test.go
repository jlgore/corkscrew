package compliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const verifyNamespace = "acme/packs"

const verifyManifest = `apiVersion: v1
kind: QueryPack
metadata:
  name: packs
  namespace: acme/packs
  version: 1.0.0
  description: Test pack
  provider: aws
spec:
  queries:
    - id: test-query
      title: Test Query
      description: A test query
      severity: HIGH
      category: security
      query_file: query.sql
      enabled: true
`

// releaseServer serves one pack release: the pack asset, optional checksum files and the
// auto-generated source tarball, and records every path requested.
type releaseServer struct {
	*httptest.Server
	mu       sync.Mutex
	files    map[string][]byte
	requests []string
}

func newReleaseServer(t *testing.T, files map[string][]byte) *releaseServer {
	t.Helper()
	rs := &releaseServer{files: files}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.requests = append(rs.requests, r.URL.Path)
		body, ok := rs.files[r.URL.Path]
		rs.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *releaseServer) requested(path string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, p := range rs.requests {
		if p == path {
			return true
		}
	}
	return false
}

func verifyPackTarball(t *testing.T, query string) []byte {
	return packTarball(t, map[string]string{
		"packs-1.0.0/manifest.yaml": verifyManifest,
		"packs-1.0.0/query.sql":     query,
	})
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// verifyClient returns a client whose cache holds one version of acme/packs served by rs.
// checksumPath is "" when the release publishes no checksum file.
func verifyClient(rs *releaseServer, checksumPath string) (*RegistryClient, *bytes.Buffer) {
	version := RegistryPackVersion{
		Version:     "v1.0.0",
		Tag:         "v1.0.0",
		AssetName:   "pack.tar.gz",
		DownloadURL: rs.URL + "/pack.tar.gz",
		TarballURL:  rs.URL + "/source.tar.gz",
	}
	if checksumPath != "" {
		version.ChecksumURL = rs.URL + checksumPath
	}
	client := NewRegistryClient()
	client.registryCache = &RegistryCache{Packs: map[string]PackInfo{
		verifyNamespace: {Namespace: verifyNamespace, Versions: []RegistryPackVersion{version},
			Repository: PackRepository{Type: "github", Owner: "acme", Name: "packs"}},
	}}
	var warnings bytes.Buffer
	client.warnings = &warnings
	return client, &warnings
}

func readTestLock(t *testing.T, destDir string) *packLock {
	t.Helper()
	lock, err := readPackLock(filepath.Join(destDir, packLockFileName))
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	return lock
}

func TestDownloadPackVerifiesChecksumsTxt(t *testing.T) {
	tarball := verifyPackTarball(t, "SELECT 1;")
	rs := newReleaseServer(t, map[string][]byte{
		"/pack.tar.gz": tarball,
		"/checksums.txt": []byte(strings.Repeat("0", 64) + "  other.tar.gz\n" +
			sha256Hex(tarball) + "  pack.tar.gz\n"),
	})
	client, warnings := verifyClient(rs, "/checksums.txt")
	destDir := t.TempDir()

	if _, err := client.DownloadPack(context.Background(), verifyNamespace, "", destDir); err != nil {
		t.Fatalf("DownloadPack: %v", err)
	}
	if rs.requested("/source.tar.gz") {
		t.Fatal("downloaded the auto-generated source tarball, want the release asset")
	}
	if warnings.Len() != 0 {
		t.Fatalf("warnings = %q, want none for a verified download", warnings.String())
	}
	pin, ok := readTestLock(t, destDir).lookup("github.com/acme/packs", "v1.0.0")
	if !ok || pin.SHA256 != sha256Hex(tarball) || !pin.Verified {
		t.Fatalf("pin = %+v (found %v), want the verified asset hash", pin, ok)
	}
}

func TestDownloadPackVerifiesPerAssetChecksum(t *testing.T) {
	tarball := verifyPackTarball(t, "SELECT 1;")
	rs := newReleaseServer(t, map[string][]byte{
		"/pack.tar.gz":        tarball,
		"/pack.tar.gz.sha256": []byte(sha256Hex(tarball) + "\n"),
	})
	client, _ := verifyClient(rs, "/pack.tar.gz.sha256")
	if _, err := client.DownloadPack(context.Background(), verifyNamespace, "v1.0.0",
		t.TempDir()); err != nil {
		t.Fatalf("DownloadPack: %v", err)
	}
}

func TestDownloadPackRejectsPublishedChecksumMismatch(t *testing.T) {
	tarball := verifyPackTarball(t, "SELECT 1;")
	rs := newReleaseServer(t, map[string][]byte{
		"/pack.tar.gz":   tarball,
		"/checksums.txt": []byte(strings.Repeat("0", 64) + "  pack.tar.gz\n"),
	})
	destDir := t.TempDir()
	// The flag covers a missing checksum, never a wrong one.
	client, _ := verifyClient(rs, "/checksums.txt")
	client.WithAllowUnverified(true)

	_, err := client.DownloadPack(context.Background(), verifyNamespace, "", destDir)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	var registryErr RegistryError
	if !errors.As(err, &registryErr) || registryErr.Operation != "verify" {
		t.Fatalf("err = %#v, want a verify RegistryError", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, verifyNamespace)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pack extracted despite mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, packLockFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lockfile written despite mismatch: %v", err)
	}
}

func TestDownloadPackRejectsMalformedChecksumFile(t *testing.T) {
	tarball := verifyPackTarball(t, "SELECT 1;")
	rs := newReleaseServer(t, map[string][]byte{
		"/pack.tar.gz":   tarball,
		"/checksums.txt": []byte("this is not a checksum file\n"),
	})
	client, _ := verifyClient(rs, "/checksums.txt")
	client.WithAllowUnverified(true)
	_, err := client.DownloadPack(context.Background(), verifyNamespace, "", t.TempDir())
	if !errors.Is(err, ErrChecksumMalformed) {
		t.Fatalf("err = %v, want ErrChecksumMalformed even with --allow-unverified", err)
	}
	if rs.requested("/pack.tar.gz") {
		t.Fatal("downloaded the pack before the checksum file parsed")
	}
}

func TestDownloadPackRejectsOversizeChecksumFile(t *testing.T) {
	rs := newReleaseServer(t, map[string][]byte{
		"/pack.tar.gz":   verifyPackTarball(t, "SELECT 1;"),
		"/checksums.txt": bytes.Repeat([]byte("x"), checksumFileBytesMax+1),
	})
	client, _ := verifyClient(rs, "/checksums.txt")
	_, err := client.DownloadPack(context.Background(), verifyNamespace, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestDownloadPackRequiresChecksum(t *testing.T) {
	missing := map[string]string{
		"no checksum asset": "",
		"no entry":          "/checksums.txt",
	}
	for name, checksumPath := range missing {
		t.Run(name, func(t *testing.T) {
			rs := newReleaseServer(t, map[string][]byte{
				"/pack.tar.gz":   verifyPackTarball(t, "SELECT 1;"),
				"/checksums.txt": []byte(strings.Repeat("0", 64) + "  other.tar.gz\n"),
			})
			client, _ := verifyClient(rs, checksumPath)
			_, err := client.DownloadPack(context.Background(), verifyNamespace, "", t.TempDir())
			if !errors.Is(err, ErrChecksumMissing) {
				t.Fatalf("err = %v, want ErrChecksumMissing", err)
			}
			if !strings.Contains(err.Error(), "--allow-unverified") {
				t.Fatalf("err = %v, want it to mention --allow-unverified", err)
			}
			if rs.requested("/pack.tar.gz") || rs.requested("/source.tar.gz") {
				t.Fatal("downloaded a pack that has no checksum")
			}
		})
	}
}

func TestDownloadPackAllowUnverifiedWarnsAndPins(t *testing.T) {
	tarball := verifyPackTarball(t, "SELECT 1;")
	rs := newReleaseServer(t, map[string][]byte{"/pack.tar.gz": tarball})
	client, warnings := verifyClient(rs, "")
	client.WithAllowUnverified(true)
	destDir := t.TempDir()

	if _, err := client.DownloadPack(context.Background(), verifyNamespace, "", destDir); err != nil {
		t.Fatalf("DownloadPack: %v", err)
	}
	if !strings.Contains(warnings.String(), "UNVERIFIED") {
		t.Fatalf("warnings = %q, want an unverified-download warning", warnings.String())
	}
	pin, ok := readTestLock(t, destDir).lookup("github.com/acme/packs", "v1.0.0")
	if !ok || pin.SHA256 != sha256Hex(tarball) || pin.Verified {
		t.Fatalf("pin = %+v (found %v), want a trust-on-first-use pin", pin, ok)
	}
}

// installChanged installs v1.0.0 once, then has the server re-publish v1.0.0 with different
// bytes and a matching checksum, as a compromised or careless publisher would.
func installChanged(t *testing.T, configure func(*RegistryClient)) (string, *bytes.Buffer, error) {
	t.Helper()
	original := verifyPackTarball(t, "SELECT 1;")
	files := map[string][]byte{
		"/pack.tar.gz":   original,
		"/checksums.txt": []byte(sha256Hex(original) + "  pack.tar.gz\n"),
	}
	rs := newReleaseServer(t, files)
	destDir := t.TempDir()
	client, _ := verifyClient(rs, "/checksums.txt")
	if _, err := client.DownloadPack(context.Background(), verifyNamespace, "", destDir); err != nil {
		t.Fatalf("first install: %v", err)
	}

	changed := verifyPackTarball(t, "SELECT 2;")
	rs.mu.Lock()
	files["/pack.tar.gz"] = changed
	files["/checksums.txt"] = []byte(sha256Hex(changed) + "  pack.tar.gz\n")
	rs.mu.Unlock()

	client, warnings := verifyClient(rs, "/checksums.txt")
	configure(client)
	_, err := client.DownloadPack(context.Background(), verifyNamespace, "v1.0.0", destDir)
	return destDir, warnings, err
}

func TestDownloadPackRejectsPinMismatch(t *testing.T) {
	configs := map[string]func(*RegistryClient){
		"default":          func(*RegistryClient) {},
		"allow-unverified": func(c *RegistryClient) { c.WithAllowUnverified(true) },
	}
	for name, configure := range configs {
		t.Run(name, func(t *testing.T) {
			destDir, _, err := installChanged(t, configure)
			if !errors.Is(err, ErrPinMismatch) {
				t.Fatalf("err = %v, want ErrPinMismatch", err)
			}
			if !strings.Contains(err.Error(), "--repin") {
				t.Fatalf("err = %v, want it to name the --repin path", err)
			}
			query, readErr := os.ReadFile(filepath.Join(destDir, verifyNamespace, "query.sql"))
			if readErr != nil || string(query) != "SELECT 1;" {
				t.Fatalf("installed query = %q (%v), want the pinned pack untouched", query,
					readErr)
			}
		})
	}
}

func TestDownloadPackRepinAcceptsChangedHash(t *testing.T) {
	destDir, warnings, err := installChanged(t, func(c *RegistryClient) { c.WithRepin(true) })
	if err != nil {
		t.Fatalf("DownloadPack with repin: %v", err)
	}
	if !strings.Contains(warnings.String(), "re-pinned") {
		t.Fatalf("warnings = %q, want a re-pin notice", warnings.String())
	}
	query, err := os.ReadFile(filepath.Join(destDir, verifyNamespace, "query.sql"))
	if err != nil || string(query) != "SELECT 2;" {
		t.Fatalf("installed query = %q (%v), want the re-pinned pack", query, err)
	}
	pin, _ := readTestLock(t, destDir).lookup("github.com/acme/packs", "v1.0.0")
	if pin.SHA256 != sha256Hex(verifyPackTarball(t, "SELECT 2;")) {
		t.Fatalf("pin = %s, want the new hash", pin.SHA256)
	}
}
