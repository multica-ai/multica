package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// batchSkillNameSeq keeps generated fixture names unique within the process.
var batchSkillNameSeq atomic.Int64

// newBatchSkillFixture builds a skill with two supporting files so batch tests
// can mix creates and updates while still leaving one file untouched to prove
// the endpoint is partial, not full replacement.
func newBatchSkillFixture(t *testing.T) string {
	t.Helper()
	skillID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         fmt.Sprintf("batch-upsert-fixture-%d", batchSkillNameSeq.Add(1)),
		"content":      "# primary",
		"created_by":   testUserID,
	})
	for path, body := range map[string]string{
		"docs/existing.md": "existing-body\n",
		"keep.md":          "keep-body\n",
	} {
		dbfx.Insert(t, "skill_file", testutil.Cols{
			"skill_id": skillID,
			"path":     path,
			"content":  body,
		})
	}
	return skillID
}

// batchUpsertRequest builds a PUT request against UpsertSkillFilesBatch.
func batchUpsertRequest(t *testing.T, skillID string, body any) *http.Request {
	t.Helper()
	return withURLParam(newRequest(http.MethodPut, "/api/skills/"+skillID+"/files/batch", body), "id", skillID)
}

// entry is shorthand for building one files[] element in request bodies.
type entry struct {
	path           string
	content        string
	expectedSHA256 string // empty = unconditional
}

