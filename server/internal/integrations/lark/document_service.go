package lark

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// DocumentInstallationStore resolves the active Feishu installation for an Agent.
type DocumentInstallationStore interface {
	GetActiveLarkInstallationForAgent(context.Context, pgtype.UUID, pgtype.UUID) (Installation, error)
}

// DocumentTaskScope is trusted task capability state supplied by the MCP handler.
type DocumentTaskScope struct {
	TaskID               pgtype.UUID
	WorkspaceID          pgtype.UUID
	AgentID              pgtype.UUID
	RuntimeID            pgtype.UUID
	DaemonID             string
	InstallationID       pgtype.UUID
	InstallationRevision int64
}

// DocumentToolResult contains only bounded document output and verification state.
type DocumentToolResult struct {
	RevisionID int64    `json:"revision_id"`
	Content    string   `json:"content,omitempty"`
	BlockIDs   []string `json:"block_ids,omitempty"`
	Changed    bool     `json:"changed,omitempty"`
	Verified   bool     `json:"verified,omitempty"`
}

// DocumentService performs guarded document operations using one Agent installation.
type DocumentService struct {
	store       DocumentInstallationStore
	credentials CredentialsResolver
	client      DocumentAPIClient
	logger      *slog.Logger
	now         func() time.Time
}

// NewDocumentService constructs the Agent-scoped document orchestrator.
func NewDocumentService(store DocumentInstallationStore, credentials CredentialsResolver, client DocumentAPIClient, logger *slog.Logger) *DocumentService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DocumentService{store: store, credentials: credentials, client: client, logger: logger, now: time.Now}
}

// Execute validates the fixed protocol, rechecks the pinned installation, and
// performs one read or one guarded write followed by verification.
func (s *DocumentService) Execute(ctx context.Context, scope DocumentTaskScope, operation DocumentOperation, input DocumentToolInput) (result DocumentToolResult, toolErr *DocumentToolError) {
	started := s.now()
	fingerprint := "invalid"
	defer func() {
		resultClass := "success"
		if toolErr != nil {
			resultClass = string(toolErr.Code)
		}
		s.logger.Info("feishu document operation",
			"operation", string(operation),
			"result_class", resultClass,
			"duration_ms", s.now().Sub(started).Milliseconds(),
			"document_fingerprint", fingerprint,
			"task_id", util.UUIDToString(scope.TaskID),
			"installation_id", util.UUIDToString(scope.InstallationID),
		)
	}()

	request, validationErr := ValidateDocumentToolInput(operation, input)
	if validationErr != nil {
		return DocumentToolResult{}, validationErr
	}
	fingerprint = documentFingerprint(request.Document.Token)
	if !validDocumentTaskScope(scope) {
		return DocumentToolResult{}, documentCapabilityDenied()
	}

	installation, err := s.store.GetActiveLarkInstallationForAgent(ctx, scope.WorkspaceID, scope.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DocumentToolResult{}, &DocumentToolError{Code: DocumentErrorNotConnected, Message: "This Agent has no active Feishu bot connection."}
		}
		return DocumentToolResult{}, documentUpstreamUnavailable()
	}
	if !installationMatchesDocumentScope(installation, scope) {
		return DocumentToolResult{}, documentCapabilityDenied()
	}
	secret, err := s.credentials.DecryptAppSecret(installation)
	if err != nil || secret == "" || installation.AppID == "" {
		return DocumentToolResult{}, documentUpstreamUnavailable()
	}
	credentials := InstallationCredentials{
		AppID:     installation.AppID,
		AppSecret: secret,
		TenantKey: installation.TenantKey.String,
		Region:    RegionOrDefault(installation.Region),
	}

	if operation == DocumentOperationFetch {
		snapshot, fetchErr := s.client.FetchDocument(ctx, credentials, fetchParamsForRequest(request))
		if fetchErr != nil {
			return DocumentToolResult{}, normalizeDocumentServiceError(fetchErr, false)
		}
		return documentResultFromSnapshot(snapshot, false, true), nil
	}
	return s.executeWrite(ctx, credentials, request)
}

