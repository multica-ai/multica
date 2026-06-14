package notetypes

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// runningDocSeparator divides entries in a running document (newest on top).
const runningDocSeparator = "\n\n---\n\n"

// Apply materialises a single note type for the period that `now` falls in.
// It is the shared core used by both the daily Sweeper and the manual RunNow
// endpoint, and should be called inside a transaction (pass q = queries.WithTx).
//
// Returns created=true when a note was created or a running doc was appended,
// created=false when the period was already materialised (idempotent no-op).
func Apply(ctx context.Context, q *cerebrodb.Queries, nt cerebrodb.CerebroNoteType, now time.Time) (bool, pgtype.UUID, error) {
	key := PeriodKey(now, nt.CadenceUnit)
	rendered := RenderTemplate(nt.TemplateBody, now, nt.CadenceUnit)

	if nt.RecurrenceMode == ModeNewNote {
		pk := pgtype.Text{String: key, Valid: true}
		// Idempotency: skip when this period already produced a note.
		if existing, err := q.FindArtifactByNoteTypePeriod(ctx, cerebrodb.FindArtifactByNoteTypePeriodParams{
			NoteTypeID: nt.ID,
			PeriodKey:  pk,
		}); err == nil && existing.Valid {
			return false, existing, nil
		}
		title := nt.Name + " – " + PeriodLabel(now, nt.CadenceUnit)
		art, err := q.CreateNoteTypeArtifact(ctx, cerebrodb.CreateNoteTypeArtifactParams{
			WorkspaceID: nt.WorkspaceID,
			Title:       title,
			Body:        rendered,
			AuthorID:    nt.CreatedBy,
			NoteTypeID:  nt.ID,
			FolderID:    nt.TargetFolderID,
			PeriodKey:   pk,
		})
		if err != nil {
			return false, pgtype.UUID{}, err
		}
		if err := q.SetCerebroNoteTypeLastPeriod(ctx, cerebrodb.SetCerebroNoteTypeLastPeriodParams{
			ID:            nt.ID,
			LastPeriodKey: key,
		}); err != nil {
			return false, pgtype.UUID{}, err
		}
		return true, art.ID, nil
	}

	// running_doc: first run creates the single rolling document.
	if !nt.RunningDocArtifactID.Valid {
		art, err := q.CreateNoteTypeArtifact(ctx, cerebrodb.CreateNoteTypeArtifactParams{
			WorkspaceID: nt.WorkspaceID,
			Title:       nt.Name,
			Body:        rendered,
			AuthorID:    nt.CreatedBy,
			NoteTypeID:  nt.ID,
			FolderID:    nt.TargetFolderID,
			PeriodKey:   pgtype.Text{}, // NULL: one row reused across periods.
		})
		if err != nil {
			return false, pgtype.UUID{}, err
		}
		if err := q.SetCerebroNoteTypeRunningDoc(ctx, cerebrodb.SetCerebroNoteTypeRunningDocParams{
			ID:                   nt.ID,
			RunningDocArtifactID: art.ID,
			LastPeriodKey:        key,
		}); err != nil {
			return false, pgtype.UUID{}, err
		}
		return true, art.ID, nil
	}

	// Subsequent runs prepend the freshly rendered section above the history.
	row, err := q.GetNoteTypeArtifactBody(ctx, nt.RunningDocArtifactID)
	if err != nil {
		return false, pgtype.UUID{}, err
	}
	newBody := rendered + runningDocSeparator + row.Body
	if err := q.UpdateNoteTypeArtifactBody(ctx, cerebrodb.UpdateNoteTypeArtifactBodyParams{
		ID:   nt.RunningDocArtifactID,
		Body: newBody,
	}); err != nil {
		return false, pgtype.UUID{}, err
	}
	if err := q.SetCerebroNoteTypeRunningDoc(ctx, cerebrodb.SetCerebroNoteTypeRunningDocParams{
		ID:                   nt.ID,
		RunningDocArtifactID: nt.RunningDocArtifactID,
		LastPeriodKey:        key,
	}); err != nil {
		return false, pgtype.UUID{}, err
	}
	return true, nt.RunningDocArtifactID, nil
}
