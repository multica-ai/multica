package corpustransfer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func ExtractVerified(archivePath, destination string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".corpus-receive-*")
	if err != nil {
		return fmt.Errorf("create receive staging directory: %w", err)
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("secure receive staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	validated, err := validateZIPStructure(zr.File)
	if err != nil {
		return err
	}
	expected := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		expected[entry.Path] = entry
	}
	if err := validateExpectedArchiveEntries(validated, expected); err != nil {
		return err
	}
	var total int64
	for _, item := range validated {
		file := item.file
		clean := item.clean
		if item.directory {
			continue
		}
		if clean == EmbeddedManifestName {
			continue
		}
		entry := expected[clean]
		if entry.SizeBytes > manifest.TotalUncompressedBytes-total {
			return fmt.Errorf("archive total exceeds manifest total")
		}
		target := filepath.Join(staging, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create extracted path: %w", err)
		}
		r, err := file.Open()
		if err != nil {
			return fmt.Errorf("open archive entry %q: %w", clean, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("create extracted file %q: %w", clean, err)
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, entry.SizeBytes+1))
		closeReadErr := r.Close()
		syncErr := out.Sync()
		closeWriteErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract %q: %w", clean, copyErr)
		}
		if closeReadErr != nil || syncErr != nil || closeWriteErr != nil {
			return fmt.Errorf("close extracted file %q", clean)
		}
		if n != entry.SizeBytes {
			return fmt.Errorf("size mismatch for %q: got %d, want %d", clean, n, entry.SizeBytes)
		}
		if digest := hex.EncodeToString(h.Sum(nil)); digest != entry.SHA256 {
			return fmt.Errorf("sha256 mismatch for %q", clean)
		}
		if err := os.Chtimes(target, entry.Mtime, entry.Mtime); err != nil {
			return fmt.Errorf("set extracted mtime: %w", err)
		}
		total += n
	}
	if total != manifest.TotalUncompressedBytes {
		return fmt.Errorf("archive total %d does not match manifest total %d", total, manifest.TotalUncompressedBytes)
	}
	if err := os.Rename(staging, destination); err != nil {
		return fmt.Errorf("publish extracted package: %w", err)
	}
	published = true
	return nil
}

func validateExpectedArchiveEntries(validated []validatedZIPEntry, expected map[string]Entry) error {
	seen := make(map[string]struct{}, len(expected))
	for _, item := range validated {
		if item.directory || item.clean == EmbeddedManifestName {
			continue
		}
		entry, ok := expected[item.clean]
		if !ok {
			return fmt.Errorf("archive entry %q is absent from manifest", item.clean)
		}
		if item.file.UncompressedSize64 != uint64(entry.SizeBytes) {
			return fmt.Errorf("size mismatch for %q: archive header has %d, want %d", item.clean, item.file.UncompressedSize64, entry.SizeBytes)
		}
		seen[item.clean] = struct{}{}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("archive contains %d manifest entries, want %d", len(seen), len(expected))
	}
	return nil
}
