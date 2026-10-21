package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// dependencyPinsSHA256 holds the SHA-256 of every archive init installs into ~/.corkscrew/bin,
// keyed by "tool@version" and then by Go GOOS-GOARCH (what init detects, not the upstream asset
// suffix).
// The binaries land on $PATH, so they are pinned in source instead of trusting an upstream
// checksum file fetched over the same connection as the archive (PRD decision 3).
//
// Provenance, recorded 2026-10-21:
//   - duckdb: each value matches both a local download and the per-asset `digest` from the
//     GitHub release API for duckdb/duckdb v1.5.2.
//   - protoc: the GitHub release API publishes no `digest` for protocolbuffers/protobuf v25.3
//     assets (they predate the field). Each value matches a local download and independent
//     third-party pins (taiki-e/install-action manifests/protoc.json, cashapp/hermit-packages
//     protoc.hcl, winget-pkgs Google.Protobuf 25.3 for win32).
//
// darwin-amd64 and darwin-arm64 share the osx-universal DuckDB archive, hence the same value.
var dependencyPinsSHA256 = map[string]map[string]string{
	"protoc@25.3": {
		"linux-amd64":   "f853e691868d0557425ea290bf7ba6384eef2fa9b04c323afab49a770ba9da80",
		"linux-arm64":   "9eae1f20f70cccc912d1c318c3929b86aebf5afd4b0f32c196ef682c222ed5ae",
		"darwin-amd64":  "247e003b8e115405172eacc50bd19825209d85940728e766f0848eee7c80e2a1",
		"darwin-arm64":  "d0fcd6d3b3ef6f22f1c47cc30a80c06727e1eccdddcaf0f4a3be47c070ffd3fe",
		"windows-amd64": "d6b336b852726364313330631656b7f395dde5b1141b169f5c4b8d43cdf01482",
		"windows-386":   "b9e7f2488c24bb3f3d507e125a9944a44ddfb09f2db6f84860319e6a7c75f137",
	},
	"duckdb@1.5.2": {
		"linux-amd64":   "fc9145affabca627431e73ddaf6b8117e5c192692480c13886f227be202d5d15",
		"linux-arm64":   "28b15a8d78e6df62f6ec43da6b0e6397dcd28e25ab93d847df3c5c97f59375f5",
		"darwin-amd64":  "de789904e1b0b5f305d31b88a6fd8bd4f6ed3f63a00d95f8c7a2559a0995a2f1",
		"darwin-arm64":  "de789904e1b0b5f305d31b88a6fd8bd4f6ed3f63a00d95f8c7a2559a0995a2f1",
		"windows-amd64": "d7b4f5774419c2e9eb14cb7361d3488821ef0244f8af461fd2c6fcb6f43bc3e0",
	},
}

// lookupDependencyPin fails closed: an archive without a pin is never downloaded, because an
// unverified binary on $PATH is worse than a missing one.
func lookupDependencyPin(tool, version, platform string) (string, error) {
	if want, ok := dependencyPinsSHA256[tool+"@"+version][platform]; ok {
		return want, nil
	}
	pinned := make([]string, 0, len(dependencyPinsSHA256))
	for key, platforms := range dependencyPinsSHA256 {
		if !strings.HasPrefix(key, tool+"@") {
			continue
		}
		for p := range platforms {
			pinned = append(pinned, strings.TrimPrefix(key, tool+"@")+"/"+p)
		}
	}
	sort.Strings(pinned)
	return "", fmt.Errorf("no pinned SHA-256 for %s v%s on %s (pinned: %s); refusing to install "+
		"an unverified binary", tool, version, platform, strings.Join(pinned, ", "))
}

// verifyFileSHA256 runs before extraction so a tampered archive never reaches the archive
// parser, let alone ~/.corkscrew/bin.
func verifyFileSHA256(path, wantHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s for checksum: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantHex {
		return fmt.Errorf("checksum mismatch for %s: expected sha256 %s, got %s",
			path, wantHex, got)
	}
	return nil
}
