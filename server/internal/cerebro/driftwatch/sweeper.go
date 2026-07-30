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

// digestOriginType is stamped into the digest issue's origin_type so
// GetOpenCapabilityDigestIssue can find the workspace's current digest in O(1).
// Keep this string stable — changing it orphans every open digest issue.
const digestOriginType = "capability_digest"

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
//
// The auto-permission flag is read once per tick rather than per workspace: it
// is a second, independently-defaulted-OFF switch (FIR-4012), and a workspace
// that has the watcher on but auto-permission off still gets the digest — it
// just gets no rules written on its behalf.
func (s *Sweeper) Tick(ctx context.Context) error {
	workspaces, err := s.Cerebro.ListWorkspacesWithCapabilityDriftWatcher(ctx)
	if err != nil {
		return fmt.Errorf("list watcher workspaces: %w", err)
	}
	autoPermission, err := s.autoPermissionWorkspaces(ctx)
	if err != nil {
		// A failed flag read must not cancel the scan; degrade to "off", which
		// is the non-writing direction.
		slog.Warn("cerebro drift watcher: auto-permission flag read failed", "error", err)
		autoPermission = map[string]bool{}
	}
	for _, wsID := range workspaces {
		if err := s.scanWorkspace(ctx, wsID, autoPermission[util.UUIDToString(wsID)]); err != nil {
			slog.Warn("cerebro drift watcher: workspace scan failed",
				"workspace_id", util.UUIDToString(wsID), "error", err)
		}
	}
	return nil
}

// autoPermissionWorkspaces indexes the workspaces that opted into having a
// permission rule created automatically for an unmapped capability.
func (s *Sweeper) autoPermissionWorkspaces(ctx context.Context) (map[string]bool, error) {
	ids, err := s.Cerebro.ListWorkspacesWithCapabilityAutoPermission(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auto-permission workspaces: %w", err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[util.UUIDToString(id)] = true
	}
	return out, nil
}

