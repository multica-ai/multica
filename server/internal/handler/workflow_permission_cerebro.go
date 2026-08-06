package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/util"
)

const manageWorkflowsPlatformAction = "manage_workflows"

func isManageWorkflowsMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// RequireManageWorkflows enforces the shared workflow authoring permission for
// agent mutations while preserving existing member and read behavior.
func (h *Handler) RequireManageWorkflows(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := h.resolveWorkspaceID(r)
		userID := requestUserID(r)
		workspaceUUID, err := util.ParseUUID(workspaceID)
		if err != nil || userID == "" {
			next.ServeHTTP(w, r)
			return
		}
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !isManageWorkflowsMutation(r.Method) {
			if actorType == "agent" {
				agentUUID, parseErr := util.ParseUUID(actorID)
				if parseErr != nil {
					writePlatformAction(w, manageWorkflowsPlatformAction, platformActionAnswer{Reason: "platform action gate unavailable"})
					return
				}
				answer := h.authorizeExactPlatformTaskMandate(r.Context(), r, workspaceUUID, agentUUID, manageWorkflowsPlatformAction, true)
				if !answer.Allowed {
					writePlatformAction(w, manageWorkflowsPlatformAction, answer)
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		answer := h.authorizePlatformAction(
			r.Context(),
			r,
			workspaceUUID,
			actorType,
			actorID,
			manageWorkflowsPlatformAction,
			"rest_api",
			map[string]any{"method": r.Method, "path": r.URL.Path},
			map[string]string{"method": r.Method, "path": r.URL.Path},
			false,
		)
		if !answer.Allowed {
			writePlatformAction(w, manageWorkflowsPlatformAction, answer)
			return
		}
		next.ServeHTTP(w, r)
	})
}
