package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/util"
)

// SlackListsAPI is the agent-facing Slack Lists surface. *slack.ListsService
// satisfies it; tests inject a fake.
type SlackListsAPI interface {
	Schema(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID string) (slack.ListsSchema, error)
	Create(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID string, fields []slack.ListsField) (slack.ListsItem, error)
	Update(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID, itemID string, fields []slack.ListsField) (slack.ListsItem, error)
}

type slackListsWriteBody struct {
	Fields json.RawMessage `json:"fields"`
}

type slackListsError struct {
	status int
	msg    string
}

type slackAPICoder interface {
	APIError() string
}

func mapSlackListsError(err error, listID string) slackListsError {
	switch {
	case errors.Is(err, slack.ErrListsNotConfigured):
		return slackListsError{http.StatusServiceUnavailable, "This agent has no active Slack app connected, so it cannot write to Slack Lists."}
	case errors.Is(err, slack.ErrListsListNotAllowed):
		return slackListsError{http.StatusForbidden, fmt.Sprintf("List %s is not an allowed Slack Lists target for this agent's Slack app.", listID)}
	case errors.Is(err, slack.ErrListsWriteNotAuthorized):
		return slackListsError{http.StatusForbidden, "Slack Lists writes are only allowed for /idea and /feature"}
	case errors.Is(err, slack.ErrListsCommandMismatch):
		return slackListsError{http.StatusForbidden, "list_id does not match the current /idea or /feature command"}
	default:
		var api slackAPICoder
		if errors.As(err, &api) {
			return slackListsError{http.StatusBadGateway, "Slack API error: " + api.APIError()}
		}
		return slackListsError{http.StatusBadRequest, err.Error()}
	}
}

// GetSlackListsSchema serves GET /api/slack/lists/{listId}/schema.
func (h *Handler) GetSlackListsSchema(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.slackListsCaller(w, r)
	if !ok {
		return
	}
	if h.SlackLists == nil {
		writeError(w, http.StatusServiceUnavailable, "This agent has no active Slack app connected, so it cannot write to Slack Lists.")
		return
	}
	listID := strings.TrimSpace(chi.URLParam(r, "listId"))
	schema, err := h.SlackLists.Schema(r.Context(), caller.workspaceID, caller.agentID, caller.sessionID, listID)
	if err != nil {
		mapped := mapSlackListsError(err, listID)
		writeError(w, mapped.status, mapped.msg)
		return
	}
	writeJSON(w, http.StatusOK, schema)
}

// CreateSlackListsItem serves POST /api/slack/lists/{listId}/items.
func (h *Handler) CreateSlackListsItem(w http.ResponseWriter, r *http.Request) {
	h.writeSlackListsItem(w, r, false)
}

// UpdateSlackListsItem serves PATCH /api/slack/lists/{listId}/items/{itemId}.
func (h *Handler) UpdateSlackListsItem(w http.ResponseWriter, r *http.Request) {
	h.writeSlackListsItem(w, r, true)
}

func (h *Handler) writeSlackListsItem(w http.ResponseWriter, r *http.Request, update bool) {
	caller, ok := h.slackListsCaller(w, r)
	if !ok {
		return
	}
	if h.SlackLists == nil {
		writeError(w, http.StatusServiceUnavailable, "This agent has no active Slack app connected, so it cannot write to Slack Lists.")
		return
	}
	listID := strings.TrimSpace(chi.URLParam(r, "listId"))
	var body slackListsWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	fields, err := parseSlackListsFields(body.Fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var (
		item slack.ListsItem
	)
	if update {
		itemID := strings.TrimSpace(chi.URLParam(r, "itemId"))
		item, err = h.SlackLists.Update(r.Context(), caller.workspaceID, caller.agentID, caller.sessionID, listID, itemID, fields)
	} else {
		item, err = h.SlackLists.Create(r.Context(), caller.workspaceID, caller.agentID, caller.sessionID, listID, fields)
	}
	if err != nil {
		mapped := mapSlackListsError(err, listID)
		writeError(w, mapped.status, mapped.msg)
		return
	}
	status := http.StatusOK
	if !update {
		status = http.StatusCreated
	}
	writeJSON(w, status, item)
}

type slackListsCaller struct {
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	sessionID   pgtype.UUID
}

// slackListsCaller authorizes the request from the task token alone and
// returns the token's own agent + chat session. A JWT / mul_ PAT leaves
// X-Actor-Source empty, so requiring task_token is load-bearing.
func (h *Handler) slackListsCaller(w http.ResponseWriter, r *http.Request) (slackListsCaller, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "slack lists is only available from within an agent task")
		return slackListsCaller{}, false
	}
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		writeError(w, http.StatusBadRequest, "missing task context")
		return slackListsCaller{}, false
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return slackListsCaller{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return slackListsCaller{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return slackListsCaller{}, false
	}
	if ws := ctxWorkspaceID(r.Context()); ws != "" && uuidToString(agent.WorkspaceID) != ws {
		writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
		return slackListsCaller{}, false
	}
	return slackListsCaller{
		workspaceID: agent.WorkspaceID,
		agentID:     task.AgentID,
		sessionID:   task.ChatSessionID,
	}, true
}

func parseSlackListsFields(raw json.RawMessage) ([]slack.ListsField, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	switch raw[0] {
	case '[':
		var fields []slack.ListsField
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("invalid fields: %w", err)
		}
		return fields, nil
	case '{':
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("invalid fields: %w", err)
		}
		fields := make([]slack.ListsField, 0, len(obj))
		for key, value := range obj {
			field, ok, err := slackFieldFromValue(key, value)
			if err != nil {
				return nil, err
			}
			if ok {
				fields = append(fields, field)
			}
		}
		return fields, nil
	default:
		return nil, errors.New("fields must be an object or array")
	}
}

func slackFieldFromValue(column string, value any) (slack.ListsField, bool, error) {
	column = strings.TrimSpace(column)
	if column == "" || value == nil {
		return slack.ListsField{}, false, nil
	}
	f := slack.ListsField{Column: column}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return slack.ListsField{}, false, nil
		}
		f.Text = v
	case bool:
		f.Checkbox = &v
	case float64:
		f.Number = []any{v}
	case json.Number:
		f.Number = []any{v}
	case []any:
		strs := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return slack.ListsField{}, false, fmt.Errorf("column %s: array values must be strings", column)
			}
			if strings.TrimSpace(s) != "" {
				strs = append(strs, s)
			}
		}
		if len(strs) == 0 {
			return slack.ListsField{}, false, nil
		}
		f.Select = strs
	default:
		return slack.ListsField{}, false, fmt.Errorf("column %s has an unsupported value type", column)
	}
	return f, true, nil
}
