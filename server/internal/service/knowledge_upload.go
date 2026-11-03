package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultKnowledgeUploadMaxBytes is WeKnora's own default file limit
// (MAX_FILE_SIZE_MB, 50 MB). A larger file would be refused by WeKnora after
// the upload had crossed the network, so Multica refuses it first.
const DefaultKnowledgeUploadMaxBytes = 50 << 20

const knowledgeMaxFileName = 255

// knowledgeKindPattern is the shape of a kind key. The taxonomy (knowledge_kind)
// decides which keys exist; this only rejects malformed ones.
var knowledgeKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// KnowledgeTooLargeError is an upload over the limit. Max is the limit in bytes.
type KnowledgeTooLargeError struct{ Max int64 }

func (e *KnowledgeTooLargeError) Error() string {
	return fmt.Sprintf("a file cannot exceed %d MB", e.Max>>20)
}

// KnowledgeUploadInput is a file a person uploads. Size is the declared size
// from the request; the limit is checked against it before any byte is sent.
type KnowledgeUploadInput struct {
	KB      string
	Kind    string
	Name    string
	Size    int64
	Content io.Reader
	// IdempotencyKey makes a retried upload return the first one. Empty means
	// every call uploads.
	IdempotencyKey string
}

// UploadLimit is the largest file the service accepts.
func (s *KnowledgeService) UploadLimit() int64 {
	if s == nil || s.UploadMaxBytes <= 0 {
		return DefaultKnowledgeUploadMaxBytes
	}
	return s.UploadMaxBytes
}

// UploadDocument stores a file in a knowledge base and indexes it at once. A
// person's upload is published at once because the person is the curator (spec
// 3.3). Agents cannot upload: they write drafts through CreateDocument. The bool
// reports whether this call stored the file; false means the idempotency key had
// already been used and the existing document is returned.
func (s *KnowledgeService) UploadDocument(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, in KnowledgeUploadInput) (KnowledgeDocumentView, bool, error) {
	if actor.Type != KnowledgeActorMember {
		return KnowledgeDocumentView{}, false, ErrKnowledgeForbidden
	}
	in.KB = strings.TrimSpace(in.KB)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Name = strings.TrimSpace(path.Base(strings.ReplaceAll(in.Name, "\\", "/")))
	switch {
	case in.KB == "":
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"kb is required"}
	case in.Kind == "":
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"kind is required"}
	case !knowledgeKindPattern.MatchString(in.Kind):
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"kind must be lowercase letters, digits and underscores"}
	case in.Name == "" || in.Name == "." || in.Name == "/":
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"a file name is required"}
	case len(in.Name) > knowledgeMaxFileName:
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{fmt.Sprintf("file name is longer than %d characters", knowledgeMaxFileName)}
	case in.Content == nil:
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"a file is required"}
	}
	if in.Size > s.UploadLimit() {
		return KnowledgeDocumentView{}, false, &KnowledgeTooLargeError{Max: s.UploadLimit()}
	}
	kind, err := s.loadKnowledgeKind(ctx, workspaceID, in.Kind)
	if errors.Is(err, ErrKnowledgeKindNotFound) {
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"unknown kind " + in.Kind + "; see multica knowledge kinds"}
	}
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	content := in.Content
	// The headings of a file with a reader are read here, before anything is
	// stored. Other formats are parsed by WeKnora only after they are stored, so
	// their headings are not checked.
	if readHeadings, ok := uploadHeadingReader(in.Name); ok && len(kind.RequiredSections) > 0 {
		body, err := io.ReadAll(in.Content)
		if err != nil {
			return KnowledgeDocumentView{}, false, fmt.Errorf("read upload: %w", err)
		}
		headings, err := readHeadings(body)
		if err != nil {
			return KnowledgeDocumentView{}, false, err
		}
		if missing := MissingSections(kind.RequiredSections, headings); len(missing) > 0 {
			return KnowledgeDocumentView{}, false, &KnowledgeMissingSectionsError{Kind: in.Kind, Missing: missing}
		}
		content = bytes.NewReader(body)
	}

	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	base, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: in.KB})
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeDocumentView{}, false, ErrKnowledgeBaseNotFound
	}
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("load knowledge base: %w", err)
	}
	grants, err := s.actorKnowledgeGrants(ctx, workspaceID, actor)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	if !knowledgeCanWrite(actor, base.Visibility, util.UUIDToString(base.ID), grants) {
		return KnowledgeDocumentView{}, false, ErrKnowledgeForbidden
	}

	// The same tags a document carries from any other write path (see
	// documentTagNames): the kind.
	tags := documentTagNames(KnowledgeDocumentInput{Kind: in.Kind})
	tagIDs, err := s.weknoraTagIDs(ctx, ws, base.WeknoraKbID, tags)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}

	key := in.IdempotencyKey
	if key == "" {
		key = "auto:" + uuid.NewString()
	}
	claimed, err := s.Queries.ClaimKnowledgeLink(ctx, db.ClaimKnowledgeLinkParams{
		WorkspaceID: workspaceID, KnowledgeBaseID: base.ID,
		SourceType: "upload", IdempotencyKey: key,
		CreatedByType: actor.Type, CreatedByID: actor.ID,
		Title: in.Name, PublishRequested: true, Kind: in.Kind, Tags: tags,
	})
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("record upload: %w", err)
	}
	link, err := s.Queries.GetKnowledgeLinkByKey(ctx, db.GetKnowledgeLinkByKeyParams{WorkspaceID: workspaceID, IdempotencyKey: key})
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("load upload: %w", err)
	}
	if claimed == 0 && link.State != KnowledgeLinkFailed {
		view, err := s.linkView(ctx, workspaceID, link)
		return view, false, err
	}

	doc, err := ws.CreateFile(ctx, base.WeknoraKbID, knowledge.FileDocument{Name: in.Name, Content: content, TagIDs: tagIDs})
	var dup *knowledge.DuplicateError
	if errors.As(err, &dup) {
		// WeKnora already holds this file in the base; adopt it.
		doc, err = dup.Existing, nil
	}
	if err != nil {
		// Nothing was stored, and the person retries their own upload, so the
		// claim is dropped rather than kept as a failed record.
		if _, delErr := s.Queries.DeleteKnowledgeLink(ctx, db.DeleteKnowledgeLinkParams{ID: link.ID, WorkspaceID: workspaceID}); delErr != nil {
			return KnowledgeDocumentView{}, false, fmt.Errorf("drop failed upload record: %w", delErr)
		}
		return KnowledgeDocumentView{}, false, err
	}
	settled, err := s.settle(ctx, link, doc.ID, KnowledgeLinkPublished, "")
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("record upload: %w", err)
	}
	view, err := s.linkView(ctx, workspaceID, settled)
	return view, true, err
}
