package corpustransfer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	EmbeddedManifestName      = "manifest.json"
	MaxArchiveEntries         = 100_000
	MaxTotalUncompressedBytes = int64(64 << 30)
	maxEmbeddedManifestBytes  = int64(16 << 20)
)

type ArchiveEnvelope struct {
	Format         string `json:"format"`
	Filename       string `json:"filename"`
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func BuildZIP(stagingDir, destination string, manifest Manifest) (ArchiveEnvelope, error) {
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("create archive directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".corpus-*.zip.tmp")
	if err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("create archive temp file: %w", err)
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("secure archive temp file: %w", err)
	}
	zw := zip.NewWriter(tmp)
	manifestHeader := &zip.FileHeader{Name: EmbeddedManifestName, Method: zip.Deflate, Modified: manifest.CreatedAt}
	manifestHeader.SetMode(0o600)
	mw, err := zw.CreateHeader(manifestHeader)
	if err != nil {
		return ArchiveEnvelope{}, err
	}
	if _, err := mw.Write(manifestJSON); err != nil {
		return ArchiveEnvelope{}, err
	}
	for _, entry := range manifest.Entries {
		filename := filepath.Join(stagingDir, filepath.FromSlash(entry.Path))
		info, err := os.Lstat(filename)
		if err != nil {
			return ArchiveEnvelope{}, fmt.Errorf("inspect staged file %q: %w", entry.Path, err)
		}
		if !info.Mode().IsRegular() {
			return ArchiveEnvelope{}, fmt.Errorf("staged entry %q is not a regular file", entry.Path)
		}
		in, err := os.Open(filename)
		if err != nil {
			return ArchiveEnvelope{}, fmt.Errorf("open staged file %q: %w", entry.Path, err)
		}
		header := &zip.FileHeader{Name: entry.Path, Method: zip.Deflate, Modified: entry.Mtime}
		header.SetMode(0o600)
		out, err := zw.CreateHeader(header)
		if err == nil {
			h := sha256.New()
			var n int64
			n, err = io.Copy(io.MultiWriter(out, h), in)
			if err == nil && n != entry.SizeBytes {
				err = fmt.Errorf("staged size changed for %q", entry.Path)
			}
			if err == nil && hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
				err = fmt.Errorf("staged sha256 changed for %q", entry.Path)
			}
		}
		closeErr := in.Close()
		if err != nil {
			return ArchiveEnvelope{}, err
		}
		if closeErr != nil {
			return ArchiveEnvelope{}, closeErr
		}
	}
	if err := zw.Close(); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("close zip: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("sync archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("close archive: %w", err)
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return ArchiveEnvelope{}, fmt.Errorf("publish archive: %w", err)
	}
	keep = true
	return envelopeForArchive(destination, manifestJSON)
}

func InspectZIP(archivePath string, source SourceInfo) (Manifest, ArchiveEnvelope, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return Manifest{}, ArchiveEnvelope{}, fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	validated, err := validateZIPStructure(zr.File)
	if err != nil {
		return Manifest{}, ArchiveEnvelope{}, err
	}
	archiveDigest, archiveSize, err := hashFile(archivePath)
	if err != nil {
		return Manifest{}, ArchiveEnvelope{}, err
	}
	var embedded []byte
	embeddedPresent := false
	entries := make([]Entry, 0, len(validated))
	var total int64
	for _, item := range validated {
		file := item.file
		clean := item.clean
		if item.directory {
			continue
		}
		if clean == EmbeddedManifestName {
			embeddedPresent = true
			embedded, err = readZipEntry(file, maxEmbeddedManifestBytes)
			if err != nil {
				return Manifest{}, ArchiveEnvelope{}, fmt.Errorf("read embedded manifest: %w", err)
			}
			continue
		}
		if file.UncompressedSize64 > uint64(MaxTotalUncompressedBytes-total) {
			return Manifest{}, ArchiveEnvelope{}, fmt.Errorf("archive uncompressed total exceeds limit")
		}
		digest, size, err := hashZipEntry(file)
		if err != nil {
			return Manifest{}, ArchiveEnvelope{}, err
		}
		total += size
		entries = append(entries, Entry{
			Path:       clean,
			SourceType: source.Type,
			SizeBytes:  size,
			Mtime:      file.Modified.UTC(),
			SHA256:     digest,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	resetReplicaLinks(entries)
	createdAt := time.Unix(0, 0).UTC()
	for _, entry := range entries {
		if entry.Mtime.After(createdAt) {
			createdAt = entry.Mtime
		}
	}
	manifest := Manifest{
		SchemaVersion:          ManifestSchemaVersion,
		PackageID:              uuid.NewSHA1(uuid.NameSpaceURL, []byte("corpus-zip:"+archiveDigest)).String(),
		CreatedAt:              createdAt,
		Source:                 source,
		Entries:                entries,
		EntryCount:             len(entries),
		TotalUncompressedBytes: total,
	}
	if embeddedPresent {
		if err := json.Unmarshal(embedded, &manifest); err != nil {
			return Manifest{}, ArchiveEnvelope{}, fmt.Errorf("decode embedded manifest: %w", err)
		}
		if err := manifest.Validate(); err != nil {
			return Manifest{}, ArchiveEnvelope{}, err
		}
		if err := compareManifestEntries(manifest.Entries, entries); err != nil {
			return Manifest{}, ArchiveEnvelope{}, err
		}
	} else if err := manifest.Validate(); err != nil {
		return Manifest{}, ArchiveEnvelope{}, err
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		return Manifest{}, ArchiveEnvelope{}, err
	}
	manifestDigest := sha256.Sum256(manifestJSON)
	return manifest, ArchiveEnvelope{
		Format: "zip", Filename: filepath.Base(archivePath), SizeBytes: archiveSize,
		SHA256: archiveDigest, ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
	}, nil
}

func compareManifestEntries(expected, actual []Entry) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("embedded manifest has %d entries, archive has %d", len(expected), len(actual))
	}
	byPath := make(map[string]Entry, len(actual))
	for _, entry := range actual {
		byPath[entry.Path] = entry
	}
	for _, entry := range expected {
		got, ok := byPath[entry.Path]
		if !ok || got.SizeBytes != entry.SizeBytes || got.SHA256 != entry.SHA256 {
			return fmt.Errorf("embedded manifest mismatch for %q", entry.Path)
		}
	}
	return nil
}

func readZipEntry(file *zip.File, max int64) ([]byte, error) {
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("entry exceeds %d bytes", max)
	}
	return body, nil
}

