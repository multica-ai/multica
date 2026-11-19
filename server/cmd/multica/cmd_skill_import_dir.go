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

// Limits for `skill import --dir`. They mirror the server's archive import
// caps (maxImportFileSize, maxImportTotalSize and maxImportFileCount in
// server/internal/handler) so an oversized folder fails before it is read into
// memory and uploaded. Within these caps the packed archive stays well under
// the server's 16 MiB upload limit.
const (
	skillDirMaxFileSize  = 1 << 20 // per file, SKILL.md included
	skillDirMaxTotalSize = 8 << 20 // sum of supporting files
	skillDirMaxFileCount = 512     // supporting files
)

// packSkillDir zips a local skill folder into the archive layout the server's
// archive import accepts: every file under one wrapper directory named after
// the folder, so a SKILL.md without a name field falls back to the folder name.
//
// The selection rules match the server importer and the web folder import:
// dot-files and dot-directories, __MACOSX and LICENSE files are left out, a
// nested SKILL.md is not a supporting file, and binary or oversized supporting
// files are skipped (each skip is reported on notes) rather than failing.
// Exceeding the file-count or total-size cap fails, as it does on the server.
//
// The folder is read through os.Root, so a symlink is followed only while its
// target stays inside the folder. A symlink to a directory, or any entry that
// is not a regular file, is rejected.
func packSkillDir(dir string, notes io.Writer) (data []byte, filename string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve skill folder: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, "", fmt.Errorf("read skill folder: %w", err)
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("%s is not a folder; use --file to import a .skill or .zip archive", dir)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, "", fmt.Errorf("open skill folder: %w", err)
	}
	defer root.Close()

	var members []string
	hasSkillMd := false
	supportingCount, supportingBytes := 0, int64(0)
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
		if d.IsDir() {
			return nil
		}
		// Stat follows a symlink only while it resolves inside the root.
		info, err := root.Stat(filepath.FromSlash(rel))
		if err != nil {
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("%s is a link that points outside the folder or to a missing file", rel)
			}
			return fmt.Errorf("%s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file; links to folders and special files cannot be imported", rel)
		}

		if strings.EqualFold(path.Base(rel), skillpkg.ContentFilename) {
			if strings.Contains(rel, "/") {
				return nil // a nested SKILL.md is never a supporting file
			}
			if info.Size() > skillDirMaxFileSize {
				return fmt.Errorf("%s is %d bytes, over the 1 MiB per-file import limit", rel, info.Size())
			}
			hasSkillMd = true
			members = append(members, rel)
			return nil
		}
		if skillpkg.IsLikelyBinaryFilePath(rel) {
			fmt.Fprintf(notes, "Skipping %s: binary files are not imported\n", rel)
			return nil
		}
		if info.Size() > skillDirMaxFileSize {
			fmt.Fprintf(notes, "Skipping %s: larger than the 1 MiB per-file import limit\n", rel)
			return nil
		}
		supportingCount++
		if supportingCount > skillDirMaxFileCount {
			return fmt.Errorf("the folder has more than %d supporting files, the import limit", skillDirMaxFileCount)
		}
		supportingBytes += info.Size()
		if supportingBytes > skillDirMaxTotalSize {
			return errors.New("the folder's supporting files exceed the 8 MiB import limit")
		}
		members = append(members, rel)
		return nil
	})
	if walkErr != nil {
		return nil, "", fmt.Errorf("read skill folder: %w", walkErr)
	}
	if !hasSkillMd {
		return nil, "", fmt.Errorf("no SKILL.md at the top of %s", dir)
	}

	wrapper := filepath.Base(abs)
	if wrapper == "." || wrapper == ".." || wrapper == string(filepath.Separator) {
		wrapper = "skill"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range members {
		content, err := root.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", rel, err)
		}
		w, err := zw.Create(wrapper + "/" + rel)
		if err != nil {
			return nil, "", fmt.Errorf("pack %s: %w", rel, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, "", fmt.Errorf("pack %s: %w", rel, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", fmt.Errorf("pack skill folder: %w", err)
	}
	return buf.Bytes(), wrapper + ".skill", nil
}

// isIgnoredSkillDirEntry mirrors isIgnoredArchiveEntry in
// server/internal/handler/skill_import_archive.go.
func isIgnoredSkillDirEntry(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "__MACOSX" || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	switch strings.ToLower(path.Base(rel)) {
	case "license", "license.md", "license.txt":
		return true
	}
	return false
}
