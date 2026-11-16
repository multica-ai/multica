package handler

import (
	"fmt"

	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

// Manual writes use the existing skill import limits for stored UTF-8 text.
func validateSkillWriteBudget(content string, files []CreateSkillFileRequest) error {
	contents := map[string]string{"SKILL.md": sanitizeNullBytes(content)}
	for _, file := range files {
		if !skillpkg.IsReservedContentPath(file.Path) {
			contents[file.Path] = sanitizeNullBytes(file.Content)
		}
	}
	if len(contents)-1 > maxImportFileCount {
		return fmt.Errorf("skill exceeds the %d supporting file limit", maxImportFileCount)
	}
	total := 0
	for path, body := range contents {
		if len(body) > maxImportFileSize {
			return fmt.Errorf("skill file %q is %d bytes, exceeding the %d byte per-file limit", path, len(body), maxImportFileSize)
		}
		if path != "SKILL.md" {
			total += len(body)
		}
	}
	if total > maxImportTotalSize {
		return fmt.Errorf("skill supporting files total %d bytes, exceeding the %d byte total limit", total, maxImportTotalSize)
	}
	return nil
}

func validateSkillResponseBudget(skill SkillWithFilesResponse) error {
	files := make([]CreateSkillFileRequest, len(skill.Files))
	for i, file := range skill.Files {
		files[i] = CreateSkillFileRequest{Path: file.Path, Content: file.Content}
	}
	return validateSkillWriteBudget(skill.Content, files)
}
