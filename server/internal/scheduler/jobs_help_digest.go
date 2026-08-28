package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Inbox item types for the agent-help attention surface.
//
// InboxItemTypeAgentHelpRequested is the source row written by
// TaskService.notifyAgentHelpRequested (GAP-25). It is mirrored here as a
// sibling constant so the GAP-2 digest job can reference it without
// importing the service package; the canonical writer still uses the
// literal string and MUST NOT be changed by this work.
const (
	InboxItemTypeAgentHelpRequested = "agent_help_requested"

	// InboxItemTypeAgentHelpDigest is the aggregated attention item (GAP-2)
	// this job maintains: one open row per (workspace, recipient) summarizing
	// every open agent_help_requested item routed to that human.
	InboxItemTypeAgentHelpDigest = "agent_help_digest"
)

// JobNameHelpDigest is the canonical audit-row name. Stable across
// releases — do not rename without a migration.
const JobNameHelpDigest = "help_digest"

// helpDigestDefaultInterval is how often the digest reconciles. 15m keeps
// the single attention item fresh without spamming the inbox on every
// individual help request (which can arrive in bursts).
const helpDigestDefaultInterval = 15 * time.Minute

// HelpDigestItem is one summarized source help request inside a digest.
type HelpDigestItem struct {
	TaskID        string   `json:"task_id"`
	AgentID       string   `json:"agent_id"`
	BlockedReason *string  `json:"blocked_reason,omitempty"`
	Needs         []string `json:"needs,omitempty"`
	Confidence    *float64 `json:"confidence,omitempty"`
}

// HelpDigestDetails is the jsonb payload of an agent_help_digest inbox item.
type HelpDigestDetails struct {
	Count int             `json:"count"`
	Items []HelpDigestItem `json:"items"`
}

// helpDigestKey identifies the recipient of a digest. A NULL workspace is
// normalized to the global bucket (empty WorkspaceID) so workspace-less
// items still roll up somewhere.
type helpDigestKey struct {
	WorkspaceID   string
	RecipientType string
	RecipientID   string
}

// HelpDigestJob returns the JobSpec that aggregates open
// agent_help_requested inbox items into a single GAP-2 attention digest
// per (workspace, recipient). The job is idempotent and safe to run on a
// fixed cadence; it never deletes or alters the source help items.
func HelpDigestJob(pool *pgxpool.Pool) JobSpec {
	return JobSpec{
		Name:              JobNameHelpDigest,
		Cadence:           helpDigestDefaultInterval,
		ScheduleDelay:     time.Minute,
		CatchUpMode:       CatchUpLatestOnly, // a digest only needs its latest state
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        2 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true, // delete+insert is idempotent
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			5 * time.Minute,
			30 * time.Minute,
		},
		Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			return RunHelpDigest(ctx, pool)
		},
	}
}

// RunHelpDigest reconciles every (workspace, recipient) digest against the
// current set of open agent_help_requested items. It returns a HandlerResult
// whose Result carries the counts for auditability.
func RunHelpDigest(ctx context.Context, pool *pgxpool.Pool) (HandlerResult, error) {
	open, err := listOpenAgentHelpRequested(ctx, pool)
	if err != nil {
		return HandlerResult{}, fmt.Errorf("help_digest: list open help items: %w", err)
	}
	groups := groupHelpItems(open)

	existing, err := listOpenAgentHelpDigestKeys(ctx, pool)
	if err != nil {
		return HandlerResult{}, fmt.Errorf("help_digest: list existing digests: %w", err)
	}
	active := make(map[helpDigestKey]bool, len(groups))
	for k := range groups {
		active[k] = true
	}

	var upserted, cleared int64
	// Upsert one digest row per group that still has open items.
	for key, items := range groups {
		if err := upsertDigestForGroup(ctx, pool, key, items); err != nil {
			return HandlerResult{}, fmt.Errorf("help_digest: upsert for %+v: %w", key, err)
		}
		upserted++
	}
	// Clear digests whose group no longer has any open help item, so a
	// resolved (archived) request does not leave a stale attention item.
	for _, dk := range existing {
		if active[dk] {
			continue
		}
		n, err := deleteOpenAgentHelpDigest(ctx, pool, dk)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("help_digest: clear stale digest for %+v: %w", dk, err)
		}
		cleared += n
	}

	slog.Info("help_digest: reconciled",
		"open_help", len(open), "groups", upserted, "cleared", cleared)
	return HandlerResult{
		RowsAffected: upserted + cleared,
		Result: map[string]any{
			"open_help": int64(len(open)),
			"groups":    upserted,
			"cleared":   cleared,
		},
	}, nil
}

