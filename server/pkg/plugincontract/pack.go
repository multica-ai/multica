package plugincontract

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var deterministicZipTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// PackDirectory creates a deterministic ZIP from a Plugin source directory and
// validates the resulting artifact with the same contract used by the Server.
func PackDirectory(root string) ([]byte, Artifact, error) {
	root = filepath.Clean(root)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, Artifact{}, fmt.Errorf("open plugin directory: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, Artifact{}, fmt.Errorf("plugin source %q is not a directory", root)
	}

	type sourceFile struct {
		path    string
		content []byte
	}
	files := make([]sourceFile, 0)
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin source path %q is a symlink", name)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("plugin source path %q is not a regular file", name)
		}
		if info.Mode().Perm()&0o111 != 0 {
			return fmt.Errorf("plugin source path %q is executable", name)
		}
		if info.Size() > MaxFileSize {
			return fmt.Errorf("plugin source path %q exceeds %d bytes", name, MaxFileSize)
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		files = append(files, sourceFile{path: name, content: content})
		return nil
	})
	if err != nil {
		return nil, Artifact{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.path, Method: zip.Deflate}
		header.SetMode(0o644)
		header.SetModTime(deterministicZipTime)
		header.Extra = nil
		header.Comment = ""
		part, err := writer.CreateHeader(header)
		if err != nil {
			return nil, Artifact{}, fmt.Errorf("create plugin archive path %q: %w", file.path, err)
		}
		if _, err := part.Write(file.content); err != nil {
			return nil, Artifact{}, fmt.Errorf("write plugin archive path %q: %w", file.path, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, Artifact{}, fmt.Errorf("close plugin archive: %w", err)
	}
	archive := buffer.Bytes()
	artifact, err := ValidateArtifact(archive)
	if err != nil {
		return nil, Artifact{}, err
	}
	return append([]byte(nil), archive...), artifact, nil
}