// scanWorkspace scans every agent in one workspace and emits ONE workspace-wide
// digest of the capabilities no permission sanctions (FIR-4012), instead of the
// per-agent alert this watcher shipped with.
//
// The per-agent scan itself still runs for every agent even when nothing is
// reportable: scanAgentCapabilities persists the scan row, and the next run's
// window start is read off that row. Skipping the persistence for a quiet agent
// would silently widen the following night's window.
func (s *Sweeper) scanWorkspace(ctx context.Context, workspaceID pgtype.UUID, autoPermission bool) error {
	recipients, err := s.Cerebro.ListCerebroWorkspaceOwnerAdmins(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list owner/admins: %w", err)
	}
	agents, err := s.Upstream.ListAgents(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	now := time.Now()
	findings := make([]agentFindings, 0, len(agents))
	windowStart, windowEnd := time.Time{}, time.Time{}
	autoCreated := 0

	for _, agent := range agents {
		result, err := s.scanAgentCapabilities(ctx, workspaceID, agent.ID, now)
		if err != nil {
			slog.Warn("cerebro drift watcher: agent scan failed",
				"agent_id", util.UUIDToString(agent.ID), "error", err)
			continue
		}
		if windowStart.IsZero() || result.WindowStart.Before(windowStart) {
			windowStart = result.WindowStart
		}
		if result.WindowEnd.After(windowEnd) {
			windowEnd = result.WindowEnd
		}

		unpermitted := unpermittedCapabilities(result.Observed)
		if len(unpermitted) == 0 {
			continue
		}
		if autoPermission {
			autoCreated += s.createPermissionRules(ctx, workspaceID, agent.ID, unpermitted)
		}
		findings = append(findings, agentFindings{
			AgentID:   util.UUIDToString(agent.ID),
			AgentName: agentDisplayName(agent),
			Caps:      unpermitted,
		})
	}

	rows := buildDigest(findings)
	if len(rows) == 0 {
		return nil
	}
	if len(recipients) == 0 {
		slog.Info("cerebro drift watcher: unpermitted capabilities found but no owner/admin recipients",
			"workspace_id", util.UUIDToString(workspaceID),
			"capability_count", len(rows),
		)
		return nil
	}
	return s.publishDigest(ctx, workspaceID, rows, recipients, windowStart, windowEnd, autoPermission, autoCreated)
}

// createPermissionRules writes an explicit rule for every capability that has no
// policy row at all, so the capability becomes visible and steerable in the
// permission table instead of sitting outside it.
//
// The rule is written as ALLOW, not deny, and that is deliberate: every
// capability in this list is one the agent has ALREADY used, so writing a deny
// would break working agents overnight without anyone deciding to. Allow at the
// agent layer is behaviour-neutral (the resolver's base for these paths is
// already allow) but it creates the control surface — blocking is then one
// change away, from the digest issue or the Capabilities tab. Blocked
// capabilities are excluded: they already have a rule, and overwriting a
// deliberate deny with an allow would invert the reader's intent.
//
// Failures are logged and skipped, never fatal: a missing rule costs visibility,
// while aborting the sweep costs the whole digest.
//
// Returns how many rules were actually written, so the digest notification can
// say that rules were created instead of leaving the reader to open the issue to
// find out (FIR-4012).
func (s *Sweeper) createPermissionRules(ctx context.Context, workspaceID, agentID pgtype.UUID, caps []observedCapability) int {
	if s.ToolPolicy == nil {
		return 0
	}
	created := 0
	for _, key := range unmappedCapabilityKeys(caps) {
		if _, err := s.ToolPolicy.Set(ctx, cerebrotoolpolicy.SetParams{
			WorkspaceID: workspaceID,
			ToolKey:     key,
			Layer:       cerebrotoolpolicy.LayerAgent,
			SubjectID:   agentID,
			Setting:     cerebrotoolpolicy.SettingAllow,
			// UpdatedBy stays zero on purpose — Store.recordAudit stamps a zero
			// actor as a "system" write, which is the honest provenance here.
		}); err != nil {
			slog.Warn("cerebro drift watcher: auto permission write failed",
				"workspace_id", util.UUIDToString(workspaceID),
				"agent_id", util.UUIDToString(agentID),
				"tool_key", key, "error", err)
			continue
		}
		created++
	}
	if created > 0 {
		slog.Info("cerebro drift watcher: auto-created permission rules",
			"workspace_id", util.UUIDToString(workspaceID),
			"agent_id", util.UUIDToString(agentID),
			"rules", created)
	}
	return created
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

// publishDigest writes the workspace's single digest issue (creating it, or
// rewriting the open one) and then notifies the owners/admins — once, about the
// whole set, with the issue attached so the notification opens something that
// can be replied to.
//
// The inbox card is only written when the finding set actually CHANGED since the
// last digest. Without that check a workspace with one stubborn ungoverned tool
// would get a card every single night for a fact its readers already know.
func (s *Sweeper) publishDigest(
	ctx context.Context,
	workspaceID pgtype.UUID,
	rows []DigestRow,
	recipients []pgtype.UUID,
	windowStart, windowEnd time.Time,
	autoPermission bool,
	autoCreated int,
) error {
	signature := digestSignature(rows)
	body := embedDigestSignature(
		renderDigestBody(rows, windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339), autoPermission),
		signature,
	)
	title := digestTitle(rows)

	issue, changed, err := s.upsertDigestIssue(ctx, workspaceID, title, body, signature, recipients)
	if err != nil {
		return fmt.Errorf("upsert digest issue: %w", err)
	}
	if !changed {
		slog.Info("cerebro drift watcher: digest unchanged, no new notification",
			"workspace_id", util.UUIDToString(workspaceID),
			"issue_id", util.UUIDToString(issue.ID),
			"capability_count", len(rows))
		return nil
	}

	s.notifyDigest(ctx, workspaceID, issue, rows, recipients, windowStart, windowEnd, autoCreated)
	return nil
}

// upsertDigestIssue reuses the workspace's open digest issue when there is one,
// and creates it otherwise. Returns the issue plus whether the finding set
// differs from what the issue already showed.
func (s *Sweeper) upsertDigestIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	title, body, signature string,
	recipients []pgtype.UUID,
) (db.Issue, bool, error) {
	existing, err := s.Cerebro.GetOpenCapabilityDigestIssue(ctx, workspaceID)
	if err == nil {
		if extractDigestSignature(existing.Description.String) == signature {
			return db.Issue(existing), false, nil
		}
		updated, err := s.Cerebro.UpdateCapabilityDigestIssue(ctx, cerebrodb.UpdateCapabilityDigestIssueParams{
			ID:          existing.ID,
			WorkspaceID: workspaceID,
			Title:       title,
			Description: pgtype.Text{String: body, Valid: true},
		})
		if err != nil {
			return db.Issue{}, false, fmt.Errorf("update digest issue: %w", err)
		}
		return db.Issue(updated), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, false, fmt.Errorf("lookup digest issue: %w", err)
	}

	// Creation is deliberately not wrapped in a transaction — the Sweeper holds
	// query handles, not a pool. The only failure mode is a burnt issue number
	// when the insert fails after the counter moved, which costs a gap in the
	// workspace's issue numbering and nothing else.
	number, err := s.Upstream.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("increment issue counter: %w", err)
	}

	// Owner/admins come back owner-first, so recipients[0] is the most senior
	// human who can act on this — the natural assignee and creator for a
	// system-authored governance issue.
	owner := recipients[0]
	issue, err := s.Upstream.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:  workspaceID,
		Title:        title,
		Description:  pgtype.Text{String: body, Valid: true},
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "member", Valid: true},
		AssigneeID:   owner,
		CreatorType:  "member",
		CreatorID:    owner,
		Number:       number,
		OriginType:   pgtype.Text{String: digestOriginType, Valid: true},
		OriginID:     workspaceID,
	})
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("create digest issue: %w", err)
	}

	for _, userID := range recipients {
		if !userID.Valid {
			continue
		}
		if err := s.Upstream.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: "member",
			UserID:   userID,
			Reason:   "manual",
		}); err != nil {
			slog.Warn("cerebro drift watcher: digest subscriber failed",
				"issue_id", util.UUIDToString(issue.ID),
				"recipient_id", util.UUIDToString(userID), "error", err)
		}
	}
	return issue, true, nil
}

