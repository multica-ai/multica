package driftwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DefaultInterval is the fallback cadence after the nightly run. The production
// runner waits until the next 02:00 Europe/Copenhagen window before the first
// scan so capability discovery does not run repeatedly during working hours.
const DefaultInterval = 24 * time.Hour

const (
	defaultNightlyRunHour = 2
	bootstrapWindow       = 24 * time.Hour
	capabilityTypeTool    = "tool"
	statusAllowed         = "allowed"
	statusNeedsApproval   = "needs_approval"
)

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
}

type observedCapability struct {
	Key        string
	Name       string
	Uses       int64
	FirstSeen  pgtype.Timestamptz
	LastSeen   pgtype.Timestamptz
	Permission string
	Status     string
	IsNew      bool
}

type agentScanResult struct {
	Scan         cerebrodb.CerebroAgentCapabilityScan
	Observed     []observedCapability
	New          []observedCapability
	Drift        []observedCapability
	WindowStart  time.Time
	WindowEnd    time.Time
	HistoryFound bool
}

// NewSweeper builds a Sweeper. ToolPolicy resolves each agent's declared policy
// (the same store the capabilities card uses); Bus may be nil (alerts are still
// written, just not broadcast live).
func NewSweeper(cerebro *cerebrodb.Queries, upstream *db.Queries, toolPolicy *cerebrotoolpolicy.Store, bus *events.Bus) *Sweeper {
	return &Sweeper{
		Cerebro:    cerebro,
		Upstream:   upstream,
		ToolPolicy: toolPolicy,
		Bus:        bus,
	}
}

