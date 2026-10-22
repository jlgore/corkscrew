// Package archive extracts untrusted tar.gz and zip archives with path confinement and
// explicit size limits. Downloaded packs and tool binaries both arrive as archives from the
// network, so every name, entry type and byte is treated as attacker-controlled.
package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jlgore/corkscrew/internal/assert"
)

// Format names the container format of an archive file.
type Format int

const (
	FormatTarGz Format = iota + 1
	FormatZip
)

// Limits bounds the work one extraction may do. Every field must be positive.
type Limits struct {
	// BytesMax bounds the uncompressed bytes of all entries together, so a small
	// compressed archive cannot expand without bound (a gzip or zip bomb).
	BytesMax int64
	// EntryCountMax bounds the number of entries read, including directories.
	EntryCountMax int
	// EntryBytesMax bounds the uncompressed bytes of a single entry.
	EntryBytesMax int64
}

func (l Limits) validate() error {
	if l.BytesMax <= 0 || l.EntryCountMax <= 0 || l.EntryBytesMax <= 0 {
		return fmt.Errorf("archive: limits must be positive: %+v", l)
	}
	return nil
}

const (
	fileMode   os.FileMode = 0o644
	binaryMode os.FileMode = 0o755
	dirMode    os.FileMode = 0o755
)

// ExtractTree writes every regular file of the archive under destDir, after removing
// stripComponents leading path elements (GitHub tarballs wrap content in one root dir).
// Directory entries are skipped; parents are created as files need them. Symlinks,
// hardlinks and devices are rejected, as is any name that resolves outside destDir.
func ExtractTree(
	format Format, archivePath, destDir string, stripComponents int, limits Limits,
) error {
	assertDestination(destDir)
	if stripComponents < 0 {
		return fmt.Errorf("archive: negative strip components %d", stripComponents)
	}
	if err := os.MkdirAll(destDir, dirMode); err != nil {
		return err
	}
	return walk(format, archivePath, limits, func(e entry, b *budget) (bool, error) {
		target, ok, err := confine(destDir, e.name, stripComponents)
		if err != nil || !ok || e.kind == kindDir {
			return false, err
		}
		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return false, err
		}
		return false, writeEntry(target, fileMode, e, b)
	})
}

// ExtractFile writes the first regular entry whose base name is exactly name (or
// name+".exe") to destPath and ignores every other entry. An exact base-name match keeps
// an entry such as "bin/evil-protoc" from being installed as "protoc".
func ExtractFile(format Format, archivePath, name, destPath string, limits Limits) error {
	assertDestination(destPath)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("archive: invalid binary name %q", name)
	}
	found := false
	err := walk(format, archivePath, limits, func(e entry, b *budget) (bool, error) {
		base := path.Base(e.name)
		if e.kind != kindFile || (base != name && base != name+".exe") {
			return false, nil
		}
		found = true
		return true, writeAtomic(destPath, e, b)
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("archive: binary %s not found", name)
	}
	return nil
}

// assertDestination asserts the caller resolved the destination itself; a relative or
// unclean path would make the confinement check depend on the working directory. Every caller
// derives it with filepath.Abs, so a violation is a caller bug, not untrusted input.
func assertDestination(dest string) {
	assert.Thatf(filepath.IsAbs(dest), "archive: destination is absolute and clean: %q", dest)
	assert.Thatf(filepath.Clean(dest) == dest,
		"archive: destination is absolute and clean: %q", dest)
}

