package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
)

type secretaryStoredInstruction struct {
	Sequence         int64           `json:"sequence"`
	RequestID        string          `json:"request_id"`
	ItemKey          string          `json:"item_key"`
	Kind             string          `json:"kind"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
	CanonicalReceipt *string         `json:"canonical_receipt"`
}

// All routes also require authenticated workspace membership. The projection
// writer additionally needs the local machine credential; member instructions
// are attributed by the server and cannot be forged by an agent task token.
func (h *Handler) secretaryAllowed(w http.ResponseWriter, r *http.Request, machine bool) bool {
	if !h.cfg.LocalMode {
		http.NotFound(w, r)
		return false
	}
	if machine && (h.cfg.LocalAutomationToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-LifeOS-Automation-Token")), []byte(h.cfg.LocalAutomationToken)) != 1) {
		writeError(w, http.StatusForbidden, "local controller required")
		return false
	}
	return true
}

func (h *Handler) GetLifeOSSecretary(w http.ResponseWriter, r *http.Request) {
	if !h.secretaryAllowed(w, r, false) {
		return
	}
	ws, err := util.ParseUUID(h.resolveWorkspaceID(r))
	if err != nil {
		writeError(w, 400, "invalid workspace")
		return
	}
	// Use one read transaction: a client never combines revisions from two migrations.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to read secretary")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		writeError(w, 500, "failed to read secretary")
		return
	}
	var revision int64
	var payload json.RawMessage
	err = tx.QueryRow(r.Context(), `SELECT revision, payload FROM lifeos_secretary_projection WHERE workspace_id=$1`, ws).Scan(&revision, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		payload = json.RawMessage(`{"items":[]}`)
		err = nil
	}
	if err != nil {
		writeError(w, 500, "failed to read secretary")
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT sequence, request_id::text, item_key, kind, payload, created_at, canonical_receipt FROM lifeos_secretary_instruction WHERE workspace_id=$1 ORDER BY sequence`, ws)
	if err != nil {
		writeError(w, 500, "failed to read instructions")
		return
	}
	instructions := []secretaryStoredInstruction{}
	for rows.Next() {
		var instruction secretaryStoredInstruction
		if err = rows.Scan(&instruction.Sequence, &instruction.RequestID, &instruction.ItemKey, &instruction.Kind, &instruction.Payload, &instruction.CreatedAt, &instruction.CanonicalReceipt); err != nil {
			rows.Close()
			writeError(w, 500, "failed to read instruction")
			return
		}
		instructions = append(instructions, instruction)
	}
	rows.Close()
	if rows.Err() != nil {
		writeError(w, 500, "failed to read instructions")
		return
	}
	var unmapped int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM issue i WHERE i.workspace_id=$1 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements($2::jsonb->'items') e WHERE e->>'issue_id'=i.id::text)`, ws, payload).Scan(&unmapped)
	if err != nil {
		writeError(w, 500, "failed to check source coverage")
		return
	}
	// The generic issue API rounds updated_at to seconds. Keep full database
	// precision here so a real change can be atomically published after the first sync.
	versions := map[string]time.Time{}
	versionRows, err := tx.Query(r.Context(), `SELECT id::text, updated_at FROM issue WHERE workspace_id=$1`, ws)
	if err != nil {
		writeError(w, 500, "failed to read source versions")
		return
	}
	for versionRows.Next() {
		var id string
		var updated time.Time
		if err = versionRows.Scan(&id, &updated); err != nil {
			versionRows.Close()
			writeError(w, 500, "failed to read source version")
			return
		}
		versions[id] = updated
	}
	versionRows.Close()
	if versionRows.Err() != nil {
		writeError(w, 500, "failed to read source versions")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to read secretary")
		return
	}
	view, err := resolveSecretaryView(payload, instructions)
	if err != nil {
		writeError(w, 500, "failed to resolve secretary instructions")
		return
	}
	if revision == 0 {
		view = json.RawMessage("null")
	}
	writeJSON(w, 200, map[string]any{"revision": revision, "projection": view, "instructions": instructions, "unmapped_count": unmapped, "resolved": true, "source_versions": versions})
}

type secretaryIssueUpdate struct {
	Status            *string        `json:"status,omitempty"`
	ID                string         `json:"id"`
	ExpectedUpdatedAt time.Time      `json:"expected_updated_at"`
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	Metadata          map[string]any `json:"metadata"`
}

type secretaryProjectionRequest struct {
	ExpectedRevision int64                  `json:"expected_revision"`
	Projection       json.RawMessage        `json:"projection"`
	Updates          []secretaryIssueUpdate `json:"updates"`
	Acknowledgements map[string]string      `json:"acknowledgements"`
}

func (h *Handler) PutLifeOSSecretary(w http.ResponseWriter, r *http.Request) {
	if !h.secretaryAllowed(w, r, true) {
		return
	}
	ws, err := util.ParseUUID(h.resolveWorkspaceID(r))
	if err != nil {
		writeError(w, 400, "invalid workspace")
		return
	}
	var req secretaryProjectionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		writeError(w, 400, "invalid projection request")
		return
	}
	p, err := validateSecretaryProjection(req.Projection)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ids := map[string]bool{}
	sourceItems := map[string]secretaryItem{}
	for _, item := range p.Items {
		if item.IssueID != nil {
			ids[*item.IssueID] = true
			sourceItems[*item.IssueID] = item
		}
	}
	seen := map[string]bool{}
	for _, update := range req.Updates {
		if !ids[update.ID] || seen[update.ID] || update.Title == "" || len(update.Title) > 1200 || len(update.Description) > 50000 || update.ExpectedUpdatedAt.IsZero() {
			writeError(w, 400, "invalid source update")
			return
		}
		seen[update.ID] = true
		sourceItem := sourceItems[update.ID]
		validStatus := update.Status == nil || (sourceItem.ActionID != "" && sourceItem.Stage == "history" &&
			((*update.Status == "cancelled" && sourceItem.BusinessStatus == "cancelled") ||
				(*update.Status == "done" && sourceItem.BusinessStatus == "completed" && sourceItem.ClosureMode == "self_report" && sourceItem.ReportedStatus == "completed")))
		if !validStatus {
			writeError(w, 400, "canonical completion or cancellation evidence required")
			return
		}
		for key, value := range update.Metadata {
			if key != "lifeos_case_id" && key != "lifeos_secretary_kind" && key != "lifeos_secretary_stage" {
				writeError(w, 400, "unsupported projection metadata")
				return
			}
			if _, ok := value.(string); !ok {
				writeError(w, 400, "invalid projection metadata")
				return
			}
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to publish projection")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `INSERT INTO lifeos_secretary_projection (workspace_id) VALUES ($1) ON CONFLICT DO NOTHING`, ws); err != nil {
		writeError(w, 500, "failed to publish projection")
		return
	}
	var current int64
	var previousHash string
	if err = tx.QueryRow(r.Context(), `SELECT revision, source_sha256 FROM lifeos_secretary_projection WHERE workspace_id=$1 FOR UPDATE`, ws).Scan(&current, &previousHash); err != nil {
		writeError(w, 500, "failed to publish projection")
		return
	}
	if current != req.ExpectedRevision {
		writeError(w, 409, "projection changed; reload before retry")
		return
	}
	// Serialize source changes with member plans; never rewrite status or spawn execution.
	for _, update := range req.Updates {
		id, parseErr := util.ParseUUID(update.ID)
		if parseErr != nil {
			writeError(w, 400, "invalid source id")
			return
		}
		metadata, _ := json.Marshal(update.Metadata)
		tag, updateErr := tx.Exec(r.Context(), `UPDATE issue SET title=$3, description=$4, metadata=metadata||$5::jsonb, status=COALESCE($7,status), updated_at=now() WHERE workspace_id=$1 AND id=$2 AND updated_at=$6`, ws, id, update.Title, update.Description, metadata, update.ExpectedUpdatedAt, update.Status)
		if updateErr != nil {
			writeError(w, 500, "failed to update source presentation")
			return
		}
		if tag.RowsAffected() != 1 {
			writeError(w, 409, "source changed; rebuild projection")
			return
		}
	}
	// All issue records must remain represented, including completed/cancelled history.
	var missing int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM issue i WHERE i.workspace_id=$1 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements($2::jsonb->'items') e WHERE e->>'issue_id'=i.id::text)`, ws, req.Projection).Scan(&missing); err != nil {
		writeError(w, 500, "failed to verify coverage")
		return
	}
	var unknown int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM jsonb_array_elements($2::jsonb->'items') e WHERE e->>'issue_id' IS NOT NULL AND NOT EXISTS (SELECT 1 FROM issue i WHERE i.workspace_id=$1 AND i.id::text=e->>'issue_id')`, ws, req.Projection).Scan(&unknown); err != nil {
		writeError(w, 500, "failed to verify source membership")
		return
	}
	if unknown != 0 {
		writeError(w, 400, "projection contains unknown source records")
		return
	}
	if missing != 0 {
		writeError(w, 409, "source inventory changed; incomplete projection rejected")
		return
	}
	for requestID, receipt := range req.Acknowledgements {
		id, parseErr := util.ParseUUID(requestID)
		if parseErr != nil || receipt == "" {
			writeError(w, 400, "invalid canonical acknowledgement")
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE lifeos_secretary_instruction SET canonical_receipt=$3 WHERE workspace_id=$1 AND request_id=$2 AND canonical_receipt IS NULL`, ws, id, receipt); err != nil {
			writeError(w, 500, "failed to acknowledge instruction")
			return
		}
	}
	digest := sha256.Sum256(req.Projection)
	hash := hex.EncodeToString(digest[:])
	if previousHash != hash {
		current++
	}
	if _, err = tx.Exec(r.Context(), `UPDATE lifeos_secretary_projection SET revision=$2, source_sha256=$3, payload=$4, updated_at=now() WHERE workspace_id=$1`, ws, current, hash, req.Projection); err != nil {
		writeError(w, 500, "failed to publish projection")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit projection")
		return
	}
	writeJSON(w, 200, map[string]any{"revision": current, "updated": len(req.Updates), "source_sha256": hash})
}

