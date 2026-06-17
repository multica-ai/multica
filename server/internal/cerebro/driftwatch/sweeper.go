package driftwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DefaultInterval is how often the watcher sweeps when main passes <= 0. Drift
// is a slow-moving operational signal (it needs runs to accumulate), so a 6h
// cadence is plenty and keeps the per-agent tool-policy resolution cheap.
const DefaultInterval = 6 * time.Hour

// inboxTypeCapabilityDrift is the inbox_item.type for a drift alert. A string,
// not a server enum; the frontend inbox renders unknown types from title/body
// (API Response Compatibility: enum drift downgrades, never crashes).
const inboxTypeCapabilityDrift = "agent_capability_drift"

// Sweeper periodically flags agents whose observed tool use drifts from their
// declared policy and alerts the workspace owners/admins. One per process.
// Gated by the cerebro_capability_drift_watcher flag (default OFF), so it does
// nothing until an admin turns it on. Mirrors
// server/internal/cerebro/recurringissue/sweeper.go.
type Sweeper struct {
	Cerebro    *cerebrodb.Queries
	Upstream   *db.Queries
	ToolPolicy *cerebrotoolpolicy.Store
	Bus        *events.Bus

	// lastSignature dedups alerts in-memory: agent ID -> the drift signature we
	// last alerted on. We re-alert only when the set of drifting tools CHANGES,
	// not every tick. In-memory on purpose: a process restart may re-alert an
	// existing drift once, which for an off-by-default security signal is
	// acceptable (and arguably a useful reminder) and avoids a state table.
	lastSignature map[[16]byte]string
}

// NewSweeper builds a Sweeper. ToolPolicy resolves each agent's declared policy
// (the same store the capabilities card uses); Bus may be nil (alerts are still
// written, just not broadcast live).
func NewSweeper(cerebro *cerebrodb.Queries, upstream *db.Queries, toolPolicy *cerebrotoolpolicy.Store, bus *events.Bus) *Sweeper {
	return &Sweeper{
		Cerebro:       cerebro,
		Upstream:      upstream,
		ToolPolicy:    toolPolicy,
		Bus:           bus,
		lastSignature: map[[16]byte]string{},
	}
}

// Run blocks on ctx and ticks the sweep at the requested interval.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("cerebro drift watcher: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro drift watcher: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one pass over every workspace where the watcher flag is ON. The flag
// defaults OFF, so a workspace that never enabled it is simply absent from the
// list and costs nothing.
func (s *Sweeper) Tick(ctx context.Context) error {
	workspaces, err := s.Cerebro.ListWorkspacesWithCapabilityDriftWatcher(ctx)
	if err != nil {
		return fmt.Errorf("list watcher workspaces: %w", err)
	}
	for _, wsID := range workspaces {
		if err := s.scanWorkspace(ctx, wsID); err != nil {
			slog.Warn("cerebro drift watcher: workspace scan failed",
				"workspace_id", util.UUIDToString(wsID), "error", err)
		}
	}
	return nil
}