func entriesBody(es []entry, extra map[string]any) map[string]any {
	files := make([]map[string]any, 0, len(es))
	for _, e := range es {
		f := map[string]any{"path": e.path, "content": e.content}
		if e.expectedSHA256 != "" {
			f["expected_sha256"] = e.expectedSHA256
		}
		files = append(files, f)
	}
	body := map[string]any{"files": files}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func sha256hex(s string) string { return contentHash(s) }

// skillFilesAggregate mirrors the server-side aggregate definition over the
// fixture's initial file set so guard tests can produce both matching and
// stale tokens without another round trip.
func skillFilesAggregate(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j] < paths[i] {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	digest := sha256.New()
	for _, p := range paths {
		digest.Write([]byte(p))
		digest.Write([]byte{0x00})
		digest.Write([]byte(files[p]))
		digest.Write([]byte{0x0A})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func batchFileContent(t *testing.T, skillID, path string) (string, bool) {
	t.Helper()
	var content string
	err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill_file WHERE skill_id = $1 AND path = $2`,
		parseUUID(skillID), path).Scan(&content)
	return content, err == nil
}

func assertUntouchedFiles(t *testing.T, skillID string) {
	t.Helper()
	// keep.md is never part of any batch in these tests: it surviving is what
	// proves the endpoint is partial. docs/existing.md may or may not have
	// been updated depending on the case.
	if got, _ := batchFileContent(t, skillID, "keep.md"); got != "keep-body\n" {
		t.Errorf("untouched file changed: %q", got)
	}
}

func TestUpsertSkillFilesBatchAppliesMixedBatchAndLeavesUntouchedFiles(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	body := entriesBody([]entry{
		{path: "docs/existing.md", content: "updated-body\n", expectedSHA256: sha256hex("existing-body\n")},
		{path: "new.md", content: "brand-new\n"},
	}, nil)

	req := batchUpsertRequest(t, skillID, body)
	var resp UpsertSkillFilesBatchResponse
	testutil.Call(t, testHandler.UpsertSkillFilesBatch, req).Want(http.StatusOK).JSON(&resp)

	if len(resp.Files) != 2 {
		t.Fatalf("got %d files in response, want 2", len(resp.Files))
	}
	if got, _ := batchFileContent(t, skillID, "docs/existing.md"); got != "updated-body\n" {
		t.Errorf("update not applied: %q", got)
	}
	gotNew, ok := batchFileContent(t, skillID, "new.md")
	if !ok || gotNew != "brand-new\n" {
		t.Errorf("create not applied: %q found=%v", gotNew, ok)
	}
	assertUntouchedFiles(t, skillID)

	// The returned aggregate must be exactly what a client would hand back as
	// its next expected_skill_sha256 — recompute it from the persisted state
	// instead of trusting the handler's own inputs.
	all := map[string]string{"keep.md": "keep-body\n"}
	for _, f := range resp.Files {
		all[f.Path] = f.Content
	}
	if want := skillFilesAggregate(all); resp.SkillFilesSHA256 != want {
		t.Errorf("skill_files_sha256 = %q, want %q", resp.SkillFilesSHA256, want)
	}

	// A follow-up replay of the same writes — with expectations refreshed from
	// the previous response, the way a real client retries — is idempotent:
	// no extra rows, contents unchanged.
	replayBody := entriesBody([]entry{
		{path: "docs/existing.md", content: "updated-body\n", expectedSHA256: sha256hex("updated-body\n")},
		{path: "new.md", content: "brand-new\n"},
	}, nil)
	replayReq := batchUpsertRequest(t, skillID, replayBody)
	var replayResp UpsertSkillFilesBatchResponse
	testutil.Call(t, testHandler.UpsertSkillFilesBatch, replayReq).Want(http.StatusOK).JSON(&replayResp)
	if n := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID); n != 3 {
		t.Errorf("after replay got %d files, want 3", n)
	}
	if replayResp.SkillFilesSHA256 != resp.SkillFilesSHA256 {
		t.Errorf("replay aggregate = %q, want stable %q", replayResp.SkillFilesSHA256, resp.SkillFilesSHA256)
	}
}

func TestUpsertSkillFilesBatchStalePerFileHashConflictsWithZeroWrites(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	before := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID)

	// One entry carries a stale digest alongside an unconditional create: the
	// whole batch must reject without applying either write.
	body := entriesBody([]entry{
		{path: "docs/existing.md", content: "should-not-land\n", expectedSHA256: strings.Repeat("ab", 32)},
		{path: "sneaky-new.md", content: "nor-this\n"},
	}, nil)

	res := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body))
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d: %s", res.Code, res.Text())
	}

	if after := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID); after != before {
		t.Fatalf("conflict wrote rows: count before=%d after=%d", before, after)
	}
	if got, _ := batchFileContent(t, skillID, "docs/existing.md"); got != "existing-body\n" {
		t.Errorf("conditional update landed despite conflict: %q", got)
	}
	if _, ok := batchFileContent(t, skillID, "sneaky-new.md"); ok {
		t.Error("unconditional create landed despite conflict")
	}
	assertUntouchedFiles(t, skillID)
}

func TestUpsertSkillFilesBatchExpectedHashForMissingPathConflicts(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	body := entriesBody([]entry{
		{path: "ghost.md", content: "x\n", expectedSHA256: strings.Repeat("cd", 32)},
	}, nil)

	res := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body))
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409 for expected_sha256 on missing path, got %d: %s", res.Code, res.Text())
	}
	assertUntouchedFiles(t, skillID)
}

func TestUpsertSkillFilesBatchAggregateHashGuard(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	t.Run("stale aggregate rejects everything", func(t *testing.T) {
		skillID := newBatchSkillFixture(t)
		stale := skillFilesAggregate(map[string]string{
			"docs/existing.md": "existing-body\n",
			"keep.md":          "keep-body\n",
			"gone.md":          "older-file-no-longer-present\n",
		})
		body := entriesBody([]entry{{path: "new.md", content: "n\n"}}, map[string]any{"expected_skill_sha256": stale})

		res := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body))
		if res.Code != http.StatusConflict {
			t.Fatalf("expected 409 for stale aggregate, got %d: %s", res.Code, res.Text())
		}
		if _, ok := batchFileContent(t, skillID, "new.md"); ok {
			t.Error("write landed despite stale aggregate guard")
		}
	})

	t.Run("matching aggregate admits the batch", func(t *testing.T) {
		skillID := newBatchSkillFixture(t)
		current := skillFilesAggregate(map[string]string{
			"docs/existing.md": "existing-body\n",
			"keep.md":          "keep-body\n",
		})
		body := entriesBody([]entry{{path: "new.md", content: "n\n"}},
			map[string]any{"expected_skill_sha256": current, "idempotency_key": "key-1"})

		var resp UpsertSkillFilesBatchResponse
		testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body)).
			Want(http.StatusOK).JSON(&resp)
		if resp.IdempotencyKey != "key-1" {
			t.Errorf("idempotency_key echoed = %q, want key-1", resp.IdempotencyKey)
		}
	})
}

func TestUpsertSkillFilesBatchRejectsBadPathsAndPayloads(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"reserved SKILL.md", entriesBody([]entry{{path: "SKILL.md", content: "override"}}, nil)},
		{"traversal path", entriesBody([]entry{{path: "../evil.md", content: "x"}}, nil)},
		{"absolute path", entriesBody([]entry{{path: "/etc/passwd", content: "x"}}, nil)},
		{"duplicate paths", entriesBody([]entry{
			{path: "dup.md", content: "a"}, {path: "dup.md", content: "b"},
		}, nil)},
		{"bad per-file hash", entriesBody([]entry{{path: "a.md", content: "a", expectedSHA256: "not-hex"}}, nil)},
		{"bad aggregate hash", entriesBody([]entry{{path: "a.md", content: "a"}}, map[string]any{"expected_skill_sha256": "zz81"})},
		{"empty files", map[string]any{"files": []any{}}},
		{"idempotency key too long", entriesBody([]entry{{path: "a.md", content: "a"}},
			map[string]any{"idempotency_key": strings.Repeat("k", 129)})},
		{"oversized batch", func() map[string]any {
			es := make([]entry, 0, maxBatchUpsertFiles+1)
			for i := 0; i <= maxBatchUpsertFiles; i++ {
				es = append(es, entry{path: fmt.Sprintf("f/%03d.md", i), content: "x"})
			}
			return entriesBody(es, nil)
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID)
			res := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, tc.body))
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Text())
			}
			if after := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID); after != before {
				t.Errorf("rejected request wrote rows: before=%d after=%d", before, after)
			}
		})
	}
}

func TestUpsertSkillFilesBatchNonCreatorMemberForbidden(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	otherUser := dbfx.User(t, "plain-member-batch", "plain-member-batch@example.com")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")

	body := entriesBody([]entry{{path: "docs/existing.md", content: "hijack\n"}}, nil)
	req := testutil.WithHeaders(batchUpsertRequest(t, skillID, body), "X-User-ID", otherUser)

	res := testutil.Call(t, testHandler.UpsertSkillFilesBatch, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-creator member, got %d: %s", res.Code, res.Text())
	}
	assertUntouchedFiles(t, skillID)
}

func TestUpsertSkillFilesBatchUTF8RoundTrip(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	const cjk = "中文内容 🚀 带换行\n"
	body := entriesBody([]entry{{path: "i18n/中文.md", content: cjk}}, nil)
	var resp UpsertSkillFilesBatchResponse
	testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body)).
		Want(http.StatusOK).JSON(&resp)

	got, ok := batchFileContent(t, skillID, "i18n/中文.md")
	if !ok || got != cjk {
		t.Errorf("UTF-8 round trip mismatch: got %q, want %q", got, cjk)
	}
}

func TestUpsertSkillFilesBatchIdempotentReplayDoesNotBumpUpdatedAt(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID := newBatchSkillFixture(t)

	body := entriesBody([]entry{{path: "docs/existing.md", content: "v1\n"}}, nil)
	first := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body))
	if first.Code != http.StatusOK {
		t.Fatalf("first write: expected 200, got %d: %s", first.Code, first.Text())
	}

	// Capture the timestamp right after the first write.
	var firstAt string
	dbfx.QueryRow(t,
		`SELECT updated_at::text FROM skill_file WHERE skill_id = $1 AND path = 'docs/existing.md'`,
		parseUUID(skillID)).Scan(&firstAt)

	// Replaying the identical payload must be a no-op: same content, so the
	// handler should skip the UPSERT and leave updated_at untouched.
	replay := testutil.Call(t, testHandler.UpsertSkillFilesBatch, batchUpsertRequest(t, skillID, body))
	if replay.Code != http.StatusOK {
		t.Fatalf("replay: expected 200, got %d: %s", replay.Code, replay.Text())
	}

	var afterAt string
	dbfx.QueryRow(t,
		`SELECT updated_at::text FROM skill_file WHERE skill_id = $1 AND path = 'docs/existing.md'`,
		parseUUID(skillID)).Scan(&afterAt)

	if afterAt != firstAt {
		t.Errorf("idempotent replay bumped updated_at: before=%q after=%q", firstAt, afterAt)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID); n != 2 {
		t.Errorf("after replay got %d files, want 2", n)
	}
}
