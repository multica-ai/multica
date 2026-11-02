package handler

import (
	"strings"
	"testing"
)

func TestLabrastroLegacyImportAndRefreshExposeDiagnostics(t *testing.T) {
	fx := labrastroPackageDBFixture(t)
	source := labrastroTestFixture(map[string]string{
		"skills/demo/SKILL.md":            "---\nname: demo\n---\n[local](references/local.md)\n[shared](../../references/a.md)",
		"skills/demo/references/local.md": "local reference",
		"references/a.md":                 "shared",
	})
	source.install(t)
	source.truncated = true
	url := source.url() + "/tree/main/skills/demo"
	var imported SkillWithFilesResponse
	labrastroCall(t, fx, testUserID, testHandler.ImportSkill, "POST", map[string]string{"url": url}).Want(201).JSON(&imported)
	if len(imported.Diagnostics) != 1 || imported.Diagnostics[0].Code != "tree_incomplete" || !imported.Diagnostics[0].Retryable {
		t.Fatalf("legacy import lost fallback diagnostic: %+v", imported.Diagnostics)
	}
	assertLocalReference := func(result SkillWithFilesResponse) {
		t.Helper()
		if len(result.Files) != 1 || result.Files[0].Path != "references/local.md" || result.Files[0].Content != "local reference" {
			t.Fatalf("fallback did not preserve the complete skill directory: %+v", result.Files)
		}
	}
	assertLocalReference(imported)
	var refreshed SkillWithFilesResponse
	labrastroCall(t, fx, testUserID, testHandler.RefreshSkill, "POST", nil, "id", imported.ID).Want(200).JSON(&refreshed)
	if len(refreshed.Diagnostics) != 1 || refreshed.Diagnostics[0].Code != "tree_incomplete" || !refreshed.Diagnostics[0].Retryable {
		t.Fatalf("legacy refresh lost fallback diagnostic: %+v", refreshed.Diagnostics)
	}
	assertLocalReference(refreshed)
	source.truncated = false
	source.Files["skills/demo/SKILL.md"] += "\nchanged"
	source.Files["skills/demo/references/local.md"] = "changed local reference"
	source.rebuild()
	source.failPath = "references/a.md"
	var failure struct {
		Error       string                  `json:"error"`
		Code        string                  `json:"code"`
		Diagnostics []SkillImportDiagnostic `json:"diagnostics"`
		Retryable   bool                    `json:"retryable"`
	}
	labrastroCall(t, fx, testUserID, testHandler.RefreshSkill, "POST", nil, "id", imported.ID).Want(502).JSON(&failure)
	if failure.Error == "" || failure.Code != "required_reference_unavailable" || !failure.Retryable || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "required_reference_unavailable" {
		t.Fatalf("legacy error contract lost diagnostics: %+v", failure)
	}
	var content string
	fx.QueryRow(t, "SELECT content FROM skill WHERE id=$1", imported.ID).Scan(&content)
	if content != imported.Content {
		t.Fatal("failed refresh modified saved content")
	}
	fx.QueryRow(t, "SELECT content FROM skill_file WHERE skill_id=$1 AND path='references/local.md'", imported.ID).Scan(&content)
	if content != "local reference" {
		t.Fatal("failed refresh modified saved supporting files")
	}
}

func TestLabrastroArchiveRejectsSiblingSkills(t *testing.T) {
	data := buildTestZip(t, map[string]string{"repo/a/SKILL.md": testSkillMd, "repo/b/SKILL.md": testSkillMd})
	if _, err := parseSkillArchive(data, "skills.zip"); err == nil || !strings.Contains(err.Error(), "skill package import") {
		t.Fatalf("multiple sibling skills were not rejected with package guidance: %v", err)
	}
	data = buildTestZip(t, map[string]string{"repo/SKILL.md": testSkillMd, "repo/nested/SKILL.md": testSkillMd})
	if _, err := parseSkillArchive(data, "skill.zip"); err != nil {
		t.Fatalf("nested SKILL.md changed the existing root behavior: %v", err)
	}
}
