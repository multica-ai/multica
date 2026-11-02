package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AskBriefInquiryRequest struct {
	SquadID  string `json:"squad_id"`
	IssueID  string `json:"issue_id"`
	Question string `json:"question"`
}

// BriefInquiryResponse is one question the Secretary put to a squad leader.
// AnsweredAt and AnswerExcerpt are null until the squad replies on the issue.
type BriefInquiryResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	SquadID       string  `json:"squad_id"`
	IssueID       *string `json:"issue_id"`
	CommentID     *string `json:"comment_id"`
	Question      string  `json:"question"`
	AskedOn       string  `json:"asked_on"`
	AnsweredAt    *string `json:"answered_at"`
	AnswerExcerpt *string `json:"answer_excerpt"`
	CreatedAt     string  `json:"created_at"`
}

func briefInquiryToResponse(b db.BriefInquiry) BriefInquiryResponse {
	return BriefInquiryResponse{
		ID:            uuidToString(b.ID),
		WorkspaceID:   uuidToString(b.WorkspaceID),
		SquadID:       uuidToString(b.SquadID),
		IssueID:       uuidToPtr(b.IssueID),
		CommentID:     uuidToPtr(b.CommentID),
		Question:      b.Question,
		AskedOn:       b.AskedOn.Time.Format("2006-01-02"),
		AnsweredAt:    timestampToPtr(b.AnsweredAt),
		AnswerExcerpt: textToPtr(b.AnswerExcerpt),
		CreatedAt:     timestampToString(b.CreatedAt),
	}
}

// isWorkspaceSecretary reports whether the agent is the workspace's Secretary.
func (h *Handler) isWorkspaceSecretary(r *http.Request, workspaceID, agentID string) bool {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	return err == nil && ws.SecretaryAgentID.Valid && uuidToString(ws.SecretaryAgentID) == agentID
}

// AskBriefInquiry is the Secretary's question to a squad leader, asked on an
// issue. The question is a comment on that issue mentioning the leader, so the
// ordinary mention path wakes the leader. At most service.BriefInquiryDailyLimit
// questions per workspace per UTC day; the next one returns 429 inquiry_limit.
//
// Only the workspace's Secretary agent may ask: the row records an agent asker.
//
//	POST /api/briefs/inquiries {squad_id, issue_id, question}
func (h *Handler) AskBriefInquiry(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req AskBriefInquiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" || !h.isWorkspaceSecretary(r, workspaceID, actorID) {
		writeError(w, http.StatusForbidden, "only the workspace's secretary agent may ask a squad leader")
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	squadUUID, ok := parseUUIDOrBadRequest(w, req.SquadID, "squad_id")
	if !ok {
		return
	}
	if strings.TrimSpace(req.IssueID) == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required: a question is a comment on the issue it asks about")
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	question := cleanProposalText(req.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}
	if n := utf8.RuneCountInString(question); n > service.BriefInquiryQuestionMax {
		writeError(w, http.StatusBadRequest, "question must be at most "+strconv.Itoa(service.BriefInquiryQuestionMax)+" characters")
		return
	}

	res, err := service.AskBriefInquiry(r.Context(), h.TxStarter, h.Queries, service.BriefInquiryAskParams{
		WorkspaceID: wsUUID,
		ActorID:     actorUUID,
		SquadID:     squadUUID,
		IssueID:     issueUUID,
		Question:    question,
		Now:         time.Now(),
	})
	var limitErr *service.BriefInquiryLimitError
	switch {
	case errors.As(err, &limitErr):
		writeErrorCode(w, http.StatusTooManyRequests, "inquiry_limit", limitErr.Message)
		return
	case errors.Is(err, service.ErrBriefInquirySquadNotFound):
		writeError(w, http.StatusNotFound, "squad not found")
		return
	case errors.Is(err, service.ErrBriefInquiryIssueNotFound):
		writeError(w, http.StatusBadRequest, "issue_id: no issue in this workspace")
		return
	case err != nil:
		slog.Warn("ask brief inquiry failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to record the question")
		return
	}

	// The comment is committed: publish it, then wake the leader it mentions.
	h.publish(protocol.EventCommentCreated, workspaceID, "agent", actorID, map[string]any{
		"comment":             commentToResponse(res.Comment, nil, nil),
		"issue_title":         res.Issue.Title,
		"issue_assignee_type": textToPtr(res.Issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(res.Issue.AssigneeID),
		"issue_status":        res.Issue.Status,
		"issue_revision":      res.CommentIssueRevision,
	})
	h.triggerTasksForComment(r.Context(), res.Issue, res.Comment, nil, "agent", actorID,
		h.invokeOriginatorFromRequest(r, "agent", actorID), nil, commentSteerRequest{})

	slog.Info("brief inquiry asked", append(logger.RequestAttrs(r),
		"inquiry_id", uuidToString(res.Inquiry.ID), "squad_id", req.SquadID)...)
	writeJSON(w, http.StatusCreated, briefInquiryToResponse(res.Inquiry))
}