func (s *DocumentService) executeWrite(ctx context.Context, credentials InstallationCredentials, request ValidatedDocumentRequest) (DocumentToolResult, *DocumentToolError) {
	preParams := guardedWriteFetchParams(request)
	before, err := s.client.FetchDocument(ctx, credentials, preParams)
	if err != nil {
		return DocumentToolResult{}, normalizeDocumentServiceError(err, false)
	}
	if before.RevisionID <= 0 {
		return DocumentToolResult{}, &DocumentToolError{Code: DocumentErrorConflict, Message: "The current document revision is unavailable; read it again before writing."}
	}
	if request.Operation == DocumentOperationReplaceText && strings.Count(before.Content, request.Input.Pattern) != 1 {
		return DocumentToolResult{}, &DocumentToolError{Code: DocumentErrorConflict, Message: "The replacement target is not unique in the current document."}
	}
	if targetErr := validateDocumentWriteTargets(request, before); targetErr != nil {
		return DocumentToolResult{}, targetErr
	}

	updateParams := updateParamsForRequest(request, before.RevisionID)
	updated, updateErr := s.client.UpdateDocument(ctx, credentials, updateParams)
	if updateErr != nil && !ambiguousDocumentWriteError(updateErr) {
		return DocumentToolResult{}, normalizeDocumentServiceError(updateErr, true)
	}
	after, verifyErr := s.client.FetchDocument(ctx, credentials, preParams)
	if verifyErr != nil {
		return DocumentToolResult{}, &DocumentToolError{Code: DocumentErrorTimeoutUncertain, Message: "The write may have completed, but its result could not be verified.", Uncertain: true}
	}
	if updateErr != nil {
		if writeAppearsApplied(request, before, after) {
			return documentResultFromSnapshot(after, true, true), nil
		}
		return DocumentToolResult{}, &DocumentToolError{Code: DocumentErrorTimeoutUncertain, Message: "The write result is uncertain; read the target again before any retry.", Uncertain: true}
	}
	if after.RevisionID == 0 {
		after.RevisionID = updated.RevisionID
	}
	return documentResultFromSnapshot(after, true, true), nil
}

func validDocumentTaskScope(scope DocumentTaskScope) bool {
	return scope.TaskID.Valid && scope.WorkspaceID.Valid && scope.AgentID.Valid && scope.RuntimeID.Valid &&
		scope.InstallationID.Valid && scope.DaemonID != "" && scope.InstallationRevision > 0
}

func installationMatchesDocumentScope(installation Installation, scope DocumentTaskScope) bool {
	if installation.ID != scope.InstallationID || installation.WorkspaceID != scope.WorkspaceID || installation.AgentID != scope.AgentID ||
		installation.Status != "active" || !installation.UpdatedAt.Valid || installation.UpdatedAt.Time.UnixNano() != scope.InstallationRevision {
		return false
	}
	switch Region(installation.Region) {
	case "", RegionFeishu, RegionLark:
		return true
	default:
		return false
	}
}

func fetchParamsForRequest(request ValidatedDocumentRequest) DocumentFetchParams {
	format := "xml"
	if request.Input.Scope == DocumentFetchScopeFull {
		format = "markdown"
	}
	return DocumentFetchParams{
		Token:        request.Document.Token,
		Format:       format,
		Scope:        request.Input.Scope,
		Keyword:      request.Input.Keyword,
		StartBlockID: request.Input.StartBlockID,
		EndBlockID:   request.Input.EndBlockID,
	}
}

func guardedWriteFetchParams(request ValidatedDocumentRequest) DocumentFetchParams {
	params := DocumentFetchParams{Token: request.Document.Token, Format: "markdown", Scope: DocumentFetchScopeFull}
	switch request.Operation {
	case DocumentOperationInsertAfter, DocumentOperationReplaceBlock, DocumentOperationDeleteBlock:
		params.Format = "xml"
		params.Scope = DocumentFetchScopeRange
		if request.Input.BlockID != "" {
			params.StartBlockID = request.Input.BlockID
			params.EndBlockID = request.Input.BlockID
		} else {
			params.StartBlockID = request.Input.StartBlockID
			params.EndBlockID = request.Input.EndBlockID
		}
	}
	return params
}

func updateParamsForRequest(request ValidatedDocumentRequest, revisionID int64) DocumentUpdateParams {
	command := map[DocumentOperation]string{
		DocumentOperationAppend:       "append",
		DocumentOperationReplaceText:  "str_replace",
		DocumentOperationInsertAfter:  "block_insert_after",
		DocumentOperationReplaceBlock: "block_replace",
		DocumentOperationDeleteBlock:  "block_delete",
	}[request.Operation]
	format := "xml"
	if request.Operation == DocumentOperationAppend {
		format = "markdown"
	}
	return DocumentUpdateParams{
		Token:        request.Document.Token,
		Format:       format,
		Command:      command,
		Content:      request.Input.Content,
		Pattern:      request.Input.Pattern,
		BlockID:      request.Input.BlockID,
		StartBlockID: request.Input.StartBlockID,
		EndBlockID:   request.Input.EndBlockID,
		RevisionID:   revisionID,
	}
}

