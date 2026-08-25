package handler

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

// maxImportArchiveUploadSize bounds the compressed upload accepted by the
// archive import path. The decompressed bundle is still held to the existing
// per-file / total / file-count caps (maxImportFileSize, maxImportTotalSize,
// maxImportFileCount); this outer cap just stops a client from streaming an
// unbounded compressed body before those decompression limits can apply.
const (
	maxImportArchiveUploadSize   = 16 << 20 // 16 MiB compressed upload
	maxImportArchiveExpandedSize = maxImportFileSize + maxImportTotalSize
	maxImportArchiveEntryCount   = 1024
)

// isMultipartForm reports whether the request carries a multipart/form-data
// body (an uploaded skill archive) rather than the JSON URL-import body.
func isMultipartForm(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

// importSkillFromArchive handles POST /api/skills/import when the body is an
// uploaded skill archive (.skill / .zip / .tar / .tar.gz). It reads the file plus the optional
// on_conflict form field, decompresses the archive into an importedSkill, and
// hands off to the shared finishSkillImport tail. The archive path always
// produces structured (status / skill / existing_skill) results — there is no
// legacy pre-on_conflict client for it to stay compatible with.
func (h *Handler) importSkillFromArchive(w http.ResponseWriter, r *http.Request, workspaceID string, workspaceUUID, creatorUUID pgtype.UUID, creatorID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportArchiveUploadSize)
	if err := r.ParseMultipartForm(maxImportArchiveUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload or file exceeds the size limit")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	onConflict := r.FormValue("on_conflict")
	if !validImportOnConflict(onConflict) {
		writeError(w, http.StatusBadRequest, "on_conflict must be one of: fail, overwrite, rename, skip")
		return
	}
	strategy := onConflict
	if strategy == "" {
		strategy = importOnConflictFail
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `a skill archive file is required (form field "file")`)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}

	filename := ""
	if header != nil {
		filename = header.Filename
	}
	imported, err := parseSkillArchive(data, filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.finishSkillImport(w, r, workspaceID, workspaceUUID, creatorUUID, creatorID, strategy, true, imported)
}

// parseSkillArchive decompresses an uploaded skill archive into an
// importedSkill. Two container formats are accepted:
//
//   - .skill / .zip: a standard zip, as produced by Anthropic's
//     package_skill.
//   - .tar / .tar.gz: a tarball, as produced by this server's skill export
//     endpoint (GET /api/skills/{id}/export) and by plain tar of a skill
//     directory.
//
// Entries may sit either at the archive root (SKILL.md, scripts/...) or
// nested under a single top-level directory (my-skill/SKILL.md,
// my-skill/scripts/...) — both layouts are accepted by rooting on the
// shallowest SKILL.md found.
//
// Safety: every entry is validated against traversal / absolute paths
// (zip-slip / tar-slip), the reserved SKILL.md supporting path is dropped,
// per-file size is bounded while reading (so a lying archive header can't blow
// up memory), and the shared addFile enforces the per-bundle byte and
// file-count caps. Tar streams additionally reject non-regular entries and
// enforce expanded-byte and archive-entry limits before buffering can grow.
func parseSkillArchive(data []byte, filename string) (*importedSkill, error) {
	entries, err := readSkillArchiveEntries(data)
	if err != nil {
		return nil, err
	}
	return buildImportedSkillFromEntries(entries, filename)
}

// skillArchiveEntry is one file or directory from an uploaded skill archive,
// normalized to a cleaned slash-delimited name and a bounded read. The zip and
// tar walkers both produce this shape so the root-finding and supporting-file
// logic in buildImportedSkillFromEntries is shared between formats.
type skillArchiveEntry struct {
	name string
	dir  bool
	read func(maxSize int64) (string, error)
}

// readSkillArchiveEntries decodes an uploaded skill archive into normalized
// entries. gzip magic selects .tar.gz, the ustar header magic selects plain
// tar, and anything else is treated as zip (.skill / .zip).
func readSkillArchiveEntries(data []byte) ([]skillArchiveEntry, error) {
	if isGzipArchive(data) || isTarArchive(data) {
		return readTarArchiveEntries(data)
	}
	return readZipArchiveEntries(data)
}

// readZipArchiveEntries walks a zip archive into normalized entries. Reads are
// lazy: each entry's read closure re-opens it on demand, preserving the zip
// reader's random access.
func readZipArchiveEntries(data []byte) ([]skillArchiveEntry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip/.tar archive")
	}
	entries := make([]skillArchiveEntry, 0, len(zr.File))
	for _, f := range zr.File {
		entries = append(entries, skillArchiveEntry{
			name: path.Clean(f.Name),
			dir:  f.FileInfo().IsDir(),
			read: func(maxSize int64) (string, error) { return readZipFile(f, maxSize) },
		})
	}
	return entries, nil
}

