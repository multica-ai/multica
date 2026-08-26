package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errSkillExportNotPortable = errors.New("skill exceeds portable archive limits")

// ExportSkill serves a single workspace skill as a portable .tar.gz archive so
// it can be shared with someone outside the workspace and re-imported through
// POST /api/skills/import (GH multica-ai/multica#7495).
//
// The bundle uses the single-wrapper layout the archive importer accepts: a
// directory named after the skill containing SKILL.md plus every supporting
// file under its stored relative path. Content is served verbatim — no
// frontmatter rewriting — so the exported copy is the exact skill stored in
// the workspace.
//
// Access mirrors GetSkill: any member who can view the skill (workspace
// membership) may download it; no edit permission is required.
func (h *Handler) ExportSkill(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}

	data, err := buildSkillTarGz(skill.Name, skill.Content, files)
	if err != nil {
		if errors.Is(err, errSkillExportNotPortable) {
			writeError(w, http.StatusUnprocessableEntity, "skill exceeds portable export limits")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to build skill archive")
		return
	}

	filename := skillArchiveDirName(skill.Name) + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// buildSkillTarGz packs a skill (primary content + supporting files) into a
// gzip-compressed tar archive. Entries are laid out under a single wrapper
// directory named after the skill:
//
//	<name>/SKILL.md
//	<name>/<supporting file path>
//
// The wrapper mirrors what Anthropic's package_skill produces for .skill
// bundles and is the layout parseSkillArchive resolves most naturally. File
// paths are re-validated (zip-slip guard) so a path that somehow landed in the
// DB outside the normal write path cannot escape the archive root.
func buildSkillTarGz(name, content string, files []db.SkillFile) ([]byte, error) {
	if err := validateSkillExportSize(content, files); err != nil {
		return nil, err
	}
	root := skillArchiveDirName(name)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	add := func(entryPath, body string) error {
		clean := path.Clean(entryPath)
		if !validateFilePath(clean) {
			return fmt.Errorf("invalid archive path %q", entryPath)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: clean,
			Mode: 0o644,
			Size: int64(len(body)),
		}); err != nil {
			return err
		}
		_, err := io.WriteString(tw, body)
		return err
	}

	if err := add(path.Join(root, skillpkg.ContentFilename), content); err != nil {
		return nil, err
	}
	for _, f := range files {
		if !validateFilePath(f.Path) {
			return nil, fmt.Errorf("invalid archive path %q", f.Path)
		}
		if err := add(path.Join(root, f.Path), f.Content); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// validateSkillExportSize keeps the exporter within the archive importer's
// content contract. Skills written through the regular API predate those
// limits, so exporting an oversized record without this check would create a
// download that cannot be imported again.
func validateSkillExportSize(content string, files []db.SkillFile) error {
	if len(content) > maxImportFileSize {
		return fmt.Errorf("%w: SKILL.md exceeds %d bytes", errSkillExportNotPortable, maxImportFileSize)
	}
	if len(files) > maxImportFileCount {
		return fmt.Errorf("%w: skill has more than %d supporting files", errSkillExportNotPortable, maxImportFileCount)
	}

	totalSize := 0
	for _, file := range files {
		if len(file.Content) > maxImportFileSize {
			return fmt.Errorf("%w: supporting file %q exceeds %d bytes", errSkillExportNotPortable, file.Path, maxImportFileSize)
		}
		totalSize += len(file.Content)
		if totalSize > maxImportTotalSize {
			return fmt.Errorf("%w: supporting files exceed %d bytes", errSkillExportNotPortable, maxImportTotalSize)
		}
	}
	return nil
}

// skillArchiveDirName renders a skill name as a safe single path segment for
// both the tar wrapper directory and the Content-Disposition filename. Runs of
// characters that are not letters, digits, '.', '_' or '-' collapse into a
// single dash (so CR/LF cannot smuggle a second header line), and the result
// is guaranteed non-empty and never "." / ".." so it stays inside the archive
// root.
func skillArchiveDirName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '_', r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	dir := strings.Trim(b.String(), "-")
	switch dir {
	case "", ".", "..":
		return "skill"
	}
	return dir
}
