package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/inboxread"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Struct-literal handlers used by tests still share a bounded guard.
var defaultInboxReadGuard = inboxread.New(inboxread.ConfigFromEnv())

func (h *Handler) inboxReadGuard() *inboxread.Guard {
	if h.InboxReads != nil {
		return h.InboxReads
	}
	return defaultInboxReadGuard
}

func runInboxRead[T any](ctx context.Context, h *Handler, operation string, query func(context.Context, *db.Queries) (T, error)) (result T, err error) {
	err = h.inboxReadGuard().Run(ctx, operation, h.TxStarter, func(ctx context.Context, tx pgx.Tx) error {
		var queryErr error
		result, queryErr = query(ctx, h.Queries.WithTx(tx))
		return queryErr
	})
	return result, err
}

func writeInboxReadError(w http.ResponseWriter, err error, message string) {
	if inboxread.IsUnavailable(err) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "inbox is temporarily busy; retry shortly")
		return
	}
	if errors.Is(err, context.Canceled) {
		w.WriteHeader(499)
		return
	}
	writeError(w, http.StatusInternalServerError, message)
}

func (h *Handler) inboxReadRequest(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := h.inboxReadGuard().Context(r.Context())
	return r.WithContext(ctx), cancel
}

// Permission failures still fail closed. A request-budget timeout during the
// lookup follows the same temporary-unavailability contract as the inbox SQL.
func (h *Handler) requestInboxHiddenProjects(w http.ResponseWriter, r *http.Request, workspaceID string) (projectaccess.Hidden, bool) {
	hidden, err := h.hiddenProjects(r, workspaceID)
	if err != nil {
		if !writeQueryCancellation(w, r, err) {
			slog.Warn("resolve inbox project access failed", append(logger.RequestAttrs(r), "error", err)...)
			writeInboxReadError(w, err, "failed to resolve project access")
		}
		return projectaccess.Hidden{}, false
	}
	return hidden, true
}