// readTarArchiveEntries walks a .tar or .tar.gz archive into normalized
// entries. tar is a sequential stream, so each regular entry's content is read
// eagerly while the reader is positioned at it. The expanded-byte and entry
// caps are enforced here, before buffered content can grow past the import
// contract. In particular, returning as soon as a file exceeds its cap avoids
// tar.Reader.Next draining the rest of an attacker-controlled giant entry.
func readTarArchiveEntries(data []byte) ([]skillArchiveEntry, error) {
	var src io.Reader = bytes.NewReader(data)
	if isGzipArchive(data) {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip/.tar archive")
		}
		defer gz.Close()
		src = gz
	}

	tr := tar.NewReader(src)
	var entries []skillArchiveEntry
	var expandedSize int64
	entryCount := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip/.tar archive")
		}

		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		// A skill archive contains regular files only. Symlinks, devices, and
		// other special tar entries cannot be represented in skill_file and must
		// not become empty supporting files on import. Reject rather than skip so
		// a later Next call never drains an attacker-controlled special entry.
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("unsupported archive entry %q", hdr.Name)
		}
		entryCount++
		if entryCount > maxImportArchiveEntryCount {
			return nil, fmt.Errorf("archive exceeds %d entry limit", maxImportArchiveEntryCount)
		}

		// Bound the read now, while the stream is positioned at this entry, so a
		// header that under-reports its size cannot force an unbounded allocation.
		buf, err := io.ReadAll(io.LimitReader(tr, maxImportFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("read archive entry %q: %w", hdr.Name, err)
		}
		if int64(len(buf)) > maxImportFileSize {
			return nil, fmt.Errorf("file %q exceeds %d bytes", hdr.Name, maxImportFileSize)
		}
		expandedSize += int64(len(buf))
		if expandedSize > maxImportArchiveExpandedSize {
			return nil, fmt.Errorf("archive exceeds %d byte expanded size limit", maxImportArchiveExpandedSize)
		}

		content := string(buf)
		entries = append(entries, skillArchiveEntry{
			name: path.Clean(hdr.Name),
			read: func(int64) (string, error) { return content, nil },
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip/.tar archive")
	}
	return entries, nil
}

// isGzipArchive reports whether data begins with the gzip magic bytes.
func isGzipArchive(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// isTarArchive reports whether data carries the ustar header magic at the
// offset tar reserves for it (257), covering both POSIX ("ustar\x00") and GNU
// ("ustar ") variants.
func isTarArchive(data []byte) bool {
	if len(data) < 263 {
		return false
	}
	magic := string(data[257:263])
	return magic == "ustar\x00" || magic == "ustar "
}

// buildImportedSkillFromEntries locates the skill root (the shallowest
// SKILL.md), reads the primary content, and collects the supporting files. See
// parseSkillArchive for the layout and safety contract.
func buildImportedSkillFromEntries(entries []skillArchiveEntry, filename string) (*importedSkill, error) {
	// Locate the skill root: the directory of the shallowest SKILL.md. This
	// accepts both a root-level SKILL.md and the common single-wrapper layout.
	// The candidate path is validated up front (absolute / traversal entries are
	// rejected) so a malicious archive cannot smuggle an unsafe path in as the
	// primary content — keeping every accepted entry zip-slip-safe.
	var skillEntry *skillArchiveEntry
	rootPrefix := ""
	for i := range entries {
		entry := &entries[i]
		if entry.dir {
			continue
		}
		clean := entry.name
		if !strings.EqualFold(path.Base(clean), skillpkg.ContentFilename) {
			continue
		}
		if !validateFilePath(clean) {
			continue
		}
		prefix := archiveEntryPrefix(clean)
		if skillEntry == nil || len(prefix) < len(rootPrefix) {
			skillEntry = entry
			rootPrefix = prefix
		}
	}
	if skillEntry == nil {
		return nil, fmt.Errorf("archive does not contain a SKILL.md")
	}

	content, err := skillEntry.read(maxImportFileSize)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	name, description := skillpkg.ParseSkillFrontmatter(content)
	if name == "" {
		name = skillNameFromArchive(rootPrefix, filename)
	}
	if name == "" {
		return nil, fmt.Errorf("could not determine the skill name: SKILL.md has no name field and the archive is unnamed")
	}

	imported := &importedSkill{
		name:        name,
		description: description,
		content:     content,
	}

	for i := range entries {
		entry := &entries[i]
		if entry.dir {
			continue
		}
		clean := entry.name
		// Only files under the resolved skill root belong to this skill.
		if rootPrefix != "" && !strings.HasPrefix(clean, rootPrefix) {
			continue
		}
		rel := strings.TrimPrefix(clean, rootPrefix)
		if rel == "" {
			continue
		}
		// A SKILL.md at any depth is never a supporting file: the top-level one
		// is the primary content, and a nested one would collide with the
		// reserved primary-content name. Mirrors the daemon's local-skill rule.
		if strings.EqualFold(path.Base(rel), skillpkg.ContentFilename) {
			continue
		}
		if isIgnoredArchiveEntry(rel) {
			continue
		}
		// zip-slip / absolute-path guard.
		if !validateFilePath(rel) {
			continue
		}
		fileContent, ferr := entry.read(maxImportFileSize)
		if ferr != nil {
			// An oversize or unreadable individual asset is skipped rather than
			// failing the whole import, matching the local-runtime importer.
			continue
		}
		// addFile enforces the per-bundle caps and drops binary assets; a cap
		// breach aborts the import instead of silently truncating it.
		if err := imported.addFile(rel, fileContent); err != nil {
			return nil, err
		}
	}

	sort.Slice(imported.files, func(i, j int) bool {
		return imported.files[i].path < imported.files[j].path
	})
	return imported, nil
}

// archiveEntryPrefix returns the directory prefix (with trailing slash) of a
// cleaned, slash-delimited archive entry: "" for a root entry, "my-skill/" for
// "my-skill/SKILL.md".
func archiveEntryPrefix(cleanName string) string {
	dir := path.Dir(cleanName)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir + "/"
}

// skillNameFromArchive derives a fallback skill name when SKILL.md carries no
// name field: the wrapper directory name if the skill is nested, else the
// uploaded filename without its extension.
func skillNameFromArchive(rootPrefix, filename string) string {
	if rootPrefix != "" {
		base := path.Base(strings.TrimSuffix(rootPrefix, "/"))
		if base != "." && base != "/" && base != ".." {
			return base
		}
	}
	clean := strings.ReplaceAll(filename, "\\", "/")
	base := path.Base(clean)
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

// isIgnoredArchiveEntry filters editor/OS noise and license files out of the
// supporting bundle, mirroring the daemon's local-skill discovery rules.
func isIgnoredArchiveEntry(rel string) bool {
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

// readZipFile reads a single zip entry, capping the read at maxSize+1 bytes so a
// header that under-reports its uncompressed size cannot force an unbounded
// allocation. Entries larger than maxSize are rejected.
func readZipFile(f *zip.File, maxSize int64) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, maxSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("file %q exceeds %d bytes", f.Name, maxSize)
	}
	return string(data), nil
}
