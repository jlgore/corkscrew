package compliance

import (
	"errors"
	"strings"
	"testing"
)

const (
	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestParseChecksumFile(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		bareAllowed bool
		want        string
		wantErr     error
	}{
		{"sha256sum text mode", digestB + "  other.tar.gz\n" + digestA + "  pack.tar.gz\n",
			false, digestA, nil},
		{"sha256sum binary mode", digestA + " *pack.tar.gz\n", false, digestA, nil},
		{"crlf line endings", digestA + "  pack.tar.gz\r\n", false, digestA, nil},
		{"uppercase digest", strings.ToUpper(digestA) + "  pack.tar.gz\n", false, digestA, nil},
		{"no trailing newline", digestA + "  pack.tar.gz", false, digestA, nil},
		{"bare digest in .sha256", digestA + "\n", true, digestA, nil},
		{"bare digest in checksums.txt", digestA + "\n", false, "", ErrChecksumMalformed},
		{"no entry for asset", digestA + "  other.tar.gz\n", false, "", ErrChecksumMissing},
		{"empty file", "", false, "", ErrChecksumMissing},
		{"short digest", "abc  pack.tar.gz\n", false, "", ErrChecksumMalformed},
		{"non-hex digest", strings.Repeat("g", 64) + "  pack.tar.gz\n", false, "",
			ErrChecksumMalformed},
		{"single space separator", digestA + " pack.tar.gz\n", false, "", ErrChecksumMalformed},
		{"missing name", digestA + "  \n", false, "", ErrChecksumMalformed},
		{"garbage line", "not a checksum line\n", false, "", ErrChecksumMalformed},
		{"duplicate entry", digestA + "  pack.tar.gz\n" + digestB + "  pack.tar.gz\n", false, "",
			ErrChecksumMalformed},
		{"bare and named entry", digestA + "\n" + digestA + "  pack.tar.gz\n", true, "",
			ErrChecksumMalformed},
		{"oversize", strings.Repeat(digestB+"  x\n", checksumFileBytesMax/68+1), false, "",
			ErrChecksumMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChecksumFile([]byte(tc.data), "pack.tar.gz", tc.bareAllowed)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("digest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackVersionsFromReleases(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "v3.0.0", TarballURL: "https://src/v3", Assets: []GitHubReleaseAsset{
			{Name: "pack.tar.gz", Size: 10, DownloadURL: "https://dl/pack.tar.gz"},
			{Name: "pack.tar.gz.sha256", DownloadURL: "https://dl/pack.tar.gz.sha256"},
			{Name: "checksums.txt", DownloadURL: "https://dl/checksums.txt"},
		}},
		{TagName: "v2.0.0", TarballURL: "https://src/v2", Assets: []GitHubReleaseAsset{
			{Name: "pack.tar.gz", DownloadURL: "https://dl/v2/pack.tar.gz"},
			{Name: "checksums.txt", DownloadURL: "https://dl/v2/checksums.txt"},
		}},
		{TagName: "v1.0.0", TarballURL: "https://src/v1"},
		{TagName: "v0.9.0", TarballURL: "https://src/v0.9", Assets: []GitHubReleaseAsset{
			{Name: "a.tar.gz", DownloadURL: "https://dl/a.tar.gz"},
			{Name: "b.tar.gz", DownloadURL: "https://dl/b.tar.gz"},
		}},
		{TagName: "v4.0.0-rc1", Prerelease: true},
		{TagName: "draft", Draft: true},
	}
	versions := packVersionsFromReleases(releases)
	if len(versions) != 4 {
		t.Fatalf("got %d versions, want 4 (drafts and prereleases skipped)", len(versions))
	}

	// The per-asset checksum file wins over checksums.txt when both are published.
	v3 := versions[0]
	if v3.DownloadURL != "https://dl/pack.tar.gz" || v3.AssetName != "pack.tar.gz" {
		t.Errorf("v3 asset = %q %q, want the release asset", v3.AssetName, v3.DownloadURL)
	}
	if v3.ChecksumURL != "https://dl/pack.tar.gz.sha256" || v3.Size != 10 {
		t.Errorf("v3 checksum url = %q size = %d", v3.ChecksumURL, v3.Size)
	}
	if versions[1].ChecksumURL != "https://dl/v2/checksums.txt" {
		t.Errorf("v2 checksum url = %q, want checksums.txt", versions[1].ChecksumURL)
	}

	// Without exactly one pack asset nothing is downloadable verified, and the auto-generated
	// source tarball must never become the download URL.
	for _, v := range versions[2:] {
		if v.DownloadURL != "" || v.ChecksumURL != "" {
			t.Errorf("%s: download %q checksum %q, want none", v.Version, v.DownloadURL,
				v.ChecksumURL)
		}
		if v.TarballURL == "" {
			t.Errorf("%s: tarball url dropped", v.Version)
		}
	}
}
