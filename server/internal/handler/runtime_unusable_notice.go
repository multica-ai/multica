package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// noteRuntimeUnusable records, on the issue itself, that a trigger was refused
// because the target's agent CLI cannot run on its machine.
//
// Written for EVERY author, not only the unattended ones. The composer chip and
// the post-send toast do reach whoever was typing, but they carry the reason
// code alone — the repair command is too long for a chip and too important to
// put in something that disappears. An agent-authored @mention has nobody
// watching a response at all. So both get the same durable record, and the
// transient surfaces stay a preview of it rather than the only copy (MUL-6164).
//
// Best-effort by construction: the refusal already happened and is correct
// whether or not this note lands, so a failure here is logged, never returned.
func (h *Handler) noteRuntimeUnusable(ctx context.Context, issue db.Issue, agent db.Agent, verdict service.AgentVerdict) {
	content := service.RuntimeUnusableNotice(agent.Name, verdict)
	h.noteTriggerFailure(ctx, issue, content, pgtype.UUID{}, slog.String("agent_id", uuidToString(agent.ID)))
}
