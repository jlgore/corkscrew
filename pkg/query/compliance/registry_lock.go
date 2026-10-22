package compliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jlgore/corkscrew/internal/assert"
)

// The lockfile pins the SHA-256 of every registry pack version installed into a pack directory.
// It lives next to the packs it describes, as <pack dir>/corkscrew-lock.json, so each install
// root (the default ~/.corkscrew/query-packs or a --dest) has its own pins.
//
//	{
//	  "version": 1,
//	  "packs": {
//	    "github.com/acme/packs": {
//	      "v1.0.0": {"sha256": "<hex>", "url": "<download url>", "verified": true}
//	    }
//	  }
//	}
//
// Keys are the pack's source (packSource), then its version. "verified" records whether a
// published checksum backed the pin; false means trust on first use under --allow-unverified.
const (
	packLockFileName      = "corkscrew-lock.json"
	packLockVersion       = 1
	packLockBytesMax      = 4 << 20
	packLockEntryCountMax = 10_000
)

// errPackLockFull: pinning one more version would pass packLockEntryCountMax.
var errPackLockFull = errors.New("lockfile is full")

type packLock struct {
	Version int                           `json:"version"`
	Packs   map[string]map[string]packPin `json:"packs"` // source -> version -> pin
}

type packPin struct {
	SHA256   string `json:"sha256"`
	URL      string `json:"url"`
	Verified bool   `json:"verified"`
}

func newPackLock() *packLock {
	return &packLock{Version: packLockVersion, Packs: make(map[string]map[string]packPin)}
}

// packSource names where a pack comes from, so a pin follows the publisher rather than a local
// alias: "github.com/<owner>/<repo>" for GitHub-hosted packs, else "registry:<namespace>".
func packSource(info PackInfo) string {
	repo := info.Repository
	if repo.Type == "github" && repo.Owner != "" && repo.Name != "" {
		return "github.com/" + repo.Owner + "/" + repo.Name
	}
	assert.That(info.Namespace != "", "registry: a pack without a repository has a namespace")
	return "registry:" + info.Namespace
}

func (l *packLock) lookup(source, version string) (packPin, bool) {
	pin, ok := l.Packs[source][version]
	return pin, ok
}

// pin records (or replaces) the pin for source at version.
func (l *packLock) pin(source, version string, pin packPin) error {
	assert.That(source != "", "registry: a pin has a source")
	assert.That(version != "", "registry: a pin has a version")
	assert.That(len(pin.SHA256) == sha256HexLen, "registry: a pin is a SHA-256 digest")
	if _, exists := l.lookup(source, version); !exists {
		if l.entryCount() >= packLockEntryCountMax {
			return fmt.Errorf("%w: %d pins", errPackLockFull, packLockEntryCountMax)
		}
	}
	if l.Packs[source] == nil {
		l.Packs[source] = make(map[string]packPin)
	}
	l.Packs[source][version] = pin
	assert.That(l.entryCount() <= packLockEntryCountMax, "registry: lockfile is within its cap")
	return nil
}

func (l *packLock) entryCount() int {
	count := 0
	for _, versions := range l.Packs {
		count += len(versions)
	}
	return count
}

// readPackLock reads the lockfile at path. A missing file is an empty lock; anything that does
// not parse strictly is an error, because replacing it would silently drop every pin.
func readPackLock(path string) (*packLock, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return newPackLock(), nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, packLockBytesMax+1))
	if err != nil {
		return nil, err
	}
	if len(data) > packLockBytesMax {
		return nil, fmt.Errorf("lockfile %s is larger than %d bytes", path, packLockBytesMax)
	}
	lock, err := decodePackLock(data)
	if err != nil {
		return nil, fmt.Errorf("lockfile %s: %w", path, err)
	}
	return lock, nil
}

func decodePackLock(data []byte) (*packLock, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var lock packLock
	if err := decoder.Decode(&lock); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errors.New("trailing data after the lock object")
	}
	if lock.Version != packLockVersion {
		return nil, fmt.Errorf("version %d, want %d", lock.Version, packLockVersion)
	}
	if lock.Packs == nil {
		lock.Packs = make(map[string]map[string]packPin)
	}
	if err := validatePackLock(&lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func validatePackLock(lock *packLock) error {
	if lock.entryCount() > packLockEntryCountMax {
		return fmt.Errorf("more than %d pins", packLockEntryCountMax)
	}
	for source, versions := range lock.Packs {
		if source == "" {
			return errors.New("pin with an empty source")
		}
		for version, pin := range versions {
			if version == "" {
				return fmt.Errorf("%s: pin with an empty version", source)
			}
			digest, err := normalizeDigest(pin.SHA256)
			if err != nil {
				return fmt.Errorf("%s %s: %w", source, version, err)
			}
			if digest != pin.SHA256 {
				return fmt.Errorf("%s %s: digest is not lowercase", source, version)
			}
		}
	}
	return nil
}

// writePackLock replaces the lockfile atomically: a crash leaves the old lockfile or the new
// one, never a truncated file that would refuse every later install. Two installs racing on
// the same pack directory can still lose one pin (last rename wins); the next install of that
// version pins it again.
func writePackLock(path string, lock *packLock) error {
	assert.That(lock.Version == packLockVersion, "registry: lockfile has the current version")
	assert.That(lock.entryCount() <= packLockEntryCountMax, "registry: lockfile within its cap")
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > packLockBytesMax {
		return fmt.Errorf("lockfile would be larger than %d bytes", packLockBytesMax)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+packLockFileName+"-*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(data)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}
