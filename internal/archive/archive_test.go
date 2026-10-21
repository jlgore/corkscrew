package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testEntry struct {
	name     string
	body     string
	size     int // when > 0, body is size zero bytes (compresses to almost nothing)
	typeflag byte
	linkname string
}

func (e testEntry) content() []byte {
	if e.size > 0 {
		return make([]byte, e.size)
	}
	return []byte(e.body)
}

func writeTarGz(t *testing.T, dir string, entries []testEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for _, e := range entries {
		content := e.content()
		header := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typeflag,
			Linkname: e.linkname}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(content))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := tw.Write(content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, dir, "test.tar.gz", buf.Bytes())
}

func writeZip(t *testing.T, dir string, entries []testEntry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		header.SetMode(0o644)
		body := e.content()
		if e.typeflag == tar.TypeSymlink {
			header.SetMode(fs.ModeSymlink | 0o777)
			body = []byte(e.linkname)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, dir, "test.zip", buf.Bytes())
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var testLimits = Limits{BytesMax: 1 << 20, EntryCountMax: 8, EntryBytesMax: 1 << 19}

var formats = []struct {
	name   string
	format Format
	write  func(*testing.T, string, []testEntry) string
}{
	{"tar.gz", FormatTarGz, writeTarGz},
	{"zip", FormatZip, writeZip},
}

func manyEntries(n int) []testEntry {
	entries := make([]testEntry, n)
	for i := range entries {
		entries[i] = testEntry{name: "root/f" + strings.Repeat("x", i), body: "x"}
	}
	return entries
}

func TestExtractTreeRejects(t *testing.T) {
	tests := []struct {
		name    string
		entries []testEntry
		limits  Limits
		tarOnly bool
		wantErr string
	}{
		{name: "dotdot traversal", wantErr: "escapes",
			entries: []testEntry{{name: "root/../../etc/passwd", body: "x"}}},
		{name: "sibling prefix escape", wantErr: "escapes",
			entries: []testEntry{{name: "root/../acme-prod/manifest.yaml", body: "x"}}},
		{name: "absolute path", wantErr: "absolute",
			entries: []testEntry{{name: "/etc/passwd", body: "x"}}},
		{name: "symlink", wantErr: "unsupported",
			entries: []testEntry{{name: "root/link", typeflag: tar.TypeSymlink,
				linkname: "/etc/passwd"}}},
		{name: "hardlink", wantErr: "unsupported", tarOnly: true,
			entries: []testEntry{{name: "root/link", typeflag: tar.TypeLink,
				linkname: "root/other"}}},
		{name: "gzip bomb over BytesMax", wantErr: "total size",
			limits:  Limits{BytesMax: 1 << 20, EntryCountMax: 8, EntryBytesMax: 1 << 30},
			entries: []testEntry{{name: "root/bomb", size: 8 << 20}}},
		{name: "too many entries", wantErr: "more than 8 entries",
			entries: manyEntries(9)},
		{name: "entry over EntryBytesMax", wantErr: "exceeds limit",
			entries: []testEntry{{name: "root/big", size: 1<<19 + 1}}},
	}
	for _, f := range formats {
		for _, tc := range tests {
			if tc.tarOnly && f.format != FormatTarGz {
				continue
			}
			t.Run(f.name+"/"+tc.name, func(t *testing.T) {
				base := t.TempDir()
				dest := filepath.Join(base, "packs", "acme")
				limits := tc.limits
				if limits == (Limits{}) {
					limits = testLimits
				}
				src := f.write(t, t.TempDir(), tc.entries)
				err := ExtractTree(f.format, src, dest, 1, limits)
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				if _, statErr := os.Stat(filepath.Join(base, "packs", "acme-prod")); statErr == nil {
					t.Fatal("sibling directory was created")
				}
			})
		}
	}
}

func TestExtractTreeWritesConfinedFiles(t *testing.T) {
	entries := []testEntry{
		{name: "root/", typeflag: tar.TypeDir},
		{name: "root/manifest.yaml", body: "name: acme"},
		{name: "root/queries/q1.sql", body: "select 1"},
		{name: "root/..hidden", body: "dots are not traversal"},
	}
	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "acme")
			src := f.write(t, t.TempDir(), entries)
			if err := ExtractTree(f.format, src, dest, 1, testLimits); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{
				"manifest.yaml":  "name: acme",
				"queries/q1.sql": "select 1",
				"..hidden":       "dots are not traversal",
			} {
				got, err := os.ReadFile(filepath.Join(dest, name))
				if err != nil || string(got) != want {
					t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
				}
			}
		})
	}
}

