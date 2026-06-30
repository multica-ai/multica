package skillbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

const (
	SourceWorkspace = "workspace"
	SourceBuiltin   = "builtin"
)

type File struct {
	Path    string
	Content string
}

type Skill struct {
	ID          string
	Source      string
	Name        string
	Description string
	Content     string
	Files       []File
}

type FileRef struct {
	Path      string
	SHA256    string
	SizeBytes int64
}

type Manifest struct {
	Hash      string
	SizeBytes int64
	FileCount int
	Files     []FileRef
}

func BuildManifest(skill Skill) Manifest {
	files := append([]File(nil), skill.Files...)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	h := sha256.New()
	writeHashPart(h, "v1")
	writeHashPart(h, skill.Source)
	writeHashPart(h, skill.ID)
	writeHashPart(h, skill.Name)
	writeHashPart(h, skill.Description)
	writeHashPart(h, skill.Content)

	size := int64(len(skill.Content))
	refs := make([]FileRef, 0, len(files))
	for _, file := range files {
		fileHash := sha256.Sum256([]byte(file.Content))
		fileDigest := "sha256:" + hex.EncodeToString(fileHash[:])
		writeHashPart(h, file.Path)
		writeHashPart(h, fileDigest)
		writeHashPart(h, file.Content)
		size += int64(len(file.Content))
		refs = append(refs, FileRef{
			Path:      file.Path,
			SHA256:    fileDigest,
			SizeBytes: int64(len(file.Content)),
		})
	}

	return Manifest{
		Hash:      "sha256:" + hex.EncodeToString(h.Sum(nil)),
		SizeBytes: size,
		FileCount: len(files),
		Files:     refs,
	}
}

func writeHashPart(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = fmt.Fprintf(h, "%d:%s\n", len(value), value)
}
