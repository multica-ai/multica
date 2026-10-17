package handler

// channel_binding_notification.go — SHAR-146 B1: the settings API that lets a
// workspace member pick which WeCom bot (i.e. which channel_user_binding row)
// receives their task-notification pushes. Two endpoints, both scoped to the
// CALLING member (an admin may not manage another member's notification
// target — that would let one person move somebody else's notifications), so
// both read the member's own user_id off the session and fence every query
// on it.
//
//   GET  /api/workspaces/{id}/wecom/bindings               -> ListWecomBindings
//   PUT  /api/workspaces/{id}/wecom/bindings/{bindingId}/notification-target
//                                                -> SetWecomBindingNotificationTarget
//
// ListWecomBindings returns the caller's WeCom bindings joined with the
// installation they sit on, so the UI can name the bot and show which one is
// the current notification target. SetWecomBindingNotificationTarget is a
// radio: setting a binding as the target clears every other target in the
// same (workspace, channel_type) scope atomically; unsetting just clears the
// flag. Ownership is enforced by fencing every read and write on the session
// user's id, so a foreign binding id reads as 404 rather than leaking that
// it exists.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const wecomNotificationTargetChannelType = "wecom"

// WecomBindingResponse is the wire shape for one row in the caller's WeCom
// binding list. It carries everything the UI needs to show "which bot / which
// binding / is it the notification target" without a second fetch.
type WecomBindingResponse struct {
	ID                   string `json:"id"`
	InstallationID       string `json:"installation_id"`
	AgentID              string `json:"agent_id"`
	InstallationStatus   string `json:"installation_status"`
	ChannelUserID        string `json:"channel_user_id"`
	IsNotificationTarget bool   `json:"is_notification_target"`
}

// WecomBindingListResponse is the GET response envelope. Bindings are in
// bound_at ASC order, which is also the notification fallback order.
type WecomBindingListResponse struct {
	Bindings    []WecomBindingResponse `json:"bindings"`
	ChannelType string                 `json:"channel_type"`
}

// SetWecomBindingNotificationTargetRequest is the PUT body. IsNotificationTarget
// is the desired end state; true clears every other target in scope first
// (radio semantics), false just clears this one. The field is a pointer so
// "unset" (explicit false) is distinguishable from "no body" (400).
type SetWecomBindingNotificationTargetRequest struct {
	IsNotificationTarget *bool `json:"is_notification_target"`
}

// WecomBindingNotificationTargetResponse echoes the effective state after the
// write so the UI can confirm without re-reading.
type WecomBindingNotificationTargetResponse struct {
	ID                   string `json:"id"`
	IsNotificationTarget bool   `json:"is_notification_target"`
}

// ListWecomBindings (GET /api/workspaces/{id}/wecom/bindings) returns the
// calling member's WeCom bindings in their workspace. Fenced to the session
// user — there is no member_id query parameter, and an admin cannot list
// another member's bindings.
func (h *Handler) ListWecomBindings(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListChannelUserBindingsForMember(r.Context(), db.ListChannelUserBindingsForMemberParams{
		WorkspaceID:   wsUUID,
		MulticaUserID: userUUID,
		ChannelType:   wecomNotificationTargetChannelType,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "list wecom bindings failed",
			"error", err, "workspace_id", chi.URLParam(r, "id"))
		writeError(w, http.StatusInternalServerError, "failed to list wecom bindings")
		return
	}
	out := make([]WecomBindingResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, WecomBindingResponse{
			ID:                   uuidToString(row.ID),
			InstallationID:       uuidToString(row.InstallationID),
			AgentID:              uuidToString(row.AgentID),
			InstallationStatus:   row.InstallationStatus,
			ChannelUserID:        row.ChannelUserID,
			IsNotificationTarget: row.IsNotificationTarget,
		})
	}
	writeJSON(w, http.StatusOK, WecomBindingListResponse{
		Bindings:    out,
		ChannelType: wecomNotificationTargetChannelType,
	})
}

