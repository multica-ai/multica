package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Per-issue metadata is a small JSONB KV map agents use to record pipeline
// state (PR number, pipeline_status, waiting_on, ...). Three rules govern
// the V1 surface — they're enforced both in the handler and at the DB:
//
//   - keys match `^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$` (handler)
//   - at most 50 keys per issue (handler)
//   - values are primitive: string / number / bool (handler)
//   - JSONB column is an object and ≤ 8KB (DB CHECK; defense in depth)
//
// All mutations are single-key atomic. UpdateIssue does NOT touch metadata —
// any whole-blob overwrite would race with concurrent agent writes (see the
// design discussion on MUL-2017).
const (
	maxIssueMetadataKeys = 50
)

var issueMetadataKeyRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)

var issueMetadataMutationLogSequence atomic.Uint64

// SetIssueMetadataKeyRequest carries the JSON value to write under the key
// named in the URL. Value is a RawMessage so we can preserve numeric vs.
// string typing through to PostgreSQL — once decoded into `any`, JSON
// numbers all collapse to float64 and we'd lose integer fidelity.
type SetIssueMetadataKeyRequest struct {
	Value json.RawMessage `json:"value"`
}

func validateIssueMetadataKey(key string) error {
	if key == "" {
		return errors.New("key is required")
	}
	if !issueMetadataKeyRE.MatchString(key) {
		return errors.New("key must match ^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$")
	}
	return nil
}

// validateIssueMetadataValue rejects anything other than a primitive JSON
// scalar. Null, arrays, and objects are not allowed — the V1 surface is
// flat KV. Removing a key uses DELETE, not a null value.
func validateIssueMetadataValue(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("value is required")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("value must be valid JSON: %w", err)
	}
	switch v.(type) {
	case string, bool, float64:
		return nil
	case nil:
		return errors.New("value cannot be null (use DELETE to remove a key)")
	default:
		return errors.New("value must be a primitive: string, number, or bool")
	}
}

// parseIssueMetadata decodes the JSONB bytes from db.Issue.Metadata into a
// Go map suitable for response serialization. Empty or unparseable blobs
// degrade to an empty map — the DB CHECK guarantees object shape, so this
// path is only hit on rows somehow predating the migration. Shared with the
// service-layer broadcast rendering (service.IssueToMap) so both ways of
// describing an issue agree on what an unset bag looks like on the wire.
func parseIssueMetadata(raw []byte) map[string]any {
	return util.JSONObjectOrEmpty(raw)
}

func (h *Handler) recordIssueMetadataMutation(r *http.Request, op, result, issueID, key string, duration time.Duration) {
	h.Metrics.RecordIssueMetadataMutation(op, result, duration)
	// Keep issue IDs and metadata keys out of metric labels. A one-percent
	// process-local sample retains request-level correlation without turning a
	// hot metadata endpoint into an equally hot high-cardinality log stream.
	if !slog.Default().Enabled(r.Context(), slog.LevelDebug) || issueMetadataMutationLogSequence.Add(1)%100 != 1 {
		return
	}
	slog.Debug("issue metadata mutation", append(logger.RequestAttrs(r),
		"source", "api",
		"operation", op,
		"result", result,
		"issue_id", issueID,
		"key", key,
		"db_duration_ms", duration.Milliseconds(),
	)...)
}

// parseMetadataFilterParam reads the `metadata` query parameter (a JSON
// object) and returns it as the JSONB filter blob passed to ListIssues /
// CountIssues / ListOpenIssues. Empty input means "no filter" and returns
// a nil []byte, which the SQL layer interprets as "skip the @> check".
//
// Validates that the filter is itself a flat object of primitives, mirroring
// the constraints we apply at write time — querying for `{key: {nested}}`
// would never match since written values are primitive by construction.
func parseMetadataFilterParam(w http.ResponseWriter, raw string) ([]byte, bool) {
	if raw == "" {
		return nil, true
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "metadata filter must be a JSON object")
		return nil, false
	}
	for k, v := range parsed {
		if err := validateIssueMetadataKey(k); err != nil {
			writeError(w, http.StatusBadRequest, "metadata filter "+err.Error())
			return nil, false
		}
		switch v.(type) {
		case string, bool, float64:
			// ok
		default:
			writeError(w, http.StatusBadRequest, "metadata filter values must be primitives (string, number, bool)")
			return nil, false
		}
	}
	// Re-marshal so we send canonical JSON to PG (and not the raw, possibly
	// whitespace-padded user input).
	buf, err := json.Marshal(parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "metadata filter is invalid")
		return nil, false
	}
	return buf, true
}

func (h *Handler) ListIssueMetadata(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": parseIssueMetadata(issue.Metadata)})
}

