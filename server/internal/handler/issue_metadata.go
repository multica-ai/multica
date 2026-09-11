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

	// Enforce the key-count cap in the handler. The DB only guards size,
	// and a clear 4xx for "too many keys" beats a CHECK violation that
	// happens to fire on the size cap once enough keys accumulate.
	existing := parseIssueMetadata(issue.Metadata)
	if _, present := existing[key]; !present && len(existing) >= maxIssueMetadataKeys {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("metadata cannot exceed %d keys", maxIssueMetadataKeys))
		return
	}

	queryStarted := time.Now()
	updated, err := h.Queries.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Key:         key,
		Value:       []byte(req.Value),
	})
	queryDuration := time.Since(queryStarted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fallbackStarted := time.Now()
			current, getErr := h.Queries.GetIssueMetadataInWorkspace(r.Context(), db.GetIssueMetadataInWorkspaceParams{
				ID:          issue.ID,
				WorkspaceID: issue.WorkspaceID,
			})
			queryDuration += time.Since(fallbackStarted)
			if errors.Is(getErr, pgx.ErrNoRows) {
				h.recordIssueMetadataMutation(r, "set", "not_found", issueID, key, queryDuration)
				writeError(w, http.StatusNotFound, "issue not found")
				return
			}
			if getErr != nil {
				h.recordIssueMetadataMutation(r, "set", "error", issueID, key, queryDuration)
				slog.Warn("GetIssueMetadataInWorkspace after metadata set no-op failed", append(logger.RequestAttrs(r), "error", getErr, "issue_id", issueID, "key", key)...)
				writeError(w, http.StatusInternalServerError, "failed to load issue metadata")
				return
			}
			h.recordIssueMetadataMutation(r, "set", "noop", issueID, key, queryDuration)
			writeJSON(w, http.StatusOK, map[string]any{
				"metadata":       parseIssueMetadata(current.Metadata),
				"issue_revision": current.Revision,
			})
			return
		}
		h.recordIssueMetadataMutation(r, "set", "error", issueID, key, queryDuration)
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "metadata exceeds the 8KB size limit")
			return
		}
		slog.Warn("SetIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to set metadata key")
		return
	}
	h.recordIssueMetadataMutation(r, "set", "changed", issueID, key, queryDuration)

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	metadata := parseIssueMetadata(updated.Metadata)
	h.publish(protocol.EventIssueMetadataChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id":       uuidToString(updated.ID),
		"metadata":       metadata,
		"issue_revision": updated.Revision,
	})
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata, "issue_revision": updated.Revision})
}

func (h *Handler) DeleteIssueMetadataKey(w http.ResponseWriter, r *http.Request) {
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

	queryStarted := time.Now()
	updated, err := h.Queries.DeleteIssueMetadataKey(r.Context(), db.DeleteIssueMetadataKeyParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Key:         key,
	})
	queryDuration := time.Since(queryStarted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fallbackStarted := time.Now()
			current, getErr := h.Queries.GetIssueMetadataInWorkspace(r.Context(), db.GetIssueMetadataInWorkspaceParams{
				ID:          issue.ID,
				WorkspaceID: issue.WorkspaceID,
			})
			queryDuration += time.Since(fallbackStarted)
			if errors.Is(getErr, pgx.ErrNoRows) {
				h.recordIssueMetadataMutation(r, "delete", "not_found", issueID, key, queryDuration)
				writeError(w, http.StatusNotFound, "issue not found")
				return
			}
			if getErr != nil {
				h.recordIssueMetadataMutation(r, "delete", "error", issueID, key, queryDuration)
				slog.Warn("GetIssueMetadataInWorkspace after metadata delete no-op failed", append(logger.RequestAttrs(r), "error", getErr, "issue_id", issueID, "key", key)...)
				writeError(w, http.StatusInternalServerError, "failed to load issue metadata")
				return
			}
			h.recordIssueMetadataMutation(r, "delete", "noop", issueID, key, queryDuration)
			writeJSON(w, http.StatusOK, map[string]any{
				"metadata":       parseIssueMetadata(current.Metadata),
				"issue_revision": current.Revision,
			})
			return
		}
		h.recordIssueMetadataMutation(r, "delete", "error", issueID, key, queryDuration)
		slog.Warn("DeleteIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to delete metadata key")
		return
	}
	h.recordIssueMetadataMutation(r, "delete", "changed", issueID, key, queryDuration)

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	metadata := parseIssueMetadata(updated.Metadata)
	h.publish(protocol.EventIssueMetadataChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id":       uuidToString(updated.ID),
		"metadata":       metadata,
		"issue_revision": updated.Revision,
	})
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata, "issue_revision": updated.Revision})
}
