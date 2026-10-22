package compliance

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jlgore/corkscrew/internal/assert"
)

// Verification failures. Each one is the Cause of a RegistryError whose Operation is "verify",
// so callers tell them apart with errors.Is.
var (
	// ErrChecksumMissing: the release publishes no checksum for the pack asset. Installing
	// anyway needs --allow-unverified.
	ErrChecksumMissing = errors.New("no published checksum")
	// ErrChecksumMalformed: a checksum file or recorded digest does not parse. Always fatal,
	// because a broken checksum is not the same as an absent one.
	ErrChecksumMalformed = errors.New("malformed checksum")
	// ErrChecksumMismatch: the download does not hash to the published checksum. Always fatal.
	ErrChecksumMismatch = errors.New("checksum mismatch")
	// ErrPinMismatch: the download does not hash to the digest pinned in the lockfile for that
	// version. Fatal unless the user re-pins deliberately with --repin.
	ErrPinMismatch = errors.New("pinned hash mismatch")
)

// Release asset names a pack publishes. See docs/PACK_PUBLISHING.md.
const (
	checksumsAssetName   = "checksums.txt"
	checksumAssetSuffix  = ".sha256"
	packAssetSuffix      = ".tar.gz"
	sha256HexLen         = 64
	checksumFileBytesMax = 64 << 10 // far above any real release's checksums.txt
	checksumLineCountMax = 1024
)

// packVersionsFromReleases turns GitHub releases into registry versions. Only a release asset is
// ever used as the download: GitHub's auto-generated source tarballs are not byte-stable, so no
// checksum could be published for them. A release with no single .tar.gz asset gets no
// DownloadURL, and installing it needs --allow-unverified.
func packVersionsFromReleases(releases []GitHubRelease) []RegistryPackVersion {
	versions := make([]RegistryPackVersion, 0, len(releases))
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		version := RegistryPackVersion{
			Version:     release.TagName,
			Tag:         release.TagName,
			ReleaseDate: release.PublishedAt,
			TarballURL:  release.TarballURL,
		}
		if asset, ok := packAsset(release.Assets); ok {
			version.AssetName = asset.Name
			version.DownloadURL = asset.DownloadURL
			version.Size = asset.Size
			version.ChecksumURL = checksumAssetURL(release.Assets, asset.Name)
		}
		versions = append(versions, version)
	}
	assert.That(len(versions) <= len(releases), "registry: at most one version per release")
	return versions
}

// packAsset returns the release's pack archive: its one .tar.gz asset. Two candidates are
// ambiguous, and guessing would let an unrelated archive stand in for the pack.
func packAsset(assets []GitHubReleaseAsset) (GitHubReleaseAsset, bool) {
	var found GitHubReleaseAsset
	count := 0
	for _, asset := range assets {
		if strings.HasSuffix(asset.Name, packAssetSuffix) && asset.DownloadURL != "" {
			found = asset
			count++
		}
	}
	if count != 1 {
		return GitHubReleaseAsset{}, false
	}
	assert.That(found.Name != "", "registry: a pack asset has a name")
	return found, true
}

// checksumAssetURL returns the URL of the checksum file covering assetName. A per-asset
// "<asset>.sha256" file wins over a release-wide checksums.txt.
func checksumAssetURL(assets []GitHubReleaseAsset, assetName string) string {
	assert.That(assetName != "", "registry: checksum lookup names an asset")
	perAsset := ""
	releaseWide := ""
	for _, asset := range assets {
		switch asset.Name {
		case assetName + checksumAssetSuffix:
			perAsset = asset.DownloadURL
		case checksumsAssetName:
			releaseWide = asset.DownloadURL
		}
	}
	if perAsset != "" {
		return perAsset
	}
	return releaseWide
}

// checksumFileIsPerAsset reports whether url names an "<asset>.sha256" file, which may hold a
// bare digest, rather than a checksums.txt, which must name each file.
func checksumFileIsPerAsset(url string) bool {
	return strings.HasSuffix(url, checksumAssetSuffix)
}

// parseChecksumFile returns the SHA-256 that a checksum file publishes for assetName, in
// lowercase hex. The format is sha256sum's: one "<hex>  <name>" (text mode) or "<hex> *<name>"
// (binary mode) line per file. bareAllowed also accepts a line holding only the digest, as
// "<asset>.sha256" files often do. Anything else is ErrChecksumMalformed; a well-formed file
// with no entry for assetName is ErrChecksumMissing.
func parseChecksumFile(data []byte, assetName string, bareAllowed bool) (string, error) {
	assert.That(assetName != "", "registry: checksum parse names an asset")
	if len(data) > checksumFileBytesMax {
		return "", fmt.Errorf("%w: file is larger than %d bytes", ErrChecksumMalformed,
			checksumFileBytesMax)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > checksumLineCountMax+1 { // +1: the empty string after a final newline
		return "", fmt.Errorf("%w: file has more than %d lines", ErrChecksumMalformed,
			checksumLineCountMax)
	}
	found := ""
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		digest, name, err := parseChecksumLine(line, bareAllowed)
		if err != nil {
			return "", fmt.Errorf("%w: line %d: %v", ErrChecksumMalformed, index+1, err)
		}
		if name != "" && name != assetName {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("%w: more than one entry for %s", ErrChecksumMalformed,
				assetName)
		}
		found = digest
	}
	if found == "" {
		return "", fmt.Errorf("%w: no entry for %s", ErrChecksumMissing, assetName)
	}
	assert.That(len(found) == sha256HexLen, "registry: a parsed digest is 64 hex characters")
	return found, nil
}

// parseChecksumLine splits one sha256sum line. name is empty only for a bare digest.
func parseChecksumLine(line string, bareAllowed bool) (digest, name string, err error) {
	if len(line) < sha256HexLen {
		return "", "", errors.New("line is shorter than a SHA-256 digest")
	}
	digest, err = normalizeDigest(line[:sha256HexLen])
	if err != nil {
		return "", "", err
	}
	rest := line[sha256HexLen:]
	if rest == "" {
		if !bareAllowed {
			return "", "", errors.New("digest has no file name")
		}
		return digest, "", nil
	}
	if !strings.HasPrefix(rest, "  ") && !strings.HasPrefix(rest, " *") {
		return "", "", errors.New(`digest is not followed by "  <name>" or " *<name>"`)
	}
	name = rest[2:]
	if name == "" {
		return "", "", errors.New("file name is empty")
	}
	assert.That(len(digest) == sha256HexLen, "registry: a line digest is 64 hex characters")
	return digest, name, nil
}

// normalizeDigest validates a hex SHA-256 digest and returns it in lowercase.
func normalizeDigest(digest string) (string, error) {
	if len(digest) != sha256HexLen {
		return "", fmt.Errorf("digest has %d characters, want %d", len(digest), sha256HexLen)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("digest is not hexadecimal")
	}
	return strings.ToLower(digest), nil
}