// upsertDigestForGroup atomically replaces the open digest for one
// (workspace, recipient) with a fresh summary. No unique constraint is
// required: we delete the open digest(s) for the key then insert exactly
// one, inside a transaction so a concurrent tick can never observe two
// (or zero) rows.
func upsertDigestForGroup(ctx context.Context, pool *pgxpool.Pool, key helpDigestKey, items []db.InboxItem) error {
	details, err := buildHelpDigestDetails(items)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("help_digest: encode digest: %w", err)
	}

	var ws pgtype.UUID
	if key.WorkspaceID != "" {
		if id, perr := util.ParseUUID(key.WorkspaceID); perr == nil {
			ws = id
		}
	}
	recipientID, err := util.ParseUUID(key.RecipientID)
	if err != nil {
		return fmt.Errorf("help_digest: parse recipient %q: %w", key.RecipientID, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("help_digest: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qt := db.New(tx)
	if _, err := tx.Exec(ctx, deleteOpenAgentHelpDigestSQL,
		InboxItemTypeAgentHelpDigest,
		workspaceParam(key.WorkspaceID),
		key.RecipientType,
		key.RecipientID,
	); err != nil {
		return fmt.Errorf("help_digest: clear prior digest: %w", err)
	}
	title := fmt.Sprintf("Agent help requests (%d)", len(items))
	body := summarizeDigestBody(items)
	if _, err := qt.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   ws,
		RecipientType: key.RecipientType,
		RecipientID:   recipientID,
		Type:          InboxItemTypeAgentHelpDigest,
		Severity:      "action_required",
		IssueID:       pgtype.UUID{},
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: body != ""},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		ActorID:       pgtype.UUID{},
		Details:       payload,
	}); err != nil {
		return fmt.Errorf("help_digest: create digest: %w", err)
	}
	return tx.Commit(ctx)
}

// buildHelpDigestDetails converts a group of open help-request inbox items
// into the digest details payload. Pure: no DB, no clock, no randomness.
func buildHelpDigestDetails(items []db.InboxItem) (HelpDigestDetails, error) {
	det := HelpDigestDetails{
		Count: len(items),
		Items: make([]HelpDigestItem, 0, len(items)),
	}
	for _, it := range items {
		var src struct {
			TaskID        string   `json:"task_id"`
			AgentID       string   `json:"agent_id"`
			BlockedReason *string  `json:"blocked_reason"`
			Needs         []string `json:"needs"`
			Confidence    *float64 `json:"confidence"`
		}
		if err := json.Unmarshal(it.Details, &src); err != nil {
			return HelpDigestDetails{}, fmt.Errorf("help_digest: decode source details for item %s: %w", util.UUIDToString(it.ID), err)
		}
		det.Items = append(det.Items, HelpDigestItem{
			TaskID:        src.TaskID,
			AgentID:       src.AgentID,
			BlockedReason: src.BlockedReason,
			Needs:         src.Needs,
			Confidence:    src.Confidence,
		})
	}
	// Deterministic ordering by task_id so digests are stable across runs.
	sort.Slice(det.Items, func(i, j int) bool {
		return det.Items[i].TaskID < det.Items[j].TaskID
	})
	return det, nil
}

// groupHelpItems buckets open help-request items by (workspace, recipient).
// A NULL workspace is normalized to the global bucket (empty WorkspaceID).
// Given an empty list it returns an empty map — every existing digest is
// then considered stale and cleared by the caller.
func groupHelpItems(items []db.InboxItem) map[helpDigestKey][]db.InboxItem {
	out := make(map[helpDigestKey][]db.InboxItem, len(items))
	for _, it := range items {
		key := helpDigestKey{
			WorkspaceID:   workspaceKey(it.WorkspaceID),
			RecipientType: it.RecipientType,
			RecipientID:   util.UUIDToString(it.RecipientID),
		}
		out[key] = append(out[key], it)
	}
	return out
}

