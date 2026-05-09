package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/textpatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrDocumentConflict = errors.New("document revision conflict: base revision is stale")
)

// DocumentPayload holds the mutable fields for a document upsert.
type DocumentPayload struct {
	Title         *string
	Description   *string
	Content       string
	Tags          []string
}

// DocumentProvenance captures who is making the change.
type DocumentProvenance struct {
	AuthorType string        // human, agent_foreground, agent_background, import
	AuthorID   *pgtype.UUID
	TaskID     *pgtype.UUID
}

// DocumentService handles document CRUD with transactional revision tracking.
type DocumentService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// Put upserts a document at the given path. If baseRevisionID is non-nil
// and doesn't match the document's current revision, returns ErrDocumentConflict.
// Every mutation creates an append-only revision record.
func (s *DocumentService) Put(
	ctx context.Context,
	workspaceID pgtype.UUID,
	path string,
	payload DocumentPayload,
	provenance DocumentProvenance,
	baseRevisionID *pgtype.UUID,
	changeSummary string,
) (*db.WorkspaceDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Try to find existing document by path.
	existing, err := qtx.GetWorkspaceDocumentByPath(ctx, db.GetWorkspaceDocumentByPathParams{
		WorkspaceID: workspaceID,
		Path:        path,
	})

	isNew := err != nil // pgx returns ErrNoRows for not found

	if isNew {
		// Create new document.
		tags := payload.Tags
		if tags == nil {
			tags = []string{}
		}
		title := pgtype.Text{}
		if payload.Title != nil {
			title = pgtype.Text{String: *payload.Title, Valid: true}
		}
		desc := pgtype.Text{}
		if payload.Description != nil {
			desc = pgtype.Text{String: *payload.Description, Valid: true}
		}

		doc, err := qtx.CreateWorkspaceDocument(ctx, db.CreateWorkspaceDocumentParams{
			WorkspaceID: workspaceID,
			Path:        path,
			Title:       title,
			Description: desc,
			Content:     payload.Content,
			Tags:        tags,
			Pinned:      false,
			CreatedBy:   provenanceAuthorToNullableUUID(provenance.AuthorID),
		})
		if err != nil {
			return nil, fmt.Errorf("create document: %w", err)
		}

		// Insert revision 1.
		rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
			DocumentID:     doc.ID,
			RevisionNumber: 1,
			ParentRevision: pgtype.UUID{},
			Title:          title,
			Description:    desc,
			Content:        payload.Content,
			Tags:           tags,
			AuthorType:     provenance.AuthorType,
			AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
			TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
			Operation:      "create",
			ChangeSummary:  util.StrToText(changeSummary),
		})
		if err != nil {
			return nil, fmt.Errorf("insert revision: %w", err)
		}

		// Update current_revision_id on the document.
		err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
			ID:                doc.ID,
			Content:           payload.Content,
			Title:             title,
			Description:       desc,
			Tags:              tags,
			CurrentRevisionID: rev.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("update current revision: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}

		doc.CurrentRevisionID = rev.ID
		return &doc, nil
	}

	// Existing document — check for conflict.
	if baseRevisionID != nil && baseRevisionID.Valid && existing.CurrentRevisionID.Valid {
		if *baseRevisionID != existing.CurrentRevisionID {
			return nil, ErrDocumentConflict
		}
	}

	// Get next revision number.
	maxRev, err := qtx.GetMaxRevisionNumber(ctx, existing.ID)
	if err != nil {
		return nil, fmt.Errorf("get max revision: %w", err)
	}
	nextRev := maxRev + 1

	title := existing.Title
	if payload.Title != nil {
		title = pgtype.Text{String: *payload.Title, Valid: true}
	}
	desc := existing.Description
	if payload.Description != nil {
		desc = pgtype.Text{String: *payload.Description, Valid: true}
	}
	tags := payload.Tags
	if tags == nil {
		tags = existing.Tags
	}

	rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
		DocumentID:     existing.ID,
		RevisionNumber: int32(nextRev),
		ParentRevision: existing.CurrentRevisionID,
		Title:          title,
		Description:    desc,
		Content:        payload.Content,
		Tags:           tags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "edit",
		ChangeSummary:  util.StrToText(changeSummary),
	})
	if err != nil {
		return nil, fmt.Errorf("insert revision: %w", err)
	}

	err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
		ID:                existing.ID,
		Content:           payload.Content,
		Title:             title,
		Description:       desc,
		Tags:              tags,
		CurrentRevisionID: rev.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	existing.Content = payload.Content
	existing.Title = title
	existing.Description = desc
	existing.Tags = tags
	existing.CurrentRevisionID = rev.ID
	return &existing, nil
}