func validateDocumentWriteTargets(request ValidatedDocumentRequest, snapshot DocumentSnapshot) *DocumentToolError {
	if request.Operation != DocumentOperationInsertAfter && request.Operation != DocumentOperationReplaceBlock && request.Operation != DocumentOperationDeleteBlock {
		return nil
	}
	if len(snapshot.BlockIDs) > DocumentMaxBlocks {
		return &DocumentToolError{Code: DocumentErrorInvalidRequest, Message: "The selected block range exceeds the fixed limit."}
	}
	want := []string{request.Input.BlockID}
	if request.Input.BlockID == "" {
		want = []string{request.Input.StartBlockID, request.Input.EndBlockID}
	}
	for _, id := range want {
		if !containsDocumentBlock(snapshot.BlockIDs, id) {
			return &DocumentToolError{Code: DocumentErrorNotFound, Message: "A selected document block was not found in the fresh read."}
		}
	}
	return nil
}

func containsDocumentBlock(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func writeAppearsApplied(request ValidatedDocumentRequest, before, after DocumentSnapshot) bool {
	switch request.Operation {
	case DocumentOperationAppend:
		return !strings.Contains(before.Content, request.Input.Content) && strings.Contains(after.Content, request.Input.Content)
	case DocumentOperationReplaceText:
		return strings.Count(after.Content, request.Input.Pattern) == 0 && strings.Contains(after.Content, request.Input.Content)
	case DocumentOperationDeleteBlock:
		if request.Input.BlockID != "" {
			return containsDocumentBlock(before.BlockIDs, request.Input.BlockID) && !containsDocumentBlock(after.BlockIDs, request.Input.BlockID)
		}
	}
	return false
}

func ambiguousDocumentWriteError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var transportErr *documentTransportError
	if errors.As(err, &transportErr) {
		return true
	}
	var statusErr *larkAPIStatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode >= http.StatusInternalServerError {
		return true
	}
	var operationErr *documentOperationError
	return errors.As(err, &operationErr)
}

func normalizeDocumentServiceError(err error, write bool) *DocumentToolError {
	if write && ambiguousDocumentWriteError(err) {
		return &DocumentToolError{Code: DocumentErrorTimeoutUncertain, Message: "The write result is uncertain; read the target again before any retry.", Uncertain: true}
	}
	var statusErr *larkAPIStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return documentPermissionDenied()
		case http.StatusNotFound:
			return &DocumentToolError{Code: DocumentErrorNotFound, Message: "The document or selected block was not found."}
		case http.StatusConflict:
			return &DocumentToolError{Code: DocumentErrorConflict, Message: "The document changed; read it again before retrying."}
		}
	}
	switch larkErrorCode(err) {
	case 99991672, 1770003:
		return documentPermissionDenied()
	case 1770002, 1061004, 1061044:
		return &DocumentToolError{Code: DocumentErrorNotFound, Message: "The document or selected block was not found."}
	}
	return documentUpstreamUnavailable()
}

func documentPermissionDenied() *DocumentToolError {
	return &DocumentToolError{Code: DocumentErrorPermissionDenied, Message: "The current Feishu bot cannot access this document. Grant that bot the required document permission and retry."}
}

func documentCapabilityDenied() *DocumentToolError {
	return &DocumentToolError{Code: DocumentErrorCapabilityDenied, Message: "This task's Feishu document capability is no longer valid."}
}

func documentUpstreamUnavailable() *DocumentToolError {
	return &DocumentToolError{Code: DocumentErrorUpstreamUnavailable, Message: "The Feishu document service is temporarily unavailable."}
}

func documentResultFromSnapshot(snapshot DocumentSnapshot, changed, verified bool) DocumentToolResult {
	return DocumentToolResult{RevisionID: snapshot.RevisionID, Content: snapshot.Content, BlockIDs: snapshot.BlockIDs, Changed: changed, Verified: verified}
}

func documentFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:6])
}

func formatDocumentRevision(revision int64) string {
	return strconv.FormatInt(revision, 10)
}
