package handler

// FIR-4220: route-level enforcement for platform capabilities that were
// previously advisory "Managed externally" rows in Settings → Permissions.
// The Allow/Ask/Deny choice authored in the tool-policy engine becomes the
// live gate for agent actors; the pre-existing security checks (issue
// visibility, autopilot scope, owner/admin role, workspace membership) stay
// in place as tighten-only ceilings — a policy Allow can never open what
// those checks close.
//
// Unlike the FIR-4076 mutation keys, these capability keys are not runtime
// tool names, so a task-mandate snapshot never contains them. The gate
// therefore resolves the tool-policy chain only (see
// authorizePlatformActionWithMandate) — requiring the mandate here would
// blanket-deny agent calls such as schedule_wakeup regardless of policy.

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaction"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	rerunIssuePlatformAction          = "rerun_issue"
	createAutopilotPlatformAction     = "create_autopilot"
	triggerAutopilotPlatformAction    = "trigger_autopilot"
	autopilotScopePlatformAction      = "autopilot_scope"
	scheduleAgentWakeupPlatformAction = "schedule_agent_wakeup"
	manageProjectAccessPlatformAction = "manage_project_access"
	// FIR-4220 slice 2.
	useOtherRuntimePlatformAction = "use_other_runtime"
	readIssuesPlatformAction      = "read_issues"
	readProjectsPlatformAction    = "read_projects"
	// The three machine-intake capabilities: no actor to judge, so the policy
	// row is a workspace-layer off-switch (see platformaction.IntakeAllowed).
	autopilotWebhookPlatformAction      = "autopilot_webhook"
	daemonRuntimeCallbackPlatformAction = "daemon_runtime_callback"
)

// RequirePlatformCapability returns route middleware that enforces the named
// platform capability through the tool-policy engine for agent actors. Member
// requests pass through unchanged EXCEPT for the autopilot capabilities listed
// in autopilotCapabilityUsesMemberPolicy, where the same policy chain decides
// the member too (FIR-4359) — for every other capability a member's access is
// still decided by the existing role/membership gates on the wrapped handler.
func (h *Handler) RequirePlatformCapability(capability string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.allowPlatformCapability(w, r, capability) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func autopilotCapabilityUsesMemberPolicy(capability string) bool {
	switch capability {
	case createAutopilotPlatformAction, triggerAutopilotPlatformAction, autopilotScopePlatformAction:
		return true
	default:
		return false
	}
}

// RequirePlatformReadCapability is RequirePlatformCapability restricted to
// read requests (GET/HEAD). It exists so a whole route group can be wrapped
// with one gate for read_issues / read_projects without touching the group's
// mutation routes, which carry their own capability gates (FIR-4220 slice 2).
func (h *Handler) RequirePlatformReadCapability(capability string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				if !h.allowPlatformCapability(w, r, capability) {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// platformIntakeAllowed is the handler-side wrapper for
// platformaction.IntakeAllowed — nil-safe on a handler without a configured
// PlatformActionGate (upstream-only test fixtures keep working).
func (h *Handler) platformIntakeAllowed(ctx context.Context, workspaceID pgtype.UUID, capability string) bool {
	if h.PlatformActionGate == nil {
		return true
	}
	return platformaction.IntakeAllowed(ctx, h.PlatformActionGate.Policy, workspaceID, capability)
}

// RequireDaemonIntake enforces the daemon_runtime_callback workspace
// off-switch on daemon callback routes. The workspace comes from the daemon
// token the DaemonAuth middleware resolved; requests on the PAT path carry no
// daemon workspace in context and pass through to their own membership gates.
// Daemon-token authentication itself is unchanged (FIR-4220 slice 2).
func (h *Handler) RequireDaemonIntake(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); wsID != "" {
			workspaceUUID, err := util.ParseUUID(wsID)
			if err == nil && !h.platformIntakeAllowed(r.Context(), workspaceUUID, daemonRuntimeCallbackPlatformAction) {
				writeError(w, http.StatusForbidden, "daemon callbacks are turned off by workspace policy")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// allowPlatformCapability resolves the actor and authorizes the capability.
// Returns true when the request may proceed; on false the denial/pending
// response has already been written.
func (h *Handler) allowPlatformCapability(w http.ResponseWriter, r *http.Request, capability string) bool {
	workspaceID := h.resolveWorkspaceID(r)
	userID := requestUserID(r)
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil || userID == "" {
		// No workspace/user context to resolve an actor from — the wrapped
		// handler's own auth checks decide the request.
		return true
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	requireMandate := false
	if registered, ok := platformcatalog.ByKey(capability); ok {
		requireMandate = len(registered.ToolBindings) > 0
	}
	answer := h.authorizePlatformActionWithMandate(
		r.Context(), r, workspaceUUID, actorType, actorID, capability, "rest_api",
		map[string]any{"method": r.Method, "path": r.URL.Path},
		map[string]string{"method": r.Method, "path": r.URL.Path},
		false, requireMandate,
	)
	if !answer.Allowed {
		writePlatformAction(w, capability, answer)
		return false
	}
	return true
}