func (h *Handler) SetIssueMetadataKey(w http.ResponseWriter, r *http.Request) {
	r = h.withWakeupActor(r)
	issueID := chi.URLParam(r, "id")
	key := chi.URLParam(r, "key")
	if err := validateIssueMetadataKey(key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req SetIssueMetadataKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateIssueMetadataValue(req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	result, err := h.setIssueMetadataKey(r, issue, key, req.Value, issueMetadataWriter{actorType: actorType, actorID: actorID})
	if err != nil {
		writeIssueMetadataAppError(w, err, "failed to set metadata key")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteIssueMetadataKey(w http.ResponseWriter, r *http.Request) {
	r = h.withWakeupActor(r)
	issueID := chi.URLParam(r, "id")
	key := chi.URLParam(r, "key")
	if err := validateIssueMetadataKey(key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	result, err := h.deleteIssueMetadataKey(r, issue, key, issueMetadataWriter{actorType: actorType, actorID: actorID})
	if err != nil {
		writeIssueMetadataAppError(w, err, "failed to delete metadata key")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Failures shared by every caller of the metadata mutation core. Each surface
// (App API, Plugin Action API) maps them onto its own error envelope.
var (
	errIssueMetadataNotFound     = errors.New("issue not found")
	errIssueMetadataTooManyKeys  = fmt.Errorf("metadata cannot exceed %d keys", maxIssueMetadataKeys)
	errIssueMetadataSizeExceeded = errors.New("metadata exceeds the 8KB size limit")
	errIssueMetadataConflict     = errors.New("issue changed since the expected revision")
	errIssueMetadataLoad         = errors.New(issueMetadataLoadMessage)
)

const issueMetadataLoadMessage = "failed to load issue metadata"

// issueMetadataWriter is who a metadata mutation is attributed to and,
// optionally, the issue revision it was computed against.
type issueMetadataWriter struct {
	actorType string
	actorID   string
	// viaPluginID names the installation that made a write on a member's
	// behalf (a ui/manual hook or a panel), so the change stays traceable to
	// the plugin while remaining the member's.
	viaPluginID string
	// expectedRevision, when set, makes the write conditional: it applies only
	// if the issue is still at that revision.
	expectedRevision *int64
}

// issueMetadataResult is the response body every metadata mutation returns:
// the whole bag after the write plus the revision it produced, so a caller can
// chain an If-Match write on the issue without a second read.
type issueMetadataResult struct {
	Metadata      map[string]any `json:"metadata"`
	IssueRevision int64          `json:"issue_revision"`
}

func writeIssueMetadataAppError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, errIssueMetadataNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errIssueMetadataTooManyKeys), errors.Is(err, errIssueMetadataSizeExceeded):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errIssueMetadataLoad):
		writeError(w, http.StatusInternalServerError, issueMetadataLoadMessage)
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

// setIssueMetadataKey is the single-key write shared by the App API and the
// Plugin Action API. The caller has already validated key and value and
// authorized access to issue; this enforces the key cap, writes through the
// wakeup-attributed transaction, and publishes issue:metadata_changed only
// when the bag actually changed.
func (h *Handler) setIssueMetadataKey(r *http.Request, issue db.Issue, key string, value json.RawMessage, writer issueMetadataWriter) (issueMetadataResult, error) {
	issueID := uuidToString(issue.ID)
	if writer.expectedRevision != nil && issue.Revision != *writer.expectedRevision {
		return issueMetadataResult{}, errIssueMetadataConflict
	}
	// Enforce the key-count cap in the handler. The DB only guards size,
	// and a clear 4xx for "too many keys" beats a CHECK violation that
	// happens to fire on the size cap once enough keys accumulate.
	existing := parseIssueMetadata(issue.Metadata)
	if _, present := existing[key]; !present && len(existing) >= maxIssueMetadataKeys {
		return issueMetadataResult{}, errIssueMetadataTooManyKeys
	}

	queryStarted := time.Now()
	updated, err := wakeupWrite(h, r, func(q *db.Queries) (db.SetIssueMetadataKeyRow, error) {
		row, err := q.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         key,
			Value:       []byte(value),
		})
		if err == nil && !writer.revisionFollows(row.Revision) {
			return row, errIssueMetadataConflict
		}
		return row, err
	})
	queryDuration := time.Since(queryStarted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return h.issueMetadataNoop(r, "set", issue, key, writer, queryDuration)
		}
		if errors.Is(err, errIssueMetadataConflict) {
			h.recordIssueMetadataMutation(r, "set", "conflict", issueID, key, queryDuration)
			return issueMetadataResult{}, err
		}
		h.recordIssueMetadataMutation(r, "set", "error", issueID, key, queryDuration)
		if isCheckViolation(err) {
			return issueMetadataResult{}, errIssueMetadataSizeExceeded
		}
		slog.Warn("SetIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		return issueMetadataResult{}, err
	}
	h.recordIssueMetadataMutation(r, "set", "changed", issueID, key, queryDuration)
	return h.publishIssueMetadataChanged(updated.ID, updated.WorkspaceID, updated.Metadata, updated.Revision, writer), nil
}

// revisionFollows reports whether a write that produced revision was applied
// on top of the expected one. The UPDATE bumps revision by exactly one under
// its row lock, so this is the atomic form of the If-Match check: a stale
// write is rolled back instead of committed.
func (w issueMetadataWriter) revisionFollows(revision int64) bool {
	return w.expectedRevision == nil || revision == *w.expectedRevision+1
}

// deleteIssueMetadataKey is the single-key delete shared by the App API and
// the Plugin Action API. Deleting an absent key is a successful no-op.
func (h *Handler) deleteIssueMetadataKey(r *http.Request, issue db.Issue, key string, writer issueMetadataWriter) (issueMetadataResult, error) {
	issueID := uuidToString(issue.ID)
	if writer.expectedRevision != nil && issue.Revision != *writer.expectedRevision {
		return issueMetadataResult{}, errIssueMetadataConflict
	}
	queryStarted := time.Now()
	updated, err := wakeupWrite(h, r, func(q *db.Queries) (db.DeleteIssueMetadataKeyRow, error) {
		row, err := q.DeleteIssueMetadataKey(r.Context(), db.DeleteIssueMetadataKeyParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         key,
		})
		if err == nil && !writer.revisionFollows(row.Revision) {
			return row, errIssueMetadataConflict
		}
		return row, err
	})
	queryDuration := time.Since(queryStarted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return h.issueMetadataNoop(r, "delete", issue, key, writer, queryDuration)
		}
		if errors.Is(err, errIssueMetadataConflict) {
			h.recordIssueMetadataMutation(r, "delete", "conflict", issueID, key, queryDuration)
			return issueMetadataResult{}, err
		}
		h.recordIssueMetadataMutation(r, "delete", "error", issueID, key, queryDuration)
		slog.Warn("DeleteIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		return issueMetadataResult{}, err
	}
	h.recordIssueMetadataMutation(r, "delete", "changed", issueID, key, queryDuration)
	return h.publishIssueMetadataChanged(updated.ID, updated.WorkspaceID, updated.Metadata, updated.Revision, writer), nil
}

// issueMetadataNoop handles a mutation whose UPDATE matched no row: either
// the write would not change the bag (same value, or deleting an absent key)
// or the issue vanished in between. Re-read to tell the two apart.
//
// A conditional write that changed nothing still has to be current: a value
// that happens to be stored already does not make a stale request valid.
func (h *Handler) issueMetadataNoop(r *http.Request, op string, issue db.Issue, key string, writer issueMetadataWriter, queryDuration time.Duration) (issueMetadataResult, error) {
	issueID := uuidToString(issue.ID)
	fallbackStarted := time.Now()
	current, err := h.Queries.GetIssueMetadataInWorkspace(r.Context(), db.GetIssueMetadataInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	queryDuration += time.Since(fallbackStarted)
	if errors.Is(err, pgx.ErrNoRows) {
		h.recordIssueMetadataMutation(r, op, "not_found", issueID, key, queryDuration)
		return issueMetadataResult{}, errIssueMetadataNotFound
	}
	if err != nil {
		h.recordIssueMetadataMutation(r, op, "error", issueID, key, queryDuration)
		slog.Warn("GetIssueMetadataInWorkspace after metadata "+op+" no-op failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		return issueMetadataResult{}, fmt.Errorf("%w: %w", errIssueMetadataLoad, err)
	}
	if writer.expectedRevision != nil && current.Revision != *writer.expectedRevision {
		h.recordIssueMetadataMutation(r, op, "conflict", issueID, key, queryDuration)
		return issueMetadataResult{}, errIssueMetadataConflict
	}
	h.recordIssueMetadataMutation(r, op, "noop", issueID, key, queryDuration)
	return issueMetadataResult{Metadata: parseIssueMetadata(current.Metadata), IssueRevision: current.Revision}, nil
}

func (h *Handler) publishIssueMetadataChanged(id, workspace pgtype.UUID, raw []byte, revision int64, writer issueMetadataWriter) issueMetadataResult {
	metadata := parseIssueMetadata(raw)
	payload := map[string]any{
		"issue_id":       uuidToString(id),
		"metadata":       metadata,
		"issue_revision": revision,
	}
	if writer.viaPluginID != "" {
		payload["via_plugin_id"] = writer.viaPluginID
	}
	h.publish(protocol.EventIssueMetadataChanged, uuidToString(workspace), writer.actorType, writer.actorID, payload)
	return issueMetadataResult{Metadata: metadata, IssueRevision: revision}
}
