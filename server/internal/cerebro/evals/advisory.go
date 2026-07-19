package evals

// advisory.go implements the warn (not block) half of a workflow eval binding.
// An advisory binding (blocking=false) never changes a gate's advance decision;
// instead, when its latest run for the issue is not a pass, the AdvisoryWarner
// writes one inbox card per workspace owner/admin so the failing eval is seen
// without stopping the workflow. Fail-safe: a nil warner, no recipients, or a
// per-card write error is skipped, never fatal — the gate proceeds regardless.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// inboxTypeEvalAdvisoryFailed is the inbox_item.type for a failing advisory
// eval. A string, not a server enum; the inbox renders unknown types from
// title/body (API Response Compatibility: enum drift downgrades, never crashes).
const inboxTypeEvalAdvisoryFailed = "eval_advisory_failed"

// OwnerAdminLister resolves the owner/admin recipients of a workspace. Satisfied
// by *cerebrodb.Queries (ListCerebroWorkspaceOwnerAdmins).
type OwnerAdminLister interface {
	ListCerebroWorkspaceOwnerAdmins(ctx context.Context, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
}

// InboxWriter persists one inbox card. Satisfied by *db.Queries.
type InboxWriter interface {
	CreateInboxItem(ctx context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error)
}

// AdvisoryWarner notifies workspace owners/admins about failing advisory
// (non-blocking) eval bindings. It never blocks a gate — callers ignore its
// error for gating and only log it.
type AdvisoryWarner struct {
	store      *Store
	recipients OwnerAdminLister
	inbox      InboxWriter
	bus        *events.Bus
}

// NewAdvisoryWarner builds an AdvisoryWarner. bus may be nil (cards are still
// written, just not broadcast live), mirroring driftwatch.Sweeper.
func NewAdvisoryWarner(store *Store, recipients OwnerAdminLister, inbox InboxWriter, bus *events.Bus) *AdvisoryWarner {
	return &AdvisoryWarner{store: store, recipients: recipients, inbox: inbox, bus: bus}
}

// Warn writes an inbox card to every owner/admin for each advisory binding of
// the given phase whose latest run for this workflow+issue is not a pass. It is
// a no-op when nothing is wired or there is nothing failing, and it NEVER
// changes a gate's advance decision — the caller logs and continues on error.
func (w *AdvisoryWarner) Warn(ctx context.Context, workflowID, issueID uuid.UUID, phase string) error {
	if w == nil || w.store == nil || w.recipients == nil || w.inbox == nil {
		return nil
	}
	failing, err := w.store.FailingAdvisoryEvals(ctx, workflowID, issueID, phase)
	if err != nil {
		return fmt.Errorf("list failing advisory evals: %w", err)
	}
	if len(failing) == 0 {
		return nil
	}
	workspaceID := failing[0].WorkspaceID
	recipients, err := w.recipients.ListCerebroWorkspaceOwnerAdmins(ctx, pgUUID(workspaceID))
	if err != nil {
		return fmt.Errorf("list owner/admins: %w", err)
	}
	if len(recipients) == 0 {
		slog.Info("eval advisory: failing advisory evals but no owner/admin recipients",
			"workspace_id", workspaceID.String(), "failing_count", len(failing))
		return nil
	}
	for _, binding := range failing {
		title := fmt.Sprintf("Advisory eval failing: %s", binding.EvalTitle)
		body := fmt.Sprintf(
			"The advisory eval %q (%s) has no passing run for this issue in the %s phase. It does not block the workflow — review it when you can.",
			binding.EvalTitle, binding.EvalKey, phase,
		)
		details, _ := json.Marshal(map[string]any{
			"eval":     binding.EvalKey,
			"eval_id":  binding.EvalID.String(),
			"workflow": binding.WorkflowID.String(),
			"issue":    issueID.String(),
			"phase":    phase,
			"blocking": false,
		})
		for _, recipientID := range recipients {
			item, err := w.inbox.CreateInboxItem(ctx, db.CreateInboxItemParams{
				WorkspaceID:   pgUUID(workspaceID),
				RecipientType: "member",
				RecipientID:   recipientID,
				Type:          inboxTypeEvalAdvisoryFailed,
				Severity:      "attention",
				IssueID:       pgUUID(issueID),
				Title:         title,
				Body:          pgtype.Text{String: body, Valid: true},
				ActorType:     pgtype.Text{String: "system", Valid: true},
				ActorID:       pgtype.UUID{},
				Details:       details,
				Route:         "inbox",
			})
			if err != nil {
				slog.Warn("eval advisory: inbox write failed",
					"eval", binding.EvalKey, "recipient_id", recipientID.String(), "error", err)
				continue
			}
			publishInboxNew(w.bus, item)
		}
	}
	return nil
}

// pgUUID converts a google/uuid value to the pgtype.UUID the generated queries
// expect. uuid.UUID is a [16]byte, so the bytes map directly.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// publishInboxNew fans a freshly written inbox card out on the bus, scoped to
// the recipient so only their inbox refetches. Package-level so U11's drift
// sweeper reuses the same broadcast shape. Mirrors driftwatch.Sweeper.
func publishInboxNew(bus *events.Bus, item db.InboxItem) {
	if bus == nil {
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
	bus.Publish(events.Event{
		Type:            protocol.EventInboxNew,
		WorkspaceID:     util.UUIDToString(item.WorkspaceID),
		ActorType:       "system",
		Payload:         map[string]any{"item": resp},
		AudienceUserIDs: []string{recipientID},
	})
}
