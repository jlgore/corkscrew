package compliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jlgore/corkscrew/internal/archive"
	"github.com/jlgore/corkscrew/internal/assert"
)

// packDownload is a pack archive downloaded to a temporary file.
type packDownload struct {
	path   string
	url    string
	sha256 string // lowercase hex of the bytes at path
}

// downloadAndExtract verifies, extracts and pins one pack version. Every check that can refuse
// the pack (published checksum, lockfile pin) runs before anything is written into destDir.
func (c *RegistryClient) downloadAndExtract(ctx context.Context, packInfo PackInfo,
	version RegistryPackVersion, destDir string) (*QueryPack, error) {
	extractDir, err := packExtractDir(destDir, packInfo.Namespace)
	if err != nil {
		return nil, RegistryError{Operation: "extract", URL: packInfo.Namespace,
			Message: "invalid pack destination", Cause: err}
	}
	lockPath, err := packLockPath(destDir)
	if err != nil {
		return nil, RegistryError{Operation: "lock", URL: destDir,
			Message: "invalid pack destination", Cause: err}
	}

	checksum, err := c.publishedChecksum(ctx, packInfo, version)
	if err != nil {
		return nil, err
	}
	download, err := c.downloadVerified(ctx, version, checksum)
	if err != nil {
		return nil, err
	}
	// The download is a temp file; failing to remove it leaks a file in TMPDIR, nothing more.
	defer func() { _ = os.Remove(download.path) }()

	lock, err := readPackLock(lockPath)
	if err != nil {
		return nil, RegistryError{Operation: "lock", URL: lockPath,
			Message: "cannot read the pack lockfile; fix or remove it", Cause: err}
	}
	source := packSource(packInfo)
	if err := c.checkPin(lock, source, version.Version, download.sha256); err != nil {
		return nil, err
	}

	if err := extractPack(download.path, extractDir); err != nil {
		return nil, err
	}
	pin := packPin{SHA256: download.sha256, URL: download.url, Verified: checksum != ""}
	if err := savePin(lockPath, lock, source, version.Version, pin); err != nil {
		return nil, err
	}

	loader := NewPackLoader().WithSearchPaths(destDir)
	return loader.LoadPack(ctx, packInfo.Namespace)
}

// packLockPath returns the lockfile for the packs installed under destDir.
func packLockPath(destDir string) (string, error) {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, packLockFileName), nil
}

// publishedChecksum returns the pack asset's published SHA-256. It returns "" only when the
// release publishes none and the user passed --allow-unverified; a checksum file that exists but
// does not parse is always an error.
func (c *RegistryClient) publishedChecksum(ctx context.Context, packInfo PackInfo,
	version RegistryPackVersion) (string, error) {
	checksum, err := c.resolveChecksum(ctx, version)
	if err == nil {
		assert.That(len(checksum) == sha256HexLen, "registry: a resolved checksum is a digest")
		return checksum, nil
	}
	if !errors.Is(err, ErrChecksumMissing) {
		return "", RegistryError{Operation: "verify", URL: version.ChecksumURL,
			Message: "cannot read the published checksum", Cause: err}
	}
	if !c.allowUnverified {
		return "", RegistryError{Operation: "verify", URL: packInfo.Namespace,
			Message: fmt.Sprintf("version %s publishes no checksum for its pack asset; "+
				"re-run with --allow-unverified to install it unverified", version.Version),
			Cause: err}
	}

	// An unverified download lets whoever serves the URL choose the pack's SQL, so say so.
	_, _ = fmt.Fprintf(c.warnings,
		"⚠️  WARNING: pack %s %s publishes no checksum; installing an UNVERIFIED download "+
			"(--allow-unverified)\n", packInfo.Namespace, version.Version)
	return "", nil
}

// resolveChecksum returns the version's SHA-256: the cached Checksum if set, else the entry for
// AssetName in the checksum file at ChecksumURL. No checksum at all is ErrChecksumMissing.
func (c *RegistryClient) resolveChecksum(ctx context.Context,
	version RegistryPackVersion) (string, error) {
	if version.Checksum != "" {
		digest, err := normalizeDigest(version.Checksum)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrChecksumMalformed, err)
		}
		return digest, nil
	}
	if version.ChecksumURL == "" {
		return "", fmt.Errorf("%w: release has no %s or <asset>%s asset", ErrChecksumMissing,
			checksumsAssetName, checksumAssetSuffix)
	}
	if version.AssetName == "" {
		return "", fmt.Errorf("%w: checksum file but no pack asset", ErrChecksumMalformed)
	}

	var body bytes.Buffer
	if err := c.fetch(ctx, version.ChecksumURL, &body, checksumFileBytesMax); err != nil {
		return "", err
	}
	perAsset := checksumFileIsPerAsset(version.ChecksumURL)
	return parseChecksumFile(body.Bytes(), version.AssetName, perAsset)
}

