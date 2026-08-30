package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/service/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type replaceAgentToolPolicyRequest struct {
	ExpectedRevision int64             `json:"expected_revision"`
	Rules            []toolpolicy.Rule `json:"rules"`
}

type agentToolActionListResponse struct {
	Items      []toolaction.Event `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type agentToolActionCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func (h *Handler) GetAgentToolPolicy(w http.ResponseWriter, r *http.Request) {
	agent, actor, ok := h.authorizeAgentToolControlRead(w, r)
	if !ok {
		return
	}
	policy, err := h.ToolPolicyService.Get(r.Context(), toolpolicy.ReadRequest{
		WorkspaceID: uuidToString(agent.WorkspaceID),
		AgentID:     uuidToString(agent.ID),
		Actor:       actor,
	})
	if err != nil {
		writeToolControlError(w, err, "failed to get agent tool policy")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) ReplaceAgentToolPolicy(w http.ResponseWriter, r *http.Request) {
	agent, member, actor, ok := h.agentToolControlContext(w, r)
	if !ok {
		return
	}
	if actor.Kind != toolpolicy.ActorHuman || !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var request replaceAgentToolPolicyRequest
	if err := decodeStrictJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Rules == nil {
		request.Rules = []toolpolicy.Rule{}
	}
	policy, err := h.ToolPolicyService.Replace(r.Context(), toolpolicy.Replacement{
		WorkspaceID:      uuidToString(agent.WorkspaceID),
		AgentID:          uuidToString(agent.ID),
		Actor:            actor,
		ExpectedRevision: request.ExpectedRevision,
		Rules:            request.Rules,
	})
	if err != nil {
		writeToolControlError(w, err, "failed to replace agent tool policy")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) ListAgentToolActions(w http.ResponseWriter, r *http.Request) {
	agent, actor, ok := h.authorizeAgentToolControlRead(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	agentID := uuidToString(agent.ID)
	if _, err := h.ToolPolicyService.Get(r.Context(), toolpolicy.ReadRequest{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		Actor:       actor,
	}); err != nil {
		writeToolControlError(w, err, "failed to authorize agent tool actions")
		return
	}

	params, ok := parseAgentToolActionListParams(w, r, workspaceID, agentID)
	if !ok {
		return
	}
	events, err := h.ToolActionService.List(r.Context(), params)
	if err != nil {
		writeToolControlError(w, err, "failed to list agent tool actions")
		return
	}
	response := agentToolActionListResponse{Items: events}
	if len(events) == int(params.Limit) && len(events) > 0 {
		last := events[len(events)-1]
		response.NextCursor = encodeAgentToolActionCursor(agentToolActionCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) authorizeAgentToolControlRead(w http.ResponseWriter, r *http.Request) (db.Agent, toolpolicy.Actor, bool) {
	agent, _, actor, ok := h.agentToolControlContext(w, r)
	if !ok {
		return db.Agent{}, toolpolicy.Actor{}, false
	}
	switch actor.Kind {
	case toolpolicy.ActorHuman:
	case toolpolicy.ActorAgent, toolpolicy.ActorTask:
		if actor.AgentID == "" || actor.AgentID != uuidToString(agent.ID) {
			writeError(w, http.StatusForbidden, "agent actors may read only their own tool controls")
			return db.Agent{}, toolpolicy.Actor{}, false
		}
	default:
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Agent{}, toolpolicy.Actor{}, false
	}
	return agent, actor, true
}

func (h *Handler) agentToolControlContext(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, toolpolicy.Actor, bool) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return db.Agent{}, db.Member{}, toolpolicy.Actor{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin", "member")
	if !ok {
		return db.Agent{}, db.Member{}, toolpolicy.Actor{}, false
	}
	actor := h.toolPolicyActor(r, workspaceID, member.Role)
	return agent, member, actor, true
}

func (h *Handler) toolPolicyActor(r *http.Request, workspaceID, role string) toolpolicy.Actor {
	actor := toolpolicy.Actor{Kind: toolpolicy.ActorHuman, UserID: requestUserID(r), WorkspaceRole: role}
	if source := r.Header.Get("X-Actor-Source"); source != "" {
		switch source {
		case "task_token":
			actor.Kind = toolpolicy.ActorTask
			actor.AgentID = r.Header.Get("X-Agent-ID")
		case "agent":
			actor.Kind = toolpolicy.ActorAgent
			actor.AgentID = r.Header.Get("X-Agent-ID")
		default:
			actor.Kind = toolpolicy.ActorDaemon
		}
		return actor
	}
	if r.Header.Get("X-Agent-ID") != "" || r.Header.Get("X-Task-ID") != "" {
		actor.Kind = toolpolicy.ActorDaemon
	}
	return actor
}

func parseAgentToolActionListParams(w http.ResponseWriter, r *http.Request, workspaceID, agentID string) (toolaction.ListParams, bool) {
	params := toolaction.ListParams{WorkspaceID: workspaceID, AgentID: agentID, Limit: 50}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return toolaction.ListParams{}, false
		}
		params.Limit = int32(value)
	}
	params.EventType = strings.TrimSpace(r.URL.Query().Get("event_type"))
	if !toolaction.IsValidEventType(params.EventType) {
		writeError(w, http.StatusBadRequest, "invalid event_type")
		return toolaction.ListParams{}, false
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since")
			return toolaction.ListParams{}, false
		}
		value = value.UTC()
		params.Since = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		cursor, err := decodeAgentToolActionCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return toolaction.ListParams{}, false
		}
		params.CursorCreatedAt = &cursor.CreatedAt
		params.CursorID = cursor.ID
	}
	return params, true
}

func encodeAgentToolActionCursor(cursor agentToolActionCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAgentToolActionCursor(raw string) (agentToolActionCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return agentToolActionCursor{}, err
	}
	var cursor agentToolActionCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.CreatedAt.IsZero() || cursor.ID == "" {
		return agentToolActionCursor{}, errors.New("invalid cursor")
	}
	if _, err := util.ParseUUID(cursor.ID); err != nil {
		return agentToolActionCursor{}, errors.New("invalid cursor")
	}
	return cursor, nil
}

func decodeStrictJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func writeToolControlError(w http.ResponseWriter, err error, internalMessage string) {
	switch {
	case errors.Is(err, toolpolicy.ErrForbidden):
		writeError(w, http.StatusForbidden, "insufficient permissions")
	case errors.Is(err, toolpolicy.ErrNotFound):
		writeError(w, http.StatusNotFound, "agent not found")
	case errors.Is(err, toolpolicy.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "tool policy revision conflict")
	case errors.Is(err, toolpolicy.ErrInvalidPolicy), errors.Is(err, toolaction.ErrInvalidMetadata), errors.Is(err, toolaction.ErrRawValue), errors.Is(err, toolaction.ErrSensitiveMetadata):
		writeError(w, http.StatusBadRequest, "invalid metadata-only tool control request")
	default:
		writeError(w, http.StatusInternalServerError, internalMessage)
	}
}
