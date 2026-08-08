package corpustransfer

import (
	"context"
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
)

const ManifestSchemaVersion = "corpus-manifest.v1"

type SourceRoot struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Path string `json:"-"`
}

type SourceInfo struct {
	Adapter string `json:"adapter"`
	Version string `json:"version,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
}

type Entry struct {
	Path       string    `json:"path"`
	SourceType string    `json:"source_type"`
	SizeBytes  int64     `json:"size_bytes"`
	Mtime      time.Time `json:"mtime"`
	SHA256     string    `json:"sha256"`
	ReplicaOf  string    `json:"replica_of,omitempty"`
}

type Manifest struct {
	SchemaVersion          string       `json:"schema_version"`
	PackageID              string       `json:"package_id"`
	CreatedAt              time.Time    `json:"created_at"`
	Source                 SourceInfo   `json:"source"`
	Entries                []Entry      `json:"entries"`
	EntryCount             int          `json:"entry_count"`
	TotalUncompressedBytes int64        `json:"total_uncompressed_bytes"`
	MissingSources         []string     `json:"missing_sources,omitempty"`
	Roots                  []SourceRoot `json:"roots,omitempty"`
}

func (m Manifest) CanonicalJSON() ([]byte, error) {
	clone := m
	clone.Entries = append([]Entry(nil), m.Entries...)
	clone.MissingSources = append([]string(nil), m.MissingSources...)
	clone.Roots = append([]SourceRoot(nil), m.Roots...)
	sort.Slice(clone.Entries, func(i, j int) bool { return clone.Entries[i].Path < clone.Entries[j].Path })
	sort.Strings(clone.MissingSources)
	sort.Slice(clone.Roots, func(i, j int) bool {
		if clone.Roots[i].Type != clone.Roots[j].Type {
			return clone.Roots[i].Type < clone.Roots[j].Type
		}
		return clone.Roots[i].Name < clone.Roots[j].Name
	})
	if err := clone.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(clone, "", "  ")
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported manifest schema %q", m.SchemaVersion)
	}
	if !validPathComponent(m.PackageID) || strings.TrimSpace(m.PackageID) != m.PackageID {
		return fmt.Errorf("manifest package_id must be a safe path component")
	}
	if m.EntryCount != len(m.Entries) {
		return fmt.Errorf("manifest entry_count %d does not match %d entries", m.EntryCount, len(m.Entries))
	}
	if m.EntryCount > MaxArchiveEntries {
		return fmt.Errorf("manifest has too many entries: %d", m.EntryCount)
	}
	var total int64
	seen := make(map[string]Entry, len(m.Entries))
	collisions := make(map[string]string, len(m.Entries))
	for _, entry := range m.Entries {
		clean, err := validateArchivePath(entry.Path)
		if err != nil || clean != entry.Path {
			return fmt.Errorf("unsafe manifest path %q: %w", entry.Path, err)
		}
		if _, ok := seen[entry.Path]; ok {
			return fmt.Errorf("duplicate manifest path %q", entry.Path)
		}
		key := archiveCollisionKey(entry.Path)
		if key == archiveCollisionKey(EmbeddedManifestName) {
			return fmt.Errorf("manifest path %q is reserved", entry.Path)
		}
		if prior, ok := collisions[key]; ok {
			return fmt.Errorf("manifest path collision between %q and %q", prior, entry.Path)
		}
		collisions[key] = entry.Path
		if entry.SizeBytes < 0 {
			return fmt.Errorf("negative size for %q", entry.Path)
		}
		if !validSHA256(entry.SHA256) {
			return fmt.Errorf("invalid sha256 for %q", entry.Path)
		}
		if entry.ReplicaOf != "" {
			original, ok := seen[entry.ReplicaOf]
			if !ok {
				return fmt.Errorf("replica_of for %q does not name an earlier entry", entry.Path)
			}
			if original.SizeBytes != entry.SizeBytes || original.SHA256 != entry.SHA256 {
				return fmt.Errorf("replica_of for %q does not match %q", entry.Path, entry.ReplicaOf)
			}
		}
		seen[entry.Path] = entry
		if entry.SizeBytes > MaxTotalUncompressedBytes-total {
			return fmt.Errorf("manifest total exceeds limit")
		}
		total += entry.SizeBytes
	}
	if total != m.TotalUncompressedBytes {
		return fmt.Errorf("manifest total %d does not match entry total %d", m.TotalUncompressedBytes, total)
	}
	return nil
}

func StageAndBuildManifest(ctx context.Context, roots []SourceRoot, cutoff time.Time, stagingDir string) (Manifest, error) {
	if len(roots) == 0 {
		return Manifest{}, fmt.Errorf("at least one source root is required")
	}
	if err := os.MkdirAll(filepath.Dir(stagingDir), 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create staging parent: %w", err)
	}
	if err := os.Mkdir(stagingDir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create exclusive staging directory: %w", err)
	}
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("secure staging directory: %w", err)
	}

	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		PackageID:     uuid.NewString(),
		CreatedAt:     time.Now().UTC(),
		Source:        SourceInfo{Adapter: "path", Version: "v1", Type: "mixed", Name: "local"},
		Roots:         append([]SourceRoot(nil), roots...),
	}
	firstByDigest := make(map[string]string)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		if !validPathComponent(root.Type) || !validPathComponent(root.Name) {
			return Manifest{}, fmt.Errorf("unsafe source identity %q/%q", root.Type, root.Name)
		}
		rootInfo, err := os.Lstat(root.Path)
		if os.IsNotExist(err) {
			manifest.MissingSources = append(manifest.MissingSources, root.Type+"/"+root.Name+":"+root.Path)
			continue
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("inspect source root %s: %w", root.Path, err)
		}
		if !rootInfo.IsDir() {
			return Manifest{}, fmt.Errorf("source root is not a directory: %s", root.Path)
		}

		err = walkSourceRoot(root.Path, func(rel string, info os.FileInfo, in *os.File) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
				return nil
			}
			archivePath, err := validateArchivePath(path.Join(root.Type, root.Name, filepath.ToSlash(rel)))
			if err != nil {
				return err
			}
			destination := filepath.Join(stagingDir, filepath.FromSlash(archivePath))
			filename := filepath.Join(root.Path, rel)
			digest, size, err := copyOpenedSourceFile(filename, destination, info, in)
			if err != nil {
				return err
			}
			entry := Entry{
				Path:       archivePath,
				SourceType: root.Type,
				SizeBytes:  size,
				Mtime:      info.ModTime().UTC(),
				SHA256:     digest,
			}
			if first, ok := firstByDigest[digest]; ok {
				entry.ReplicaOf = first
			} else {
				firstByDigest[digest] = archivePath
			}
			manifest.Entries = append(manifest.Entries, entry)
			manifest.TotalUncompressedBytes += size
			return nil
		})
		if err != nil {
			return Manifest{}, fmt.Errorf("scan source %s: %w", root.Path, err)
		}
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	resetReplicaLinks(manifest.Entries)
	manifest.EntryCount = len(manifest.Entries)
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func copySourceFile(source, destination string, before os.FileInfo) (string, int64, error) {
	in, err := openSourceNoFollow(source)
	if err != nil {
		return "", 0, fmt.Errorf("open source %s: %w", source, err)
	}
	defer in.Close()
	return copyOpenedSourceFile(source, destination, before, in)
}

func copyOpenedSourceFile(source, destination string, before os.FileInfo, in *os.File) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", 0, fmt.Errorf("create staging path: %w", err)
	}
	opened, err := in.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat opened source %s: %w", source, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !sameFileSnapshot(before, opened) {
		return "", 0, fmt.Errorf("source changed before staging: %s", source)
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("create staged file: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if err != nil {
		return "", 0, fmt.Errorf("stage source %s: %w", source, err)
	}
	if err := out.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync staged file: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", 0, fmt.Errorf("close staged file: %w", err)
	}
	afterHandle, err := in.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("restat source: %w", err)
	}
	afterPath, err := os.Lstat(source)
	if err != nil {
		return "", 0, fmt.Errorf("restat source path: %w", err)
	}
	if !os.SameFile(before, afterHandle) || !os.SameFile(before, afterPath) ||
		n != before.Size() || !sameFileSnapshot(before, afterHandle) || !sameFileSnapshot(before, afterPath) {
		return "", 0, fmt.Errorf("source changed while staging: %s", source)
	}
	if err := os.Chtimes(destination, before.ModTime(), before.ModTime()); err != nil {
		return "", 0, fmt.Errorf("set staged mtime: %w", err)
	}
	ok = true
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func sameFileSnapshot(expected, actual os.FileInfo) bool {
	return expected.Size() == actual.Size() &&
		expected.Mode() == actual.Mode() &&
		expected.ModTime().Equal(actual.ModTime())
}

func validPathComponent(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\\x00")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func resetReplicaLinks(entries []Entry) {
	firstByDigest := make(map[string]string, len(entries))
	for i := range entries {
		entries[i].ReplicaOf = ""
		if first, ok := firstByDigest[entries[i].SHA256]; ok {
			entries[i].ReplicaOf = first
		} else {
			firstByDigest[entries[i].SHA256] = entries[i].Path
		}
	}
}
