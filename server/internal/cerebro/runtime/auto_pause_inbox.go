package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// inboxTypeRuntimePaused is the inbox_item.type for the FIR-2611 aggregated
// auth/quota auto-pause card. Mirrored in the frontend union + locales.
const inboxTypeRuntimePaused = "runtime_auto_paused"

// upsertRuntimePauseCard maintains exactly one aggregated inbox card per
// (runtime, day) for the runtime owner. The first auth/quota auto-pause of the
// day inserts the card; every later pause for the same runtime that day bumps
// the failed-run counter and refreshes the reset time in place, rather than
// adding another failed-run row to the owner's inbox.
//
// Runtime-level (issue_id NULL) so the card aggregates across every issue the
// runtime was working. Best-effort end to end: any DB / bus error is logged and
// swallowed — the card is a notification, never a gate on the pause itself.
func (s *Service) upsertRuntimePauseCard(ctx context.Context, runtimeID pgtype.UUID, count int32, unpauseAt time.Time, circuitOpen bool) {
	if !runtimeID.Valid {
		return
	}
	rt, err := s.Cerebro.GetRuntimeOwnerForInbox(ctx, runtimeID)
	if err != nil {
		slog.Warn("auto-pause card: runtime lookup failed",
			"runtime_id", util.UUIDToString(runtimeID), "error", err)
		return
	}
	if !rt.OwnerID.Valid {
		// No owner to address — nothing to surface. The issue-level comment
		// (notifyAutoPause) still covers the on-issue audience.
		return
	}

	day := time.Now().UTC().Format("2006-01-02")
	runtimeIDStr := util.UUIDToString(runtimeID)
	severity := runtimePauseSeverity(circuitOpen)
	title := runtimePauseCardTitle(rt.Name)
	body := runtimePauseCardBody(circuitOpen, unpauseAt)
	resetsAt := ""
	if !circuitOpen {
		resetsAt = unpauseAt.UTC().Format(time.RFC3339)
	}

	existing, err := s.Cerebro.FindRuntimePauseInboxCard(ctx, cerebrodb.FindRuntimePauseInboxCardParams{
		WorkspaceID: rt.WorkspaceID,
		RecipientID: rt.OwnerID,
		Column3:     runtimeIDStr,
		Column4:     day,
	})
	if err == nil {
		bumped, bumpErr := s.Cerebro.BumpRuntimePauseInboxCard(ctx, cerebrodb.BumpRuntimePauseInboxCardParams{
			ID:       existing.ID,
			Severity: severity,
			Title:    title,
			Body:     pgtype.Text{String: body, Valid: true},
			Column5:  resetsAt,
			Column6:  fmt.Sprintf("%t", circuitOpen),
		})
		if bumpErr != nil {
			slog.Warn("auto-pause card: bump failed",
				"runtime_id", runtimeIDStr, "error", bumpErr)
			return
		}
		s.publishInboxNew(bumped)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("auto-pause card: dedup lookup failed",
			"runtime_id", runtimeIDStr, "error", err)
		return
	}

	details, _ := json.Marshal(map[string]string{
		"runtime_id":   runtimeIDStr,
		"pause_date":   day,
		"failed_runs":  fmt.Sprintf("%d", count),
		"resets_at":    resetsAt,
		"reason":       "rate_limit",
		"circuit_open": fmt.Sprintf("%t", circuitOpen),
	})
	created, err := s.Cerebro.CreateRuntimePauseInboxCard(ctx, cerebrodb.CreateRuntimePauseInboxCardParams{
		WorkspaceID: rt.WorkspaceID,
		RecipientID: rt.OwnerID,
		Severity:    severity,
		Title:       title,
		Body:        pgtype.Text{String: body, Valid: true},
		Details:     details,
	})
	if err != nil {
		slog.Warn("auto-pause card: create failed",
			"runtime_id", runtimeIDStr, "error", err)
		return
	}
	s.publishInboxNew(created)
}

func runtimePauseSeverity(circuitOpen bool) string {
	if circuitOpen {
		return "action_required"
	}
	return "attention"
}

func runtimePauseCardTitle(name string) string {
	if name == "" {
		name = "Runtime"
	}
	return fmt.Sprintf("%s — login eller kvote udløbet", name)
}

// runtimePauseCardBody is the human one-liner under the title. When the circuit
// breaker is open the runtime needs manual intervention; otherwise it states
// the automatic resume time.
func runtimePauseCardBody(circuitOpen bool, unpauseAt time.Time) string {
	if circuitOpen {
		return "Auto-genoptagelse er stoppet — runtimen kræver manuel handling (skift konto/nøgle eller genoptag manuelt)."
	}
	return fmt.Sprintf("Genoptager automatisk omkring kl. %s.", unpauseAt.UTC().Format("15:04 02-01-2006 MST"))
}

// publishInboxNew fans the new/updated card out on the bus, scoped to the owner
// so only their inbox refetches. Mirrors privateagentrun.Service.publishInboxNew.
func (s *Service) publishInboxNew(item cerebrodb.InboxItem) {
	if s.Bus == nil {
		return
	}
	ownerID := util.UUIDToString(item.RecipientID)
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   ownerID,
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
		AudienceUserIDs: []string{ownerID},
	})
}
