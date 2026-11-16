package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestSkillWriteEndpointsRejectOverBudgetWithoutChanges(t *testing.T) {
	cases := []struct {
		name    string
		content string
		files   []CreateSkillFileRequest
		want    string
	}{
		{"main file", strings.Repeat("x", maxImportFileSize+1), nil, "SKILL.md"},
		{"supporting script", "body", []CreateSkillFileRequest{{Path: "scripts/tool.js", Content: strings.Repeat("x", maxImportFileSize+1)}}, "scripts/tool.js"},
		{"UTF-8 bytes", "body", []CreateSkillFileRequest{{Path: "references/guide.md", Content: strings.Repeat("界", maxImportFileSize/3+1)}}, "references/guide.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, route := range []string{"create", "update", "upsert"} {
				if route == "upsert" && tc.files == nil {
					continue
				}
				t.Run(route, func(t *testing.T) {
					skillID := insertHandlerTestSkill(t, "skill-write-budget", "original body")
					dbfx.Insert(t, "skill_file", testutil.Cols{
						"skill_id": skillID, "path": "original.md", "content": "original file",
					})
					before, err := testHandler.Queries.GetSkill(context.Background(), parseUUID(skillID))
					if err != nil {
						t.Fatal(err)
					}
					name := "rejected-budget-" + skillID
					w := httptest.NewRecorder()
					switch route {
					case "create":
						testHandler.CreateSkill(w, newRequest(http.MethodPost, "/api/skills", CreateSkillRequest{
							Name: name, Content: tc.content, Files: tc.files,
						}))
					case "update":
						testHandler.UpdateSkill(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID, UpdateSkillRequest{
							Name: &name, Content: &tc.content, Files: tc.files,
						}), "id", skillID))
					case "upsert":
						testHandler.UpsertSkillFile(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID+"/files", tc.files[0]), "id", skillID))
					}
					if w.Code != http.StatusRequestEntityTooLarge {
						t.Fatalf("status = %d, want 413", w.Code)
					}
					if !strings.Contains(w.Body.String(), tc.want) || !strings.Contains(w.Body.String(), "1048576") {
						t.Fatalf("missing file path or byte limit in error: %.300s", w.Body.String())
					}
					after, err := testHandler.Queries.GetSkill(context.Background(), parseUUID(skillID))
					if err != nil {
						t.Fatal(err)
					}
					if after.Name != before.Name || after.Content != before.Content || !after.UpdatedAt.Time.Equal(before.UpdatedAt.Time) {
						t.Fatal("rejected write changed the skill")
					}
					files, err := testHandler.Queries.ListSkillFiles(context.Background(), parseUUID(skillID))
					if err != nil || len(files) != 1 || files[0].Path != "original.md" || files[0].Content != "original file" {
						t.Fatalf("rejected write changed supporting files: count=%d, error=%v", len(files), err)
					}
					if versions := listVersions(t, skillID); len(versions) != 0 {
						t.Fatal("rejected write created a skill version")
					}
					var created int
					if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM skill WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name).Scan(&created); err != nil || created != 0 {
						t.Fatalf("rejected write persisted a new skill: count=%d, error=%v", created, err)
					}
				})
			}
		})
	}
}

func TestSkillWriteEndpointsEnforceResultingBundleBudget(t *testing.T) {
	for _, limit := range []string{"file count", "total bytes"} {
		t.Run(limit, func(t *testing.T) {
			files := make([]CreateSkillFileRequest, maxImportFileCount)
			for i := range files {
				files[i] = CreateSkillFileRequest{Path: fmt.Sprintf("references/%d.md", i)}
			}
			if limit == "total bytes" {
				files = files[:8]
				for i := range files {
					files[i].Content = strings.Repeat("x", maxImportFileSize)
				}
			}
			skill := createVersionTestSkill(t, "bundle-budget-"+strings.ReplaceAll(limit, " ", "-"), files[:len(files)-1])
			beforeVersions := len(listVersions(t, skill.ID))
			if limit == "file count" {
				files = append(files, CreateSkillFileRequest{Path: "overflow.md"})
			} else {
				files = append(files, CreateSkillFileRequest{Path: "overflow.md", Content: "x"})
			}
			for _, route := range []string{"create", "update", "upsert"} {
				t.Run(route, func(t *testing.T) {
					w := httptest.NewRecorder()
					switch route {
					case "create":
						testHandler.CreateSkill(w, newRequest(http.MethodPost, "/api/skills", CreateSkillRequest{
							Name: "over-" + skill.ID, Content: "body", Files: files,
						}))
					case "update":
						testHandler.UpdateSkill(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID, UpdateSkillRequest{Files: files}), "id", skill.ID))
					case "upsert":
						// Fill the remaining budget before attempting the overflow file.
						req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", files[len(files)-2]), "id", skill.ID)
						testutil.Call(t, testHandler.UpsertSkillFile, req).Want(http.StatusOK)
						beforeVersions++
						testHandler.UpsertSkillFile(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", files[len(files)-1]), "id", skill.ID))
					}
					if w.Code != http.StatusRequestEntityTooLarge {
						t.Fatalf("status = %d, want 413", w.Code)
					}
					if got := len(listVersions(t, skill.ID)); got != beforeVersions {
						t.Fatalf("rejected write changed version count: got %d, want %d", got, beforeVersions)
					}
				})
			}
		})
	}
}