// confine maps an archive name to a path under destDir. ok is false for names that strip to
// nothing (the root directory itself). Names that escape destDir are an error, not a skip,
// because a well-formed archive never contains them.
func confine(destDir, name string, stripComponents int) (string, bool, error) {
	if strings.ContainsRune(name, '\\') || strings.ContainsRune(name, 0) {
		return "", false, fmt.Errorf("archive: invalid entry name %q", name)
	}
	if path.IsAbs(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", false, fmt.Errorf("archive: absolute entry name %q", name)
	}
	parts := strings.Split(name, "/")
	if len(parts) <= stripComponents {
		return "", false, nil
	}
	relative := strings.Join(parts[stripComponents:], "/")
	if relative == "" {
		return "", false, nil
	}
	target := filepath.Join(destDir, filepath.FromSlash(relative))
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", false, err
	}
	if rel == "." {
		return "", false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("archive: entry %q escapes destination", name)
	}
	return target, true, nil
}

func writeEntry(target string, mode os.FileMode, e entry, b *budget) error {
	body, err := e.open()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return errors.Join(err, body.Close())
	}
	copyErr := b.copy(out, body)
	return errors.Join(copyErr, out.Close(), body.Close())
}

// writeAtomic writes through a temporary sibling and renames it, so a limit failure part
// way through never leaves a truncated executable at destPath.
func writeAtomic(destPath string, e entry, b *budget) error {
	tmp := destPath + ".partial"
	if err := writeEntry(tmp, binaryMode, e, b); err != nil {
		return errors.Join(err, removeIfExists(tmp))
	}
	if err := os.Rename(tmp, destPath); err != nil {
		return errors.Join(err, removeIfExists(tmp))
	}
	return nil
}

func removeIfExists(name string) error {
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// budget tracks the byte limits across one extraction. Every admitted byte is either
// written or skipped, so written + skipped + remaining == bytesMax throughout.
type budget struct {
	bytesMax  int64
	remaining int64
	entryMax  int64
	written   int64
	skipped   int64
	charged   bool // set when the current entry's bytes were counted
}

func newBudget(limits Limits) *budget {
	return &budget{
		bytesMax:  limits.BytesMax,
		remaining: limits.BytesMax,
		entryMax:  limits.EntryBytesMax,
	}
}

// assertBalanced checks the budget's books after an extraction: the bytes written stay
// within BytesMax, and every byte is accounted as written, skipped or remaining.
func (b *budget) assertBalanced() {
	assert.Thatf(b.written <= b.bytesMax,
		"archive: bytes written %d are within BytesMax %d", b.written, b.bytesMax)
	assert.That(b.remaining >= 0, "archive: remaining budget is not negative")
	assert.Thatf(b.written+b.skipped+b.remaining == b.bytesMax,
		"archive: every byte is accounted: written %d + skipped %d + remaining %d != %d",
		b.written, b.skipped, b.remaining, b.bytesMax)
}

// admit checks a declared entry size against the limits before any bytes are read.
func (b *budget) admit(size int64) error {
	if size < 0 {
		return fmt.Errorf("archive: negative entry size %d", size)
	}
	if size > b.entryMax {
		return fmt.Errorf("archive: entry size %d exceeds limit %d", size, b.entryMax)
	}
	if size > b.remaining {
		return fmt.Errorf("archive: total size exceeds limit")
	}
	return nil
}

// skip charges an entry that was not read. The tar reader still decompresses the skipped
// bytes, so they count against the total.
func (b *budget) skip(size int64) error {
	if err := b.admit(size); err != nil {
		return err
	}
	b.remaining -= size
	b.skipped += size
	assert.That(b.remaining >= 0, "archive: skip stays within the budget")
	return nil
}

// copy reads at most one byte past the tighter of the two limits, so overflow is detected
// without trusting any declared size.
func (b *budget) copy(dst io.Writer, src io.Reader) error {
	limit := min(b.entryMax, b.remaining)
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	b.charged = true
	if err != nil {
		return err
	}
	if n > b.entryMax {
		return fmt.Errorf("archive: entry exceeds limit %d bytes", b.entryMax)
	}
	if n > b.remaining {
		return fmt.Errorf("archive: total size exceeds limit")
	}
	b.remaining -= n
	b.written += n
	assert.That(b.remaining >= 0, "archive: copy stays within the budget")
	return nil
}
