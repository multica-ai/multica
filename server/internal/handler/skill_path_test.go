package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The complete path matrix lives beside skill.IsSafeFilePath. These checks
// prove that each public write route applies that shared contract.
func TestSkillFileWriteEndpointsRejectNonCanonicalPaths(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skillID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "skill-path-validation-fixture",
		"description":  "fixture for path validation",
		"content":      "# Skill path validation",
		"created_by":   testUserID,
	})

	t.Run("create", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
			Name:    "skill-path-validation-create",
			Content: "# Skill path validation",
			Files: []CreateSkillFileRequest{
				{Path: "references//guide.md", Content: "guide"},
			},
		})
		testutil.Call(t, testHandler.CreateSkill, req).Want(http.StatusBadRequest)
	})

	t.Run("update", func(t *testing.T) {
		req := newRequest(http.MethodPut, "/api/skills/"+skillID, UpdateSkillRequest{
			Files: []CreateSkillFileRequest{
				{Path: "references/./guide.md", Content: "guide"},
			},
		})
		req = withURLParam(req, "id", skillID)
		testutil.Call(t, testHandler.UpdateSkill, req).Want(http.StatusBadRequest)
	})

	t.Run("upsert file", func(t *testing.T) {
		req := newRequest(http.MethodPut, "/api/skills/"+skillID+"/files", CreateSkillFileRequest{
			Path:    `references\guide.md`,
			Content: "guide",
		})
		req = withURLParam(req, "id", skillID)
		testutil.Call(t, testHandler.UpsertSkillFile, req).Want(http.StatusBadRequest)
	})
}
