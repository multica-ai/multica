package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReplaceVersion creates a new immutable source version. It never mutates an
// existing version in place, which keeps citations stable while parsing or
// embedding work is running.
func (s *Service) ReplaceVersion(ctx context.Context, workspaceID, actorID, baseID, documentID string, input ReplaceVersionInput) (CreateDocumentResult, error) {
	if err := s.checkEnabled(); err != nil {
		return CreateDocumentResult{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return CreateDocumentResult{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return CreateDocumentResult{}, err
	}
	if input.ExpectedRevision <= 0 {
		return CreateDocumentResult{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return CreateDocumentResult{}, err
	}
	var sourceKind, sourceURL, oldTitle string
	var documentRevision int64
	if err := s.db.QueryRow(ctx, `SELECT source_kind, COALESCE(source_url,''), title, revision FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND deleted_at IS NULL`, doc, params.base).Scan(&sourceKind, &sourceURL, &oldTitle, &documentRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateDocumentResult{}, notFound()
		}
		return CreateDocumentResult{}, internal("failed to load document for replacement", err)
	}
	if documentRevision != input.ExpectedRevision {
		return CreateDocumentResult{}, conflict("revision_conflict", "the document changed; refresh before replacing its source")
	}
	versionID, versionIDString := newID()
	filename := filepath.Base(strings.TrimSpace(input.Filename))
	if filename == "." || filename == "" {
		filename = oldTitle
	}
	mimeType := strings.TrimSpace(input.ContentType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	stage := "parse"
	var sourceObjectKey, sourceHash string
	var payload []byte
	if sourceKind == SourceKindURL {
		if strings.TrimSpace(input.URL) != "" {
			sourceURL, err = NormalizeURL(input.URL)
			if err != nil {
				return CreateDocumentResult{}, badRequest("invalid_url", "only absolute http or https URLs are supported")
			}
		}
		stage = "fetch"
	} else {
		if len(input.Bytes) == 0 {
			return CreateDocumentResult{}, badRequest("empty_file", "file is empty")
		}
		if int64(len(input.Bytes)) > s.maxUpload {
			return CreateDocumentResult{}, badRequest("file_too_large", "file exceeds the knowledge upload limit")
		}
		sourceHash = HashBytes(input.Bytes)
		sourceObjectKey = fmt.Sprintf("knowledge/%s/%s/%s/%s/%s", workspaceID, baseID, documentID, versionIDString, sourceHash)
		payload = input.Bytes
	}
	if sourceObjectKey != "" {
		if s.store == nil {
			return CreateDocumentResult{}, unavailable()
		}
		if _, err := s.store.Upload(ctx, sourceObjectKey, payload, mimeType, filename); err != nil {
			return CreateDocumentResult{}, internal("failed to store replacement source", err)
		}
	}
	cleanup := func() {
		_ = s.deleteObjectIfUnreferenced(ctx, sourceObjectKey)
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to start replacement transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		cleanup()
		return CreateDocumentResult{}, err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		cleanup()
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateDocumentResult{}, notFound()
		}
		return CreateDocumentResult{}, internal("failed to lock knowledge base for replacement", err)
	}
	var lockedRevision int64
	if err := tx.QueryRow(ctx, `SELECT id,revision FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND deleted_at IS NULL FOR UPDATE`, doc, params.base).Scan(&doc, &lockedRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cleanup()
			return CreateDocumentResult{}, notFound()
		}
		cleanup()
		return CreateDocumentResult{}, internal("failed to lock document for replacement", err)
	}
	if lockedRevision != input.ExpectedRevision {
		cleanup()
		return CreateDocumentResult{}, conflict("revision_conflict", "the document changed; refresh before replacing its source")
	}
	var next int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM knowledge_document_version WHERE document_id=$1`, doc).Scan(&next); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to allocate replacement version", err)
	}
	metadataValue := cloneStringAnyMap(input.Metadata)
	metadataValue["filename"] = filename
	metadataValue["content_type"] = mimeType
	metadata, err := jsonBytes(metadataValue)
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to encode replacement metadata", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knowledge_document_version(id,workspace_id,knowledge_base_id,document_id,version_number,source_object_key,source_hash,byte_size,mime_type,source_metadata,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'processing')`, versionID, params.workspace, params.base, doc, next, sourceObjectKey, sourceHash, len(payload), mimeType, metadata); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to create replacement version", err)
	}
	job, err := s.enqueueJob(ctx, tx, params.workspace, params.base, &versionID, nil, stage, jobKey(stage, versionIDString, "", sourceHash), map[string]any{"filename": filename, "source_url": sourceURL})
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to enqueue replacement job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to commit replacement version", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	version, err := s.getVersionByID(ctx, params.workspace, params.base, doc, versionID)
	if err != nil {
		return CreateDocumentResult{}, internal("failed to load replacement version", err)
	}
	document, err := s.GetDocument(ctx, workspaceID, actorID, baseID, documentID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	return CreateDocumentResult{Document: document, Version: version, Job: &job}, nil
}

