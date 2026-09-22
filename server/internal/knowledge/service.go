package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// DBTX is the small database surface used by the knowledge package. Keeping
// this interface local makes the package easy to test and prevents it from
// depending on generated queries that are unrelated to this domain.
type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type Config struct {
	Enabled         bool
	Store           storage.Storage
	SecretBox       *secretbox.Box
	MaxUploadBytes  int64
	MaxFetchBytes   int64
	FetchTimeout    time.Duration
	ProviderTimeout time.Duration
	ParserURL       string
	ParserToken     string
	ParserTimeout   time.Duration
	// PrivateModelHosts is the deployment-owned allowlist for plain HTTP model
	// gateways. HTTPS endpoints do not need this extra opt-in; HTTP is kept
	// explicit because provider API keys would otherwise cross the network in
	// clear text. Entries may be host names, host:port values, or URLs.
	PrivateModelHosts []string
	ModelConcurrency  int
	// EventBus is optional so the knowledge package remains usable in tests and
	// in command-line tools that do not run the WebSocket broadcaster.
	EventBus    *events.Bus
	FetchClient *http.Client
}

type Service struct {
	db                DBTX
	txStarter         TxStarter
	store             storage.Storage
	secretBox         *secretbox.Box
	enabled           bool
	maxUpload         int64
	maxFetch          int64
	fetchClient       *http.Client
	fetchTO           time.Duration
	providerTO        time.Duration
	parserURL         string
	parserToken       string
	parserTO          time.Duration
	parserClient      *http.Client
	privateModelHosts map[string]struct{}
	eventBus          *events.Bus
	modelConcurrency  int
	modelGatesMu      sync.Mutex
	modelGates        map[string]chan struct{}
	now               func() time.Time
}

func NewService(db DBTX, txStarter TxStarter, cfg Config) *Service {
	maxUpload := cfg.MaxUploadBytes
	if maxUpload <= 0 {
		maxUpload = 100 << 20
	}
	maxFetch := cfg.MaxFetchBytes
	if maxFetch <= 0 {
		maxFetch = 20 << 20
	}
	fetchTimeout := cfg.FetchTimeout
	if fetchTimeout <= 0 {
		fetchTimeout = 20 * time.Second
	}
	providerTimeout := cfg.ProviderTimeout
	if providerTimeout <= 0 {
		providerTimeout = 45 * time.Second
	}
	parserTimeout := cfg.ParserTimeout
	if parserTimeout <= 0 {
		parserTimeout = 15 * time.Minute
	}
	modelConcurrency := cfg.ModelConcurrency
	if modelConcurrency <= 0 {
		modelConcurrency = 2
	}
	if modelConcurrency > 32 {
		modelConcurrency = 32
	}
	fetchClient := cfg.FetchClient
	if fetchClient == nil {
		fetchClient = &http.Client{Timeout: fetchTimeout}
	}
	privateModelHosts := make(map[string]struct{}, len(cfg.PrivateModelHosts))
	for _, rawHost := range cfg.PrivateModelHosts {
		for _, host := range normalizePrivateModelHosts(rawHost) {
			privateModelHosts[host] = struct{}{}
		}
	}
	return &Service{
		db:                db,
		txStarter:         txStarter,
		store:             cfg.Store,
		secretBox:         cfg.SecretBox,
		enabled:           cfg.Enabled && db != nil && txStarter != nil,
		maxUpload:         maxUpload,
		maxFetch:          maxFetch,
		fetchClient:       fetchClient,
		fetchTO:           fetchTimeout,
		providerTO:        providerTimeout,
		parserURL:         strings.TrimRight(strings.TrimSpace(cfg.ParserURL), "/"),
		parserToken:       strings.TrimSpace(cfg.ParserToken),
		parserTO:          parserTimeout,
		parserClient:      &http.Client{Timeout: parserTimeout},
		privateModelHosts: privateModelHosts,
		eventBus:          cfg.EventBus,
		modelConcurrency:  modelConcurrency,
		modelGates:        make(map[string]chan struct{}),
		now:               time.Now,
	}
}

func (s *Service) Enabled() bool { return s != nil && s.enabled }

// Error is the stable application error returned to HTTP and CLI adapters.
// Err is retained for server logs and tests but is never serialized directly.
type Error struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return "knowledge error"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func knowledgeError(status int, code, message string, err error) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: err}
}

func unavailable() *Error {
	return knowledgeError(http.StatusServiceUnavailable, "knowledge_disabled", "knowledge base is not enabled", nil)
}

func notFound() *Error {
	return knowledgeError(http.StatusNotFound, "knowledge_not_found", "knowledge base resource not found", pgx.ErrNoRows)
}

func badRequest(code, message string) *Error {
	return knowledgeError(http.StatusBadRequest, code, message, nil)
}

func conflict(code, message string) *Error {
	return knowledgeError(http.StatusConflict, code, message, nil)
}

func forbidden(code, message string) *Error {
	return knowledgeError(http.StatusForbidden, code, message, nil)
}

func internal(message string, err error) *Error {
	return knowledgeError(http.StatusInternalServerError, "knowledge_internal_error", message, err)
}

func retryableKnowledge(status int, code, message string, err error) *Error {
	result := knowledgeError(status, code, message, err)
	result.Retryable = true
	return result
}

func (s *Service) checkEnabled() error {
	if !s.Enabled() {
		return unavailable()
	}
	return nil
}

// lockKnowledgeWorkspace coordinates knowledge writes with the workspace
// teardown transaction. Knowledge tables intentionally do not use foreign
// keys, so every transaction that can create or mutate knowledge rows must
// hold this share lock before touching the domain rows. DeleteWorkspace holds
// the same workspace row FOR UPDATE while it snapshots and removes the
// workspace-owned data.
func lockKnowledgeWorkspace(ctx context.Context, q DBTX, workspace pgtype.UUID) error {
	var locked pgtype.UUID
	if err := q.QueryRow(ctx, `SELECT id FROM workspace WHERE id=$1 FOR SHARE`, workspace).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to lock workspace for knowledge mutation", err)
	}
	return nil
}

