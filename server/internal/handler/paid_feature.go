package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/service"
)

// Paid features (MUL-9482).
//
// A paid feature is restricted for a workspace whose plan does not include
// it. The rule is the same for every feature:
//
//   - Existing configuration keeps working. Reads, access checks and anything
//     that runs on the configuration never consult the restriction.
//   - Reducing or removing configuration is never restricted: removing a
//     person, making something public again, deleting.
//   - Adding to it is restricted: creating, adding a person, widening.
//   - Edits that leave the feature's configuration unchanged (renaming, a
//     same-value write-back) are not restricted.
//
// Each feature classifies its own writes and calls requireFeatureAddition
// only for the ones that add. The restriction is commercial, not
// authorization: callers run their normal permission checks first, so a
// caller who could not make the change anyway gets the ordinary 403.
//
// The decision comes from h.Features. Nil, as on a self-hosted deployment
// without Cloud, leaves every feature usable.

const featureLockedCode = "feature_locked"

// featureAdmission is an admitted addition to a paid feature that is being
// observed. Call admitted once the write has committed; it records the attempt
// and is a no-op when nothing needs recording.
type featureAdmission struct {
	attempts service.EntitlementFeatureAttempts
	attempt  service.FeatureAttempt
	record   bool
}

func (a featureAdmission) admitted(ctx context.Context) {
	if a.record {
		a.attempts.Record(context.WithoutCancel(ctx), a.attempt)
	}
}

// requireFeatureAddition decides whether the caller may add to feature's
// configuration in workspaceID. operation names the addition for the attempt
// record, e.g. "create" or "add_members". Under enforce it records the
// rejection and answers 403 feature_locked; under observe it admits the
// addition and returns an admission to record after the write commits.
func (h *Handler) requireFeatureAddition(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, feature entitlement.FeatureName, operation string) (featureAdmission, bool) {
	if h.Features == nil || !workspaceID.Valid {
		return featureAdmission{}, true
	}
	decision := h.Features.Feature(r.Context(), uuid.UUID(workspaceID.Bytes), feature)
	if decision.Action == entitlement.ActionOff {
		return featureAdmission{}, true
	}
	attempts := service.EntitlementFeatureAttempts{Queries: h.Queries}
	attempt := service.FeatureAttempt{
		WorkspaceID: workspaceID,
		Feature:     feature,
		Operation:   operation,
		Decision:    decision,
	}
	if decision.Restricted() {
		attempts.Record(context.WithoutCancel(r.Context()), attempt)
		writeFeatureLocked(w, feature)
		return featureAdmission{}, false
	}
	return featureAdmission{attempts: attempts, attempt: attempt, record: true}, true
}

func writeFeatureLocked(w http.ResponseWriter, feature entitlement.FeatureName) {
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error":   "This feature is not included in the workspace's plan. Existing settings keep working; upgrade to add more.",
		"code":    featureLockedCode,
		"feature": string(feature),
	})
}

// WorkspaceFeatureState is what a client may do with one paid feature.
// available covers off and observe, so an observed rollout is invisible to
// users; locked is enforce.
type WorkspaceFeatureState struct {
	State string `json:"state"`
}

const (
	workspaceFeatureAvailable = "available"
	workspaceFeatureLocked    = "locked"
)

type WorkspaceFeaturesResponse struct {
	Features map[string]WorkspaceFeatureState `json:"features"`
}

// GetWorkspaceFeatures — GET /api/workspaces/{id}/features
//
// Member-readable: every member sees the same restriction on the shared
// workspace. The response carries only the evaluated state per feature, never
// plan, subscription or policy details. Clients use it to show read-only and
// upgrade states; the server enforces on every write regardless.
func (h *Handler) GetWorkspaceFeatures(w http.ResponseWriter, r *http.Request) {
	wsID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	features := entitlement.Features()
	resp := WorkspaceFeaturesResponse{Features: make(map[string]WorkspaceFeatureState, len(features))}
	for _, feature := range features {
		state := workspaceFeatureAvailable
		if h.Features != nil && h.Features.Feature(r.Context(), uuid.UUID(workspaceID.Bytes), feature).Restricted() {
			state = workspaceFeatureLocked
		}
		resp.Features[string(feature)] = WorkspaceFeatureState{State: state}
	}
	writeJSON(w, http.StatusOK, resp)
}
