package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// commentMetadataKey is the top-level key under comment.metadata that holds the
// ask_user_question payload. Keeping it namespaced leaves room for other
// structured comment types to store their own payloads alongside it later.
const commentMetadataKey = "ask_user_question"

const maxAskUserQuestionOptions = 20

// AskUserQuestionOption is one selectable choice. label is the short headline
// (rendered on top), description is the longer explanation (rendered below).
type AskUserQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestionAnswer is written once the target user confirms or ignores.
// It is nil until then.
//
// Selection is recorded in one of three ways depending on the question mode:
//   - single-select: SelectedIndex (kept for backward compat with old rows)
//   - multi-select:  SelectedIndices
//   - custom input:  CustomText (when the user picked the auto-appended "Other")
// A multi-select + allow_custom question may set both SelectedIndices and CustomText.
type AskUserQuestionAnswer struct {
	State           string `json:"state"` // "submitted" | "ignored"
	SelectedIndex   *int   `json:"selected_index,omitempty"`
	SelectedIndices []int  `json:"selected_indices,omitempty"`
	CustomText      string `json:"custom_text,omitempty"`
	AnsweredAt      string `json:"answered_at"`
}

// AskUserQuestionMeta is the full structured payload stored under
// comment.metadata.ask_user_question for an ask_user_question comment.
type AskUserQuestionMeta struct {
	// TargetUser is the user_id of the human being asked. Only this user may
	// answer (enforced in AnswerAskUserQuestion).
	TargetUser string `json:"target_user"`
	// SourceUser is the agent id that asked the question (= comment author).
	// The confirmation reply @mentions this agent so it can continue.
	SourceUser string                  `json:"source_user"`
	Question   string                  `json:"question"`
	Options    []AskUserQuestionOption `json:"options"`
	// MultiSelect lets the user pick more than one option (checkbox semantics).
	// Default false = single-select (radio). Mirrors the SDK's per-question
	// multiSelect flag.
	MultiSelect bool `json:"multi_select,omitempty"`
	// AllowCustom appends an auto "Other" choice that reveals a free-text input,
	// mirroring the SDK's auto-provided Other option. Default false.
	AllowCustom bool                   `json:"allow_custom,omitempty"`
	Answer      *AskUserQuestionAnswer `json:"answer,omitempty"`
}

// CommentMetadata is the decoded shape of comment.metadata. Only the
// ask_user_question key is modeled today; unknown keys are ignored on decode
// and dropped on re-encode, which is acceptable because the column is only
// written by this handler.
type CommentMetadata struct {
	AskUserQuestion *AskUserQuestionMeta `json:"ask_user_question,omitempty"`
}

// parseCommentMetadata decodes the raw JSONB bytes into CommentMetadata.
// Returns an empty (non-nil-safe) struct on empty/invalid input so callers can
// branch on AskUserQuestion == nil without a separate error path.
func parseCommentMetadata(raw []byte) CommentMetadata {
	var m CommentMetadata
	if len(raw) == 0 {
		return m
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return CommentMetadata{}
	}
	return m
}

// validateAskUserQuestionMeta checks the inbound payload for a new
// ask_user_question comment. answer must be absent on create.
func validateAskUserQuestionMeta(m *AskUserQuestionMeta) error {
	if m == nil {
		return fmt.Errorf("ask_user_question payload is required")
	}
	if strings.TrimSpace(m.TargetUser) == "" {
		return fmt.Errorf("target_user is required")
	}
	if strings.TrimSpace(m.Question) == "" {
		return fmt.Errorf("question is required")
	}
	if len(m.Options) == 0 {
		return fmt.Errorf("at least one option is required")
	}
	if len(m.Options) > maxAskUserQuestionOptions {
		return fmt.Errorf("too many options (max %d)", maxAskUserQuestionOptions)
	}
	for i, o := range m.Options {
		if strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("option %d: label is required", i)
		}
		if strings.TrimSpace(o.Description) == "" {
			return fmt.Errorf("option %d: description is required", i)
		}
	}
	return nil
}