func workspaceKey(ws pgtype.UUID) string {
	if !ws.Valid {
		return ""
	}
	return util.UUIDToString(ws)
}

func summarizeDigestBody(items []db.InboxItem) string {
	if len(items) == 1 {
		return "1 agent is waiting on you for help. Open the item to see what it needs."
	}
	return fmt.Sprintf("%d agents are waiting on you for help. Open the item to see what each needs.", len(items))
}

// listOpenAgentHelpRequested returns the open (unarchived) help-request
// inbox items the digest rolls up. We select only the columns the digest
// needs so the scan stays stable without a schema regeneration.
func listOpenAgentHelpRequested(ctx context.Context, pool *pgxpool.Pool) ([]db.InboxItem, error) {
	rows, err := pool.Query(ctx, listOpenAgentHelpRequestedSQL,
		InboxItemTypeAgentHelpRequested)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []db.InboxItem{}
	for rows.Next() {
		var it db.InboxItem
		if err := rows.Scan(
			&it.ID,
			&it.WorkspaceID,
			&it.RecipientType,
			&it.RecipientID,
			&it.Details,
		); err != nil {
			return nil, err
		}
		it.Type = InboxItemTypeAgentHelpRequested
		it.Severity = "action_required"
		out = append(out, it)
	}
	return out, rows.Err()
}

// listOpenAgentHelpDigestKeys returns the (workspace, recipient) keys of
// every open digest row, so the reconciler can clear the ones no longer
// backed by open help items.
func listOpenAgentHelpDigestKeys(ctx context.Context, pool *pgxpool.Pool) ([]helpDigestKey, error) {
	rows, err := pool.Query(ctx, listOpenAgentHelpDigestKeysSQL,
		InboxItemTypeAgentHelpDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []helpDigestKey{}
	for rows.Next() {
		var ws pgtype.UUID
		var rt, rid string
		if err := rows.Scan(&ws, &rt, &rid); err != nil {
			return nil, err
		}
		out = append(out, helpDigestKey{
			WorkspaceID:   workspaceKey(ws),
			RecipientType: rt,
			RecipientID:   rid,
		})
	}
	return out, rows.Err()
}

const listOpenAgentHelpRequestedSQL = `
SELECT id, workspace_id, recipient_type, recipient_id, details
FROM inbox_item
WHERE type = $1
  AND severity = 'action_required'
  AND archived = false
ORDER BY workspace_id, recipient_id, created_at;`

const listOpenAgentHelpDigestKeysSQL = `
SELECT workspace_id, recipient_type, recipient_id
FROM inbox_item
WHERE type = $1
  AND archived = false;`

// deleteOpenAgentHelpDigest removes the open digest row(s) for a key.
func deleteOpenAgentHelpDigest(ctx context.Context, pool *pgxpool.Pool, key helpDigestKey) (int64, error) {
	tag, err := pool.Exec(ctx, deleteOpenAgentHelpDigestSQL,
		InboxItemTypeAgentHelpDigest,
		workspaceParam(key.WorkspaceID),
		key.RecipientType,
		key.RecipientID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// deleteOpenAgentHelpDigestSQL is the DELETE used both for the per-group
// clear path and (inside a transaction) for the upsert's replace step.
const deleteOpenAgentHelpDigestSQL = `
DELETE FROM inbox_item
WHERE type = $1
  AND archived = false
  AND (workspace_id IS NOT DISTINCT FROM $2)
  AND recipient_type = $3
  AND recipient_id = $4;`

// workspaceParam returns the pgtype.UUID to bind for a digest key's
// workspace. The global bucket (empty string) binds as NULL so the
// IS NOT DISTINCT FROM clause matches NULL workspace rows.
func workspaceParam(key string) pgtype.UUID {
	if key == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(key)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
