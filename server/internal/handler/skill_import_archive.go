package handler

import (
	"archive/zip"
	"bytes"
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
const maxImportArchiveUploadSize = 16 << 20 // 16 MiB

// isMultipartForm reports whether the request carries a multipart/form-data
// body (an uploaded skill archive) rather than the JSON URL-import body.
func isMultipartForm(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

// importSkillFromArchive handles POST /api/skills/import when the body is an
// uploaded skill archive (.skill / .zip). It reads the file plus the optional
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

// parseSkillArchive decompresses an uploaded skill archive (.skill / .zip) into
// an importedSkill. A .skill file is a standard zip whose entries sit either at
// the archive root (SKILL.md, scripts/...) or nested under a single top-level
// directory (my-skill/SKILL.md, my-skill/scripts/...) — the layout produced by
// Anthropic's package_skill. Both are accepted by rooting on the shallowest
// SKILL.md found.
//
// Safety: every entry is validated against traversal / absolute paths
// (zip-slip), the reserved SKILL.md supporting path is dropped, per-file size is
// bounded while reading (so a lying zip header can't blow up memory), and the
// shared addFile enforces the per-bundle byte and file-count caps.
func parseSkillArchive(data []byte, filename string) (*importedSkill, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("uploaded file is not a valid .skill/.zip archive")
	}

	// Locate the skill root: the directory of the shallowest SKILL.md. This
	// accepts both a root-level SKILL.md and the common single-wrapper layout.
	// The candidate path is validated up front (absolute / traversal entries are
	// rejected) so a malicious archive cannot smuggle an unsafe path in as the
	// primary content — keeping every accepted entry zip-slip-safe.
	var skillMd *zip.File
	rootPrefix := ""
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)
		if !strings.EqualFold(path.Base(clean), skillpkg.ContentFilename) {
			continue
		}
		if !validateFilePath(clean) {
			continue
		}
		prefix := archiveEntryPrefix(clean)
		if skillMd == nil || len(prefix) < len(rootPrefix) {
			skillMd = f
			rootPrefix = prefix
		}
	}
	if skillMd == nil {
		return nil, fmt.Errorf("archive does not contain a SKILL.md")
	}

	content, err := readZipFile(skillMd, maxImportFileSize)
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

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)
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
		fileContent, ferr := readZipFile(f, maxImportFileSize)
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
