package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedAskUserQuestion inserts an issue + a single-select ask_user_question
// comment authored by a synthetic agent. Returns issueID, commentID.
func seedAskUserQuestion(t *testing.T, targetUserID string) (string, string) {
	return seedAskUserQuestionOpts(t, targetUserID, false, false)
}

// seedAskUserQuestionOpts is seedAskUserQuestion with explicit multi_select /
// allow_custom flags.
func seedAskUserQuestionOpts(t *testing.T, targetUserID string, multiSelect, allowCustom bool) (string, string) {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "ask_user_question fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// source_user is a synthetic agent id — the answer path only formats it into
	// the reply mention (falling back to the raw id when the agent is absent),
	// so it does not need to be a real agent row for this test.
	meta := map[string]any{
		"ask_user_question": map[string]any{
			"target_user": targetUserID,
			"source_user": "33333333-3333-3333-3333-333333333333",
			"question":    "Which cache?",
			"options": []map[string]any{
				{"label": "Redis", "description": "distributed"},
				{"label": "Local", "description": "single-node"},
			},
			"multi_select": multiSelect,
			"allow_custom": allowCustom,
		},
	}
	metaJSON, _ := json.Marshal(meta)

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, metadata)
		VALUES ($1, $2, 'agent', $3, $4, 'ask_user_question', $5)
		RETURNING id
	`, issueID, testWorkspaceID, "33333333-3333-3333-3333-333333333333",
		"**Which cache?**", metaJSON).Scan(&commentID); err != nil {
		t.Fatalf("insert ask_user_question comment: %v", err)
	}
	return issueID, commentID
}

func answerReq(t *testing.T, userID, commentID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, "POST", "/api/comments/"+commentID+"/answer", body)
	req = withURLParam(req, "commentId", commentID)
	testHandler.AnswerAskUserQuestion(w, req)
	return w
}

// TestAnswerAskUserQuestion_SubmittedByTarget: the target user submits a valid
// selection → 200, metadata records the answer, and a confirmation reply is
// posted that mentions the source agent.
func TestAnswerAskUserQuestion_SubmittedByTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, commentID := seedAskUserQuestion(t, testUserID)

	idx := 1
	w := answerReq(t, testUserID, commentID, map[string]any{"state": "submitted", "selected_index": idx})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Metadata == nil || resp.Metadata.AskUserQuestion == nil || resp.Metadata.AskUserQuestion.Answer == nil {
		t.Fatalf("expected answer recorded in metadata, got %+v", resp.Metadata)
	}
	ans := resp.Metadata.AskUserQuestion.Answer
	if ans.State != "submitted" || ans.SelectedIndex == nil || *ans.SelectedIndex != 1 {
		t.Fatalf("unexpected answer: %+v", ans)
	}

	// A confirmation reply must have been posted, parented at the question.
	var replyCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND parent_id = $2 AND type = 'comment'`,
		issueID, commentID).Scan(&replyCount); err != nil {
		t.Fatalf("count replies: %v", err)
	}
	if replyCount != 1 {
		t.Fatalf("expected exactly 1 confirmation reply, got %d", replyCount)
	}
}

// TestAnswerAskUserQuestion_ForbiddenForNonTarget: a member who is not the
// target user gets 403 and no answer is recorded.
func TestAnswerAskUserQuestion_ForbiddenForNonTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// Target is a different user; the request is made as testUserID.
	otherUserID := "22222222-2222-2222-2222-222222222222"
	_, commentID := seedAskUserQuestion(t, otherUserID)

	w := answerReq(t, testUserID, commentID, map[string]any{"state": "submitted", "selected_index": 0})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Metadata must still have no answer.
	c, err := testHandler.Queries.GetComment(context.Background(), parseUUID(commentID))
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if m := parseCommentMetadata(c.Metadata); m.AskUserQuestion != nil && m.AskUserQuestion.Answer != nil {
		t.Fatalf("answer should not be recorded on 403")
	}
}

// TestAnswerAskUserQuestion_IgnoredThenReanswerRejected: ignoring records the
// terminal state and a subsequent answer is rejected (409, idempotent).
func TestAnswerAskUserQuestion_IgnoredThenReanswerRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, commentID := seedAskUserQuestion(t, testUserID)

	w := answerReq(t, testUserID, commentID, map[string]any{"state": "ignored"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on ignore, got %d: %s", w.Code, w.Body.String())
	}

	// Second answer must be rejected.
	w2 := answerReq(t, testUserID, commentID, map[string]any{"state": "submitted", "selected_index": 0})
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 on re-answer, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestAnswerAskUserQuestion_MultiSelect: a multi_select question accepts
// selected_indices and the reply lists all chosen labels.
func TestAnswerAskUserQuestion_MultiSelect(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, commentID := seedAskUserQuestionOpts(t, testUserID, true, false)

	w := answerReq(t, testUserID, commentID, map[string]any{
		"state":            "submitted",
		"selected_indices": []int{0, 1},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	ans := resp.Metadata.AskUserQuestion.Answer
	if ans == nil || len(ans.SelectedIndices) != 2 {
		t.Fatalf("expected 2 selected indices, got %+v", ans)
	}

	// The confirmation reply must mention both labels.
	var replyContent string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM comment WHERE issue_id=$1 AND parent_id=$2 AND type='comment'`,
		issueID, commentID).Scan(&replyContent); err != nil {
		t.Fatalf("load reply: %v", err)
	}
	if !strings.Contains(replyContent, "Redis") || !strings.Contains(replyContent, "Local") {
		t.Fatalf("reply should list both labels, got %q", replyContent)
	}
}

// TestAnswerAskUserQuestion_CustomText: allow_custom question accepts custom_text.
func TestAnswerAskUserQuestion_CustomText(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, commentID := seedAskUserQuestionOpts(t, testUserID, false, true)

	w := answerReq(t, testUserID, commentID, map[string]any{
		"state":       "submitted",
		"custom_text": "my own answer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Metadata.AskUserQuestion.Answer.CustomText != "my own answer" {
		t.Fatalf("custom_text not recorded: %+v", resp.Metadata.AskUserQuestion.Answer)
	}
	var replyContent string
	testPool.QueryRow(context.Background(),
		`SELECT content FROM comment WHERE issue_id=$1 AND parent_id=$2 AND type='comment'`,
		issueID, commentID).Scan(&replyContent)
	if !strings.Contains(replyContent, "my own answer") {
		t.Fatalf("reply should contain custom text, got %q", replyContent)
	}
}

// TestAnswerAskUserQuestion_CustomTextRejectedWhenDisallowed: custom_text on a
// question without allow_custom → 400.
func TestAnswerAskUserQuestion_CustomTextRejectedWhenDisallowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, commentID := seedAskUserQuestionOpts(t, testUserID, false, false)
	w := answerReq(t, testUserID, commentID, map[string]any{
		"state":       "submitted",
		"custom_text": "nope",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAnswerAskUserQuestion_MultiSelectEmptyRejected: multi_select with no
// selection and no custom → 400.
func TestAnswerAskUserQuestion_MultiSelectEmptyRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, commentID := seedAskUserQuestionOpts(t, testUserID, true, false)
	w := answerReq(t, testUserID, commentID, map[string]any{
		"state":            "submitted",
		"selected_indices": []int{},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