func TestSkillWriteEndpointsAcceptFileAtByteLimit(t *testing.T) {
	content := strings.Repeat("界", maxImportFileSize/3) + "x"
	skill := createVersionTestSkill(t, "skill-exact-byte-budget", []CreateSkillFileRequest{{Path: "script.mjs", Content: content}})
	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: &content})
	req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", CreateSkillFileRequest{Path: "script.mjs", Content: content}), "id", skill.ID)
	testutil.Call(t, testHandler.UpsertSkillFile, req).Want(http.StatusOK)
}

func TestSkillWriteEndpointsAcceptMaximumImportBudget(t *testing.T) {
	content := strings.Repeat("x", maxImportFileSize)
	files := make([]CreateSkillFileRequest, maxImportTotalSize/maxImportFileSize)
	for i := range files {
		files[i] = CreateSkillFileRequest{Path: fmt.Sprintf("references/%d.md", i), Content: content}
	}
	for _, route := range []string{"create", "update", "upsert"} {
		t.Run(route, func(t *testing.T) {
			var skill SkillWithFilesResponse
			wantContent := content
			if route == "create" {
				testutil.Call(t, testHandler.CreateSkill, newRequest(http.MethodPost, "/api/skills", CreateSkillRequest{
					Name: "maximum-import-budget", Content: content, Files: files,
				})).Want(http.StatusCreated).JSON(&skill)
				dbfx.Cleanup(t, `DELETE FROM skill WHERE id = $1`, skill.ID)
				dbfx.Cleanup(t, `DELETE FROM skill_file WHERE skill_id = $1`, skill.ID)
			} else {
				skill.ID = insertHandlerTestSkill(t, "maximum-import-budget", content)
				storedFiles := files
				if route == "upsert" {
					storedFiles = files[:len(files)-1]
				}
				for _, file := range storedFiles {
					dbfx.Insert(t, "skill_file", testutil.Cols{"skill_id": skill.ID, "path": file.Path, "content": file.Content})
				}
				if route == "update" {
					// The web editor submits both content and supporting files on every save.
					wantContent = content[:len(content)-1] + "y"
					updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: &wantContent, Files: files})
				} else {
					req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", files[len(files)-1]), "id", skill.ID)
					testutil.Call(t, testHandler.UpsertSkillFile, req).Want(http.StatusOK)
				}
			}
			dbfx.Cleanup(t, `DELETE FROM skill_version WHERE skill_id = $1`, skill.ID)
			saved, err := testHandler.Queries.GetSkill(context.Background(), parseUUID(skill.ID))
			if err != nil {
				t.Fatal(err)
			}
			if saved.Content != wantContent {
				t.Fatal("write did not preserve the requested main content at the byte limit")
			}
			stored, err := testHandler.Queries.ListSkillFiles(context.Background(), parseUUID(skill.ID))
			if err != nil {
				t.Fatal(err)
			}
			total := 0
			for _, file := range stored {
				total += len(file.Content)
			}
			if len(stored) != len(files) || total != maxImportTotalSize {
				t.Fatalf("saved supporting files: count=%d, bytes=%d", len(stored), total)
			}
		})
	}
}

func TestSkillWriteEndpointsAllowMetadataUpdateOverBudget(t *testing.T) {
	content := strings.Repeat("x", maxImportFileSize+1)
	skillID := insertHandlerTestSkill(t, "legacy-over-budget", content)
	dbfx.Insert(t, "skill_file", testutil.Cols{"skill_id": skillID, "path": "legacy.md", "content": content})
	dbfx.Cleanup(t, `DELETE FROM skill_version WHERE skill_id = $1`, skillID)
	name, description := "renamed-"+skillID, "updated metadata"
	var updated SkillWithFilesResponse
	req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID, UpdateSkillRequest{
		Name: &name, Description: &description,
	}), "id", skillID)
	testutil.Call(t, testHandler.UpdateSkill, req).Want(http.StatusOK).JSON(&updated)
	if updated.Name != name || updated.Description != description || updated.Content != content ||
		len(updated.Files) != 1 || updated.Files[0].Content != content {
		t.Fatal("metadata update changed the legacy skill contents or failed to save metadata")
	}
}