// downloadVerified downloads the version's pack archive and, when checksum is set, rejects it
// unless it hashes to checksum. The caller removes the returned file.
func (c *RegistryClient) downloadVerified(ctx context.Context, version RegistryPackVersion,
	checksum string) (packDownload, error) {
	url := version.DownloadURL
	if url == "" {
		// Only an unverified install gets here: checksums are published for release assets,
		// never for GitHub's auto-generated (and not byte-stable) source tarball.
		url = version.TarballURL
	}
	if url == "" {
		return packDownload{}, RegistryError{Operation: "download", URL: version.Version,
			Message: "version has no download URL"}
	}

	path, digest, err := c.downloadToTemp(ctx, url)
	if err != nil {
		return packDownload{}, RegistryError{Operation: "download", URL: url,
			Message: "failed to download pack", Cause: err}
	}
	assert.That(len(digest) == sha256HexLen, "registry: a download hash is a digest")
	if checksum != "" && digest != checksum {
		mismatch := fmt.Errorf("%w: published %s, downloaded %s", ErrChecksumMismatch,
			checksum, digest)
		return packDownload{}, RegistryError{Operation: "verify", URL: url,
			Message: "checksum verification failed", Cause: errors.Join(mismatch, os.Remove(path))}
	}
	return packDownload{path: path, url: url, sha256: digest}, nil
}

// checkPin fails when the lockfile pins this version to a different hash, whatever
// --allow-unverified says: a pinned version that changes is a re-release or a compromise.
// Only --repin accepts the new hash, and it says so.
func (c *RegistryClient) checkPin(lock *packLock, source, version, digest string) error {
	pinned, ok := lock.lookup(source, version)
	if !ok || pinned.SHA256 == digest {
		return nil
	}
	assert.That(pinned.SHA256 != digest, "registry: only a changed hash reaches re-pin")
	if !c.repin {
		return RegistryError{Operation: "verify", URL: source,
			Message: fmt.Sprintf("version %s is pinned to sha256 %s but the download hashes "+
				"to %s; if the publisher re-released it on purpose, re-run with --repin",
				version, pinned.SHA256, digest),
			Cause: ErrPinMismatch}
	}
	_, _ = fmt.Fprintf(c.warnings,
		"⚠️  WARNING: %s %s re-pinned (--repin): sha256 %s replaces %s\n",
		source, version, digest, pinned.SHA256)
	return nil
}

func extractPack(archivePath, extractDir string) error {
	// Pack archives hold one top-level directory, as GitHub source tarballs and
	// `git archive --prefix` do; it is stripped.
	limits := archive.Limits{
		BytesMax:      packBytesMax,
		EntryCountMax: packEntryCountMax,
		EntryBytesMax: packEntryBytesMax,
	}
	if err := archive.ExtractTree(archive.FormatTarGz, archivePath, extractDir, 1,
		limits); err != nil {
		return RegistryError{Operation: "extract", URL: archivePath,
			Message: "failed to extract tarball", Cause: err}
	}
	return nil
}

func savePin(lockPath string, lock *packLock, source, version string, pin packPin) error {
	if err := lock.pin(source, version, pin); err != nil {
		return RegistryError{Operation: "lock", URL: lockPath, Message: "cannot pin pack",
			Cause: err}
	}
	if err := writePackLock(lockPath, lock); err != nil {
		return RegistryError{Operation: "lock", URL: lockPath,
			Message: "cannot write the pack lockfile", Cause: err}
	}
	return nil
}

// downloadToTemp downloads url into a new temporary file, hashing it on the way, and returns
// the file's path and SHA-256.
func (c *RegistryClient) downloadToTemp(ctx context.Context, url string) (string, string, error) {
	tmpFile, err := os.CreateTemp("", "corkscrew-pack-*.tar.gz")
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	downloadErr := c.fetch(ctx, url, io.MultiWriter(tmpFile, hash), packDownloadBytesMax)
	if err := errors.Join(downloadErr, tmpFile.Close()); err != nil {
		return "", "", errors.Join(err, os.Remove(tmpFile.Name()))
	}
	return tmpFile.Name(), hex.EncodeToString(hash.Sum(nil)), nil
}

// fetch writes the body of a GET of url to dest, failing past bytesMax.
func (c *RegistryClient) fetch(ctx context.Context, url string, dest io.Writer,
	bytesMax int64) error {
	assert.That(bytesMax > 0, "registry: a fetch has a size bound")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.githubToken))
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.doRequestWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	n, err := io.Copy(dest, io.LimitReader(resp.Body, bytesMax+1))
	if err != nil {
		return err
	}
	if n > bytesMax {
		return fmt.Errorf("download exceeds %d bytes", bytesMax)
	}
	return nil
}