// scanWorkspace checks every agent in one workspace and alerts on drift.
func (s *Sweeper) scanWorkspace(ctx context.Context, workspaceID pgtype.UUID) error {
	recipients, err := s.Cerebro.ListCerebroWorkspaceOwnerAdmins(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list owner/admins: %w", err)
	}
	if len(recipients) == 0 {
		// No one who can act on a drift alert — skip rather than write inbox
		// rows nobody will see.
		return nil
	}
	agents, err := s.Upstream.ListAgents(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	for _, agent := range agents {
		drift := s.agentDrift(ctx, workspaceID, agent.ID)
		key := agent.ID.Bytes
		if len(drift) == 0 {
			// Drift cleared (or never had any) — forget the last signature so a
			// future re-drift alerts again instead of being deduped away.
			delete(s.lastSignature, key)
			continue
		}
		sig := driftSignature(drift)
		if s.lastSignature[key] == sig {
			continue // already alerted on this exact drift set
		}
		s.lastSignature[key] = sig
		s.alert(ctx, workspaceID, agent, drift, recipients)
	}
	return nil
}

// agentDrift resolves one agent's observed-vs-declared drift, reusing the same
// inputs the capabilities card uses: observed tool usage (Bid B) compared to the
// declared tool-policy table. Returns nil on any lookup error so one bad agent
// never aborts the workspace sweep.
func (s *Sweeper) agentDrift(ctx context.Context, workspaceID, agentID pgtype.UUID) []DriftTool {
	usage, err := s.Cerebro.ListAgentObservedToolUsage(ctx, cerebrodb.ListAgentObservedToolUsageParams{
		AgentID:    agentID,
		WindowDays: observedWindowDays,
	})
	if err != nil {
		slog.Warn("cerebro drift watcher: observed usage lookup failed",
			"agent_id", util.UUIDToString(agentID), "error", err)
		return nil
	}
	if len(usage) == 0 {
		return nil
	}
	rows, err := s.ToolPolicy.Table(ctx, cerebrotoolpolicy.TableQuery{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		Base:            cerebrotoolpolicy.SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		slog.Warn("cerebro drift watcher: tool-policy lookup failed",
			"agent_id", util.UUIDToString(agentID), "error", err)
		return nil
	}
	return computeDrift(usage, permissionLookup(rows))
}

// alert writes one inbox card per owner/admin recipient and broadcasts each so
// it lands live. Best-effort: a failed write for one recipient is logged and the
// rest still proceed.
func (s *Sweeper) alert(ctx context.Context, workspaceID pgtype.UUID, agent db.Agent, drift []DriftTool, recipients []pgtype.UUID) {
	names := driftToolNames(drift)
	title := fmt.Sprintf("Capability drift: %s", agentDisplayName(agent))
	body := fmt.Sprintf(
		"%s used %d tool%s its policy does not allow (%s). Review on the agent's Capabilities tab.",
		agentDisplayName(agent), len(drift), plural(len(drift)), strings.Join(names, ", "),
	)
	details, _ := json.Marshal(map[string]any{
		"agent_id":    util.UUIDToString(agent.ID),
		"agent_name":  agent.Name,
		"drift_tools": names,
		"drift_count": len(drift),
		"reason":      "capability_drift",
	})

	for _, userID := range recipients {
		item, err := s.Upstream.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   workspaceID,
			RecipientType: "member",
			RecipientID:   userID,
			Type:          inboxTypeCapabilityDrift,
			Severity:      "attention",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: "system", Valid: true},
			ActorID:       pgtype.UUID{},
			Details:       details,
			Route:         "inbox",
		})
		if err != nil {
			slog.Warn("cerebro drift watcher: inbox write failed",
				"agent_id", util.UUIDToString(agent.ID),
				"recipient_id", util.UUIDToString(userID), "error", err)
			continue
		}
		s.publishInboxNew(item)
	}

	slog.Info("cerebro drift watcher: alerted",
		"workspace_id", util.UUIDToString(workspaceID),
		"agent_id", util.UUIDToString(agent.ID),
		"drift_count", len(drift),
		"recipients", len(recipients),
	)
}

// publishInboxNew fans the new card out on the bus, scoped to the recipient so
// only their inbox refetches. Mirrors runtime.Service.publishInboxNew.
func (s *Sweeper) publishInboxNew(item db.InboxItem) {
	if s.Bus == nil {
		return
	}
	recipientID := util.UUIDToString(item.RecipientID)
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   recipientID,
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"route":          item.Route,
	}
	s.Bus.Publish(events.Event{
		Type:            protocol.EventInboxNew,
		WorkspaceID:     util.UUIDToString(item.WorkspaceID),
		ActorType:       "system",
		Payload:         map[string]any{"item": resp},
		AudienceUserIDs: []string{recipientID},
	})
}

func agentDisplayName(agent db.Agent) string {
	if strings.TrimSpace(agent.Name) == "" {
		return "Agent"
	}
	return agent.Name
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
