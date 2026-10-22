package compliance

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadPackLockMissingIsEmpty(t *testing.T) {
	lock, err := readPackLock(filepath.Join(t.TempDir(), packLockFileName))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, ok := lock.lookup("github.com/acme/packs", "v1.0.0"); ok {
		t.Fatal("empty lockfile has a pin")
	}
}

func TestPackLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), packLockFileName)
	lock := newPackLock()
	pin := packPin{SHA256: digestA, URL: "https://dl/pack.tar.gz", Verified: true}
	if err := lock.pin("github.com/acme/packs", "v1.0.0", pin); err != nil {
		t.Fatal(err)
	}
	if err := writePackLock(path, lock); err != nil {
		t.Fatal(err)
	}
	read, err := readPackLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := read.lookup("github.com/acme/packs", "v1.0.0"); !ok || got != pin {
		t.Fatalf("pin = %+v (found %v), want %+v", got, ok, pin)
	}

	// The write goes through a temporary file and a rename, so nothing else is left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != packLockFileName {
		t.Fatalf("directory holds %v, want only the lockfile", entries)
	}
}

// A lockfile is security state: one that does not parse must stop the install rather than be
// silently replaced, which would drop every pin in it.
func TestReadPackLockRejectsMalformed(t *testing.T) {
	pinJSON := func(digest string) string {
		return `{"version":1,"packs":{"github.com/acme/packs":{"v1.0.0":{"sha256":"` +
			digest + `","url":"u","verified":true}}}}`
	}
	tests := map[string]string{
		"not json":        "not json",
		"wrong version":   `{"version":2,"packs":{}}`,
		"unknown field":   `{"version":1,"packs":{},"extra":true}`,
		"trailing data":   `{"version":1,"packs":{}} {}`,
		"short digest":    pinJSON("abc"),
		"uppercase":       pinJSON(strings.ToUpper(digestA)),
		"non-hex digest":  pinJSON(strings.Repeat("g", 64)),
		"empty source":    `{"version":1,"packs":{"":{"v1":{"sha256":"` + digestA + `"}}}}`,
		"empty version":   `{"version":1,"packs":{"s":{"":{"sha256":"` + digestA + `"}}}}`,
		"oversize":        `{"version":1,"packs":{}}` + strings.Repeat(" ", packLockBytesMax),
		"missing version": `{"packs":{}}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), packLockFileName)
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readPackLock(path); err == nil {
				t.Fatal("malformed lockfile accepted")
			}
		})
	}
}

func TestPackLockEntryCountIsBounded(t *testing.T) {
	lock := newPackLock()
	versions := make(map[string]packPin, packLockEntryCountMax)
	for index := range packLockEntryCountMax {
		versions["v"+strconv.Itoa(index)] = packPin{SHA256: digestA}
	}
	lock.Packs["github.com/acme/packs"] = versions
	err := lock.pin("github.com/acme/other", "v1.0.0", packPin{SHA256: digestB})
	if !errors.Is(err, errPackLockFull) {
		t.Fatalf("err = %v, want errPackLockFull", err)
	}
	// Re-pinning an existing entry does not grow the lockfile, so it stays allowed.
	if err := lock.pin("github.com/acme/packs", "v0", packPin{SHA256: digestB}); err != nil {
		t.Fatalf("re-pin at capacity: %v", err)
	}
}

func TestPackSource(t *testing.T) {
	github := PackInfo{Namespace: "acme/packs",
		Repository: PackRepository{Type: "github", Owner: "acme", Name: "packs"}}
	if got := packSource(github); got != "github.com/acme/packs" {
		t.Errorf("github source = %q", got)
	}
	if got := packSource(PackInfo{Namespace: "acme"}); got != "registry:acme" {
		t.Errorf("registry source = %q", got)
	}
}
