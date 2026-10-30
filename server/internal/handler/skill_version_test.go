package handler

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func createVersionTestSkill(t *testing.T, name string, files []CreateSkillFileRequest) SkillWithFilesResponse {
	t.Helper()
	var skill SkillWithFilesResponse
	testutil.Call(t, testHandler.CreateSkill, newRequest(http.MethodPost, "/api/skills", CreateSkillRequest{
		Name:    name,
		Content: "v1 body",
		Files:   files,
	})).Want(http.StatusCreated).JSON(&skill)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill_version WHERE skill_id = $1`, skill.ID)
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skill.ID)
	})
	return skill
}

func listVersions(t *testing.T, skillID string) []SkillVersionSummaryResponse {
	t.Helper()
	var versions []SkillVersionSummaryResponse
	req := withURLParam(newRequest(http.MethodGet, "/api/skills/"+skillID+"/versions", nil), "id", skillID)
	testutil.Call(t, testHandler.ListSkillVersions, req).Want(http.StatusOK).JSON(&versions)
	return versions
}

func updateSkillForVersionTest(t *testing.T, skillID string, body UpdateSkillRequest) {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID, body), "id", skillID)
	testutil.Call(t, testHandler.UpdateSkill, req).Want(http.StatusOK)
}

func skillVersionRequest(method, skillID string, version string, suffix string) *http.Request {
	req := newRequest(method, "/api/skills/"+skillID+"/versions/"+version+suffix, nil)
	return withURLParams(req, "id", skillID, "version", version)
}

func TestSkillVersionLifecycle(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-version-lifecycle", []CreateSkillFileRequest{
		{Path: "notes.md", Content: "v1 notes"},
	})

	versions := listVersions(t, skill.ID)
	if len(versions) != 1 || versions[0].Version != 1 || versions[0].Source != skillVersionSourceCreate {
		t.Fatalf("after create: want [v1 create], got %+v", versions)
	}

	// One save with content and files produces exactly one version.
	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{
		Content: strPtr("v2 body"),
		Files: []CreateSkillFileRequest{
			{Path: "notes.md", Content: "v2 notes"},
			{Path: "extra.md", Content: "v2 extra"},
		},
	})
	// Saving identical content records nothing.
	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: strPtr("v2 body")})

	// Single-file endpoint edits are versioned too.
	fileReq := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", CreateSkillFileRequest{
		Path: "extra.md", Content: "v3 extra",
	}), "id", skill.ID)
	testutil.Call(t, testHandler.UpsertSkillFile, fileReq).Want(http.StatusOK)

	versions = listVersions(t, skill.ID)
	if len(versions) != 3 {
		t.Fatalf("want 3 versions, got %+v", versions)
	}
	for i, want := range []int32{3, 2, 1} {
		if versions[i].Version != want {
			t.Fatalf("versions not newest-first: %+v", versions)
		}
	}
	if versions[0].Source != skillVersionSourceUpdate || versions[0].CreatedBy == nil || *versions[0].CreatedBy != testUserID {
		t.Fatalf("v3 metadata: %+v", versions[0])
	}

	var v1 SkillVersionResponse
	testutil.Call(t, testHandler.GetSkillVersion, skillVersionRequest(http.MethodGet, skill.ID, "1", "")).
		Want(http.StatusOK).JSON(&v1)
	if v1.Content != "v1 body" || len(v1.Files) != 1 || v1.Files[0].Path != "notes.md" || v1.Files[0].Content != "v1 notes" {
		t.Fatalf("v1 snapshot: %+v", v1)
	}

	var restored SkillWithFilesResponse
	testutil.Call(t, testHandler.RestoreSkillVersion, skillVersionRequest(http.MethodPost, skill.ID, "1", "/restore")).
		Want(http.StatusOK).JSON(&restored)
	if restored.Content != "v1 body" || len(restored.Files) != 1 || restored.Files[0].Content != "v1 notes" {
		t.Fatalf("restore response: %+v", restored)
	}
	var fileCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skill.ID).Scan(&fileCount); err != nil {
		t.Fatal(err)
	}
	if fileCount != 1 {
		t.Fatalf("restore must drop files added after v1, got %d files", fileCount)
	}

	versions = listVersions(t, skill.ID)
	if len(versions) != 4 || versions[0].Version != 4 || versions[0].Source != skillVersionSourceRestore ||
		versions[0].RestoredFromVersion == nil || *versions[0].RestoredFromVersion != 1 {
		t.Fatalf("after restore: %+v", versions)
	}

	// Restore can itself be undone by restoring the version before it.
	testutil.Call(t, testHandler.RestoreSkillVersion, skillVersionRequest(http.MethodPost, skill.ID, "3", "/restore")).
		Want(http.StatusOK).JSON(&restored)
	if restored.Content != "v2 body" || len(restored.Files) != 2 {
		t.Fatalf("restore v3: %+v", restored)
	}

	testutil.Call(t, testHandler.GetSkillVersion, skillVersionRequest(http.MethodGet, skill.ID, "99", "")).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.RestoreSkillVersion, skillVersionRequest(http.MethodPost, skill.ID, "abc", "/restore")).Want(http.StatusBadRequest)

	delReq := withURLParam(newRequest(http.MethodDelete, "/api/skills/"+skill.ID, nil), "id", skill.ID)
	testutil.Call(t, testHandler.DeleteSkill, delReq).Want(http.StatusNoContent)
	var remaining int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM skill_version WHERE skill_id = $1`, skill.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("delete skill must remove its versions, %d left", remaining)
	}
}

// Skills created before versioning have no history. The first write snapshots
// the pre-write state so that write can be reverted.
func TestSkillVersionBaselineForUnversionedSkill(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-version-baseline", nil)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM skill_version WHERE skill_id = $1`, skill.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE skill SET content = 'legacy body' WHERE id = $1`, skill.ID); err != nil {
		t.Fatal(err)
	}

	if got := listVersions(t, skill.ID); len(got) != 0 {
		t.Fatalf("unversioned skill should list no versions, got %+v", got)
	}

	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: strPtr("new body")})

	versions := listVersions(t, skill.ID)
	if len(versions) != 2 || versions[1].Source != skillVersionSourceBaseline || versions[0].Source != skillVersionSourceUpdate {
		t.Fatalf("want [v2 update, v1 baseline], got %+v", versions)
	}
	var baseline SkillVersionResponse
	testutil.Call(t, testHandler.GetSkillVersion, skillVersionRequest(http.MethodGet, skill.ID, strconv.Itoa(int(versions[1].Version)), "")).
		Want(http.StatusOK).JSON(&baseline)
	if baseline.Content != "legacy body" {
		t.Fatalf("baseline must hold the pre-write content, got %q", baseline.Content)
	}
}

func TestSkillVersionSkipsPluginSkills(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-version-plugin", nil)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM skill_version WHERE skill_id = $1`, skill.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE skill SET plugin_installation_id = gen_random_uuid() WHERE id = $1`, skill.ID); err != nil {
		t.Fatal(err)
	}

	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: strPtr("plugin edit")})
	if got := listVersions(t, skill.ID); len(got) != 0 {
		t.Fatalf("plugin skills keep no history, got %+v", got)
	}
}
