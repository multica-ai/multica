package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/rundiff"
)

func diffPost(t *testing.T, id string, c rundiff.Collection) *http.Request {
	t.Helper()
	req := testutil.WithURLParams(testutil.JSONRequest("POST", "/api/daemon/tasks/"+id+"/file-changes", c), "taskId", id)
	// The fixture runtime's owner, as a real daemon authenticates.
	req.Header.Set("X-User-ID", testUserID)
	return req
}
func diffGet(id, ws, suffix string) *http.Request {
	req := httptest.NewRequest("GET", "/api/tasks/"+id+"/file-changes"+suffix, nil)
	req = testutil.WithURLParams(req, "taskId", id)
	return req.WithContext(middleware.SetMemberContext(req.Context(), ws, db.Member{}))
}
func TestRunFileChangesPersistenceContract(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}
	id := seedBatchTask(t, "run-diff")
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", id)
	legacy := testutil.Decode[rundiff.Collection](t, testHandler.ListRunFileChanges, diffGet(id, testWorkspaceID, ""), 200)
	if legacy.Status != "unavailable" || legacy.Reason != "legacy_no_baseline" {
		t.Fatalf("%+v", legacy)
	}
	c := rundiff.Empty(uuid.NewString(), "collecting", "")
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", id)
	interrupted := testutil.Decode[rundiff.Collection](t, testHandler.ListRunFileChanges, diffGet(id, testWorkspaceID, ""), 200)
	if interrupted.Status != "unavailable" || interrupted.Reason != "interrupted" {
		t.Fatalf("%+v", interrupted)
	}
	a, b := "prior dirty", strings.Repeat("x", 210*1024)+"\nTOKEN=hidden-value\n"
	c.Status = "ready"
	c.Roots = []rundiff.Root{{ID: "a", Label: "a", Kind: "git", Coverage: "complete"}, {ID: "b", Label: "b", Kind: "git", Coverage: "complete"}}
	c.Files = []rundiff.File{{ID: "1", RootID: "a", Path: "same.txt", Change: "modified", BeforeBytes: 11, AfterBytes: int64(len(b)), BodyStatus: "available", Before: &a, After: &b}, {ID: "2", RootID: "b", Path: "same.txt", Change: "added", AfterBytes: 0, BodyStatus: "available", After: new(string)}}
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)
	page := testutil.Decode[rundiff.Collection](t, testHandler.ListRunFileChanges, diffGet(id, testWorkspaceID, "?limit=1"), 200)
	if page.FilesTotal != 2 || len(page.Files) != 1 || page.NextCursor == nil || page.Files[0].After != nil {
		t.Fatalf("page: %+v", page)
	}
	// A legacy-style upload that still carries git
	// bodies is accepted with 200 but stored with bodies='{}'.
	var bodiesRaw string
	dbfx.QueryRow(t, `SELECT bodies::text FROM run_file_change WHERE task_id=$1`, id).Scan(&bodiesRaw)
	if bodiesRaw != "{}" {
		t.Fatalf("git bodies persisted: %s", bodiesRaw)
	}

	// Collection immutability: a different collection id or a different final
	// snapshot for the same id conflicts.
	other := c
	other.CollectionID = uuid.NewString()
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, other)).Want(409)
	c.Files = c.Files[:1]
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(409)

	// Git-root files relay to the runtime instead of reading stored bodies.
	// The fixture task has no runtime row, so the relay answers offline
	// before any daemon round trip is paid.
	dbfx.Exec(t, "UPDATE agent_task_queue SET completed_at=now(), runtime_id=NULL WHERE id=$1", id)
	relayReq := testutil.WithURLParams(diffGet(id, testWorkspaceID, "/1"), "taskId", id, "fileId", "1")
	var relayErr map[string]string
	relay := testutil.Call(t, testHandler.GetRunFileChange, relayReq).Want(http.StatusServiceUnavailable)
	relay.JSON(&relayErr)
	if relayErr["code"] != "runtime_offline" {
		t.Fatalf("relay error: %+v", relayErr)
	}
	if got := relay.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("503 cache-control: %q", got)
	}
	testutil.Call(t, testHandler.ListRunFileChanges, diffGet(id, uuid.NewString(), "")).Want(404)
	testutil.Call(t, testHandler.ListRunFileChanges, diffGet(id, testWorkspaceID, "?cursor=-1")).Want(400)
}
func TestRunFileChangesRejectInvalidPayloadAndWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}
	id := seedBatchTask(t, "run-diff-invalid")
	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "r", Label: "r", Kind: "git", Coverage: "complete"}}
	for _, path := range []string{"../secret", ".env", "/absolute", "a/../b"} {
		c.Files = []rundiff.File{{ID: "f", RootID: "r", Path: path, Change: "added", BodyStatus: "available"}}
		testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(400)
	}
	req := diffPost(t, id, rundiff.Empty(uuid.NewString(), "collecting", ""))
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), uuid.NewString(), "wrong"))
	testutil.Call(t, testHandler.SaveRunFileChanges, req).Want(404)
	testutil.Call(t, testHandler.ListRunFileChanges, diffGet(id, uuid.NewString(), "")).Want(404)
}

