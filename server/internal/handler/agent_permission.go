package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentInvocationTargetDTO is the wire shape of one invocation allow-list
// entry (MUL-3963). target_id is null for team placeholders that a client
// omitted, but is always present for workspace (the workspace id) and member
// (the user id) rows persisted by the backend.
type AgentInvocationTargetDTO struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

const (
	permissionModePrivate  = "private"
	permissionModePublicTo = "public_to"

	invocationTargetWorkspace = "workspace"
	invocationTargetMember    = "member"
	invocationTargetTeam      = "team"
)

// deriveLegacyVisibility maps the permission model back onto the legacy
// two-value visibility field so old clients never observe a widening:
//   - public_to WITH a workspace target -> "workspace" (everyone can invoke)
//   - everything else (private, or public_to limited to member/team) -> "private"
func deriveLegacyVisibility(permissionMode string, targets []db.AgentInvocationTarget) string {
	if permissionMode == permissionModePublicTo {
		for _, t := range targets {
			if t.TargetType == invocationTargetWorkspace {
				return "workspace"
			}
		}
	}
	return "private"
}

// applyInvocationTargetsToResponse fills InvocationTargets and recomputes the
// derived legacy Visibility from the loaded targets, keeping both views of the
// permission consistent in a single place.
func applyInvocationTargetsToResponse(resp *AgentResponse, targets []db.AgentInvocationTarget) {
	dto := make([]AgentInvocationTargetDTO, 0, len(targets))
	for _, t := range targets {
		var idPtr *string
		if t.TargetID.Valid {
			s := uuidToString(t.TargetID)
			idPtr = &s
		}
		dto = append(dto, AgentInvocationTargetDTO{TargetType: t.TargetType, TargetID: idPtr})
	}
	resp.InvocationTargets = dto
	resp.Visibility = deriveLegacyVisibility(resp.PermissionMode, targets)
}

// resolvedPermission is the normalised outcome of parsing the permission
// fields (or legacy visibility) off a create/update request.
type resolvedPermission struct {
	mode    string
	targets []targetSpec
}

type targetSpec struct {
	targetType string
	targetID   pgtype.UUID // invalid for team placeholders
}

// legacyVisibility is what this permission maps to for the visibility column
// we keep in sync for backwards compatibility.
func (p resolvedPermission) legacyVisibility() string {
	if p.mode == permissionModePublicTo {
		for _, t := range p.targets {
			if t.targetType == invocationTargetWorkspace {
				return "workspace"
			}
		}
	}
	return "private"
}

