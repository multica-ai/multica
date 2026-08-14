package plugincontract

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxArchiveSize   = 16 << 20
	MaxArtifactSize  = 32 << 20
	MaxArtifactFiles = 1024
	MaxFileSize      = 1 << 20
	MaxSkillSize     = 8 << 20
	MaxSkillFiles    = 256
)

type Artifact struct {
	Manifest          Manifest
	CanonicalManifest []byte
	ArchiveDigest     string
	ArtifactDigest    string
	SizeBytes         int64
	Files             []ArtifactFile
}

type ArtifactFile struct {
	Path      string
	Content   []byte
	Digest    string
	SizeBytes int64
}

// ValidateArtifact verifies an uploaded ZIP without extracting it to disk. It
// accepts only regular UTF-8 text files under declared contribution roots and
// returns content-addressed metadata suitable for immutable release storage.
func ValidateArtifact(archive []byte) (Artifact, error) {
	if len(archive) == 0 {
		return Artifact{}, fmt.Errorf("plugin archive is empty")
	}
	if len(archive) > MaxArchiveSize {
		return Artifact{}, fmt.Errorf("plugin archive exceeds %d bytes", MaxArchiveSize)
	}

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Artifact{}, fmt.Errorf("open plugin archive: %w", err)
	}

	files := make(map[string]ArtifactFile, len(reader.File))
	caseFolded := make(map[string]string, len(reader.File))
	var totalSize int64
	for _, zipped := range reader.File {
		name, isDirectory, err := validateArchivePath(zipped.Name)
		if err != nil {
			return Artifact{}, err
		}
		if isDirectory {
			continue
		}
		if isInstallHookPath(name) {
			return Artifact{}, fmt.Errorf("plugin archive path %q is an installation or build hook", name)
		}
		if len(files) >= MaxArtifactFiles {
			return Artifact{}, fmt.Errorf("plugin artifact exceeds %d files", MaxArtifactFiles)
		}
		if prior, exists := caseFolded[strings.ToLower(name)]; exists {
			return Artifact{}, fmt.Errorf("plugin archive path %q collides with %q", name, prior)
		}
		caseFolded[strings.ToLower(name)] = name

		mode := zipped.Mode()
		if !mode.IsRegular() {
			return Artifact{}, fmt.Errorf("plugin archive path %q is not a regular file", name)
		}
		if mode.Perm()&0o111 != 0 {
			return Artifact{}, fmt.Errorf("plugin archive path %q is executable", name)
		}
		if zipped.UncompressedSize64 > MaxFileSize {
			return Artifact{}, fmt.Errorf("plugin archive path %q exceeds %d bytes", name, MaxFileSize)
		}

		content, err := readZipFile(zipped)
		if err != nil {
			return Artifact{}, fmt.Errorf("read plugin archive path %q: %w", name, err)
		}
		if !utf8.Valid(content) {
			return Artifact{}, fmt.Errorf("plugin archive path %q is not UTF-8 text", name)
		}
		totalSize += int64(len(content))
		if totalSize > MaxArtifactSize {
			return Artifact{}, fmt.Errorf("plugin artifact exceeds %d uncompressed bytes", MaxArtifactSize)
		}
		files[name] = ArtifactFile{
			Path:      name,
			Content:   content,
			Digest:    DigestBytes(content),
			SizeBytes: int64(len(content)),
		}
	}

	manifestFile, ok := files[ManifestFilename]
	if !ok {
		return Artifact{}, fmt.Errorf("plugin archive is missing root %s", ManifestFilename)
	}
	manifest, canonicalManifest, err := ParseManifest(manifestFile.Content)
	if err != nil {
		return Artifact{}, err
	}
	totalSize += int64(len(canonicalManifest) - len(manifestFile.Content))
	manifestFile.Content = canonicalManifest
	manifestFile.Digest = DigestBytes(canonicalManifest)
	manifestFile.SizeBytes = int64(len(canonicalManifest))
	files[ManifestFilename] = manifestFile

	allowedRoots := make(map[string]AgentSkillContribution, len(manifest.Contributes.AgentSkills))
	for _, contribution := range manifest.Contributes.AgentSkills {
		root := "skills/" + contribution.Key + "/"
		allowedRoots[root] = contribution
		if _, ok := files[contribution.Entry]; !ok {
			return Artifact{}, fmt.Errorf("contribution %q entry %q is missing", contribution.Key, contribution.Entry)
		}
	}

	counts := make(map[string]int, len(allowedRoots))
	sizes := make(map[string]int64, len(allowedRoots))
	for name, file := range files {
		if name == ManifestFilename {
			continue
		}
		root := contributionRoot(name, allowedRoots)
		if root == "" {
			return Artifact{}, fmt.Errorf("plugin archive path %q is outside a declared contribution", name)
		}
		counts[root]++
		sizes[root] += file.SizeBytes
		if counts[root] > MaxSkillFiles {
			return Artifact{}, fmt.Errorf("contribution %q exceeds %d files", allowedRoots[root].Key, MaxSkillFiles)
		}
		if sizes[root] > MaxSkillSize {
			return Artifact{}, fmt.Errorf("contribution %q exceeds %d bytes", allowedRoots[root].Key, MaxSkillSize)
		}
	}

	ordered := make([]ArtifactFile, 0, len(files))
	for _, file := range files {
		ordered = append(ordered, file)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	return Artifact{
		Manifest:          manifest,
		CanonicalManifest: canonicalManifest,
		ArchiveDigest:     DigestBytes(archive),
		ArtifactDigest:    digestArtifactFiles(ordered),
		SizeBytes:         totalSize,
		Files:             ordered,
	}, nil
}

func isInstallHookPath(name string) bool {
	base := strings.ToLower(path.Base(name))
	switch base {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"install", "install.sh", "install.ps1", "install.cmd", "install.bat",
		"preinstall", "preinstall.sh", "preinstall.ps1", "preinstall.cmd", "preinstall.bat",
		"postinstall", "postinstall.sh", "postinstall.ps1", "postinstall.cmd", "postinstall.bat",
		"prepare", "prepare.sh", "prepare.ps1", "prepare.cmd", "prepare.bat",
		"build", "build.sh", "build.ps1", "build.cmd", "build.bat":
		return true
	default:
		return false
	}
}

func validateArchivePath(name string) (string, bool, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("plugin archive path %q is unsafe", name)
	}
	isDirectory := strings.HasSuffix(name, "/")
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || path.Clean(trimmed) != trimmed || trimmed == "." || strings.HasPrefix(trimmed, "../") {
		return "", false, fmt.Errorf("plugin archive path %q is unsafe", name)
	}
	return trimmed, isDirectory, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	content, err := io.ReadAll(io.LimitReader(reader, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > MaxFileSize {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxFileSize)
	}
	return content, nil
}

func contributionRoot(name string, roots map[string]AgentSkillContribution) string {
	for root := range roots {
		if strings.HasPrefix(name, root) {
			return root
		}
	}
	return ""
}

func digestArtifactFiles(files []ArtifactFile) string {
	hash := sha256.New()
	writeDigestPart(hash, "multica.plugin.artifact/v1")
	for _, file := range files {
		writeDigestPart(hash, file.Path)
		writeDigestPart(hash, file.Digest)
		writeDigestPart(hash, fmt.Sprintf("%d", file.SizeBytes))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func writeDigestPart(writer io.Writer, value string) {
	_, _ = fmt.Fprintf(writer, "%d:%s\n", len(value), value)
}

func DigestBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