// AnswerAskUserQuestionRequest is the body for POST /api/comments/{id}/answer.
type AnswerAskUserQuestionRequest struct {
	State           string `json:"state"` // "submitted" | "ignored"
	SelectedIndex   *int   `json:"selected_index"`
	SelectedIndices []int  `json:"selected_indices"`
	CustomText      string `json:"custom_text"`
}

// AnswerAskUserQuestion records the target user's response to an
// ask_user_question comment. Only the target_user may answer (403 otherwise),
// and only once (409 on re-answer). On "submitted" it also posts a reply that
// @mentions the source agent so it resumes work.
func (h *Handler) AnswerAskUserQuestion(w http.ResponseWriter, r *http.Request) {
	commentID := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	commentUUID, ok := parseUUIDOrBadRequest(w, commentID, "comment id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if comment.Type != commentMetadataKey {
		writeError(w, http.StatusBadRequest, "comment is not an ask_user_question")
		return
	}

	meta := parseCommentMetadata(comment.Metadata)
	if meta.AskUserQuestion == nil {
		writeError(w, http.StatusBadRequest, "comment has no ask_user_question payload")
		return
	}
	aq := meta.AskUserQuestion

	// Only the target user may answer. resolveActor's member actorID is the
	// user_id, which is what target_user stores.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" || actorID != aq.TargetUser {
		writeError(w, http.StatusForbidden, "only the target user may answer this question")
		return
	}

	// Idempotency: reject a second answer so the confirmation reply (and its
	// agent trigger) fires at most once.
	if aq.Answer != nil {
		writeError(w, http.StatusConflict, "question already answered")
		return
	}

	var req AnswerAskUserQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.State != "submitted" && req.State != "ignored" {
		writeError(w, http.StatusBadRequest, "state must be 'submitted' or 'ignored'")
		return
	}
	answer := AskUserQuestionAnswer{
		State:      req.State,
		AnsweredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if req.State == "submitted" {
		// Validate + record the selection according to the question's mode.
		customText := strings.TrimSpace(req.CustomText)
		if customText != "" && !aq.AllowCustom {
			writeError(w, http.StatusBadRequest, "custom_text is not allowed for this question")
			return
		}
		validIdx := func(i int) bool { return i >= 0 && i < len(aq.Options) }

		if aq.MultiSelect {
			// De-dup + range-check the selected indices.
			seen := map[int]bool{}
			var idxs []int
			for _, i := range req.SelectedIndices {
				if !validIdx(i) {
					writeError(w, http.StatusBadRequest, "selected_indices out of range")
					return
				}
				if !seen[i] {
					seen[i] = true
					idxs = append(idxs, i)
				}
			}
			// At least one signal required: a picked option OR custom text.
			if len(idxs) == 0 && customText == "" {
				writeError(w, http.StatusBadRequest, "select at least one option")
				return
			}
			answer.SelectedIndices = idxs
			answer.CustomText = customText
		} else {
			// Single-select. A custom-only answer (no option index) is allowed
			// when allow_custom is on; otherwise selected_index is required.
			if req.SelectedIndex != nil {
				if !validIdx(*req.SelectedIndex) {
					writeError(w, http.StatusBadRequest, "selected_index out of range")
					return
				}
				answer.SelectedIndex = req.SelectedIndex
			} else if customText == "" {
				writeError(w, http.StatusBadRequest, "selected_index is required when state is 'submitted'")
				return
			}
			answer.CustomText = customText
		}
	}

	answerJSON, err := json.Marshal(answer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode answer")
		return
	}

	updated, err := h.Queries.UpdateCommentAnswer(r.Context(), db.UpdateCommentAnswerParams{
		ID:          comment.ID,
		WorkspaceID: wsUUID,
		Answer:      answerJSON,
	})
	if err != nil {
		slog.Warn("update comment answer failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentID)...)
		writeError(w, http.StatusInternalServerError, "failed to record answer")
		return
	}

	// Broadcast the updated comment so every client flips the card to its
	// terminal state in place (no new event type needed).
	grouped := h.groupReactions(r, []pgtype.UUID{updated.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{updated.ID})
	resp := commentToResponse(updated, grouped[uuidToString(updated.ID)], groupedAtt[uuidToString(updated.ID)])

	var issueTitle, issueStatus string
	if issue, err := h.Queries.GetIssue(r.Context(), updated.IssueID); err == nil {
		issueTitle = issue.Title
		issueStatus = issue.Status
	}
	h.publish(protocol.EventCommentUpdated, workspaceID, actorType, actorID, map[string]any{
		"comment":      resp,
		"issue_id":     uuidToString(updated.IssueID),
		"issue_title":  issueTitle,
		"issue_status": issueStatus,
	})

	// On submit, post a reply that @mentions the source agent so it continues.
	if req.State == "submitted" {
		h.postAskUserQuestionReply(r, updated, aq, &answer, actorID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// buildSelectedSummary renders the human-readable selection for the reply
// text, unifying single-select / multi-select / custom-input. Examples:
//   single:          "Redis"
//   multi:           "Redis、Local"
//   multi + custom:  "Redis、其他:自定义方案"
//   custom only:     "其他:自定义方案"
func buildSelectedSummary(aq *AskUserQuestionMeta, answer *AskUserQuestionAnswer) string {
	labelAt := func(i int) string {
		if i >= 0 && i < len(aq.Options) {
			return aq.Options[i].Label
		}
		return ""
	}
	var parts []string
	if answer.SelectedIndex != nil {
		if l := labelAt(*answer.SelectedIndex); l != "" {
			parts = append(parts, l)
		}
	}
	for _, i := range answer.SelectedIndices {
		if l := labelAt(i); l != "" {
			parts = append(parts, l)
		}
	}
	if strings.TrimSpace(answer.CustomText) != "" {
		parts = append(parts, "其他:"+strings.TrimSpace(answer.CustomText))
	}
	return strings.Join(parts, "、")
}

// postAskUserQuestionReply creates the member reply that confirms the choice and
// @mentions the source agent, which re-triggers the agent via the normal
// comment-mention path. Best-effort: a failure here is logged but does not fail
// the answer (the answer itself already committed).
func (h *Handler) postAskUserQuestionReply(r *http.Request, parent db.Comment, aq *AskUserQuestionMeta, answer *AskUserQuestionAnswer, memberUserID string) {
	agentMention := aq.SourceUser
	if agentUUID, err := util.ParseUUID(aq.SourceUser); err == nil {
		if agent, err := h.Queries.GetAgent(r.Context(), agentUUID); err == nil {
			agentMention = fmt.Sprintf("[@%s](mention://agent/%s)", agent.Name, aq.SourceUser)
		}
	}
	content := fmt.Sprintf("问题:【%s】我选择【%s】,请继续 %s", aq.Question, buildSelectedSummary(aq, answer), agentMention)

	reply, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     parent.IssueID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    parseUUID(memberUserID),
		Content:     content,
		Type:        "comment",
		ParentID:    parent.ID,
		Metadata:    []byte("{}"),
	})
	if err != nil {
		slog.Warn("post ask_user_question reply failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(parent.ID))...)
		return
	}

	resp := commentToResponse(reply, nil, nil)
	workspaceID := uuidToString(parent.WorkspaceID)
	issue, issueErr := h.Queries.GetIssue(r.Context(), parent.IssueID)
	if issueErr == nil {
		h.publish(protocol.EventCommentCreated, workspaceID, "member", memberUserID, map[string]any{
			"comment":             resp,
			"issue_title":         issue.Title,
			"issue_assignee_type": textToPtr(issue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
			"issue_status":        issue.Status,
		})
		// Trigger the mentioned source agent to resume, mirroring the normal
		// comment path's mention-driven enqueue. The member confirming is the
		// originator; there is no autopilot delegation and no agents to
		// suppress on this member-authored reply.
		h.triggerTasksForComment(r.Context(), issue, reply, &parent, "member", memberUserID, memberUserID, "", nil)
	}
}