// Run blocks on ctx and runs the sweep once nightly. Passing a custom non-default
// interval keeps tests/manual invocations able to tick on a shorter cadence.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	for {
		wait := interval
		if interval == DefaultInterval {
			wait = time.Until(nextNightlyRun(time.Now(), defaultNightlyRunHour, nightlyLocation()))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
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

// scanWorkspace checks every agent in one workspace and alerts owners/admins
// when the nightly delta found capabilities not already present in the agent's
// persisted scan history.
func (s *Sweeper) scanWorkspace(ctx context.Context, workspaceID pgtype.UUID) error {
	recipients, err := s.Cerebro.ListCerebroWorkspaceOwnerAdmins(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list owner/admins: %w", err)
	}
	agents, err := s.Upstream.ListAgents(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	now := time.Now()
	for _, agent := range agents {
		result, err := s.scanAgentCapabilities(ctx, workspaceID, agent.ID, now)
		if err != nil {
			slog.Warn("cerebro drift watcher: agent scan failed",
				"agent_id", util.UUIDToString(agent.ID), "error", err)
			continue
		}
		if len(result.New) == 0 {
			continue
		}
		if len(recipients) == 0 {
			slog.Info("cerebro drift watcher: new capabilities found but no owner/admin recipients",
				"workspace_id", util.UUIDToString(workspaceID),
				"agent_id", util.UUIDToString(agent.ID),
				"new_capability_count", len(result.New),
			)
			continue
		}
		s.alert(ctx, workspaceID, agent, result, recipients)
	}
	return nil
}

func (s *Sweeper) scanAgentCapabilities(ctx context.Context, workspaceID, agentID pgtype.UUID, now time.Time) (agentScanResult, error) {
	windowStart, historyFound, err := s.scanWindowStart(ctx, agentID, now)
	if err != nil {
		return agentScanResult{}, err
	}
	windowEnd := now.UTC()
	usage, err := s.Cerebro.ListAgentObservedToolUsageBetween(ctx, cerebrodb.ListAgentObservedToolUsageBetweenParams{
		AgentID:       agentID,
		WindowStartAt: timestamptz(windowStart),
		WindowEndAt:   timestamptz(windowEnd),
	})
	if err != nil {
		return agentScanResult{}, fmt.Errorf("list observed tool usage delta: %w", err)
	}

	known, err := s.knownCapabilities(ctx, agentID)
	if err != nil {
		return agentScanResult{}, err
	}

	permByName := map[string]string{}
	if len(usage) > 0 {
		rows, err := s.ToolPolicy.Table(ctx, cerebrotoolpolicy.TableQuery{
			WorkspaceID:     workspaceID,
			AgentID:         agentID,
			Base:            cerebrotoolpolicy.SettingAllow,
			IncludePlatform: true,
		})
		if err != nil {
			return agentScanResult{}, fmt.Errorf("load tool policy: %w", err)
		}
		permByName = permissionLookup(rows)
	}

	observed, newlyFound, drifting := computeObservedCapabilities(usage, permByName, known)
	scan, err := s.Cerebro.CreateAgentCapabilityScan(ctx, cerebrodb.CreateAgentCapabilityScanParams{
		WorkspaceID:   workspaceID,
		AgentID:       agentID,
		WindowStartAt: timestamptz(windowStart),
		WindowEndAt:   timestamptz(windowEnd),
	})
	if err != nil {
		return agentScanResult{}, fmt.Errorf("create capability scan: %w", err)
	}
	for _, cap := range observed {
		if _, err := s.Cerebro.InsertAgentCapabilityScanItem(ctx, cerebrodb.InsertAgentCapabilityScanItemParams{
			ScanID:         scan.ID,
			WorkspaceID:    workspaceID,
			AgentID:        agentID,
			CapabilityType: capabilityTypeTool,
			CapabilityKey:  cap.Key,
			DisplayName:    cap.Name,
			Uses:           cap.Uses,
			FirstSeenAt:    cap.FirstSeen,
			LastSeenAt:     cap.LastSeen,
			Permission:     nullableText(cap.Permission),
			Status:         cap.Status,
			IsNew:          cap.IsNew,
		}); err != nil {
			return agentScanResult{}, fmt.Errorf("insert capability scan item %q: %w", cap.Name, err)
		}
	}
	scan, err = s.Cerebro.UpdateAgentCapabilityScanCounts(ctx, cerebrodb.UpdateAgentCapabilityScanCountsParams{
		ObservedCapabilityCount: int32(len(observed)),
		NewCapabilityCount:      int32(len(newlyFound)),
		DriftCapabilityCount:    int32(len(drifting)),
		CompletedAt:             timestamptz(time.Now().UTC()),
		ID:                      scan.ID,
	})
	if err != nil {
		return agentScanResult{}, fmt.Errorf("update capability scan counts: %w", err)
	}
	return agentScanResult{
		Scan:         scan,
		Observed:     observed,
		New:          newlyFound,
		Drift:        drifting,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		HistoryFound: historyFound,
	}, nil
}

func (s *Sweeper) scanWindowStart(ctx context.Context, agentID pgtype.UUID, now time.Time) (time.Time, bool, error) {
	latest, err := s.Cerebro.GetAgentLatestCapabilityScan(ctx, agentID)
	if err == nil && latest.WindowEndAt.Valid {
		return latest.WindowEndAt.Time.UTC(), true, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("load latest capability scan: %w", err)
	}
	return now.Add(-bootstrapWindow).UTC(), false, nil
}

func (s *Sweeper) knownCapabilities(ctx context.Context, agentID pgtype.UUID) (map[string]bool, error) {
	keys, err := s.Cerebro.ListAgentKnownCapabilityKeys(ctx, cerebrodb.ListAgentKnownCapabilityKeysParams{
		AgentID:        agentID,
		CapabilityType: capabilityTypeTool,
	})
	if err != nil {
		return nil, fmt.Errorf("list known capability keys: %w", err)
	}
	known := make(map[string]bool, len(keys))
	for _, key := range keys {
		known[key] = true
	}
	return known, nil
}

// alert writes one inbox card per owner/admin recipient and broadcasts each so
// it lands live. Best-effort: a failed write for one recipient is logged and the
// rest still proceed.
func (s *Sweeper) alert(ctx context.Context, workspaceID pgtype.UUID, agent db.Agent, result agentScanResult, recipients []pgtype.UUID) {
	names := observedCapabilityNames(result.New)
	title := fmt.Sprintf("New capabilities: %s", agentDisplayName(agent))
	body := fmt.Sprintf(
		"%s used %d new tool%s since the previous capability scan (%s). Review the agent's Capabilities tab.",
		agentDisplayName(agent), len(result.New), plural(len(result.New)), strings.Join(names, ", "),
	)
	details, _ := json.Marshal(map[string]any{
		"agent_id":             util.UUIDToString(agent.ID),
		"agent_name":           agent.Name,
		"scan_id":              util.UUIDToString(result.Scan.ID),
		"new_capabilities":     names,
		"new_capability_count": len(result.New),
		"drift_count":          len(result.Drift),
		"window_start_at":      result.WindowStart.Format(time.RFC3339),
		"window_end_at":        result.WindowEnd.Format(time.RFC3339),
		"reason":               "agent_capability_new",
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
		"new_capability_count", len(result.New),
		"drift_count", len(result.Drift),
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

func computeObservedCapabilities(usage []cerebrodb.ListAgentObservedToolUsageBetweenRow, permByName map[string]string, known map[string]bool) ([]observedCapability, []observedCapability, []observedCapability) {
	observed := make([]observedCapability, 0, len(usage))
	newlyFound := []observedCapability{}
	drifting := []observedCapability{}
	for _, u := range usage {
		key := capabilityKey(u.Tool)
		perm, hasRow := permByName[key]
		status, drift := capabilityStatus(perm, hasRow)
		cap := observedCapability{
			Key:        key,
			Name:       u.Tool,
			Uses:       u.Uses,
			FirstSeen:  u.FirstUsed,
			LastSeen:   u.LastUsed,
			Permission: perm,
			Status:     status,
			IsNew:      !known[key],
		}
		observed = append(observed, cap)
		if cap.IsNew {
			newlyFound = append(newlyFound, cap)
		}
		if drift {
			drifting = append(drifting, cap)
		}
	}
	return observed, newlyFound, drifting
}

func capabilityStatus(permission string, hasRow bool) (status string, drift bool) {
	if !hasRow {
		return statusUnmapped, true
	}
	switch permission {
	case "allow":
		return statusAllowed, false
	case "ask":
		return statusNeedsApproval, false
	case "deny":
		return statusBlocked, true
	default:
		return statusUnmapped, true
	}
}

func capabilityKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func observedCapabilityNames(caps []observedCapability) []string {
	names := make([]string, 0, len(caps))
	for _, cap := range caps {
		names = append(names, cap.Name)
	}
	return names
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func nullableText(s string) pgtype.Text {
	if strings.TrimSpace(s) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func nightlyLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		return time.Local
	}
	return loc
}

func nextNightlyRun(now time.Time, hour int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
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
