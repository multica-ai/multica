package managedruntime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxArchiveEntries = 100_000

func extractArchive(archivePath, destination string, maxExtractedBytes int64) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZip(archivePath, destination, maxExtractedBytes)
	}
	// Download temp files intentionally have no extension. Detect ZIP from the
	// magic bytes and otherwise treat the provider's supported archive as gzip.
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	var magic [4]byte
	n, readErr := io.ReadFull(f, magic[:])
	_ = f.Close()
	if readErr == nil && n == len(magic) && string(magic[:]) == "PK\x03\x04" {
		return extractZip(archivePath, destination, maxExtractedBytes)
	}
	return extractTarGz(archivePath, destination, maxExtractedBytes)
}

func extractTarGz(archivePath, destination string, maxExtractedBytes int64) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var total int64
	for entries := 0; ; entries++ {
		if entries >= maxArchiveEntries {
			return fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
		}
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		target, err := safeArchiveTarget(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxExtractedBytes-total {
				return fmt.Errorf("archive expands beyond %d-byte limit", maxExtractedBytes)
			}
			total += header.Size
			if err := writeArchiveFile(target, tr, header.Size, os.FileMode(header.Mode)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
	}
}

func extractZip(archivePath, destination string, maxExtractedBytes int64) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
	}
	var total int64
	for _, entry := range zr.File {
		target, err := safeArchiveTarget(destination, entry.Name)
		if err != nil {
			return err
		}
		info := entry.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive entry %q is a symlink", entry.Name)
		}
		if info.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archive entry %q is not a regular file", entry.Name)
		}
		size := int64(entry.UncompressedSize64)
		if size < 0 || size > maxExtractedBytes-total {
			return fmt.Errorf("archive expands beyond %d-byte limit", maxExtractedBytes)
		}
		total += size
		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", entry.Name, err)
		}
		writeErr := writeArchiveFile(target, rc, size, info.Mode())
		closeErr := rc.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeArchiveTarget(root, name string) (string, error) {
	normalized := filepath.FromSlash(name)
	if normalized == "" || filepath.IsAbs(normalized) {
		return "", fmt.Errorf("archive entry %q has an unsafe path", name)
	}
	clean := filepath.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}

func writeArchiveFile(target string, src io.Reader, size int64, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create archive entry %q: %w", target, err)
	}
	written, copyErr := io.CopyN(f, src, size)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write archive entry %q: %w", target, copyErr)
	}
	if written != size {
		return fmt.Errorf("archive entry %q size mismatch", target)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}
