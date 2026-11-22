package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func int64Ptr(v int64) *int64 { return &v }

func updateSkillRequest(skillID string, body UpdateSkillRequest) *http.Request {
	return withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID, body), "id", skillID)
}

func skillRevisionFromDB(t *testing.T, skillID string) (int64, string) {
	t.Helper()
	var revision int64
	var content string
	if err := testPool.QueryRow(context.Background(),
		`SELECT revision, content FROM skill WHERE id = $1`, skillID).Scan(&revision, &content); err != nil {
		t.Fatal(err)
	}
	return revision, content
}

func wantSkillRevisionConflict(t *testing.T, resp *testutil.Response, expected, actual int64) {
	t.Helper()
	body := resp.Want(http.StatusConflict).Map()
	if body["code"] != "revision_conflict" || body["resource_type"] != "skill" ||
		body["expected_revision"] != float64(expected) || body["actual_revision"] != float64(actual) {
		t.Fatalf("conflict body: %+v", body)
	}
}

// Two writers that read the same revision: the second is refused and the
// first one's content survives, instead of being silently overwritten.
func TestSkillUpdateBaseRevisionConflict(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-revision-conflict", nil)
	if skill.Revision != 1 {
		t.Fatalf("new skill revision: want 1, got %d", skill.Revision)
	}

	var first SkillWithFilesResponse
	testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
		Content: strPtr("first writer"), BaseRevision: int64Ptr(1),
	})).Want(http.StatusOK).JSON(&first)
	if first.Revision != 2 {
		t.Fatalf("revision after first write: want 2, got %d", first.Revision)
	}

	wantSkillRevisionConflict(t, testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
		Content: strPtr("second writer"), BaseRevision: int64Ptr(1),
	})), 1, 2)

	if revision, content := skillRevisionFromDB(t, skill.ID); revision != 2 || content != "first writer" {
		t.Fatalf("conflicting write must change nothing: revision %d, content %q", revision, content)
	}

	// Retrying from the revision the conflict reported succeeds.
	var retried SkillWithFilesResponse
	testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
		Content: strPtr("second writer, merged"), BaseRevision: int64Ptr(2),
	})).Want(http.StatusOK).JSON(&retried)
	if retried.Revision != 3 || retried.Content != "second writer, merged" {
		t.Fatalf("retry: revision %d, content %q", retried.Revision, retried.Content)
	}
}

// The check and the write share the row lock, so writers racing from the same
// revision cannot both pass the check.
func TestSkillUpdateBaseRevisionConcurrentWriters(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-revision-race", nil)

	const writers = 4
	codes := make([]int, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
				Content: strPtr("writer " + strconv.Itoa(i)), BaseRevision: int64Ptr(1),
			}))
			codes[i] = resp.Code
		}(i)
	}
	wg.Wait()

	ok, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicts++
		}
	}
	if ok != 1 || conflicts != writers-1 {
		t.Fatalf("want exactly one winner and %d conflicts, got codes %v", writers-1, codes)
	}
	if revision, _ := skillRevisionFromDB(t, skill.ID); revision != 2 {
		t.Fatalf("one successful write must leave revision 2, got %d", revision)
	}
}

// Clients that predate skill revisions send no base_revision and keep
// last-write-wins, while still advancing the revision for clients that do.
func TestSkillUpdateWithoutBaseRevisionStillWrites(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-revision-legacy", nil)
	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: strPtr("legacy one")})
	updateSkillForVersionTest(t, skill.ID, UpdateSkillRequest{Content: strPtr("legacy two")})

	if revision, content := skillRevisionFromDB(t, skill.ID); revision != 3 || content != "legacy two" {
		t.Fatalf("legacy writes: revision %d, content %q", revision, content)
	}

	// A revision-aware writer that read before those saves is refused.
	wantSkillRevisionConflict(t, testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
		Content: strPtr("stale"), BaseRevision: int64Ptr(1),
	})), 1, 3)
}

func TestSkillUpdateRejectsNonPositiveBaseRevision(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-revision-invalid", nil)
	testutil.Call(t, testHandler.UpdateSkill, updateSkillRequest(skill.ID, UpdateSkillRequest{
		Content: strPtr("x"), BaseRevision: int64Ptr(0),
	})).Want(http.StatusBadRequest)
	if revision, _ := skillRevisionFromDB(t, skill.ID); revision != 1 {
		t.Fatalf("rejected write must not bump revision, got %d", revision)
	}
}

// Supporting-file writes replace a file's whole body, so they take the same
// precondition, and they advance the skill revision like any other write.
func TestSkillFileWritesBaseRevision(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	skill := createVersionTestSkill(t, "test-skill-revision-files", []CreateSkillFileRequest{
		{Path: "notes.md", Content: "original notes"},
	})
	upsert := func(body UpsertSkillFileRequest) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID+"/files", body), "id", skill.ID)
		return testutil.Call(t, testHandler.UpsertSkillFile, req)
	}
	notes := func() string {
		var content string
		if err := testPool.QueryRow(context.Background(),
			`SELECT content FROM skill_file WHERE skill_id = $1 AND path = 'notes.md'`, skill.ID).Scan(&content); err != nil {
			t.Fatal(err)
		}
		return content
	}

	upsert(UpsertSkillFileRequest{
		CreateSkillFileRequest: CreateSkillFileRequest{Path: "notes.md", Content: "first notes"},
		BaseRevision:           int64Ptr(1),
	}).Want(http.StatusOK)

	wantSkillRevisionConflict(t, upsert(UpsertSkillFileRequest{
		CreateSkillFileRequest: CreateSkillFileRequest{Path: "notes.md", Content: "stale notes"},
		BaseRevision:           int64Ptr(1),
	}), 1, 2)
	if got := notes(); got != "first notes" {
		t.Fatalf("conflicting file write must change nothing, got %q", got)
	}

	// Without base_revision the upsert behaves as before.
	upsert(UpsertSkillFileRequest{
		CreateSkillFileRequest: CreateSkillFileRequest{Path: "notes.md", Content: "legacy notes"},
	}).Want(http.StatusOK)
	if got := notes(); got != "legacy notes" {
		t.Fatalf("legacy upsert: got %q", got)
	}

	var meta SkillWithFileMetadataResponse
	getReq := withURLParam(newRequest(http.MethodGet, "/api/skills/"+skill.ID+"?include=metadata", nil), "id", skill.ID)
	testutil.Call(t, testHandler.GetSkill, getReq).Want(http.StatusOK).JSON(&meta)
	if meta.Revision != 3 {
		t.Fatalf("metadata revision: want 3, got %d", meta.Revision)
	}

	var fileID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM skill_file WHERE skill_id = $1 AND path = 'notes.md'`, skill.ID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	deleteFile := func(query string) *testutil.Response {
		req := withURLParams(newRequest(http.MethodDelete, "/api/skills/"+skill.ID+"/files/"+fileID+query, nil),
			"id", skill.ID, "fileId", fileID)
		return testutil.Call(t, testHandler.DeleteSkillFile, req)
	}
	wantSkillRevisionConflict(t, deleteFile("?base_revision=2"), 2, 3)
	if got := notes(); got != "legacy notes" {
		t.Fatalf("conflicting delete must keep the file, got %q", got)
	}
	deleteFile("?base_revision=abc").Want(http.StatusBadRequest)
	deleteFile("?base_revision=3").Want(http.StatusNoContent)
	if revision, _ := skillRevisionFromDB(t, skill.ID); revision != 4 {
		t.Fatalf("file delete must bump revision to 4, got %d", revision)
	}
}