// deleteObjectIfUnreferenced is used after an upload whose database commit did
// not complete. Retried jobs intentionally use deterministic object keys, so a
// late transaction failure may be racing a previous successful commit that
// already references the same key. Never delete an object while any version
// still points at it. Database/storage errors are returned to durable cleanup
// jobs so they can retry rather than silently losing the cleanup attempt.
func (s *Service) deleteObjectIfUnreferenced(ctx context.Context, key string) error {
	if s.store == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	var references int
	if err := s.db.QueryRow(ctx, `SELECT count(*)::int FROM knowledge_document_version WHERE source_object_key=$1 OR parsed_object_key=$1`, key).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return nil
	}
	return s.store.DeleteObject(ctx, key)
}

func parseID(value, field string) (pgtype.UUID, error) {
	id, err := util.ParseUUID(strings.TrimSpace(value))
	if err != nil {
		return pgtype.UUID{}, badRequest("invalid_id", "invalid "+field)
	}
	return id, nil
}

func newID() (pgtype.UUID, string) {
	id := uuid.New()
	return pgtype.UUID{Bytes: id, Valid: true}, id.String()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+2)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func jsonBytes(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal knowledge JSON: %w", err)
	}
	return data, nil
}

func nullableText(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}

func nullableValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	copy := value.Time
	return &copy
}

func nullableID(value pgtype.Text) *string { return nullableText(value) }

func scanBase(row pgx.Row) (Base, error) {
	var base Base
	var activeIndex pgtype.Text
	err := row.Scan(
		&base.ID, &base.WorkspaceID, &base.CreatorID, &base.Name, &base.Description,
		&base.Visibility, &base.Revision, &base.ACLRevision, &base.CorpusRevision,
		&activeIndex, &base.CreatedAt, &base.UpdatedAt,
	)
	if err != nil {
		return Base{}, err
	}
	base.ActiveIndexID = nullableID(activeIndex)
	return base, nil
}

func scanDocument(row pgx.Row) (Document, error) {
	var document Document
	var sourceURL, currentVersion pgtype.Text
	var deletedAt pgtype.Timestamptz
	err := row.Scan(
		&document.ID, &document.WorkspaceID, &document.KnowledgeBaseID, &document.Title,
		&document.SourceKind, &sourceURL, &document.Tags, &currentVersion, &document.Revision,
		&document.Status, &deletedAt, &document.CreatedAt, &document.UpdatedAt,
	)
	if err != nil {
		return Document{}, err
	}
	document.SourceURL = nullableText(sourceURL)
	document.CurrentVersionID = nullableText(currentVersion)
	document.DeletedAt = nullableTime(deletedAt)
	return document, nil
}

func scanVersion(row pgx.Row) (DocumentVersion, error) {
	var version DocumentVersion
	var metadata, config []byte
	var parsedKey, errorCode pgtype.Text
	err := row.Scan(
		&version.ID, &version.DocumentID, &version.VersionNumber, &version.SourceObjectKey,
		&version.SourceHash, &version.ByteSize, &version.MIMEType, &metadata, &parsedKey,
		&version.ParserVersion, &version.ChunkerVersion, &config, &version.Status,
		&errorCode, &version.CreatedAt,
	)
	if err != nil {
		return DocumentVersion{}, err
	}
	version.SourceMetadata = mapJSON(metadata)
	version.ConfigSnapshot = mapJSON(config)
	version.ParsedObjectKey = nullableText(parsedKey)
	version.ErrorCode = nullableText(errorCode)
	return version, nil
}

func scanChunk(row pgx.Row) (Chunk, error) {
	var chunk Chunk
	var refs, locator []byte
	err := row.Scan(
		&chunk.ID, &chunk.DocumentID, &chunk.VersionID, &chunk.Ordinal, &refs, &chunk.Text,
		&locator, &chunk.TokenEstimate, &chunk.TextHash,
	)
	if err != nil {
		return Chunk{}, err
	}
	if err := json.Unmarshal(refs, &chunk.BlockRefs); err != nil || chunk.BlockRefs == nil {
		chunk.BlockRefs = []string{}
	}
	chunk.SourceLocator = mapJSON(locator)
	return chunk, nil
}

func scanJob(row pgx.Row) (Job, error) {
	var job Job
	var versionID, indexID, errorCode pgtype.Text
	var progress []byte
	err := row.Scan(
		&job.ID, &job.KnowledgeBaseID, &versionID, &indexID, &job.Stage, &job.Status,
		&job.Attempt, &job.AvailableAt, &progress, &errorCode, &job.CreatedAt,
	)
	if err != nil {
		return Job{}, err
	}
	job.DocumentVersionID = nullableText(versionID)
	job.IndexID = nullableText(indexID)
	job.Progress = mapJSON(progress)
	job.ErrorCode = nullableText(errorCode)
	return job, nil
}

type CreateBaseInput struct {
	Name        string
	Description string
	Visibility  string
}

type UpdateBaseInput struct {
	Name             *string
	Description      *string
	Visibility       *string
	ExpectedRevision int64
}

type CreateURLInput struct {
	URL      string
	Title    string
	Tags     []string
	Metadata map[string]any
}

type CreateFileInput struct {
	Filename    string
	ContentType string
	Bytes       []byte
	Title       string
	Tags        []string
	Metadata    map[string]any
}

type ReplaceVersionInput struct {
	DocumentID       string
	Filename         string
	ContentType      string
	Bytes            []byte
	URL              string
	Metadata         map[string]any
	ExpectedRevision int64
}

type CreateDocumentResult struct {
	Document         Document        `json:"document"`
	Version          DocumentVersion `json:"version"`
	Job              *Job            `json:"job,omitempty"`
	Deduplicated     bool            `json:"deduplicated"`
	MatchedVersionID *string         `json:"matched_version_id,omitempty"`
}

