package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/textpatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrDocumentConflict = errors.New("document revision conflict: base revision is stale")
	ErrInvalidPath      = errors.New("invalid document path: must be a slug-like path ending in .md (e.g., 'folder/doc.md')")
)

var pathRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9/_\-]*\.md$`)

func isValidPath(path string) bool {
	return pathRegex.MatchString(path)
}

// IsValidDocumentPath is the exported boundary check for handler callers
// that need to validate a path before reaching the service layer (e.g.
// rename, which doesn't currently route through Put).
func IsValidDocumentPath(path string) bool {
	return isValidPath(path)
}

// DocumentPayload holds the mutable fields for a document upsert.
type DocumentPayload struct {
	Title            *string
	Description      *string
	Content          string
	Tags             []string
	ForceNewRevision bool
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

const collapseRevisionWindow = 5 * time.Minute

// Put upserts a document at the given path. If baseRevisionID is non-nil
// and doesn't match the document's current revision, returns ErrDocumentConflict.
// Every mutation creates or updates an append-only revision record.
func (s *DocumentService) Put(
	ctx context.Context,
	workspaceID pgtype.UUID,
	path string,
	payload DocumentPayload,
	provenance DocumentProvenance,
	baseRevisionID *pgtype.UUID,
	changeSummary string,
) (*db.WorkspaceDocument, error) {
	if !isValidPath(path) {
		return nil, ErrInvalidPath
	}

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

	title := pgtype.Text{}
	if payload.Title != nil {
		title = pgtype.Text{String: *payload.Title, Valid: true}
	}
	desc := pgtype.Text{}
	if payload.Description != nil {
		desc = pgtype.Text{String: *payload.Description, Valid: true}
	}
	tags := payload.Tags
	if tags == nil {
		tags = []string{}
	}

	if isNew {
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

	// Existing document — lock the row before the conflict check, so concurrent
	// edits serialize. Without FOR UPDATE the optimistic check at the
	// next line can pass for two writers reading the same base_revision_id;
	// they then both compute next revision_number from a stale MAX and
	// collide on the UNIQUE constraint.
	locked, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, existing.ID)
	if err != nil {
		return nil, fmt.Errorf("lock document: %w", err)
	}
	existing = locked

	// Conflict check uses the locked row.
	if baseRevisionID != nil && baseRevisionID.Valid && existing.CurrentRevisionID.Valid {
		if *baseRevisionID != existing.CurrentRevisionID {
			return nil, ErrDocumentConflict
		}
	}

	// Determine if we should collapse into the last revision.
	canCollapse := false
	if !payload.ForceNewRevision && existing.CurrentRevisionID.Valid {
		revs, err := qtx.ListWorkspaceDocumentRevisions(ctx, existing.ID)
		if err == nil && len(revs) > 0 {
			lastRev := revs[0]
			sameAuthor := lastRev.AuthorType == provenance.AuthorType &&
				lastRev.AuthorID == provenanceAuthorToNullableUUID(provenance.AuthorID)
			inWindow := time.Since(lastRev.CreatedAt.Time) < collapseRevisionWindow
			isEdit := lastRev.Operation == "edit" || lastRev.Operation == "create"

			if sameAuthor && inWindow && isEdit {
				canCollapse = true
			}
		}
	}

	if canCollapse {
		// Update the existing revision instead of creating a new one.
		err = qtx.UpdateWorkspaceDocumentRevisionContent(ctx, db.UpdateWorkspaceDocumentRevisionContentParams{
			ID:            existing.CurrentRevisionID,
			Content:       payload.Content,
			Title:         title,
			Description:   desc,
			Tags:          tags,
			ChangeSummary: util.StrToText(changeSummary),
		})
		if err != nil {
			return nil, fmt.Errorf("collapse revision: %w", err)
		}

		err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
			ID:                existing.ID,
			Content:           payload.Content,
			Title:             title,
			Description:       desc,
			Tags:              tags,
			CurrentRevisionID: existing.CurrentRevisionID,
		})
		if err != nil {
			return nil, fmt.Errorf("update document: %w", err)
		}
	} else {
		// Create a NEW revision.
		maxRev, err := qtx.GetMaxRevisionNumber(ctx, existing.ID)
		if err != nil {
			return nil, fmt.Errorf("get max revision: %w", err)
		}

		rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
			DocumentID:     existing.ID,
			RevisionNumber: int32(maxRev + 1),
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
		existing.CurrentRevisionID = rev.ID
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	existing.Content = payload.Content
	existing.Title = title
	existing.Description = desc
	existing.Tags = tags
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

	// Row-lock the document up-front so concurrent mutations serialize and
	// the GetMaxRevisionNumber + InsertRevision pair can't collide on the
	// UNIQUE(document_id, revision_number) constraint.
	doc, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, documentID)
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

	// Row-lock the document up-front so concurrent mutations serialize and
	// the GetMaxRevisionNumber + InsertRevision pair can't collide on the
	// UNIQUE(document_id, revision_number) constraint.
	doc, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, documentID)
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

// UpdateTags adds and removes tags atomically, creating a new revision with operation='tag'.
func (s *DocumentService) UpdateTags(
	ctx context.Context,
	documentID pgtype.UUID,
	addTags, removeTags []string,
	provenance DocumentProvenance,
) (*db.WorkspaceDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Row-lock the document up-front so concurrent mutations serialize and
	// the GetMaxRevisionNumber + InsertRevision pair can't collide on the
	// UNIQUE(document_id, revision_number) constraint.
	doc, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	// Build new tag set: start with existing, add new, remove unwanted.
	tagSet := make(map[string]struct{}, len(doc.Tags))
	for _, t := range doc.Tags {
		tagSet[t] = struct{}{}
	}
	for _, t := range addTags {
		tagSet[t] = struct{}{}
	}
	for _, t := range removeTags {
		delete(tagSet, t)
	}
	newTags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		newTags = append(newTags, t)
	}
	// Sort for deterministic output.
	sortStrings(newTags)

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
		Content:        doc.Content,
		Tags:           newTags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "tag",
		ChangeSummary:  util.StrToText("Updated tags"),
	})
	if err != nil {
		return nil, fmt.Errorf("insert revision: %w", err)
	}

	err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
		ID:                doc.ID,
		Content:           doc.Content,
		Title:             doc.Title,
		Description:       doc.Description,
		Tags:              newTags,
		CurrentRevisionID: rev.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	doc.Tags = newTags
	doc.CurrentRevisionID = rev.ID
	return &doc, nil
}

// SetPinned pins or unpins a document, creating a new revision with operation='pin'.
func (s *DocumentService) SetPinned(
	ctx context.Context,
	documentID pgtype.UUID,
	pinned bool,
	provenance DocumentProvenance,
) (*db.WorkspaceDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Row-lock the document up-front so concurrent mutations serialize and
	// the GetMaxRevisionNumber + InsertRevision pair can't collide on the
	// UNIQUE(document_id, revision_number) constraint.
	doc, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	err = qtx.SetWorkspaceDocumentPinned(ctx, db.SetWorkspaceDocumentPinnedParams{
		ID:     doc.ID,
		Pinned: pinned,
	})
	if err != nil {
		return nil, fmt.Errorf("set pinned: %w", err)
	}

	maxRev, err := qtx.GetMaxRevisionNumber(ctx, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("get max revision: %w", err)
	}

	summary := "Pinned"
	if !pinned {
		summary = "Unpinned"
	}

	rev, err := qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
		DocumentID:     doc.ID,
		RevisionNumber: int32(maxRev + 1),
		ParentRevision: doc.CurrentRevisionID,
		Title:          doc.Title,
		Description:    doc.Description,
		Content:        doc.Content,
		Tags:           doc.Tags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "pin",
		ChangeSummary:  util.StrToText(summary),
	})
	if err != nil {
		return nil, fmt.Errorf("insert revision: %w", err)
	}

	// Update current_revision_id to point to the new revision.
	err = qtx.UpdateWorkspaceDocumentContent(ctx, db.UpdateWorkspaceDocumentContentParams{
		ID:                doc.ID,
		Content:           doc.Content,
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

	doc.Pinned = pinned
	doc.CurrentRevisionID = rev.ID
	return &doc, nil
}

// Archive soft-deletes a document, creating a new revision with operation='archive'.
func (s *DocumentService) Archive(
	ctx context.Context,
	documentID pgtype.UUID,
	provenance DocumentProvenance,
) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Row-lock the document up-front so concurrent mutations serialize and
	// the GetMaxRevisionNumber + InsertRevision pair can't collide on the
	// UNIQUE(document_id, revision_number) constraint.
	doc, err := qtx.GetWorkspaceDocumentByIDForUpdate(ctx, documentID)
	if err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	maxRev, err := qtx.GetMaxRevisionNumber(ctx, doc.ID)
	if err != nil {
		return fmt.Errorf("get max revision: %w", err)
	}

	_, err = qtx.InsertWorkspaceDocumentRevision(ctx, db.InsertWorkspaceDocumentRevisionParams{
		DocumentID:     doc.ID,
		RevisionNumber: int32(maxRev + 1),
		ParentRevision: doc.CurrentRevisionID,
		Title:          doc.Title,
		Description:    doc.Description,
		Content:        doc.Content,
		Tags:           doc.Tags,
		AuthorType:     provenance.AuthorType,
		AuthorID:       provenanceAuthorToNullableUUID(provenance.AuthorID),
		TaskID:         provenanceTaskToNullableUUID(provenance.TaskID),
		Operation:      "archive",
		ChangeSummary:  util.StrToText("Archived"),
	})
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}

	if err := qtx.ArchiveWorkspaceDocument(ctx, doc.ID); err != nil {
		return fmt.Errorf("archive document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	sort.Strings(s)
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