func hashZipEntry(file *zip.File) (string, int64, error) {
	r, err := file.Open()
	if err != nil {
		return "", 0, fmt.Errorf("open archive entry %q: %w", file.Name, err)
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, MaxTotalUncompressedBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("hash archive entry %q: %w", file.Name, err)
	}
	if n != int64(file.UncompressedSize64) {
		return "", 0, fmt.Errorf("archive entry %q size mismatch", file.Name)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func envelopeForArchive(filename string, manifestJSON []byte) (ArchiveEnvelope, error) {
	digest, size, err := hashFile(filename)
	if err != nil {
		return ArchiveEnvelope{}, err
	}
	manifestDigest := sha256.Sum256(manifestJSON)
	return ArchiveEnvelope{
		Format:         "zip",
		Filename:       filepath.Base(filename),
		SizeBytes:      size,
		SHA256:         digest,
		ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
	}, nil
}

func hashFile(filename string) (string, int64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", 0, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("hash archive: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func validateArchivePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe path")
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("unsafe drive path")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("unsafe path")
	}
	return clean, nil
}

func validateZIPEntryPath(file *zip.File) (string, bool, error) {
	directory := strings.HasSuffix(file.Name, "/")
	name := file.Name
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	clean, err := validateArchivePath(name)
	if err != nil || clean != name {
		return "", false, fmt.Errorf("unsafe path")
	}
	if directory {
		clean += "/"
	}
	return clean, directory, nil
}

type validatedZIPEntry struct {
	file      *zip.File
	clean     string
	directory bool
}

func validateZIPStructure(files []*zip.File) ([]validatedZIPEntry, error) {
	if len(files) > MaxArchiveEntries+1 {
		return nil, fmt.Errorf("archive has too many entries")
	}
	validated := make([]validatedZIPEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	collisions := make(map[string]string, len(files))
	embeddedManifestPresent := false
	for _, file := range files {
		clean, directory, err := validateZIPEntryPath(file)
		if err != nil {
			return nil, fmt.Errorf("unsafe archive entry %q", file.Name)
		}
		if _, ok := seen[clean]; ok {
			return nil, fmt.Errorf("duplicate archive entry %q", clean)
		}
		seen[clean] = struct{}{}
		key := archiveCollisionKey(strings.TrimSuffix(clean, "/"))
		if prior, ok := collisions[key]; ok {
			return nil, fmt.Errorf("archive path collision between %q and %q", prior, clean)
		}
		collisions[key] = clean
		if directory {
			if !file.Mode().IsDir() {
				return nil, fmt.Errorf("archive entry %q is not a directory", file.Name)
			}
		} else if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("archive entry %q is not a regular file", file.Name)
		}
		if !directory && clean == EmbeddedManifestName {
			embeddedManifestPresent = true
		}
		validated = append(validated, validatedZIPEntry{file: file, clean: clean, directory: directory})
	}
	if len(files) > MaxArchiveEntries && !embeddedManifestPresent {
		return nil, fmt.Errorf("archive has too many entries")
	}
	return validated, nil
}

func archiveCollisionKey(name string) string {
	return cases.Fold().String(norm.NFC.String(name))
}