func TestExtractFileExactNameMatch(t *testing.T) {
	tests := []struct {
		name    string
		entries []testEntry
		want    string
		wantErr string
	}{
		{name: "skips suffix match", want: "real",
			entries: []testEntry{{name: "bin/evil-protoc", body: "evil"},
				{name: "bin/protoc", body: "real"}}},
		{name: "suffix only is not found", wantErr: "not found",
			entries: []testEntry{{name: "bin/evil-protoc", body: "evil"}}},
		{name: "windows exe", want: "win",
			entries: []testEntry{{name: "bin/protoc.exe", body: "win"}}},
		{name: "symlink rejected", wantErr: "unsupported",
			entries: []testEntry{{name: "bin/protoc", typeflag: tar.TypeSymlink,
				linkname: "/bin/sh"}}},
		{name: "oversize binary leaves nothing", wantErr: "exceeds limit",
			entries: []testEntry{{name: "bin/protoc", size: 1<<19 + 1}}},
	}
	for _, f := range formats {
		for _, tc := range tests {
			t.Run(f.name+"/"+tc.name, func(t *testing.T) {
				destDir := t.TempDir()
				dest := filepath.Join(destDir, "protoc")
				src := f.write(t, t.TempDir(), tc.entries)
				err := ExtractFile(f.format, src, "protoc", dest, testLimits)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
					}
					if names, _ := os.ReadDir(destDir); len(names) != 0 {
						t.Fatalf("destination not empty after failure: %v", names)
					}
					return
				}
				got, err := os.ReadFile(dest)
				if err != nil || string(got) != tc.want {
					t.Fatalf("got %q, %v; want %q", got, err, tc.want)
				}
			})
		}
	}
}

func TestDestinationMustBeAbsoluteAndClean(t *testing.T) {
	src := writeTarGz(t, t.TempDir(), []testEntry{{name: "root/a", body: "x"}})
	for _, dest := range []string{"relative/dir", "/tmp/a/../b", "/tmp/a/"} {
		if err := ExtractTree(FormatTarGz, src, dest, 1, testLimits); err == nil {
			t.Errorf("ExtractTree(%q) succeeded", dest)
		}
		if err := ExtractFile(FormatTarGz, src, "a", dest, testLimits); err == nil {
			t.Errorf("ExtractFile(%q) succeeded", dest)
		}
	}
}

// Declared sizes are checked first, so the read limit is exercised directly with a body
// that is longer than either limit.
func TestBudgetCopyStopsOneBytePastLimit(t *testing.T) {
	tests := []struct {
		name    string
		b       budget
		wantErr string
	}{
		{name: "entry limit", b: budget{remaining: 100, entryMax: 10}, wantErr: "entry exceeds"},
		{name: "total limit", b: budget{remaining: 10, entryMax: 100}, wantErr: "total size"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var dst bytes.Buffer
			err := tc.b.copy(&dst, bytes.NewReader(make([]byte, 1000)))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if dst.Len() != 11 {
				t.Fatalf("read %d bytes, want 11", dst.Len())
			}
		})
	}
}

func TestLimitsMustBePositive(t *testing.T) {
	src := writeTarGz(t, t.TempDir(), []testEntry{{name: "root/a", body: "x"}})
	err := ExtractTree(FormatTarGz, src, filepath.Join(t.TempDir(), "d"), 1, Limits{})
	if err == nil {
		t.Fatal("zero limits accepted")
	}
}