type ConfirmBlocksInput struct {
	VersionID        string          `json:"version_id"`
	ExpectedRevision int64           `json:"expected_revision"`
	Blocks           []DocumentBlock `json:"blocks"`
}

// ConfirmBlocks records a user-confirmed parse as a new version. Blocks are
// copied into a private parsed object and then use the same chunk/index path as
// every other version; the old version remains immutable and citable.
func (s *Service) ConfirmBlocks(ctx context.Context, workspaceID, actorID, baseID, documentID string, input ConfirmBlocksInput) (CreateDocumentResult, error) {
	if err := s.checkEnabled(); err != nil {
		return CreateDocumentResult{}, err
	}
	if len(input.Blocks) == 0 {
		return CreateDocumentResult{}, badRequest("blocks_required", "at least one confirmed block is required")
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return CreateDocumentResult{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return CreateDocumentResult{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return CreateDocumentResult{}, err
	}
	if input.ExpectedRevision <= 0 {
		return CreateDocumentResult{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	var oldVersionID string
	var oldTitle string
	var oldRevision int64
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(current_version_id::text,''),title,revision FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND deleted_at IS NULL`, doc, params.base).Scan(&oldVersionID, &oldTitle, &oldRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateDocumentResult{}, notFound()
		}
		return CreateDocumentResult{}, internal("failed to load document for block confirmation", err)
	}
	if input.VersionID != "" && input.VersionID != oldVersionID {
		return CreateDocumentResult{}, conflict("revision_conflict", "the document version changed; refresh before confirming blocks")
	}
	if oldRevision != input.ExpectedRevision {
		return CreateDocumentResult{}, conflict("revision_conflict", "the document changed; refresh before confirming blocks")
	}
	if oldVersionID == "" {
		return CreateDocumentResult{}, conflict("version_not_ready", "the document has no current parsed version to confirm")
	}
	var parsedKeyValue pgtype.Text
	if err := s.db.QueryRow(ctx, `SELECT parsed_object_key FROM knowledge_document_version WHERE id=$1 AND document_id=$2 AND knowledge_base_id=$3`, mustID(oldVersionID), doc, params.base).Scan(&parsedKeyValue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateDocumentResult{}, conflict("version_not_ready", "the current document version has no parsed blocks")
		}
		return CreateDocumentResult{}, internal("failed to load parsed blocks for confirmation", err)
	}
	if !parsedKeyValue.Valid || strings.TrimSpace(parsedKeyValue.String) == "" || s.store == nil {
		return CreateDocumentResult{}, conflict("version_not_ready", "the current document version has no readable parsed blocks")
	}
	reader, err := s.store.GetReader(ctx, parsedKeyValue.String)
	if err != nil {
		return CreateDocumentResult{}, internal("failed to read parsed blocks for confirmation", err)
	}
	parsedBytes, readErr := io.ReadAll(io.LimitReader(reader, 128<<20+1))
	_ = reader.Close()
	if readErr != nil {
		return CreateDocumentResult{}, internal("failed to read parsed blocks for confirmation", readErr)
	}
	if len(parsedBytes) > 128<<20 {
		return CreateDocumentResult{}, badRequest("parsed_source_too_large", "parsed source exceeds the confirmation limit")
	}
	var currentParsed ParsedDocument
	if err := json.Unmarshal(parsedBytes, &currentParsed); err != nil {
		return CreateDocumentResult{}, internal("invalid parsed blocks for confirmation", err)
	}
	if currentParsed, err = sanitizeParsedDocument(currentParsed); err != nil {
		return CreateDocumentResult{}, conflict("version_not_ready", "the current parsed blocks failed validation")
	}
	if len(currentParsed.Blocks) != len(input.Blocks) {
		return CreateDocumentResult{}, badRequest("blocks_changed", "confirmation must include the complete unchanged block list")
	}
	confirmedBlocks := make([]DocumentBlock, len(currentParsed.Blocks))
	confirmedAny := false
	for index, requested := range input.Blocks {
		source := currentParsed.Blocks[index]
		if !equivalentDocumentBlock(source, requested) {
			return CreateDocumentResult{}, badRequest("blocks_changed", "confirmation cannot edit parsed block content or locators")
		}
		if !source.ModelDerived && requested.ModelDerived {
			return CreateDocumentResult{}, badRequest("invalid_model_derived_state", "an original block cannot be marked as model-derived")
		}
		confirmedBlocks[index] = source
		if source.ModelDerived && !requested.ModelDerived {
			confirmedBlocks[index].ModelDerived = false
			confirmedAny = true
		}
	}
	if !confirmedAny {
		return CreateDocumentResult{}, badRequest("blocks_required", "at least one model-derived block must be selected for confirmation")
	}
	currentParsed.Blocks = confirmedBlocks
	currentParsed.ParserVersion = currentParsed.ParserVersion + "+confirmed"
	currentParsed.Stats["confirmed"] = true
	parsed := currentParsed
	data, err := json.Marshal(parsed)
	if err != nil {
		return CreateDocumentResult{}, internal("failed to encode confirmed blocks", err)
	}
	versionID, versionIDString := newID()
	parsedKey := fmt.Sprintf("knowledge/%s/%s/%s/%s/parsed.json", workspaceID, baseID, documentID, versionIDString)
	if s.store == nil {
		return CreateDocumentResult{}, unavailable()
	}
	if _, err := s.store.Upload(ctx, parsedKey, data, "application/json", "parsed.json"); err != nil {
		return CreateDocumentResult{}, internal("failed to store confirmed blocks", err)
	}
	cleanup := func() { _ = s.deleteObjectIfUnreferenced(ctx, parsedKey) }
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to start block confirmation transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		cleanup()
		return CreateDocumentResult{}, err
	}
	var lockedVersionID, lockedTitle string
	var lockedRevision int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(current_version_id::text,''),title,revision FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND deleted_at IS NULL FOR UPDATE`, doc, params.base).Scan(&lockedVersionID, &lockedTitle, &lockedRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cleanup()
			return CreateDocumentResult{}, notFound()
		}
		cleanup()
		return CreateDocumentResult{}, internal("failed to lock document for block confirmation", err)
	}
	if lockedRevision != input.ExpectedRevision || (input.VersionID != "" && input.VersionID != lockedVersionID) {
		cleanup()
		return CreateDocumentResult{}, conflict("revision_conflict", "the document changed; refresh before confirming blocks")
	}
	if lockedVersionID == "" {
		cleanup()
		return CreateDocumentResult{}, conflict("version_not_ready", "the document has no current parsed version to confirm")
	}
	oldVersionID = lockedVersionID
	oldTitle = lockedTitle
	var next int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM knowledge_document_version WHERE document_id=$1`, doc).Scan(&next); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to allocate confirmed version", err)
	}
	var sourceObjectKey, sourceHash, mimeType string
	var sourceMetadata []byte
	var sourceByteSize int64
	if err := tx.QueryRow(ctx, `SELECT source_object_key,source_hash,byte_size,mime_type,source_metadata FROM knowledge_document_version WHERE document_id=$1 AND id=$2`, doc, mustID(oldVersionID)).Scan(&sourceObjectKey, &sourceHash, &sourceByteSize, &mimeType, &sourceMetadata); err != nil {
		cleanup()
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateDocumentResult{}, conflict("version_not_ready", "the current document version cannot be confirmed")
		}
		return CreateDocumentResult{}, internal("failed to load source for confirmed version", err)
	}
	metadataValue := mapJSON(sourceMetadata)
	metadataValue["confirmed_from_version_id"] = oldVersionID
	metadata, metadataErr := jsonBytes(metadataValue)
	if metadataErr != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to encode confirmed version metadata", metadataErr)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knowledge_document_version(id,workspace_id,knowledge_base_id,document_id,version_number,source_object_key,source_hash,byte_size,mime_type,source_metadata,parsed_object_key,parser_version,chunker_version,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'processing')`, versionID, params.workspace, params.base, doc, next, sourceObjectKey, sourceHash, sourceByteSize, mimeType, metadata, parsedKey, parsed.ParserVersion, ChunkerVersion); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to create confirmed version", err)
	}
	job, err := s.enqueueJob(ctx, tx, params.workspace, params.base, &versionID, nil, "chunk", jobKey("chunk", versionIDString, "", HashText(string(data))), map[string]any{"confirmed": true})
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to enqueue confirmed chunk job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to commit confirmed version", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	version, err := s.getVersionByID(ctx, params.workspace, params.base, doc, versionID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	document, err := s.GetDocument(ctx, workspaceID, actorID, baseID, documentID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	return CreateDocumentResult{Document: document, Version: version, Job: &job}, nil
}

func equivalentDocumentBlock(left, right DocumentBlock) bool {
	if left.BlockID != right.BlockID || left.Kind != right.Kind || left.Text != right.Text {
		return false
	}
	leftLocator, _ := json.Marshal(left.Locator)
	rightLocator, _ := json.Marshal(right.Locator)
	if string(leftLocator) != string(rightLocator) {
		return false
	}
	leftHeading, _ := json.Marshal(left.HeadingPath)
	rightHeading, _ := json.Marshal(right.HeadingPath)
	return string(leftHeading) == string(rightHeading)
}
