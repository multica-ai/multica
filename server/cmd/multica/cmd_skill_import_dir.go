package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

// Limits for `skill import --dir`. They mirror the server importer
// (maxImportFileSize / maxImportTotalSize / maxImportFileCount /
// maxImportArchiveUploadSize in internal/handler) and the browser folder
// picker (packages/core/skills/pack-archive.ts), so a folder the CLI accepts
// is one the server accepts, and an over-cap folder fails before upload.
const (
	skillDirMaxFileBytes    = 1 << 20  // per file, SKILL.md included
	skillDirMaxBundleBytes  = 8 << 20  // sum of supporting files
	skillDirMaxFileCount    = 512      // supporting files
	skillDirMaxArchiveBytes = 16 << 20 // compressed upload
)

// skillDirArchive is a local skill directory packed into an in-memory zip
// that the existing archive import endpoint accepts.
type skillDirArchive struct {
	data     []byte
	filename string
	// skipped lists supporting files left out because they exceed the
	// per-file cap. The server would drop them too; the CLI reports them.
	skipped []string
}

type skillDirMember struct {
	rel  string // slash-separated path relative to the skill directory
	size int64
}

// packSkillDirectory zips one local skill directory. The directory must hold
// SKILL.md at its root. Selection follows the server and browser importers:
// dotfiles, __MACOSX and license files are ignored, nested SKILL.md files and
// likely-binary assets are dropped, and supporting files over the per-file cap
// are skipped. Exceeding the file-count or bundle-size cap is an error.
//
// The directory is read through an os.Root, so a symbolic link may point at a
// regular file inside the directory but can never reach outside it. Links to
// directories and special files (devices, sockets, pipes) are rejected.
func packSkillDirectory(dir string) (*skillDirArchive, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("read skill directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory; use --file for a .skill/.zip archive", dir)
	}

	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open skill directory: %w", err)
	}
	defer root.Close()

	var (
		skillMd    *skillDirMember
		supporting []skillDirMember
	)
	walkErr := fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if isIgnoredSkillDirEntry(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		mode := d.Type()
		var size int64
		switch {
		case mode.IsDir():
			return nil
		case mode.IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			size = fi.Size()
		case mode&fs.ModeSymlink != 0:
			// Root.Stat follows the link but refuses to leave the root.
			fi, err := root.Stat(rel)
			if err != nil {
				return fmt.Errorf("%s: symbolic link cannot be followed inside the skill directory: %w", rel, err)
			}
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("%s: symbolic links are only supported for regular files", rel)
			}
			size = fi.Size()
		default:
			return fmt.Errorf("%s: unsupported file type %s", rel, mode.Type())
		}

		if strings.EqualFold(path.Base(rel), skillpkg.ContentFilename) {
			// Only the root SKILL.md is the skill's content; a nested one is
			// never a supporting file (the server drops it as well).
			if !strings.Contains(rel, "/") {
				skillMd = &skillDirMember{rel: rel, size: size}
			}
			return nil
		}
		supporting = append(supporting, skillDirMember{rel: rel, size: size})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("read skill directory: %w", walkErr)
	}
	if skillMd == nil {
		return nil, fmt.Errorf("%s does not contain a %s at its root", dir, skillpkg.ContentFilename)
	}
	if skillMd.size > skillDirMaxFileBytes {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", skillMd.rel, skillDirMaxFileBytes)
	}

	archive := &skillDirArchive{}
	selected := []skillDirMember{*skillMd}
	var count int
	var total int64
	for _, m := range supporting {
		if skillpkg.IsLikelyBinaryFilePath(m.rel) {
			continue
		}
		if m.size > skillDirMaxFileBytes {
			archive.skipped = append(archive.skipped, m.rel)
			continue
		}
		count++
		if count > skillDirMaxFileCount {
			return nil, fmt.Errorf("skill directory has more than %d supporting files", skillDirMaxFileCount)
		}
		total += m.size
		if total > skillDirMaxBundleBytes {
			return nil, fmt.Errorf("skill directory supporting files exceed the %d byte limit", skillDirMaxBundleBytes)
		}
		selected = append(selected, m)
	}

	// Wrap entries in a single top-level folder named after the directory:
	// the server falls back to that name when SKILL.md has no name field.
	base := filepath.Base(abs)
	prefix := ""
	if base != "" && base != "." && base != string(filepath.Separator) {
		prefix = base + "/"
	}
	archive.filename = "skill.zip"
	if prefix != "" {
		archive.filename = base + ".zip"
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range selected {
		content, err := readSkillDirFile(root, m.rel)
		if err != nil {
			return nil, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: prefix + m.rel, Method: zip.Deflate})
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", m.rel, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("pack %s: %w", m.rel, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("pack skill directory: %w", err)
	}
	if buf.Len() > skillDirMaxArchiveBytes {
		return nil, fmt.Errorf("packed skill directory is %d bytes, over the %d byte upload limit", buf.Len(), skillDirMaxArchiveBytes)
	}
	archive.data = buf.Bytes()
	return archive, nil
}

// readSkillDirFile reads one selected file, re-checking the per-file cap so a
// file that grew after the walk cannot exceed it.
func readSkillDirFile(root *os.Root, rel string) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, skillDirMaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if len(content) > skillDirMaxFileBytes {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", rel, skillDirMaxFileBytes)
	}
	return content, nil
}

// isIgnoredSkillDirEntry mirrors the server's isIgnoredArchiveEntry: dotfiles
// and dot-directories (.git, .DS_Store), __MACOSX and license files are never
// part of an imported skill.
func isIgnoredSkillDirEntry(rel string) bool {
	segments := strings.Split(rel, "/")
	for _, seg := range segments {
		if seg == "" || seg == "__MACOSX" || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	switch strings.ToLower(segments[len(segments)-1]) {
	case "license", "license.md", "license.txt":
		return true
	}
	return false
}

var errSkillImportSourceFlags = errors.New("exactly one of --url, --file or --dir is required")
