// JEH-1173: persona-mask redaction-audit writer. Persists one row per
// deny / allow-with-mask read into cerebro_persona_mask_audit. Lives in
// the cerebro mask package so the upstream handler stays free of any
// cerebrodb import.
//
// Failures are logged at WARN and swallowed: a missing audit row must
// never break a real read. The masking decision itself has already
// been made by the time Write is called.

package mask

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

// AuditWriter inserts persona-mask audit rows via the cerebrodb Queries
// handle. Construct with NewAuditWriter; a nil writer is the no-op
// fallback the router uses when persona itself isn't configured.
type AuditWriter struct {
	q   *cerebrodb.Queries
	log *slog.Logger
}

// NewAuditWriter wraps a cerebrodb.Queries handle. Pass nil log to use
// slog.Default.
func NewAuditWriter(q *cerebrodb.Queries, log *slog.Logger) *AuditWriter {
	if log == nil {
		log = slog.Default()
	}
	return &AuditWriter{q: q, log: log}
}

// Write satisfies handler.PersonaMaskAuditWriter. Fire-and-forget: on
// error, logs and returns. workspace IDs that don't parse fall back to
// a zero UUID — the row still lands, just without a workspace foreign
// key value (the column is plain UUID, no FK).
func (w *AuditWriter) Write(ctx context.Context, evt handler.PersonaMaskAuditEvent) {
	if w == nil || w.q == nil {
		return
	}
	wsUUID, err := util.ParseUUID(evt.WorkspaceID)
	if err != nil {
		// Empty / malformed workspace IDs reach here only when the caller
		// didn't pass one (anonymous-link path, system actor). Insert
		// with a NULL-shaped pgtype.UUID so the row is still searchable
		// by resource_id / actor_id.
		wsUUID = pgtype.UUID{}
	}
	masked := evt.MaskedFields
	if masked == nil {
		masked = []string{}
	}
	maskedJSON, err := json.Marshal(masked)
	if err != nil {
		w.log.Warn("persona-mask-audit: masked_fields marshal failed",
			"err", err, "resource_kind", evt.ResourceKind, "resource_id", evt.ResourceID)
		maskedJSON = []byte("[]")
	}
	err = w.q.InsertPersonaMaskAudit(ctx, cerebrodb.InsertPersonaMaskAuditParams{
		WorkspaceID:    wsUUID,
		ActorType:      evt.ActorType,
		ActorID:        evt.ActorID,
		Action:         evt.Action,
		ResourceKind:   evt.ResourceKind,
		ResourceID:     evt.ResourceID,
		Classification: evt.Classification,
		Decision:       evt.Decision,
		MaskedFields:   maskedJSON,
		DecisionID:     evt.DecisionID,
		Reason:         evt.Reason,
	})
	if err != nil {
		w.log.Warn("persona-mask-audit: insert failed",
			"err", err,
			"workspace_id", evt.WorkspaceID,
			"actor_id", evt.ActorID,
			"resource_kind", evt.ResourceKind,
			"resource_id", evt.ResourceID,
			"decision", evt.Decision,
		)
	}
}