func (h *Handler) CreateLifeOSSecretaryInstruction(w http.ResponseWriter, r *http.Request) {
	if !h.secretaryAllowed(w, r, false) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsText := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, userID, wsText)
	if actorType != "member" || actorID != userID {
		writeError(w, 403, "member session required")
		return
	}
	ws, err := util.ParseUUID(wsText)
	if err != nil {
		writeError(w, 400, "invalid workspace")
		return
	}
	var req secretaryInstructionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		writeError(w, 400, "invalid instruction")
		return
	}
	requestID, err := util.ParseUUID(req.RequestID)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	if err = validateSecretaryInstruction(req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to save instruction")
		return
	}
	defer tx.Rollback(r.Context())
	var revision int64
	var raw json.RawMessage
	if err = tx.QueryRow(r.Context(), `SELECT revision, payload FROM lifeos_secretary_projection WHERE workspace_id=$1 FOR UPDATE`, ws).Scan(&revision, &raw); err != nil {
		writeError(w, 409, "secretary is not ready")
		return
	}
	// Retry after a lost response returns the original receipt even after a new revision.
	var sequence int64
	var saved json.RawMessage
	body, _ := json.Marshal(req)
	err = tx.QueryRow(r.Context(), `SELECT sequence, payload FROM lifeos_secretary_instruction WHERE workspace_id=$1 AND request_id=$2`, ws, requestID).Scan(&sequence, &saved)
	if err == nil {
		var old secretaryInstructionRequest
		_ = json.Unmarshal(saved, &old)
		old.ExpectedRevision = req.ExpectedRevision
		oldBody, _ := json.Marshal(old)
		if string(oldBody) != string(body) {
			writeError(w, 409, "request id already used")
			return
		}
		writeJSON(w, 200, map[string]any{"sequence": sequence, "request_id": req.RequestID})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to read instruction")
		return
	}
	if revision != req.ExpectedRevision {
		writeError(w, 409, "information changed; reload before saving")
		return
	}
	p, err := validateSecretaryProjection(raw)
	if err != nil {
		writeError(w, 409, "invalid secretary projection")
		return
	}
	found := req.Kind == "capacity"
	for _, item := range p.Items {
		if item.Key != req.ItemKey {
			continue
		}
		found = true
		if item.Kind != "action" || item.Stage == "history" {
			writeError(w, 409, "historical or reference item cannot be scheduled")
			return
		}
	}
	if !found {
		writeError(w, 404, "item not found")
		return
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO lifeos_secretary_instruction (workspace_id, request_id, user_id, item_key, kind, payload) VALUES ($1,$2,$3,$4,$5,$6) RETURNING sequence`, ws, requestID, util.MustParseUUID(userID), req.ItemKey, req.Kind, body).Scan(&sequence); err != nil {
		writeError(w, 500, "failed to save instruction")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to save instruction")
		return
	}
	writeJSON(w, 200, map[string]any{"sequence": sequence, "request_id": req.RequestID})
}
