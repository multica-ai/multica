package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type previewClaims struct {
	WorkspaceID string    `json:"workspace_id"`
	BaseID      string    `json:"base_id"`
	DocumentID  string    `json:"document_id"`
	VersionID   string    `json:"version_id"`
	ActorID     string    `json:"actor_id"`
	ACLRevision int64     `json:"acl_revision"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// IssuePreviewCapability creates a short-lived, authenticated capability for
// a private source object. The URL is only an application endpoint; the
// object store itself is never made public or exposed as a presigned URL.
func (s *Service) IssuePreviewCapability(ctx context.Context, workspaceID, actorID, baseID, documentID, versionID string) (PreviewCapability, error) {
	if err := s.checkEnabled(); err != nil {
		return PreviewCapability{}, err
	}
	if s.store == nil || s.secretBox == nil {
		return PreviewCapability{}, unavailable()
	}
	baseInfo, err := s.GetBase(ctx, workspaceID, actorID, baseID)
	if err != nil {
		return PreviewCapability{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return PreviewCapability{}, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return PreviewCapability{}, err
	}
	document, err := parseID(documentID, "document_id")
	if err != nil {
		return PreviewCapability{}, err
	}
	version, err := parseID(versionID, "version_id")
	if err != nil {
		return PreviewCapability{}, err
	}
	var objectKey, sourceVersionID string
	err = s.db.QueryRow(ctx, `
		SELECT v.source_object_key, v.id::text
		FROM knowledge_document_version v
		JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.id=$1 AND v.document_id=$2 AND v.knowledge_base_id=$3 AND d.deleted_at IS NULL`, version, document, base).Scan(&objectKey, &sourceVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreviewCapability{}, notFound()
	}
	if err != nil {
		return PreviewCapability{}, internal("failed to load preview source", err)
	}
	if strings.TrimSpace(objectKey) == "" {
		return PreviewCapability{}, knowledgeError(409, "preview_not_ready", "the source object is not available for preview yet", nil)
	}
	// Preview capabilities are deliberately shorter-lived than normal API
	// sessions. RedeemPreview also re-checks the base ACL.
	expiresAt := s.now().Add(60 * time.Second)
	claims := previewClaims{WorkspaceID: workspaceID, BaseID: baseID, DocumentID: documentID, VersionID: sourceVersionID, ActorID: uuidString(actor), ACLRevision: baseInfo.ACLRevision, ExpiresAt: expiresAt}
	claimBytes, err := json.Marshal(claims)
	if err != nil {
		return PreviewCapability{}, internal("failed to encode preview capability", err)
	}
	sealed, err := s.secretBox.Seal(claimBytes)
	if err != nil {
		return PreviewCapability{}, internal("failed to protect preview capability", err)
	}
	token := base64.RawURLEncoding.EncodeToString(sealed)
	return PreviewCapability{
		VersionID:  sourceVersionID,
		DocumentID: documentID,
		ExpiresAt:  expiresAt,
		Token:      token,
		PreviewURL: "/api/knowledge/previews/" + url.PathEscape(sourceVersionID) + "?capability=" + url.QueryEscape(token),
	}, nil
}

// RedeemPreview re-checks the actor's base ACL after decrypting the
// capability. Possession of a token is therefore not a substitute for the
// normal workspace authentication boundary.
func (s *Service) RedeemPreview(ctx context.Context, actorID, versionID, token string) (io.ReadCloser, string, string, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, "", "", err
	}
	if s.store == nil || s.secretBox == nil {
		return nil, "", "", unavailable()
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	claimBytes, err := s.secretBox.Open(sealed)
	if err != nil {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	var claims previewClaims
	if err := json.Unmarshal(claimBytes, &claims); err != nil || claims.VersionID == "" || claims.BaseID == "" || claims.DocumentID == "" || claims.WorkspaceID == "" || claims.ActorID == "" || claims.ACLRevision <= 0 {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	if claims.VersionID != strings.TrimSpace(versionID) || !s.now().Before(claims.ExpiresAt) {
		return nil, "", "", knowledgeError(410, "preview_capability_expired", "preview capability has expired", nil)
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil || uuidString(actor) != claims.ActorID {
		return nil, "", "", knowledgeError(http.StatusForbidden, "preview_not_authorized", "preview capability belongs to another user", nil)
	}
	baseInfo, err := s.GetBase(ctx, claims.WorkspaceID, actorID, claims.BaseID)
	if err != nil {
		return nil, "", "", err
	}
	if baseInfo.ACLRevision != claims.ACLRevision {
		return nil, "", "", knowledgeError(http.StatusForbidden, "preview_not_authorized", "preview capability is no longer authorized", nil)
	}
	version, err := parseID(claims.VersionID, "version_id")
	if err != nil {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	document, err := parseID(claims.DocumentID, "document_id")
	if err != nil {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	base, err := parseID(claims.BaseID, "knowledge_base_id")
	if err != nil {
		return nil, "", "", badRequest("invalid_preview_capability", "preview capability is invalid")
	}
	var objectKey, mimeType, filename string
	err = s.db.QueryRow(ctx, `
		SELECT v.source_object_key, v.mime_type, d.title
		FROM knowledge_document_version v
		JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.id=$1 AND v.document_id=$2 AND v.knowledge_base_id=$3 AND d.deleted_at IS NULL`, version, document, base).Scan(&objectKey, &mimeType, &filename)
	if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(objectKey) == "" {
		return nil, "", "", notFound()
	}
	if err != nil {
		return nil, "", "", internal("failed to load preview object", err)
	}
	reader, err := s.store.GetReader(ctx, objectKey)
	if err != nil {
		return nil, "", "", internal("failed to read preview object", err)
	}
	return reader, mimeType, strings.TrimSpace(filename), nil
}

func (s *Service) ListVersionBlocks(ctx context.Context, workspaceID, actorID, baseID, documentID, versionID, cursor string, limit int) (VersionBlocksResponse, error) {
	if err := s.checkEnabled(); err != nil {
		return VersionBlocksResponse{}, err
	}
	if s.store == nil {
		return VersionBlocksResponse{}, unavailable()
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return VersionBlocksResponse{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return VersionBlocksResponse{}, err
	}
	document, err := parseID(documentID, "document_id")
	if err != nil {
		return VersionBlocksResponse{}, err
	}
	version, err := parseID(versionID, "version_id")
	if err != nil {
		return VersionBlocksResponse{}, err
	}
	var parsedKey string
	err = s.db.QueryRow(ctx, `SELECT COALESCE(parsed_object_key,'') FROM knowledge_document_version WHERE id=$1 AND document_id=$2 AND knowledge_base_id=$3`, version, document, params.base).Scan(&parsedKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return VersionBlocksResponse{}, notFound()
	}
	if err != nil {
		return VersionBlocksResponse{}, internal("failed to load parsed document", err)
	}
	if parsedKey == "" {
		return VersionBlocksResponse{}, knowledgeError(409, "blocks_not_ready", "parsed blocks are not available yet", nil)
	}
	reader, err := s.store.GetReader(ctx, parsedKey)
	if err != nil {
		return VersionBlocksResponse{}, internal("failed to read parsed document", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxKnowledgeArchiveMemberSize+1))
	if err != nil {
		return VersionBlocksResponse{}, internal("failed to read parsed document", err)
	}
	if len(data) > maxKnowledgeArchiveMemberSize {
		return VersionBlocksResponse{}, knowledgeError(http.StatusUnprocessableEntity, "parse_output_too_large", "parsed document exceeds the normalized output limit", nil)
	}
	var parsed ParsedDocument
	if err := json.Unmarshal(data, &parsed); err != nil {
		return VersionBlocksResponse{}, internal("parsed document is invalid", err)
	}
	if parsed, err = sanitizeParsedDocument(parsed); err != nil {
		return VersionBlocksResponse{}, internal("parsed document is invalid", err)
	}
	limit = knowledgePageLimit(limit)
	offset := 0
	if strings.TrimSpace(cursor) != "" {
		offset, err = strconv.Atoi(cursor)
		if err != nil || offset < 0 {
			return VersionBlocksResponse{}, badRequest("invalid_cursor", "cursor must be a non-negative block offset")
		}
	}
	if offset > len(parsed.Blocks) {
		offset = len(parsed.Blocks)
	}
	end := offset + limit
	if end > len(parsed.Blocks) {
		end = len(parsed.Blocks)
	}
	blocks := append([]DocumentBlock(nil), parsed.Blocks[offset:end]...)
	response := VersionBlocksResponse{VersionID: uuidString(version), Blocks: blocks}
	if end < len(parsed.Blocks) {
		response.NextCursor = strconv.Itoa(end)
	}
	return response, nil
}