// notifyDigest writes one inbox card per owner/admin — one card for the whole
// workspace, carrying the issue id so the row opens the repliable digest instead
// of a dead-end alert. Best-effort per recipient.
//
// autoCreated is how many rules the auto-permission flag wrote during this scan.
// It is stated on the card itself, not only inside the issue body: a write that
// happened on the reader's behalf must be visible in the same message that tells
// them a scan ran, otherwise they only learn about it by opening the issue.
func (s *Sweeper) notifyDigest(
	ctx context.Context,
	workspaceID pgtype.UUID,
	issue db.Issue,
	rows []DigestRow,
	recipients []pgtype.UUID,
	windowStart, windowEnd time.Time,
	autoCreated int,
) {
	agentCount := countDigestAgents(rows)
	title := digestTitle(rows)
	body := renderDigestNotificationBody(rows, agentCount, autoCreated)
	details, _ := json.Marshal(map[string]any{
		"issue_id":           util.UUIDToString(issue.ID),
		"capability_count":   len(rows),
		"agent_count":        agentCount,
		"capabilities":       digestRowNames(rows),
		"auto_created_rules": autoCreated,
		"window_start_at":    windowStart.Format(time.RFC3339),
		"window_end_at":      windowEnd.Format(time.RFC3339),
		"reason":             "agent_capability_unpermitted",
	})

	for _, userID := range recipients {
		item, err := s.Upstream.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   workspaceID,
			RecipientType: "member",
			RecipientID:   userID,
			Type:          inboxTypeCapabilityDrift,
			Severity:      "attention",
			IssueID:       issue.ID,
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: "system", Valid: true},
			ActorID:       pgtype.UUID{},
			Details:       details,
			Route:         "inbox",
		})
		if err != nil {
			slog.Warn("cerebro drift watcher: inbox write failed",
				"issue_id", util.UUIDToString(issue.ID),
				"recipient_id", util.UUIDToString(userID), "error", err)
			continue
		}
		s.publishInboxNew(item)
	}

	slog.Info("cerebro drift watcher: digest published",
		"workspace_id", util.UUIDToString(workspaceID),
		"issue_id", util.UUIDToString(issue.ID),
		"capability_count", len(rows),
		"agent_count", agentCount,
		"auto_created_rules", autoCreated,
		"recipients", len(recipients),
	)
}

// countDigestAgents counts the DISTINCT agents across every row — the same agent
// hitting three capabilities is one agent, not three.
func countDigestAgents(rows []DigestRow) int {
	seen := map[string]bool{}
	for _, row := range rows {
		for _, a := range row.Agents {
			seen[a.ID] = true
		}
	}
	return len(seen)
}

func digestRowNames(rows []DigestRow) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

// topDigestNames is the preview in the inbox body — the first few names, with
// the remainder counted rather than dropped.
func topDigestNames(rows []DigestRow, limit int) []string {
	names := digestRowNames(rows)
	if len(names) <= limit {
		return names
	}
	return append(names[:limit:limit], fmt.Sprintf("+%d more", len(names)-limit))
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
		if u.MandateDenials > 0 {
			status, drift = statusBlocked, true
		}
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