// Get retrieves a document by ID.
func (s *DocumentService) Get(ctx context.Context, id pgtype.UUID) (*db.WorkspaceDocument, error) {
	doc, err := s.Queries.GetWorkspaceDocumentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// GetByPath retrieves a non-archived document by workspace + path.
func (s *DocumentService) GetByPath(ctx context.Context, workspaceID pgtype.UUID, path string) (*db.WorkspaceDocument, error) {
	doc, err := s.Queries.GetWorkspaceDocumentByPath(ctx, db.GetWorkspaceDocumentByPathParams{
		WorkspaceID: workspaceID,
		Path:        path,
	})
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Patch applies a fuzzy find-and-replace on the document's current content
// and persists the result as a new revision.
func (s *DocumentService) Patch(
	ctx context.Context,
	documentID pgtype.UUID,
	findText, replaceText string,
	provenance DocumentProvenance,
	changeSummary string,
) (*db.WorkspaceDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	doc, err := qtx.GetWorkspaceDocumentByID(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	result, err := textpatch.FuzzyReplace(doc.Content, findText, replaceText)
	if err != nil {
		return nil, err
	}

	maxRev, err := qtx.GetMaxRevisionNumber(ctx, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("get max revision: %w", err)
	}

	rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
		DocumentID:     doc.ID,
		RevisionNumber: int32(maxRev + 1),
		ParentRevision: doc.CurrentRevisionID,
		Title:          doc.Title,
		Description:    doc.Description,
		Content:        result.Content,
		Tags:           doc.Tags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "edit",
		ChangeSummary:  util.StrToText(changeSummary),
	})
	if err != nil {
		return nil, fmt.Errorf("insert revision: %w", err)
	}

	err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
		ID:                doc.ID,
		Content:           result.Content,
		Title:             doc.Title,
		Description:       doc.Description,
		Tags:              doc.Tags,
		CurrentRevisionID: rev.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	doc.Content = result.Content
	doc.CurrentRevisionID = rev.ID
	return &doc, nil
}

// Restore creates a new revision whose content equals the specified
// revision's content. Does not destroy intermediate revisions.
func (s *DocumentService) Restore(
	ctx context.Context,
	documentID pgtype.UUID,
	revisionNumber int,
	provenance DocumentProvenance,
) (*db.WorkspaceDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	doc, err := qtx.GetWorkspaceDocumentByID(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	oldRev, err := qtx.GetWorkspaceDocumentRevision(ctx, db.GetWorkspaceDocumentRevisionParams{
		DocumentID:     documentID,
		RevisionNumber: int32(revisionNumber),
	})
	if err != nil {
		return nil, fmt.Errorf("get revision %d: %w", revisionNumber, err)
	}

	maxRev, err := qtx.GetMaxRevisionNumber(ctx, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("get max revision: %w", err)
	}

	summary := fmt.Sprintf("Restored from revision %d", revisionNumber)
	rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
		DocumentID:     doc.ID,
		RevisionNumber: int32(maxRev + 1),
		ParentRevision: doc.CurrentRevisionID,
		Title:          oldRev.Title,
		Description:    oldRev.Description,
		Content:        oldRev.Content,
		Tags:           oldRev.Tags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "restore",
		ChangeSummary:  util.StrToText(summary),
	})
	if err != nil {
		return nil, fmt.Errorf("insert revision: %w", err)
	}

	err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
		ID:                doc.ID,
		Content:           oldRev.Content,
		Title:             oldRev.Title,
		Description:       oldRev.Description,
		Tags:              oldRev.Tags,
		CurrentRevisionID: rev.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	doc.Content = oldRev.Content
	doc.Title = oldRev.Title
	doc.Description = oldRev.Description
	doc.Tags = oldRev.Tags
	doc.CurrentRevisionID = rev.ID
	return &doc, nil
}

// provenanceAuthorToNullableUUID converts a *pgtype.UUID to a pgtype.UUID,
// returning an invalid UUID if the pointer is nil.
func provenanceAuthorToNullableUUID(u *pgtype.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return *u
}

// provenanceTaskToNullableUUID converts a *pgtype.UUID to a pgtype.UUID.
func provenanceTaskToNullableUUID(u *pgtype.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return *u
}