func (s *Service) ListBases(ctx context.Context, workspaceID, actorID string, limit int) ([]Base, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, workspace_id::text, creator_id::text, name, description,
		       visibility, revision, acl_revision, corpus_revision, active_index_id::text,
		       created_at, updated_at
		FROM knowledge_base
		WHERE workspace_id=$1 AND deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$2)
		  AND (visibility='workspace' OR (creator_id=$2 AND $3::boolean))
		ORDER BY created_at DESC, id DESC
		LIMIT $4`, ws, actor, privateAccessAllowed(ctx), limit)
	if err != nil {
		return nil, internal("failed to list knowledge bases", err)
	}
	defer rows.Close()
	result := make([]Base, 0)
	for rows.Next() {
		base, scanErr := scanBase(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge base", scanErr)
		}
		result = append(result, base)
	}
	if err := rows.Err(); err != nil {
		return nil, internal("failed to list knowledge bases", err)
	}
	return result, nil
}

func (s *Service) GetBase(ctx context.Context, workspaceID, actorID, baseID string) (Base, error) {
	if err := s.checkEnabled(); err != nil {
		return Base{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return Base{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return Base{}, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return Base{}, err
	}
	result, err := scanBase(s.db.QueryRow(ctx, `
		SELECT id::text, workspace_id::text, creator_id::text, name, description,
		       visibility, revision, acl_revision, corpus_revision, active_index_id::text,
		       created_at, updated_at
		FROM knowledge_base
		WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$3)
		  AND (visibility='workspace' OR (creator_id=$3 AND $4::boolean))`, base, ws, actor, privateAccessAllowed(ctx)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Base{}, notFound()
	}
	if err != nil {
		return Base{}, internal("failed to load knowledge base", err)
	}
	return result, nil
}

func (s *Service) CreateBase(ctx context.Context, workspaceID, actorID string, input CreateBaseInput) (Base, error) {
	if err := s.checkEnabled(); err != nil {
		return Base{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return Base{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return Base{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 200 {
		return Base{}, badRequest("invalid_name", "name is required and must be at most 200 characters")
	}
	visibility := strings.TrimSpace(input.Visibility)
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	if visibility != VisibilityPrivate && visibility != VisibilityWorkspace {
		return Base{}, badRequest("invalid_visibility", "visibility must be private or workspace")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Base{}, internal("failed to start knowledge base transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return Base{}, err
	}
	var isMember bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE workspace_id=$1 AND user_id=$2)`, ws, actor).Scan(&isMember); err != nil {
		return Base{}, internal("failed to verify workspace membership", err)
	}
	if !isMember {
		return Base{}, forbidden("workspace_member_required", "only workspace members can create a knowledge base")
	}
	base, err := scanBase(tx.QueryRow(ctx, `
		INSERT INTO knowledge_base(workspace_id, creator_id, name, description, visibility)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id::text, workspace_id::text, creator_id::text, name, description,
		          visibility, revision, acl_revision, corpus_revision, active_index_id::text,
		          created_at, updated_at`, ws, actor, name, strings.TrimSpace(input.Description), visibility))
	if err != nil {
		return Base{}, internal("failed to create knowledge base", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Base{}, internal("failed to commit knowledge base", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, base.ID, actorID)
	return base, nil
}

func (s *Service) UpdateBase(ctx context.Context, workspaceID, actorID, baseID string, input UpdateBaseInput) (Base, error) {
	if err := s.checkEnabled(); err != nil {
		return Base{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return Base{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return Base{}, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return Base{}, err
	}
	if input.ExpectedRevision <= 0 {
		return Base{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	if input.Visibility != nil && *input.Visibility != VisibilityPrivate && *input.Visibility != VisibilityWorkspace {
		return Base{}, badRequest("invalid_visibility", "visibility must be private or workspace")
	}
	currentBase, err := s.GetBase(ctx, workspaceID, actorID, baseID)
	if err != nil {
		return Base{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return Base{}, err
	}
	var formerRecipients []string
	if input.Visibility != nil && *input.Visibility == VisibilityPrivate && currentBase.Visibility == VisibilityWorkspace {
		formerRecipients, _ = s.workspaceMemberIDs(ctx, ws)
	}
	// Actor is included in the predicate so a private base cannot be changed by
	// a member merely because the route itself is workspace-scoped.
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Base{}, internal("failed to start knowledge base update", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return Base{}, err
	}
	result, err := scanBase(tx.QueryRow(ctx, `
		UPDATE knowledge_base
		SET name=COALESCE($3,name), description=COALESCE($4,description),
		    visibility=COALESCE($5,visibility), revision=revision+1,
		    acl_revision=CASE WHEN $5 IS NOT NULL AND $5<>visibility THEN acl_revision+1 ELSE acl_revision END,
		    updated_at=now()
		WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND revision=$7
		  AND (creator_id=$6 OR (visibility='workspace' AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$6 AND m.role IN ('owner','admin'))))
		RETURNING id::text, workspace_id::text, creator_id::text, name, description,
		          visibility, revision, acl_revision, corpus_revision, active_index_id::text,
		          created_at, updated_at`, base, ws, cleanOptional(input.Name), cleanOptional(input.Description), input.Visibility, actor, input.ExpectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		return Base{}, conflict("revision_conflict", "knowledge base changed; refresh before editing")
	}
	if err != nil {
		return Base{}, internal("failed to update knowledge base", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Base{}, internal("failed to commit knowledge base update", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID, formerRecipients...)
	return result, nil
}

func cleanOptional(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func (s *Service) DeleteBase(ctx context.Context, workspaceID, actorID, baseID string, expectedRevision int64) error {
	if err := s.checkEnabled(); err != nil {
		return err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	if expectedRevision <= 0 {
		return badRequest("expected_revision_required", "expected_revision is required")
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return internal("failed to start knowledge base deletion", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return err
	}
	var lockedRevision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND revision=$4 AND (creator_id=$3 OR (visibility='workspace' AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$3 AND m.role IN ('owner','admin')))) FOR UPDATE`, base, ws, actor, expectedRevision).Scan(&lockedRevision); errors.Is(err, pgx.ErrNoRows) {
		return conflict("revision_conflict", "knowledge base changed; refresh before deleting")
	} else if err != nil {
		return internal("failed to lock knowledge base for deletion", err)
	}
	rows, err := tx.Query(ctx, `SELECT source_object_key,parsed_object_key FROM knowledge_document_version WHERE knowledge_base_id=$1`, base)
	if err != nil {
		return internal("failed to collect knowledge objects for cleanup", err)
	}
	keys := make([]string, 0)
	seenKeys := map[string]struct{}{}
	for rows.Next() {
		var sourceKey string
		var parsedKey pgtype.Text
		if scanErr := rows.Scan(&sourceKey, &parsedKey); scanErr != nil {
			rows.Close()
			return internal("failed to collect knowledge objects for cleanup", scanErr)
		}
		for _, key := range []string{sourceKey, nullableValue(parsedKey)} {
			if key == "" {
				continue
			}
			if _, exists := seenKeys[key]; !exists {
				seenKeys[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return internal("failed to collect knowledge objects for cleanup", err)
	}
	rows.Close()
	command, err := tx.Exec(ctx, `
		UPDATE knowledge_base SET deleted_at=now(), revision=revision+1, updated_at=now()
		WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND revision=$4`, base, ws, actor, expectedRevision)
	if err != nil {
		return internal("failed to delete knowledge base", err)
	}
	if command.RowsAffected() == 0 {
		return conflict("revision_conflict", "knowledge base changed; refresh before deleting")
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_document SET deleted_at=now(),revision=revision+1,updated_at=now() WHERE knowledge_base_id=$1 AND deleted_at IS NULL`, base); err != nil {
		return internal("failed to hide knowledge documents", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_job SET status='cancelled',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,updated_at=now() WHERE knowledge_base_id=$1 AND stage<>'cleanup' AND status IN ('queued','running','waiting_config')`, base); err != nil {
		return internal("failed to cancel knowledge jobs", err)
	}
	cleanupInput := map[string]any{"base_id": baseID, "object_keys": keys}
	if _, err := s.enqueueJob(ctx, tx, ws, base, nil, nil, "cleanup", jobKey("cleanup", baseID, "", "delete"), cleanupInput); err != nil {
		return internal("failed to enqueue knowledge cleanup", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return internal("failed to commit knowledge base deletion", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return nil
}

func baseReadableSQL() string {
	return `id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND (visibility='workspace' OR (creator_id=$3 AND $4::boolean))`
}

func (s *Service) ensureBaseManager(ctx context.Context, workspaceID, actorID, baseID string) error {
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	var creator, visibility string
	if err := s.db.QueryRow(ctx, `SELECT creator_id::text,visibility FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL`, base, workspace).Scan(&creator, &visibility); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to check knowledge base permissions", err)
	}
	if creator == uuidString(actor) {
		return nil
	}
	if visibility != VisibilityWorkspace {
		return forbidden("knowledge_base_forbidden", "only the knowledge base creator can manage this private base")
	}
	var role string
	if err := s.db.QueryRow(ctx, `SELECT role FROM member WHERE workspace_id=$1 AND user_id=$2`, workspace, actor).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return forbidden("knowledge_base_forbidden", "only a workspace manager can manage this knowledge base")
		}
		return internal("failed to check workspace role", err)
	}
	if role != "owner" && role != "admin" {
		return forbidden("knowledge_base_forbidden", "only the creator or a workspace manager can manage this knowledge base")
	}
	return nil
}

func (s *Service) listReadableBaseIDs(ctx context.Context, workspaceID, actorID string) ([]string, error) {
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id::text FROM knowledge_base WHERE workspace_id=$1 AND deleted_at IS NULL AND (visibility='workspace' OR (creator_id=$2 AND $3::boolean))`, ws, actor, privateAccessAllowed(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) ListDocuments(ctx context.Context, workspaceID, actorID, baseID string, limit int) ([]Document, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	base, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT d.id::text, d.workspace_id::text, d.knowledge_base_id::text, d.title,
		       d.source_kind, d.source_url, d.tags, d.current_version_id::text,
		       d.revision, COALESCE(v.status,'processing'), d.deleted_at,
		       d.created_at, d.updated_at
		FROM knowledge_document d
		LEFT JOIN knowledge_document_version v ON v.id=d.current_version_id
		WHERE d.knowledge_base_id=$1 AND d.deleted_at IS NULL
		ORDER BY d.created_at DESC, d.id DESC LIMIT $2`, base, limit)
	if err != nil {
		return nil, internal("failed to list knowledge documents", err)
	}
	defer rows.Close()
	result := []Document{}
	for rows.Next() {
		document, scanErr := scanDocument(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge document", scanErr)
		}
		result = append(result, document)
	}
	if err := rows.Err(); err != nil {
		return nil, internal("failed to list knowledge documents", err)
	}
	return result, nil
}

type readableBaseParams struct{ base, workspace, actor pgtype.UUID }

func (s *Service) readableBaseParams(workspaceID, actorID, baseID string) (readableBaseParams, error) {
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return readableBaseParams{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return readableBaseParams{}, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return readableBaseParams{}, err
	}
	return readableBaseParams{base: base, workspace: ws, actor: actor}, nil
}

func (s *Service) GetDocument(ctx context.Context, workspaceID, actorID, baseID, documentID string) (Document, error) {
	if err := s.checkEnabled(); err != nil {
		return Document{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return Document{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return Document{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return Document{}, err
	}
	result, err := scanDocument(s.db.QueryRow(ctx, `
		SELECT d.id::text, d.workspace_id::text, d.knowledge_base_id::text, d.title,
		       d.source_kind, d.source_url, d.tags, d.current_version_id::text,
		       d.revision, COALESCE(v.status,'processing'), d.deleted_at,
		       d.created_at, d.updated_at
		FROM knowledge_document d
		LEFT JOIN knowledge_document_version v ON v.id=d.current_version_id
		WHERE d.id=$1 AND d.knowledge_base_id=$2 AND d.deleted_at IS NULL`, doc, params.base))
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, notFound()
	}
	if err != nil {
		return Document{}, internal("failed to load knowledge document", err)
	}
	return result, nil
}

func (s *Service) ListVersions(ctx context.Context, workspaceID, actorID, baseID, documentID string) ([]DocumentVersion, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT v.id::text, v.document_id::text, v.version_number, v.source_object_key,
		       v.source_hash, v.byte_size, v.mime_type, v.source_metadata, v.parsed_object_key,
		       v.parser_version, v.chunker_version, v.config_snapshot, v.status, v.error_code,
		       v.created_at
		FROM knowledge_document_version v JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.document_id=$1 AND v.knowledge_base_id=$2 AND d.deleted_at IS NULL
		ORDER BY v.version_number DESC`, doc, params.base)
	if err != nil {
		return nil, internal("failed to list document versions", err)
	}
	defer rows.Close()
	versions := []DocumentVersion{}
	for rows.Next() {
		version, scanErr := scanVersion(rows)
		if scanErr != nil {
			return nil, internal("failed to read document version", scanErr)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, internal("failed to list document versions", err)
	}
	return versions, nil
}

func (s *Service) GetChunk(ctx context.Context, workspaceID, actorID, baseID, chunkID string) (Chunk, error) {
	if err := s.checkEnabled(); err != nil {
		return Chunk{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return Chunk{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return Chunk{}, err
	}
	chunk, err := parseID(chunkID, "chunk_id")
	if err != nil {
		return Chunk{}, err
	}
	result, err := scanChunk(s.db.QueryRow(ctx, `
		SELECT c.id::text, c.document_id::text, c.version_id::text, c.ordinal,
		       c.block_refs, c.text, c.source_locator, c.token_estimate, c.text_hash
		FROM knowledge_chunk c JOIN knowledge_document d ON d.id=c.document_id
		WHERE c.id=$1 AND c.knowledge_base_id=$2 AND d.current_version_id=c.version_id
		  AND d.deleted_at IS NULL`, chunk, params.base))
	if errors.Is(err, pgx.ErrNoRows) {
		return Chunk{}, notFound()
	}
	if err != nil {
		return Chunk{}, internal("failed to load knowledge chunk", err)
	}
	return result, nil
}

func (s *Service) OpenVersion(ctx context.Context, workspaceID, actorID, baseID, documentID, versionID string) (io.ReadCloser, string, string, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, "", "", err
	}
	if s.store == nil {
		return nil, "", "", unavailable()
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, "", "", err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, "", "", err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return nil, "", "", err
	}
	version, err := parseID(versionID, "version_id")
	if err != nil {
		return nil, "", "", err
	}
	var key, mimeType, filename string
	err = s.db.QueryRow(ctx, `
		SELECT v.source_object_key, v.mime_type, d.title
		FROM knowledge_document_version v JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.id=$1 AND v.document_id=$2 AND v.knowledge_base_id=$3 AND d.deleted_at IS NULL`, version, doc, params.base).Scan(&key, &mimeType, &filename)
	if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(key) == "" {
		return nil, "", "", notFound()
	}
	if err != nil {
		return nil, "", "", internal("failed to load source object", err)
	}
	reader, err := s.store.GetReader(ctx, key)
	if err != nil {
		return nil, "", "", internal("failed to read source object", err)
	}
	return reader, mimeType, filepath.Base(filename), nil
}

func (s *Service) sourceDuplicate(ctx context.Context, baseID pgtype.UUID, sourceKind, identity, sourceHash string) (string, string, error) {
	if sourceKind == SourceKindURL {
		var documentID, versionID string
		err := s.db.QueryRow(ctx, `
			SELECT d.id::text, v.id::text
			FROM knowledge_document d JOIN knowledge_document_version v ON v.document_id=d.id
			WHERE d.knowledge_base_id=$1 AND d.source_kind=$2 AND d.source_identity=$3 AND d.deleted_at IS NULL
			ORDER BY v.created_at DESC LIMIT 1`, baseID, sourceKind, identity).Scan(&documentID, &versionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return documentID, versionID, err
	}
	var documentID, versionID string
	err := s.db.QueryRow(ctx, `
		SELECT d.id::text, v.id::text
		FROM knowledge_document d JOIN knowledge_document_version v ON v.document_id=d.id
		WHERE d.knowledge_base_id=$1 AND d.deleted_at IS NULL AND v.source_hash=$2
		ORDER BY v.created_at DESC LIMIT 1`, baseID, sourceHash).Scan(&documentID, &versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return documentID, versionID, err
}

func (s *Service) enqueueJob(ctx context.Context, q DBTX, workspaceID, baseID pgtype.UUID, versionID, indexID *pgtype.UUID, stage, logicalKey string, input any) (Job, error) {
	data, err := jsonBytes(input)
	if err != nil {
		return Job{}, err
	}
	var versionParam, indexParam any
	if versionID != nil {
		versionParam = *versionID
	}
	if indexID != nil {
		indexParam = *indexID
	}
	job, err := scanJob(q.QueryRow(ctx, `
		INSERT INTO knowledge_job(workspace_id, knowledge_base_id, document_version_id, index_id, stage, logical_key, input)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (workspace_id, logical_key) DO UPDATE SET updated_at=now()
		RETURNING id::text, knowledge_base_id::text, document_version_id::text, index_id::text,
		          stage, status, attempt, available_at, progress, error_code, created_at`, workspaceID, baseID, versionParam, indexParam, stage, logicalKey, data))
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

func jobKey(stage, versionID, indexID, fingerprint string) string {
	return HashText(strings.Join([]string{stage, versionID, indexID, fingerprint}, "|"))
}

func (s *Service) createVersionAndJob(ctx context.Context, params readableBaseParams, documentID string, sourceKind, sourceURL, sourceObjectKey, sourceHash, filename, mimeType string, size int64, metadata map[string]any, title string, tags []string, stage string, upload []byte) (CreateDocumentResult, error) {
	docID, err := parseID(documentID, "document_id")
	if err != nil {
		return CreateDocumentResult{}, err
	}
	versionID, versionIDString := newID()
	metadataValue := cloneStringAnyMap(metadata)
	if strings.TrimSpace(filename) != "" {
		metadataValue["filename"] = filepath.Base(filename)
	}
	if strings.TrimSpace(mimeType) != "" {
		metadataValue["content_type"] = mimeType
	}
	metadataBytes, err := jsonBytes(metadataValue)
	if err != nil {
		return CreateDocumentResult{}, internal("failed to encode source metadata", err)
	}
	if sourceKind == SourceKindFile && upload != nil {
		if s.store == nil {
			return CreateDocumentResult{}, unavailable()
		}
		if _, err := s.store.Upload(ctx, sourceObjectKey, upload, mimeType, filename); err != nil {
			return CreateDocumentResult{}, internal("failed to store knowledge source", err)
		}
	}
	cleanup := func() {
		_ = s.deleteObjectIfUnreferenced(ctx, sourceObjectKey)
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to start document transaction", err)
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
		return CreateDocumentResult{}, internal("failed to lock knowledge base for document creation", err)
	}
	var versionNumber int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM knowledge_document_version WHERE document_id=$1`, docID).Scan(&versionNumber); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to allocate document version", err)
	}
	if sourceKind == SourceKindFile {
		_, err = tx.Exec(ctx, `
			INSERT INTO knowledge_document(id, workspace_id, knowledge_base_id, title, source_kind, source_identity, tags)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, docID, params.workspace, params.base, title, sourceKind, sourceHash, tags)
		if err != nil {
			cleanup()
			if isUniqueViolation(err) {
				return CreateDocumentResult{}, conflict("duplicate_source", "the source was imported concurrently; retry to load the existing document")
			}
			return CreateDocumentResult{}, internal("failed to create knowledge document", err)
		}
	}
	if sourceKind == SourceKindURL {
		_, err = tx.Exec(ctx, `
			INSERT INTO knowledge_document(id, workspace_id, knowledge_base_id, title, source_kind, source_url, source_identity, tags)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, docID, params.workspace, params.base, title, sourceKind, sourceURL, sourceURL, tags)
		if err != nil {
			cleanup()
			if isUniqueViolation(err) {
				return CreateDocumentResult{}, conflict("duplicate_source", "the URL was imported concurrently; retry to load the existing document")
			}
			return CreateDocumentResult{}, internal("failed to create knowledge document", err)
		}
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO knowledge_document_version(id, workspace_id, knowledge_base_id, document_id, version_number,
		       source_object_key, source_hash, byte_size, mime_type, source_metadata, status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'processing')`, versionID, params.workspace, params.base, docID, versionNumber, sourceObjectKey, sourceHash, size, mimeType, metadataBytes); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to create document version", err)
	}
	job, err := s.enqueueJob(ctx, tx, params.workspace, params.base, &versionID, nil, stage, jobKey(stage, versionIDString, "", sourceHash), map[string]any{"filename": filename, "source_url": sourceURL})
	if err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to enqueue document job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return CreateDocumentResult{}, internal("failed to commit document", err)
	}
	s.notifyKnowledgeInvalidation(ctx, params.workspaceString(), params.baseString(), params.actorString())
	document, err := s.GetDocument(ctx, params.workspaceString(), params.actorString(), params.baseString(), documentID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	version, err := s.getVersionByID(ctx, params.workspace, params.base, docID, versionID)
	if err != nil {
		return CreateDocumentResult{}, err
	}
	return CreateDocumentResult{Document: document, Version: version, Job: &job}, nil
}

func (p readableBaseParams) workspaceString() string { return uuidString(p.workspace) }
func (p readableBaseParams) actorString() string     { return uuidString(p.actor) }
func (p readableBaseParams) baseString() string      { return uuidString(p.base) }

func uuidString(value pgtype.UUID) string { return uuid.UUID(value.Bytes).String() }

func (s *Service) CreateFile(ctx context.Context, workspaceID, actorID, baseID string, input CreateFileInput) (CreateDocumentResult, error) {
	if err := s.checkEnabled(); err != nil {
		return CreateDocumentResult{}, err
	}
	if int64(len(input.Bytes)) > s.maxUpload {
		return CreateDocumentResult{}, badRequest("file_too_large", "file exceeds the knowledge upload limit")
	}
	if len(input.Bytes) == 0 {
		return CreateDocumentResult{}, badRequest("empty_file", "file is empty")
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
	filename := filepath.Base(strings.TrimSpace(input.Filename))
	if filename == "." || filename == "" {
		filename = "upload.bin"
	}
	hash := HashBytes(input.Bytes)
	matchedDocument, matchedVersion, err := s.sourceDuplicate(ctx, params.base, SourceKindFile, "", hash)
	if err != nil {
		return CreateDocumentResult{}, internal("failed to check duplicate source", err)
	}
	if matchedDocument != "" {
		document, getErr := s.GetDocument(ctx, workspaceID, actorID, baseID, matchedDocument)
		if getErr != nil {
			return CreateDocumentResult{}, getErr
		}
		version, getErr := s.getVersionByStrings(ctx, workspaceID, actorID, baseID, matchedDocument, matchedVersion)
		if getErr != nil {
			return CreateDocumentResult{}, getErr
		}
		return CreateDocumentResult{Document: document, Version: version, Deduplicated: true, MatchedVersionID: &matchedVersion}, nil
	}
	docID := uuid.NewString()
	key := fmt.Sprintf("knowledge/%s/%s/%s/%s/%s", workspaceID, baseID, docID, uuid.NewString(), hash)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	mimeType := strings.TrimSpace(input.ContentType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	result, err := s.createVersionAndJob(ctx, params, docID, SourceKindFile, "", key, hash, filename, mimeType, int64(len(input.Bytes)), input.Metadata, title, input.Tags, "parse", input.Bytes)
	if err != nil && strings.Contains(err.Error(), "concurrently") {
		matchedDocument, matchedVersion, dupErr := s.sourceDuplicate(ctx, params.base, SourceKindFile, "", hash)
		if dupErr == nil && matchedDocument != "" {
			document, _ := s.GetDocument(ctx, workspaceID, actorID, baseID, matchedDocument)
			version, _ := s.getVersionByStrings(ctx, workspaceID, actorID, baseID, matchedDocument, matchedVersion)
			return CreateDocumentResult{Document: document, Version: version, Deduplicated: true, MatchedVersionID: &matchedVersion}, nil
		}
	}
	return result, err
}

func (s *Service) CreateURL(ctx context.Context, workspaceID, actorID, baseID string, input CreateURLInput) (CreateDocumentResult, error) {
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
	normalized, err := NormalizeURL(input.URL)
	if err != nil {
		return CreateDocumentResult{}, badRequest("invalid_url", "only absolute http or https URLs are supported")
	}
	matchedDocument, matchedVersion, err := s.sourceDuplicate(ctx, params.base, SourceKindURL, normalized, "")
	if err != nil {
		return CreateDocumentResult{}, internal("failed to check duplicate URL", err)
	}
	if matchedDocument != "" {
		document, getErr := s.GetDocument(ctx, workspaceID, actorID, baseID, matchedDocument)
		if getErr != nil {
			return CreateDocumentResult{}, getErr
		}
		version, getErr := s.getVersionByStrings(ctx, workspaceID, actorID, baseID, matchedDocument, matchedVersion)
		if getErr != nil {
			return CreateDocumentResult{}, getErr
		}
		return CreateDocumentResult{Document: document, Version: version, Deduplicated: true, MatchedVersionID: &matchedVersion}, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = normalized
	}
	return s.createVersionAndJob(ctx, params, uuid.NewString(), SourceKindURL, normalized, "", "", filepath.Base(normalized), "text/html", 0, input.Metadata, title, input.Tags, "fetch", nil)
}

func (s *Service) getVersionByID(ctx context.Context, workspaceID, baseID pgtype.UUID, documentID, versionID pgtype.UUID) (DocumentVersion, error) {
	return scanVersion(s.db.QueryRow(ctx, `
		SELECT id::text, document_id::text, version_number, source_object_key, source_hash,
		       byte_size, mime_type, source_metadata, parsed_object_key, parser_version,
		       chunker_version, config_snapshot, status, error_code, created_at
		FROM knowledge_document_version WHERE id=$1 AND document_id=$2 AND knowledge_base_id=$3`, versionID, documentID, baseID))
}

func (s *Service) getVersionByStrings(ctx context.Context, workspaceID, actorID, baseID, documentID, versionID string) (DocumentVersion, error) {
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return DocumentVersion{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return DocumentVersion{}, err
	}
	version, err := parseID(versionID, "version_id")
	if err != nil {
		return DocumentVersion{}, err
	}
	result, err := s.getVersionByID(ctx, params.workspace, params.base, doc, version)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentVersion{}, notFound()
	}
	if err != nil {
		return DocumentVersion{}, internal("failed to load document version", err)
	}
	return result, nil
}

func (s *Service) UpdateDocument(ctx context.Context, workspaceID, actorID, baseID, documentID string, title *string, tags []string, expectedRevision int64) (Document, error) {
	if err := s.checkEnabled(); err != nil {
		return Document{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return Document{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return Document{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return Document{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return Document{}, err
	}
	if expectedRevision <= 0 {
		return Document{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Document{}, internal("failed to start document update transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return Document{}, err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, notFound()
		}
		return Document{}, internal("failed to lock knowledge base for document update", err)
	}
	result, err := scanDocument(tx.QueryRow(ctx, `
		UPDATE knowledge_document d
		SET title=COALESCE($4,title), tags=COALESCE($5,tags), revision=revision+1, updated_at=now()
		WHERE d.id=$1 AND d.knowledge_base_id=$2 AND d.workspace_id=$3 AND d.deleted_at IS NULL
		  AND d.revision=$6
		RETURNING d.id::text, d.workspace_id::text, d.knowledge_base_id::text, d.title,
		          d.source_kind, d.source_url, d.tags, d.current_version_id::text, d.revision,
		          COALESCE((SELECT status FROM knowledge_document_version WHERE id=d.current_version_id),'processing'),
		          d.deleted_at, d.created_at, d.updated_at`, doc, params.base, params.workspace, cleanOptional(title), tags, expectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, conflict("revision_conflict", "document changed; refresh before editing")
	}
	if err != nil {
		return Document{}, internal("failed to update knowledge document", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, internal("failed to commit knowledge document update", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return result, nil
}

func (s *Service) DeleteDocument(ctx context.Context, workspaceID, actorID, baseID, documentID string, expectedRevision int64) error {
	if err := s.checkEnabled(); err != nil {
		return err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return err
	}
	if expectedRevision <= 0 {
		return badRequest("expected_revision_required", "expected_revision is required")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return internal("failed to start document deletion transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to lock knowledge base for document deletion", err)
	}
	var currentRevision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND workspace_id=$3 AND deleted_at IS NULL FOR UPDATE`, doc, params.base, params.workspace).Scan(&currentRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to lock knowledge document for deletion", err)
	}
	if currentRevision != expectedRevision {
		return conflict("revision_conflict", "document changed; refresh before deleting")
	}
	keys := []string{}
	seenKeys := map[string]struct{}{}
	rows, err := tx.Query(ctx, `SELECT source_object_key,parsed_object_key FROM knowledge_document_version WHERE document_id=$1`, doc)
	if err != nil {
		return internal("failed to collect document objects for deletion", err)
	}
	for rows.Next() {
		var source string
		var parsed pgtype.Text
		if scanErr := rows.Scan(&source, &parsed); scanErr != nil {
			rows.Close()
			return internal("failed to read document objects for deletion", scanErr)
		}
		if strings.TrimSpace(source) != "" {
			if _, seen := seenKeys[source]; !seen {
				keys = append(keys, source)
				seenKeys[source] = struct{}{}
			}
		}
		if parsed.Valid && strings.TrimSpace(parsed.String) != "" {
			if _, seen := seenKeys[parsed.String]; !seen {
				keys = append(keys, parsed.String)
				seenKeys[parsed.String] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return internal("failed to collect document objects for deletion", err)
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `UPDATE knowledge_document SET deleted_at=now(),revision=revision+1,updated_at=now() WHERE id=$1`, doc); err != nil {
		return internal("failed to delete knowledge document", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_document_version SET status='cancelled',error_code='document_deleted' WHERE document_id=$1 AND status='processing'`, doc); err != nil {
		return internal("failed to cancel document versions", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_job SET status='cancelled',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,updated_at=now() WHERE knowledge_base_id=$1 AND document_version_id IN (SELECT id FROM knowledge_document_version WHERE document_id=$2) AND status IN ('queued','running','waiting_config')`, params.base, doc); err != nil {
		return internal("failed to cancel document jobs", err)
	}
	if _, err := s.enqueueJob(ctx, tx, params.workspace, params.base, nil, nil, "cleanup", jobKey("cleanup-document", documentID, "", ""), map[string]any{"scope": "document", "document_id": documentID, "object_keys": keys}); err != nil {
		return internal("failed to enqueue document cleanup", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_base SET corpus_revision=corpus_revision+1,updated_at=now() WHERE id=$1`, params.base); err != nil {
		return internal("failed to update knowledge corpus revision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return internal("failed to commit document deletion", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return nil
}

func (s *Service) Reprocess(ctx context.Context, workspaceID, actorID, baseID, documentID, stage string) (Job, error) {
	if err := s.checkEnabled(); err != nil {
		return Job{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return Job{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return Job{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return Job{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return Job{}, err
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "parse"
	}
	if stage != "parse" && stage != "embedding" && stage != "extract" {
		return Job{}, badRequest("invalid_reprocess_stage", "stage must be parse, embedding, or extract")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Job{}, internal("failed to start reprocess transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return Job{}, err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, notFound()
		}
		return Job{}, internal("failed to lock knowledge base for reprocess", err)
	}
	var versionID string
	if err := tx.QueryRow(ctx, `SELECT current_version_id::text FROM knowledge_document d WHERE d.id=$1 AND d.knowledge_base_id=$2 AND d.deleted_at IS NULL FOR UPDATE`, doc, params.base).Scan(&versionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, notFound()
		}
		return Job{}, internal("failed to load current document version", err)
	}
	version, err := parseID(versionID, "version_id")
	if err != nil {
		return Job{}, err
	}
	if stage == "parse" {
		var sourceObjectKey, sourceHash, mimeType string
		var sourceByteSize int64
		var sourceMetadata []byte
		if err := tx.QueryRow(ctx, `SELECT source_object_key,source_hash,byte_size,mime_type,source_metadata FROM knowledge_document_version WHERE id=$1 AND document_id=$2 AND knowledge_base_id=$3`, version, doc, params.base).Scan(&sourceObjectKey, &sourceHash, &sourceByteSize, &mimeType, &sourceMetadata); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Job{}, notFound()
			}
			return Job{}, internal("failed to load source for reprocess", err)
		}
		metadata := mapJSON(sourceMetadata)
		filename, _ := metadata["filename"].(string)
		if strings.TrimSpace(filename) == "" {
			filename = "source"
		}
		metadata["reprocessed_from_version_id"] = versionID
		metadataBytes, metadataErr := jsonBytes(metadata)
		if metadataErr != nil {
			return Job{}, internal("failed to encode reprocess metadata", metadataErr)
		}
		newVersionID, newVersionIDString := newID()
		var next int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM knowledge_document_version WHERE document_id=$1`, doc).Scan(&next); err != nil {
			return Job{}, internal("failed to allocate reprocess version", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO knowledge_document_version(id,workspace_id,knowledge_base_id,document_id,version_number,source_object_key,source_hash,byte_size,mime_type,source_metadata,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'processing')`, newVersionID, params.workspace, params.base, doc, next, sourceObjectKey, sourceHash, sourceByteSize, mimeType, metadataBytes); err != nil {
			return Job{}, internal("failed to create reprocess version", err)
		}
		job, err := s.enqueueJob(ctx, tx, params.workspace, params.base, &newVersionID, nil, "parse", jobKey("parse", newVersionIDString, "", "manual:"+uuid.NewString()), map[string]any{"requested_stage": stage, "filename": filename})
		if err != nil {
			return Job{}, internal("failed to enqueue reprocess parse job", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Job{}, internal("failed to commit reprocess version", err)
		}
		s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
		return job, nil
	}
	jobStage := stage
	var indexID *pgtype.UUID
	if stage == "embedding" {
		jobStage = "embed"
		var indexText pgtype.Text
		if err := tx.QueryRow(ctx, `SELECT COALESCE(building_index_id,active_index_id)::text FROM knowledge_base WHERE id=$1`, params.base).Scan(&indexText); err != nil {
			return Job{}, internal("failed to load embedding index for reprocess", err)
		}
		if !indexText.Valid || strings.TrimSpace(indexText.String) == "" {
			return Job{}, conflict("index_not_available", "rebuild an embedding index before reprocessing embeddings")
		}
		parsedIndex, parseErr := parseID(indexText.String, "index_id")
		if parseErr != nil {
			return Job{}, parseErr
		}
		indexID = &parsedIndex
	}
	job, err := s.enqueueJob(ctx, tx, params.workspace, params.base, &version, indexID, jobStage, jobKey(jobStage, versionID, func() string {
		if indexID == nil {
			return ""
		}
		return uuidString(*indexID)
	}(), "manual:"+uuid.NewString()), map[string]any{"requested_stage": stage})
	if err != nil {
		return Job{}, internal("failed to enqueue reprocess job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, internal("failed to commit reprocess job", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return job, nil
}

func (s *Service) ListJobs(ctx context.Context, workspaceID, actorID, baseID string, limit int) ([]Job, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, knowledge_base_id::text, document_version_id::text, index_id::text,
		       stage, status, attempt, available_at, progress, error_code, created_at
		FROM knowledge_job WHERE knowledge_base_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, params.base, limit)
	if err != nil {
		return nil, internal("failed to list knowledge jobs", err)
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge job", scanErr)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Service) CancelJob(ctx context.Context, workspaceID, actorID, baseID, jobID string) error {
	if err := s.checkEnabled(); err != nil {
		return err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return err
	}
	job, err := parseID(jobID, "job_id")
	if err != nil {
		return err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return internal("failed to start knowledge job cancellation", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `UPDATE knowledge_job SET status='cancelled',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,updated_at=now() WHERE id=$1 AND knowledge_base_id=$2 AND status IN ('queued','running','waiting_config')`, job, params.base)
	if err != nil {
		return internal("failed to cancel knowledge job", err)
	}
	if command.RowsAffected() == 0 {
		return notFound()
	}
	if err := tx.Commit(ctx); err != nil {
		return internal("failed to commit knowledge job cancellation", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return nil
}

func (s *Service) LogError(err error) {
	if err != nil {
		slog.Error("knowledge service error", "error", err)
	}
}