// SetWecomBindingNotificationTarget (PUT /api/workspaces/{id}/wecom/bindings/{bindingId}/notification-target)
// flips the caller's chosen binding to / from the notification target with
// radio semantics. The status matrix:
//
//   - 404: the binding id does not exist in this workspace, OR it is not
//     owned by the calling user. The over-permission path (an admin trying
//     to manage another member's target) reads as "not found", not "forbidden",
//     so the caller cannot probe which binding ids exist.
//   - 400: a non-WeCom binding id, or a body without the field.
//   - 409: a concurrent writer set a different target in the same scope
//     between our read and our write — the partial unique index caught it.
//   - 500: anything else.
//
// The radio is two writes, so it runs in one transaction: clear every target
// the caller holds in this (workspace, channel_type) scope, then set the
// chosen one. The partial unique index on
// (workspace_id, multica_user_id, channel_type) WHERE is_notification_target
// is the race backstop; the handler maps its violation to 409.
func (h *Handler) SetWecomBindingNotificationTarget(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	bindingIDStr := chi.URLParam(r, "bindingId")
	bindingUUID, ok := parseUUIDOrBadRequest(w, bindingIDStr, "binding id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var body SetWecomBindingNotificationTargetRequest
	r.Body = http.MaxBytesReader(w, r.Body, wecomBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.IsNotificationTarget == nil {
		writeError(w, http.StatusBadRequest, "is_notification_target is required")
		return
	}

	// Owner + workspace fence in one read: a foreign binding (or one that no
	// longer exists in this workspace) is a 404. The channel_type check keeps
	// a Slack or Lark binding id from being passed to the WeCom target
	// surface — the radio is per (member, channel_type), so the write must
	// agree with the scope it is about to touch.
	existing, err := h.Queries.GetChannelUserBindingByID(r.Context(), db.GetChannelUserBindingByIDParams{
		ID:          bindingUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "wecom binding not found in this workspace")
			return
		}
		slog.ErrorContext(r.Context(), "load wecom binding failed",
			"error", err, "workspace_id", uuidToString(wsUUID), "binding_id", bindingIDStr)
		writeError(w, http.StatusInternalServerError, "failed to load wecom binding")
		return
	}
	if existing.MulticaUserID != userUUID {
		// Over-permission path: someone else's binding. 404, not 403, so the
		// caller cannot enumerate another member's binding ids.
		writeError(w, http.StatusNotFound, "wecom binding not found in this workspace")
		return
	}
	if existing.ChannelType != wecomNotificationTargetChannelType {
		writeError(w, http.StatusBadRequest, "binding is not a wecom binding")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "begin tx for wecom target set failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	qtx := h.Queries.WithTx(tx)
	if *body.IsNotificationTarget {
		// Clear every other target in this scope, then set this one.
		if err := qtx.ClearChannelUserBindingNotificationTarget(r.Context(), db.ClearChannelUserBindingNotificationTargetParams{
			WorkspaceID:   wsUUID,
			MulticaUserID: userUUID,
			ChannelType:   wecomNotificationTargetChannelType,
		}); err != nil {
			slog.ErrorContext(r.Context(), "clear wecom notification targets failed",
				"error", err, "workspace_id", uuidToString(wsUUID), "binding_id", bindingIDStr)
			writeError(w, http.StatusInternalServerError, "failed to update wecom notification target")
			return
		}
		if err := qtx.SetChannelUserBindingNotificationTarget(r.Context(), db.SetChannelUserBindingNotificationTargetParams{
			ID:          bindingUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			if isUniqueViolation(err) {
				// Another actor set a target in this scope between our read
				// and our write; the partial unique index refused a second.
				writeError(w, http.StatusConflict,
					"another binding was just set as the notification target; try again")
				return
			}
			slog.ErrorContext(r.Context(), "set wecom notification target failed",
				"error", err, "workspace_id", uuidToString(wsUUID), "binding_id", bindingIDStr)
			writeError(w, http.StatusInternalServerError, "failed to update wecom notification target")
			return
		}
	} else {
		// Unset. A binding that is not the target already is a no-op the
		// zero-row update swallows; the response echoes the requested state.
		if err := qtx.UnsetChannelUserBindingNotificationTarget(r.Context(), db.UnsetChannelUserBindingNotificationTargetParams{
			ID:          bindingUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.ErrorContext(r.Context(), "unset wecom notification target failed",
				"error", err, "workspace_id", uuidToString(wsUUID), "binding_id", bindingIDStr)
			writeError(w, http.StatusInternalServerError, "failed to update wecom notification target")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.ErrorContext(r.Context(), "commit wecom notification target failed",
			"error", err, "workspace_id", uuidToString(wsUUID), "binding_id", bindingIDStr)
		writeError(w, http.StatusInternalServerError, "failed to persist wecom notification target")
		return
	}
	committed = true
	writeJSON(w, http.StatusOK, WecomBindingNotificationTargetResponse{
		ID:                   bindingIDStr,
		IsNotificationTarget: *body.IsNotificationTarget,
	})
}