// directory roots have no object database on the runtime, so their
// bodies keep the inline JSON path — including the redaction pass — and a
// cleared bodies row answers "purged" instead of pretending nothing changed.
func TestRunFileChangesDirectoryRootBodyLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}
	id := seedBatchTask(t, "run-diff-dir")
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", id)
	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "d", Label: "workdir", Kind: "directory", Coverage: "complete"}}
	secret := strings.Repeat("x", 210*1024) + "\nTOKEN=dir-hidden-value\n"
	c.Files = []rundiff.File{{ID: "d1", RootID: "d", Path: "notes.txt", Change: "modified", BeforeBytes: 11, AfterBytes: int64(len(secret)), BodyStatus: "available", Before: ptr("old"), After: &secret}}
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)

	req := testutil.WithURLParams(diffGet(id, testWorkspaceID, "/d1"), "taskId", id, "fileId", "d1")
	body := testutil.Decode[rundiff.File](t, testHandler.GetRunFileChange, req, 200)
	if body.After == nil || strings.Contains(*body.After, "dir-hidden-value") || body.BodyStatus != "redacted" {
		t.Fatalf("directory body/redaction mismatch: after=%s status=%s", *body.After, body.BodyStatus)
	}

	// After the bodies-clearing migration the same row answers 410 purged.
	dbfx.Exec(t, "UPDATE run_file_change SET bodies='{}' WHERE task_id=$1", id)
	purged := testutil.Call(t, testHandler.GetRunFileChange, req).Want(http.StatusGone)
	if got := purged.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("410 cache-control: %q", got)
	}
}

// The capture-time body digest must degrade a drifted row instead of
// rendering content nothing vouches for: a stored directory body that no
// longer matches its manifest anchor answers 410 digest_mismatch, and a
// digest that does not cover its own manifest is refused at upload.
func TestRunFileChangesDigestMismatchDegrades(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}
	id := seedBatchTask(t, "run-diff-digest")
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", id)
	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "d", Label: "workdir", Kind: "directory", Coverage: "complete"}}
	before, after := "old body", "new body"
	c.Files = []rundiff.File{{ID: "d1", RootID: "d", Path: "notes.txt", Change: "modified", BeforeBytes: int64(len(before)), AfterBytes: int64(len(after)), BodyStatus: "available", Before: &before, After: &after}}
	// The collector pins these at capture time; emulate that before upload.
	for i := range c.Files {
		c.Files[i].BodySHA256 = rundiff.BodyDigest(stringOf(c.Files[i].Before), stringOf(c.Files[i].After))
	}
	c.Digest = c.ComputeDigest()
	c.DigestVersion = rundiff.DigestVersion
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)

	req := testutil.WithURLParams(diffGet(id, testWorkspaceID, "/d1"), "taskId", id, "fileId", "d1")
	testutil.Decode[rundiff.File](t, testHandler.GetRunFileChange, req, 200)

	// Rewrite the stored row's body behind the manifest's back (the drift the
	// digest exists to catch) and require the degraded answer.
	dbfx.Exec(t, `UPDATE run_file_change
	  SET bodies = jsonb_set(bodies, '{d1,after}', '"TAMPERED"')
	  WHERE task_id=$1`, id)
	mismatch := testutil.Call(t, testHandler.GetRunFileChange, req).Want(http.StatusGone)
	var errBody map[string]string
	mismatch.JSON(&errBody)
	if errBody["code"] != "digest_mismatch" {
		t.Fatalf("drifted body must answer digest_mismatch, got %+v", errBody)
	}
	if got := mismatch.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("mismatch cache-control: %q", got)
	}

	// An upload whose digest does not cover its own manifest is rejected
	// before anything is stored.
	bad := rundiff.Empty(uuid.NewString(), "ready", "")
	bad.Roots = c.Roots
	bad.Files = []rundiff.File{c.Files[0]}
	bad.Files[0].BodySHA256 = rundiff.BodyDigest("before", "after")
	bad.Digest = rundiff.BodyDigest("x", "y") // covers nothing
	bad.DigestVersion = rundiff.DigestVersion
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, bad)).Want(400)
}

// stubFileChangeRelayStore drives GetRunFileChange's relay branches to
// controlled outcomes so the failure envelopes are observable without a live
// daemon: an empty store never answers (504), one holding a purged response
// answers immediately (410).
type stubFileChangeRelayStore struct {
	resp *FileChangeRelayResponse
}

