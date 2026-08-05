package corpustransfer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
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

// VerifyExtracted revalidates a previously installed package so a receive
// command can safely retry an ACK without downloading or publishing it again.
func VerifyExtracted(destination string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(destination)
	if err != nil {
		return fmt.Errorf("inspect installed package: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installed package is not a directory")
	}
	expected := make(map[string]Entry, len(manifest.Entries))
	allowedDirs := map[string]struct{}{".": {}}
	for _, entry := range manifest.Entries {
		expected[entry.Path] = entry
		for dir := path.Dir(entry.Path); dir != "."; dir = path.Dir(dir) {
			allowedDirs[dir] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(expected))
	err = filepath.WalkDir(destination, func(filename string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(destination, filename)
		if err != nil {
			return err
		}
		archivePath := filepath.ToSlash(relative)
		if archivePath == "." {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if _, ok := allowedDirs[archivePath]; !ok {
				return fmt.Errorf("unexpected installed directory %q", archivePath)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("installed entry %q is not a regular file", archivePath)
		}
		entry, ok := expected[archivePath]
		if !ok {
			return fmt.Errorf("unexpected installed file %q", archivePath)
		}
		if err := verifyInstalledFile(filename, archivePath, info, entry); err != nil {
			return err
		}
		seen[archivePath] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("installed package contains %d manifest files, want %d", len(seen), len(expected))
	}
	return nil
}

func verifyInstalledFile(filename, archivePath string, before os.FileInfo, expected Entry) error {
	if before.Size() != expected.SizeBytes {
		return fmt.Errorf("size mismatch for installed entry %q", archivePath)
	}
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open installed entry %q: %w", archivePath, err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat installed entry %q: %w", archivePath, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !sameFileSnapshot(before, opened) {
		_ = file.Close()
		return fmt.Errorf("installed entry changed before verification: %q", archivePath)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, io.LimitReader(file, expected.SizeBytes+1))
	closeErr := file.Close()
	after, statErr := os.Lstat(filename)
	if copyErr != nil {
		return fmt.Errorf("hash installed entry %q: %w", archivePath, copyErr)
	}
	if closeErr != nil || statErr != nil {
		return fmt.Errorf("close or restat installed entry %q", archivePath)
	}
	if !os.SameFile(before, after) || !sameFileSnapshot(before, after) {
		return fmt.Errorf("installed entry changed during verification: %q", archivePath)
	}
	if size != expected.SizeBytes {
		return fmt.Errorf("size mismatch for installed entry %q", archivePath)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != expected.SHA256 {
		return fmt.Errorf("sha256 mismatch for installed entry %q", archivePath)
	}
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
