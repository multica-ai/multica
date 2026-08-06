package platformaction

// FIR-4220 slice 2: the three machine-intake boundaries (autopilot_webhook,
// daemon_runtime_callback, gateway_channel_delivery) have no person or agent
// for Allow/Ask/Deny to judge — the caller authenticates with a secret/token.
// Their policy row is therefore a workspace-layer off-switch, not an actor
// gate: an authored workspace Deny (or Disable) turns the intake off, while
// Allow, Ask, and an absent row leave it on. Ask has no meaning at an
// unattended intake (there is nobody to ask), so it does not block.
// Secret/token authentication is unchanged and stays the security boundary.

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// IntakeAllowed reports whether the workspace-layer policy row for an
// unattended intake capability leaves the intake on. Fail-open by design: a
// nil store, invalid workspace, or lookup error keeps the intake available —
// the policy row is an availability switch layered over unchanged
// authentication, so an outage in the policy store must not take heartbeats
// and webhook deliveries down with it.
func IntakeAllowed(ctx context.Context, policy *toolpolicy.Store, workspaceID pgtype.UUID, capability string) bool {
	if policy == nil || !workspaceID.Valid {
		return true
	}
	effective, err := policy.Resolve(ctx, toolpolicy.Query{
		WorkspaceID: workspaceID,
		ToolKey:     capability,
		Base:        toolpolicy.SettingAllow,
	})
	if err != nil {
		slog.Warn("platformaction: intake policy lookup failed, allowing", "capability", capability, "error", err)
		return true
	}
	if effective.Setting == toolpolicy.SettingDeny || effective.Setting == toolpolicy.SettingDisable {
		policy.RecordUsage(ctx, toolpolicy.UsageParams{
			WorkspaceID: workspaceID, ToolKey: capability, EnforcementPoint: "intake",
			Decision: toolpolicy.SettingDeny, DecidedBy: string(effective.DecidedBy),
		})
		return false
	}
	return true
}