func (s *stubFileChangeRelayStore) Create(_ context.Context, _ *FileChangeRelayRequest) error {
	return nil
}
func (s *stubFileChangeRelayStore) ClaimPending(_ context.Context, _ string) ([]PendingView, error) {
	return nil, nil
}
func (s *stubFileChangeRelayStore) Get(_ context.Context, _ string) (*FileChangeRelayRequest, error) {
	return nil, nil
}
func (s *stubFileChangeRelayStore) PutResponse(_ context.Context, _ string, _ *FileChangeRelayResponse) error {
	return nil
}
func (s *stubFileChangeRelayStore) WaitResponse(_ context.Context, _ string, _ time.Time) (*FileChangeRelayResponse, bool) {
	if s.resp != nil {
		return s.resp, true
	}
	return nil, false
}

// Relay failures are per-request truth (a purged record stays purged, an
// offline runtime recovers in seconds), so every relay error envelope must
// carry Cache-Control: no-store — a cached 410 would keep reporting "purged"
// after the content returns.
func TestRunFileChangeRelayErrorsAreNoStore(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}

	runtimeID := dbfx.Runtime(t, "run-diff relay no-store runtime")
	agentID := dbfx.Agent(t, "run-diff relay no-store agent", runtimeID)
	issueID := dbfx.Issue(t, "run-diff relay no-store")
	id := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", id)

	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "g", Label: "repo", Kind: "git", Coverage: "complete"}}
	c.Files = []rundiff.File{{ID: "g1", RootID: "g", Path: "a.txt", Change: "modified", BeforeBytes: 4, AfterBytes: 5, BodyStatus: "available"}}
	// Capture pins the served content; the manifest carries the anchor.
	c.Files[0].BodySHA256 = rundiff.BodyDigest("captured-before", "captured-after")
	c.Digest = c.ComputeDigest()
	c.DigestVersion = rundiff.DigestVersion
	testutil.Call(t, testHandler.SaveRunFileChanges, diffPost(t, id, c)).Want(200)
	get := testutil.WithURLParams(diffGet(id, testWorkspaceID, "/g1"), "taskId", id, "fileId", "g1")

	// A relayed body whose recomputed digest disagrees with the manifest
	// anchor degrades to digest_mismatch instead of rendering.
	mismatchPayload, _ := json.Marshal(rundiff.File{ID: "g1", RootID: "g", Path: "a.txt", Change: "modified", BodyStatus: "available", Before: ptr("stale"), After: ptr("content"), BodySHA256: rundiff.BodyDigest("stale", "content")})
	h := *testHandler
	h.FileChangeRelayStore = &stubFileChangeRelayStore{resp: &FileChangeRelayResponse{State: "ok", File: mismatchPayload}}
	drifted := testutil.Call(t, h.GetRunFileChange, get).Want(http.StatusGone)
	var driftErr map[string]string
	drifted.JSON(&driftErr)
	if driftErr["code"] != "digest_mismatch" {
		t.Fatalf("drifted relay must answer digest_mismatch, got %+v", driftErr)
	}
	if got := drifted.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("mismatch cache-control: %q", got)
	}
	// The matching relay passes and serves the body.
	relayPayload, _ := json.Marshal(rundiff.File{ID: "g1", RootID: "g", Path: "a.txt", Change: "modified", BodyStatus: "available", Before: ptr("captured-before"), After: ptr("captured-after"), BodySHA256: rundiff.BodyDigest("captured-before", "captured-after")})
	h.FileChangeRelayStore = &stubFileChangeRelayStore{resp: &FileChangeRelayResponse{State: "ok", File: relayPayload}}
	testutil.Decode[rundiff.File](t, h.GetRunFileChange, get, 200)

	// 504: the daemon never answers within the wait window.
	h.FileChangeRelayStore = &stubFileChangeRelayStore{}
	timeout := testutil.Call(t, h.GetRunFileChange, get).Want(http.StatusGatewayTimeout)
	if got := timeout.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("504 cache-control: %q", got)
	}

	// 410: the daemon answers purged.
	h.FileChangeRelayStore = &stubFileChangeRelayStore{resp: &FileChangeRelayResponse{State: "purged"}}
	gone := testutil.Call(t, h.GetRunFileChange, get).Want(http.StatusGone)
	if got := gone.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("410 cache-control: %q", got)
	}

	// 429: every relay slot on this instance is already waiting.
	h.FileChangeRelayStore = &stubFileChangeRelayStore{}
	for i := 0; i < fileChangeRelayMaxInFlight; i++ {
		h.fileChangeRelaySlots <- struct{}{}
	}
	busy := testutil.Call(t, h.GetRunFileChange, get).Want(http.StatusTooManyRequests)
	if got := busy.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("429 cache-control: %q", got)
	}
	if retry := busy.Header().Get("Retry-After"); retry == "" {
		t.Fatal("429 missing Retry-After")
	}
	for i := 0; i < fileChangeRelayMaxInFlight; i++ {
		<-h.fileChangeRelaySlots
	}
}
