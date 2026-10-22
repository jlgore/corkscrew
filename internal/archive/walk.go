package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

type kind int

const (
	kindFile kind = iota + 1
	kindDir
)

// entry is one archive member. open may be called at most once.
type entry struct {
	name string
	kind kind
	size int64 // declared uncompressed size
	open func() (io.ReadCloser, error)
}

// visitFunc handles one entry. Returning done stops the walk without error.
type visitFunc func(e entry, b *budget) (done bool, err error)

func walk(format Format, archivePath string, limits Limits, visit visitFunc) error {
	if err := limits.validate(); err != nil {
		return err
	}
	b := newBudget(limits)
	var err error
	switch format {
	case FormatTarGz:
		err = walkTarGz(archivePath, limits.EntryCountMax, b, visit)
	case FormatZip:
		err = walkZip(archivePath, limits.EntryCountMax, b, visit)
	default:
		return fmt.Errorf("archive: unknown format %d", format)
	}
	// Limit failures return before charging, so the books balance on every path.
	b.assertBalanced()
	return err
}

func walkTarGz(archivePath string, entryCountMax int, b *budget, visit visitFunc) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	err = walkTar(tar.NewReader(gzr), entryCountMax, b, visit)
	return errors.Join(err, gzr.Close(), file.Close())
}

func walkTar(tr *tar.Reader, entryCountMax int, b *budget, visit visitFunc) error {
	for count := 0; ; count++ {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if count >= entryCountMax {
			return fmt.Errorf("archive: more than %d entries", entryCountMax)
		}
		e := entry{name: header.Name, size: header.Size}
		e.open = func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
		switch header.Typeflag {
		case tar.TypeReg:
			e.kind = kindFile
		case tar.TypeDir:
			e.kind = kindDir
		case tar.TypeXGlobalHeader:
			// git archive (and so every GitHub tarball) starts with a PAX global header;
			// the tar reader has already applied it.
			continue
		default:
			return fmt.Errorf("archive: entry %q has unsupported type %q",
				header.Name, header.Typeflag)
		}
		if err := b.admit(e.size); err != nil {
			return fmt.Errorf("%w: %q", err, header.Name)
		}
		b.charged = false
		done, err := visit(e, b)
		if err != nil || done {
			return err
		}
		if !b.charged {
			if err := b.skip(e.size); err != nil {
				return err
			}
		}
	}
}

func walkZip(archivePath string, entryCountMax int, b *budget, visit visitFunc) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	err = walkZipFiles(r.File, entryCountMax, b, visit)
	return errors.Join(err, r.Close())
}

func walkZipFiles(files []*zip.File, entryCountMax int, b *budget, visit visitFunc) error {
	if len(files) > entryCountMax {
		return fmt.Errorf("archive: more than %d entries", entryCountMax)
	}
	for _, f := range files {
		mode := f.Mode()
		e := entry{name: f.Name, open: f.Open}
		switch {
		case mode.IsRegular():
			e.kind = kindFile
		case mode.IsDir():
			e.kind = kindDir
		default:
			return fmt.Errorf("archive: entry %q has unsupported mode %s", f.Name, mode)
		}
		// The declared size is attacker-controlled; it only rejects early. copy enforces
		// the real limit on the bytes actually decompressed.
		if f.UncompressedSize64 > math.MaxInt64 {
			return fmt.Errorf("archive: entry %q size overflows", f.Name)
		}
		e.size = int64(f.UncompressedSize64)
		if err := b.admit(e.size); err != nil {
			return fmt.Errorf("%w: %q", err, f.Name)
		}
		// Unread zip entries are never decompressed, so they cost nothing to skip.
		done, err := visit(e, b)
		if err != nil || done {
			return err
		}
	}
	return nil
}
