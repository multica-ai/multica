package runtime

// firtal_gateway_inproc_bridge.go — FIR-1449 step 2.
//
// Wire the generic bridge (firtal_gateway_bridge.go) into the gateway
// executor's per-task tool build. When the in-process bridge is enabled, every
// per-task registry is augmented with the full Multica CLI/MCP tool surface
// (clitools.RegisterTools) reached through an in-process loopback that carries
// the task's own identity — so authorization is decided exactly as it would be
// for a remote CLI call, never bypassed.
//
// Default OFF: SetInProcessBridge is only called by the server when the
// MULTICA_SERVER_FIRTAL_GATEWAY_INPROCESS_BRIDGE env flag is set, so the fleet
// is completely unaffected until the rollout switch is flipped. Even with the
// flag on, a bridged tool is only EXPOSED to the model once the runtime's tool
// cascade / permission policy enables it (the authoritative per-runtime lever):
// the bridge supplies the handlers; the cascade decides what is callable. The
// admin denylist is a static safety net beside that lever.

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cerebro/clitools"
	"github.com/multica-ai/multica/server/internal/mcp"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// inProcBridgeEnvFlag gates the FIR-1449 in-process bridge. Default off.
const inProcBridgeEnvFlag = "MULTICA_SERVER_FIRTAL_GATEWAY_INPROCESS_BRIDGE"

// MaybeEnableInProcessBridge enables the in-process bridge on e when the
// MULTICA_SERVER_FIRTAL_GATEWAY_INPROCESS_BRIDGE env flag is truthy, handing it
// the server's router. No-op otherwise, so the fleet is unaffected by default.
// Mirrors MaybeEnableApprovalGate's controlled-rollout pattern.
func MaybeEnableInProcessBridge(e *FirtalGatewayExecutor, handler http.Handler) {
	if e == nil || handler == nil {
		return
	}
	raw := strings.TrimSpace(os.Getenv(inProcBridgeEnvFlag))
	if raw == "" {
		return
	}
	if on, err := strconv.ParseBool(raw); err != nil || !on {
		return
	}
	e.SetInProcessBridge(handler)
}

// inProcBridgeTokenTTL bounds the lifetime of the per-task loopback token. It
// only has to outlive a single tool loop; a short TTL limits exposure if the
// row is ever leaked.
const inProcBridgeTokenTTL = 30 * time.Minute

// SetInProcessBridge enables the FIR-1449 in-process bridge by handing the
// executor the server's own router. Per-task tool builds will then mint a
// task-scoped token and bridge the full CLI tool surface into the registry.
// Passing a nil handler leaves the bridge disabled (the default).
func (e *FirtalGatewayExecutor) SetInProcessBridge(handler http.Handler) {
	if e != nil {
		e.routerHandler = handler
	}
}

// inProcessBridgeEnabled reports whether the bridge is wired.
func (e *FirtalGatewayExecutor) inProcessBridgeEnabled() bool {
	return e != nil && e.routerHandler != nil
}

// bridgeInProcessTools augments a per-task registry with the full Multica
// CLI/MCP tool surface. It is a no-op unless the bridge is enabled. It mints a
// short-lived task-scoped (`mat_`) token so the in-process loopback
// authenticates as the agent exactly like a remote CLI call would, builds an
// *mcp.Server populated with the whole CLI tool set, and bridges every
// non-admin tool into reg, filling only gaps the native DB-direct builtins do
// not already cover (onlyNew=true).
//
// Failures are non-fatal: the task keeps its native tool set and runs without
// the bridged surface rather than erroring, so a transient token-mint problem
// can never take an agent fully offline.
func (e *FirtalGatewayExecutor) bridgeInProcessTools(ctx context.Context, reg *Registry, agentID, workspaceID, userID pgtype.UUID, taskID string) {
	if !e.inProcessBridgeEnabled() || reg == nil || e.queries == nil {
		return
	}
	if taskID == "" || !agentID.Valid || !workspaceID.Valid {
		return
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		e.warnBridge("parse task id", taskID, err)
		return
	}

	raw, err := auth.GenerateAgentTaskToken()
	if err != nil {
		e.warnBridge("generate task token", taskID, err)
		return
	}
	if _, err := e.queries.CreateTaskToken(ctx, db.CreateTaskTokenParams{
		TokenHash:   auth.HashToken(raw),
		TaskID:      taskUUID,
		AgentID:     agentID,
		WorkspaceID: workspaceID,
		UserID:      userID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(inProcBridgeTokenTTL), Valid: true},
	}); err != nil {
		e.warnBridge("persist task token", taskID, err)
		return
	}

	wsID := util.UUIDToString(workspaceID)
	agID := util.UUIDToString(agentID)
	client := InProcessAPIClient(e.routerHandler, wsID, agID, raw, taskID)

	srv := mcp.NewServer("multica-inapp", "1.0.0")
	// gitRoot is empty: server-side there is no working tree; tools that depend
	// on a local repo simply have no ambient context, exactly like a CLI run
	// outside a git checkout.
	clitools.RegisterTools(srv, client, &clitools.SessionState{}, wsID, "", "")

	RegisterBridgedMCPTools(reg, srv, DefaultInAppAdminDenylist(), true)
}

// warnBridge logs a bridge failure without aborting the task.
func (e *FirtalGatewayExecutor) warnBridge(stage, taskID string, err error) {
	if e == nil || e.logger == nil {
		return
	}
	e.logger.Warn("firtal gateway in-process bridge skipped",
		"stage", stage,
		"task_id", taskID,
		"error", err,
	)
}
