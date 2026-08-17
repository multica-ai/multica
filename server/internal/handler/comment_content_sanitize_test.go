package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestCreateComment_StripsNullBytesInsteadOf500 pins the fix for GH #5388.
//
// A comment whose content carries a byte PostgreSQL's TEXT type cannot store —
// most commonly an embedded NUL (SQLSTATE 22021) that survives a JSON round
// trip from `--content-file` — must post successfully with the offending byte
// stripped, not fail the INSERT with an opaque 500 the CLI renders as a
// generic "server unavailable" (and then retries forever).
func TestCreateComment_StripsNullBytesInsteadOf500(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createTestIssue(t, "null-byte comment fixture (GH #5388)", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "diagnosis body\x00 with a stray NUL byte",
	})
	r = withURLParam(r, "id", issueID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment with NUL byte: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got, _ := body["content"].(string)
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("stored content still contains a NUL byte: %q", got)
	}
	if want := "diagnosis body with a stray NUL byte"; got != want {
		t.Fatalf("stored content: expected %q (NUL stripped), got %q", want, got)
	}
}

// TestCommentTriggers_NullByteHiddenMention_PreviewMatchesEnqueue guards the
// preview/side-effect divergence raised in review of the #5388 fix.
//
// CreateComment/UpdateComment sanitize content (strip NUL) before storing and
// triggering, so a mention hidden behind a NUL — valid only AFTER the byte is
// stripped — would enqueue an agent. PreviewCommentTriggers must sanitize with
// the same entry point, or it would report an empty target set while submit
// silently enqueues the agent. This pins parity for both the create and edit
// paths (the fix touches both).
func TestCommentTriggers_NullByteHiddenMention_PreviewMatchesEnqueue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createCommentTriggerPreviewIssue(t, "NUL-hidden mention parity (GH #5388 review)", "", "")
	// Distinct agents for create vs edit: a single (issue, agent) pair allows
	// only one pending task (idx_one_pending_task_per_issue_agent), which would
	// mask the second enqueue.
	createTarget := createHandlerTestAgent(t, "Preview NUL Parity Create", nil)
	editTarget := createHandlerTestAgent(t, "Preview NUL Parity Edit", nil)

	// mentionWithHiddenNull places the NUL between the agent UUID and the
	// closing ')', so MentionRe does not match the raw text but does match once
	// the byte is stripped.
	mentionWithHiddenNull := func(agentID string) string {
		return fmt.Sprintf("please take a look [@Target](mention://agent/%s\x00)", agentID)
	}
	previewAgentIDs := func(resp CommentTriggerPreviewResponse) map[string]bool {
		ids := make(map[string]bool, len(resp.Agents))
		for _, a := range resp.Agents {
			ids[a.ID] = true
		}
		return ids
	}

	// --- create path ---
	createContent := mentionWithHiddenNull(createTarget)
	if n := len(util.ParseMentions(createContent)); n != 0 {
		t.Fatalf("precondition: raw NUL content should not parse as a mention, got %d", n)
	}
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": createContent})
	if ids := previewAgentIDs(preview); len(ids) != 1 || !ids[createTarget] {
		t.Fatalf("create preview targets = %v, want exactly {%s}", ids, createTarget)
	}
	postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": createContent})
	if got := countQueuedCommentTriggerTasks(t, issueID, createTarget); got != 1 {
		t.Fatalf("create enqueued %d tasks for the mentioned agent, want 1 (parity with preview)", got)
	}

	// --- edit path ---
	editContent := mentionWithHiddenNull(editTarget)
	plainID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": "no mentions here yet"})
	editPreview := previewCommentTriggersForTest(t, issueID, map[string]any{
		"content":            editContent,
		"editing_comment_id": plainID,
	})
	if ids := previewAgentIDs(editPreview); len(ids) != 1 || !ids[editTarget] {
		t.Fatalf("edit preview targets = %v, want exactly {%s}", ids, editTarget)
	}
	updateCommentForTriggerPreviewTest(t, plainID, map[string]any{"content": editContent})
	if got := countQueuedCommentTriggerTasks(t, issueID, editTarget); got != 1 {
		t.Fatalf("edit enqueued %d tasks for the mentioned agent, want 1 (parity with preview)", got)
	}
}
