package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/workflowexec"
)

type registryHostRequest struct {
	Operation  string         `json:"operation"`
	ResourceID string         `json:"resource_id"`
	Config     map[string]any `json:"config"`
	Input      any            `json:"input"`
}

type connectionHostRequest struct {
	ConnectionID string         `json:"connection_id"`
	Tool         string         `json:"tool"`
	Arguments    map[string]any `json:"arguments"`
}

func approvedRegistryScope(scopes []tokens.Scope, operation, resourceID string) bool {
	resourceType, access := "data_source", "read"
	if operation == "write" {
		resourceType, access = "data_destination", "write"
	} else if operation != "read" {
		return false
	}
	for _, scope := range scopes {
		if scope.ResourceType == resourceType && scope.ResourceID == resourceID && (scope.Access == access || scope.Access == "read_write") {
			return true
		}
	}
	return false
}

func (h *Handler) workerGrant(r *http.Request) (invocationGrant, []tokens.Scope, error) {
	if h == nil || strings.TrimSpace(h.runtimeServiceKey) == "" {
		return invocationGrant{}, nil, errors.New("invocation grants are not configured")
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	grant, err := verifyInvocationGrant(h.runtimeServiceKey, token, time.Now().UTC())
	if err != nil {
		return invocationGrant{}, nil, err
	}
	appID, appErr := uuid.Parse(grant.AppID)
	workspaceID, workspaceErr := uuid.Parse(grant.WorkspaceID)
	_, memberErr := uuid.Parse(grant.MemberID)
	if appErr != nil || workspaceErr != nil || memberErr != nil || !semverPattern.MatchString(grant.Version) {
		return invocationGrant{}, nil, errors.New("invalid invocation grant")
	}
	var rawScopes json.RawMessage
	err = h.pool.QueryRow(r.Context(), `SELECT g.scopes FROM cerebro_app a JOIN cerebro_app_grant g ON g.app_id=a.id AND g.version=$2 AND g.status='approved' WHERE a.id=$1 AND a.workspace_id=$3 AND a.current_version=$2 AND a.status='published'`, appID, grant.Version, workspaceID).Scan(&rawScopes)
	if err != nil {
		return invocationGrant{}, nil, err
	}
	var scopes []tokens.Scope
	if json.Unmarshal(rawScopes, &scopes) != nil {
		return invocationGrant{}, nil, errors.New("invalid approved scopes")
	}
	return grant, scopes, nil
}

func (h *Handler) WorkerRegistryCall(w http.ResponseWriter, r *http.Request) {
	grant, scopes, err := h.workerGrant(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invocation grant is invalid")
		return
	}
	var req registryHostRequest
	if !decodeJSONLimit(w, r, &req, 1<<20) {
		return
	}
	if !approvedRegistryScope(scopes, req.Operation, req.ResourceID) {
		writeError(w, http.StatusForbidden, "Registry resource is outside the app's approved scopes")
		return
	}
	identity := tokens.Identity{MemberID: grant.MemberID, App: tokens.AppGrant{ID: grant.AppID, Version: grant.Version, RunID: uuid.NewString(), Scopes: scopes}}
	h.tokens.Forget(identity)
	token, err := h.tokens.PersonalKey(r.Context(), identity)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Registry app token is unavailable")
		return
	}
	output, err := newRegistryAdapter(os.Getenv("FIRTAL_REGISTRY_URL"), identity.App.RunID, nil).Execute(r.Context(), token.Key, workflowexec.RegistryCall{Kind: req.Operation, ResourceID: req.ResourceID, Config: req.Config, Input: req.Input})
	if err != nil {
		writeError(w, http.StatusFailedDependency, "Registry call failed")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (h *Handler) WorkerConnectionCall(w http.ResponseWriter, r *http.Request) {
	grant, scopes, err := h.workerGrant(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invocation grant is invalid")
		return
	}
	var req connectionHostRequest
	if !decodeJSONLimit(w, r, &req, 1<<20) {
		return
	}
	if strings.TrimSpace(req.Tool) == "" || !approvedConnectionScope(scopes, req.ConnectionID) {
		writeError(w, http.StatusForbidden, "connection is outside the app's approved scopes")
		return
	}
	identity := tokens.Identity{MemberID: grant.MemberID, App: tokens.AppGrant{ID: grant.AppID, Version: grant.Version, RunID: uuid.NewString(), Scopes: scopes}}
	if req.ConnectionID == "ai_gateway" {
		if req.Tool != "chat.completions" {
			writeError(w, http.StatusForbidden, "AI Gateway tool is outside the app's approved scope")
			return
		}
		if h.tokens == nil {
			writeError(w, http.StatusServiceUnavailable, "AI Gateway token service is unavailable")
			return
		}
		h.tokens.Forget(identity)
		token, tokenErr := h.tokens.PersonalKey(r.Context(), identity)
		if tokenErr != nil {
			writeError(w, http.StatusBadGateway, "AI Gateway app token is unavailable")
			return
		}
		result, callErr := callAIGateway(r.Context(), http.DefaultClient, token, req.Arguments)
		if callErr != nil {
			writeError(w, http.StatusFailedDependency, "AI Gateway call failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
		return
	}
	connectionID, err := uuid.Parse(req.ConnectionID)
	if err != nil {
		writeError(w, http.StatusForbidden, "connection is outside the app's approved scopes")
		return
	}
	if h.connections == nil {
		writeError(w, http.StatusServiceUnavailable, "connection service is unavailable")
		return
	}
	workspaceID, _ := uuid.Parse(grant.WorkspaceID)
	memberID, _ := uuid.Parse(grant.MemberID)
	result, err := h.connections.CallForApp(r.Context(), workspaceID, memberID, connectionID, req.Tool, req.Arguments)
	if err != nil {
		writeError(w, http.StatusFailedDependency, "connection call failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func callAIGateway(ctx context.Context, client *http.Client, token tokens.Token, arguments map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(token.Key) == "" || strings.TrimSpace(token.AIBaseURL) == "" {
		return nil, errors.New("AI gateway token is incomplete")
	}
	body, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(token.AIBaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token.Key)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, (1<<20)+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("AI gateway response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("AI gateway returned HTTP %d", response.StatusCode)
	}
	if !json.Valid(raw) {
		return nil, errors.New("AI gateway response is invalid")
	}
	return json.RawMessage(raw), nil
}
