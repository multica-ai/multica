package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/inboxv2"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The v1 write adapters.
//
// Permanent facility, not a migration step. The mobile client stays on the v1
// endpoints indefinitely, so "a legacy endpoint was called" has to remain a
// first-class way for group state to change — for as long as inbox_group
// exists, not just until the switch is flipped.
//
// Each adapter runs AFTER the legacy write, not instead of it. The legacy row
// stays the complete v1 truth at every instant, which is what makes rollback a
// matter of flipping a switch rather than restoring data: turn the gate off and
// the inbox is exactly what it was, because the rows never stopped being
// authoritative.

type inboxAdaptOp int

const (
	inboxAdaptRead inboxAdaptOp = iota
	inboxAdaptUnread
	inboxAdaptArchive
	inboxAdaptUnarchive
)

// inboxService builds the v2 service over the handler's own handles.
func (h *Handler) inboxService() *inboxv2.Service {
	return inboxv2.NewService(h.Queries, h.TxStarter)
}

// syncInboxGroup mirrors one v1 single-item write onto its group.
//
// Failures are logged, not returned. The user's action has already been
// committed to inbox_item and is fully visible to every v1 client; failing the
// request now would tell them their archive did not happen when it did. What a
// failure leaves behind is a group that disagrees with its rows, and that is
// precisely the drift reconcile is defined to repair — rows win.
func (h *Handler) syncInboxGroup(r *http.Request, item db.InboxItem, op inboxAdaptOp) {
	if item.RecipientType != "member" {
		return
	}
	svc := h.inboxService()
	ctx := r.Context()
	now := time.Now()

	var err error
	switch op {
	case inboxAdaptRead:
		_, err = svc.MarkItemRead(ctx, item.WorkspaceID, item.RecipientID, item.ID, now)
	case inboxAdaptUnread:
		_, err = svc.MarkItemUnread(ctx, item.WorkspaceID, item.RecipientID, item.ID, now)
	case inboxAdaptArchive:
		_, err = svc.ArchiveItem(ctx, item.WorkspaceID, item.RecipientID, item.ID, now)
	case inboxAdaptUnarchive:
		_, err = svc.UnarchiveItem(ctx, item.WorkspaceID, item.RecipientID, item.ID, now)
	}

	// ErrNoGroup is the ordinary pre-migration case: this row's source has not
	// been folded into a group yet. Not an error — the lazy migration will read
	// the row's own booleans when it does fold it, which is where the user's
	// action is already recorded.
	if err != nil && !errors.Is(err, inboxv2.ErrNoGroup) {
		slog.Warn("inbox v1 adapter: group sync failed",
			"item_id", uuidToString(item.ID), "op", op, "error", err)
	}
}

// syncInboxGroupBatch mirrors one v1 bulk write onto the recipient's groups.
func (h *Handler) syncInboxGroupBatch(r *http.Request, workspaceID, recipientID pgtype.UUID, op inboxv2.BatchOp) {
	if _, err := h.inboxService().ApplyBatch(r.Context(), workspaceID, recipientID, op, time.Now()); err != nil {
		slog.Warn("inbox v1 adapter: batch group sync failed",
			"workspace_id", uuidToString(workspaceID), "op", op, "error", err)
	}
}