// parsePermissionInput normalises a permission_mode + invocation_targets pair,
// falling back to a legacy visibility value when the new fields are absent.
//
//   - permissionMode == nil && visibility == nil  -> caller default (returns ok=false, nil)
//   - permissionMode provided                     -> authoritative
//   - only legacy visibility provided             -> mapped:
//       "private"   -> private
//       "workspace" -> public_to + workspace target
//
// workspaceID seeds workspace targets (stored as the workspace id). The
// returned error is a client-facing 400 message.
func parsePermissionInput(workspaceID pgtype.UUID, permissionMode *string, targets []AgentInvocationTargetDTO, hasPermissionMode, hasTargets bool, legacyVisibility *string) (resolvedPermission, bool, error) {
	if !hasPermissionMode && legacyVisibility == nil {
		return resolvedPermission{}, false, nil
	}

	// Legacy-only path: map visibility onto the new model.
	if !hasPermissionMode {
		switch *legacyVisibility {
		case "workspace":
			return resolvedPermission{
				mode:    permissionModePublicTo,
				targets: []targetSpec{{targetType: invocationTargetWorkspace, targetID: workspaceID}},
			}, true, nil
		case "private", "":
			return resolvedPermission{mode: permissionModePrivate}, true, nil
		default:
			return resolvedPermission{}, false, fmt.Errorf("visibility must be 'private' or 'workspace'")
		}
	}

	mode := permissionModePrivate
	if permissionMode != nil && *permissionMode != "" {
		mode = *permissionMode
	}
	if mode != permissionModePrivate && mode != permissionModePublicTo {
		return resolvedPermission{}, false, fmt.Errorf("permission_mode must be 'private' or 'public_to'")
	}

	res := resolvedPermission{mode: mode}
	if mode == permissionModePrivate {
		// Private ignores any submitted targets: deny-by-default.
		return res, true, nil
	}

	// public_to: normalise the target list, de-duping and validating.
	if hasTargets {
		seen := map[string]struct{}{}
		for _, t := range targets {
			switch t.TargetType {
			case invocationTargetWorkspace:
				key := "workspace"
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetWorkspace, targetID: workspaceID})
			case invocationTargetMember:
				if t.TargetID == nil || *t.TargetID == "" {
					return resolvedPermission{}, false, fmt.Errorf("member invocation target requires target_id")
				}
				uid, err := util.ParseUUID(*t.TargetID)
				if err != nil {
					return resolvedPermission{}, false, fmt.Errorf("member invocation target_id is not a valid uuid")
				}
				key := "member:" + *t.TargetID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetMember, targetID: uid})
			case invocationTargetTeam:
				if t.TargetID == nil || *t.TargetID == "" {
					return resolvedPermission{}, false, fmt.Errorf("team invocation target requires target_id")
				}
				tid, err := util.ParseUUID(*t.TargetID)
				if err != nil {
					return resolvedPermission{}, false, fmt.Errorf("team invocation target_id is not a valid uuid")
				}
				key := "team:" + *t.TargetID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetTeam, targetID: tid})
			default:
				return resolvedPermission{}, false, fmt.Errorf("invocation target_type must be 'workspace', 'member', or 'team'")
			}
		}
	}
	// An empty public_to is a phantom: "shared with nobody" that the front-end
	// would render as workspace-shared while the backend admits no one. Per the
	// MUL-3963 ruling, a public_to with no resolvable targets is normalised to
	// a single workspace target — this also makes `--permission-mode public_to`
	// with no --public-to-* flags mean "public to workspace".
	if len(res.targets) == 0 {
		res.targets = append(res.targets, targetSpec{targetType: invocationTargetWorkspace, targetID: workspaceID})
	}
	return res, true, nil
}

// replaceInvocationTargets rewrites an agent's invocation allow-list wholesale:
// clear then re-insert. Called inside create/update after the agent row exists.
func (h *Handler) replaceInvocationTargets(ctx context.Context, agentID pgtype.UUID, createdBy pgtype.UUID, targets []targetSpec) error {
	if err := h.Queries.DeleteAgentInvocationTargets(ctx, agentID); err != nil {
		return err
	}
	for _, t := range targets {
		if err := h.Queries.CreateAgentInvocationTarget(ctx, db.CreateAgentInvocationTargetParams{
			AgentID:    agentID,
			TargetType: t.targetType,
			TargetID:   t.targetID,
			CreatedBy:  createdBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

// enrichAgentResponseWithTargets loads an agent's invocation targets and
// applies them to the response (InvocationTargets + derived Visibility). Used
// by the single-agent detail / create / update responses.
func (h *Handler) enrichAgentResponseWithTargets(ctx context.Context, resp *AgentResponse, agentID pgtype.UUID) error {
	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agentID)
	if err != nil {
		return err
	}
	applyInvocationTargetsToResponse(resp, targets)
	return nil
}

// enrichAgentResponseWithTargetsHTTP is the HTTP-boundary wrapper that writes a
// 500 and returns false on failure.
func (h *Handler) enrichAgentResponseWithTargetsHTTP(w http.ResponseWriter, r *http.Request, resp *AgentResponse, agentID pgtype.UUID) bool {
	if err := h.enrichAgentResponseWithTargets(r.Context(), resp, agentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return false
	}
	return true
}
