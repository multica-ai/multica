package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/runcontrol"
)

type ControllerEffectAuthority struct {
	ResourceKey       string `json:"resource_key"`
	AuthorityRecordID string `json:"authority_record_id"`
	CandidateIdentity string `json:"candidate_identity"`
}

type effectRequest struct {
	EventID           string          `json:"event_id"`
	OperationID       string          `json:"operation_id"`
	ResourceKey       string          `json:"resource_key"`
	CandidateIdentity string          `json:"candidate_identity"`
	AuthorityRecordID string          `json:"authority_record_id"`
	AuthorityEpoch    int64           `json:"authority_epoch"`
	Phase             string          `json:"phase"`
	FencingToken      int64           `json:"fencing_token"`
	Receipt           json.RawMessage `json:"receipt"`
}

// ControllerEffect is a short operation lease, never a whole-provider-turn lock.
// A downstream adapter must validate the fencing token at its own write point.
// An expired executing lease is ambiguous and is never automatically reused.
func (h *Handler) ControllerEffect(w http.ResponseWriter, r *http.Request) {
	var p effectRequest
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.EventID == "" || p.OperationID == "" || p.ResourceKey == "" || len(p.ResourceKey) > 256 || p.CandidateIdentity == "" || p.AuthorityRecordID == "" {
		writeError(w, 400, "effect operation, resource, candidate and authority required")
		return
	}
	if p.Phase != "reserve" && p.Phase != "begin" && p.Phase != "acknowledge" {
		writeError(w, 400, "unknown effect transition")
		return
	}
	tx, issue, ok := h.controllerTx(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	s, err := loadController(r, tx, issue)
	if err != nil {
		writeError(w, 404, "issue is not controlled")
		return
	}
	if s.Stopped || s.AuthorityEpoch != p.AuthorityEpoch {
		writeError(w, 409, "effect authority stopped or superseded")
		return
	}
	authorized := false
	for _, grant := range s.Policy.AllowedEffects {
		if grant.ResourceKey == p.ResourceKey && grant.AuthorityRecordID == p.AuthorityRecordID && grant.CandidateIdentity == p.CandidateIdentity {
			authorized = true
		}
	}
	if !authorized {
		writeError(w, 403, "no accepted authority for this exact effect and candidate")
		return
	}
	hash := runcontrol.Digest(p)
	key := "effect:" + p.OperationID + ":" + p.EventID
	var oldHash string
	var previous []byte
	err = tx.QueryRow(r.Context(), `SELECT request_hash,body FROM controller_event WHERE workspace_id=$1 AND issue_id=$2 AND event_id=$3`, issue.WorkspaceID, issue.ID, key).Scan(&oldHash, &previous)
	if err == nil {
		if hash != oldHash {
			writeError(w, 409, "effect event reused for different input")
			return
		}
		w.Header().Set("X-Controller-Replayed", "true")
		writeJSON(w, 200, json.RawMessage(previous))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 503, "effect ledger unavailable")
		return
	}
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, uuidToString(issue.WorkspaceID)+":effect:"+p.ResourceKey); err != nil {
		writeError(w, 503, "effect lock unavailable")
		return
	}
	if p.Phase == "reserve" {
		rows, err := tx.Exec(r.Context(), `INSERT INTO controller_effect_operation(workspace_id,operation_id,resource_key,issue_id,request_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, issue.WorkspaceID, p.OperationID, p.ResourceKey, issue.ID, hash)
		if err != nil {
			writeError(w, 503, "operation ledger unavailable")
			return
		}
		if rows.RowsAffected() != 1 {
			writeError(w, 409, "operation identity was already used; recover its original event")
			return
		}
	}
	var operation, phase, candidate, boundIssue string
	var fence, epoch int64
	var expires time.Time
	err = tx.QueryRow(r.Context(), `SELECT operation_id,phase,candidate_identity,issue_id::text,fencing_token,authority_epoch,lease_expires_at FROM controller_effect WHERE workspace_id=$1 AND resource_key=$2 FOR UPDATE`, issue.WorkspaceID, p.ResourceKey).Scan(&operation, &phase, &candidate, &boundIssue, &fence, &epoch, &expires)
	now := time.Now().UTC()
	if errors.Is(err, pgx.ErrNoRows) {
		if p.Phase != "reserve" {
			writeError(w, 409, "effect has no reservation")
			return
		}
		fence = 1
		expires = now.Add(30 * time.Second)
		phase = "reserved"
		_, err = tx.Exec(r.Context(), `INSERT INTO controller_effect(workspace_id,resource_key,operation_id,issue_id,candidate_identity,authority_epoch,fencing_token,lease_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, issue.WorkspaceID, p.ResourceKey, p.OperationID, issue.ID, p.CandidateIdentity, p.AuthorityEpoch, fence, expires)
	} else if err == nil {
		if p.Phase == "reserve" {
			if operation == p.OperationID {
				writeError(w, 409, "operation already reserved; recover its original event receipt")
				return
			}
			if phase == "executing" {
				writeError(w, 409, "effect in flight or ambiguous; reconciliation required")
				return
			}
			if phase != "acknowledged" && expires.After(now) {
				writeError(w, 429, "effect resource busy")
				return
			}
			fence++
			expires = now.Add(30 * time.Second)
			phase = "reserved"
			_, err = tx.Exec(r.Context(), `UPDATE controller_effect SET operation_id=$3,issue_id=$4,candidate_identity=$5,authority_epoch=$6,fencing_token=$7,lease_expires_at=$8,phase='reserved',receipt='{}' WHERE workspace_id=$1 AND resource_key=$2`, issue.WorkspaceID, p.ResourceKey, p.OperationID, issue.ID, p.CandidateIdentity, p.AuthorityEpoch, fence, expires)
		} else {
			if operation != p.OperationID || boundIssue != uuidToString(issue.ID) || candidate != p.CandidateIdentity || epoch != p.AuthorityEpoch || fence != p.FencingToken || !expires.After(now) {
				writeError(w, 409, "stale or mismatched effect fencing token")
				return
			}
			if p.Phase == "begin" {
				if phase != "reserved" {
					writeError(w, 409, "effect was already begun; reconcile instead of executing again")
					return
				}
				phase = "executing"
			}
			if p.Phase == "acknowledge" {
				if phase != "executing" || len(p.Receipt) == 0 || string(p.Receipt) == "null" || string(p.Receipt) == "{}" {
					writeError(w, 409, "effect acknowledgment requires an executing operation and receipt")
					return
				}
				phase = "acknowledged"
			}
			receipt := p.Receipt
			if len(receipt) == 0 {
				receipt = json.RawMessage(`{}`)
			}
			_, err = tx.Exec(r.Context(), `UPDATE controller_effect SET phase=$3,receipt=$4 WHERE workspace_id=$1 AND resource_key=$2`, issue.WorkspaceID, p.ResourceKey, phase, receipt)
		}
	}
	if err != nil {
		writeError(w, 503, "effect ledger write failed")
		return
	}
	body, _ := json.Marshal(map[string]any{"operation_id": p.OperationID, "resource_key": p.ResourceKey, "phase": phase, "fencing_token": fence, "lease_expires_at": expires, "authority_epoch": p.AuthorityEpoch, "candidate_identity": p.CandidateIdentity})
	_, err = tx.Exec(r.Context(), `INSERT INTO controller_event(workspace_id,issue_id,event_id,request_hash,revision,body) VALUES($1,$2,$3,$4,$5,$6)`, issue.WorkspaceID, issue.ID, key, hash, s.Revision, body)
	if err != nil {
		writeError(w, 503, "effect receipt failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "effect commit uncertain; recover the same event")
		return
	}
	writeJSON(w, 200, json.RawMessage(body))
}

func (h *Handler) GetControllerOutbox(w http.ResponseWriter, r *http.Request) {
	tx, issue, ok := h.controllerTx(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := loadController(r, tx, issue); err != nil {
		writeError(w, 404, "issue is not controlled")
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT event_id,revision,body FROM controller_outbox WHERE workspace_id=$1 AND issue_id=$2 AND delivered_at IS NULL ORDER BY revision LIMIT 100`, issue.WorkspaceID, issue.ID)
	if err != nil {
		writeError(w, 503, "outbox unavailable")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id string
		var rev int64
		var body json.RawMessage
		if err = rows.Scan(&id, &rev, &body); err != nil {
			writeError(w, 503, "outbox read failed")
			return
		}
		items = append(items, map[string]any{"event_id": id, "revision": rev, "body": body})
	}
	if err = rows.Err(); err != nil {
		writeError(w, 503, "outbox read failed")
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) AckControllerOutbox(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Revision      int64 `json:"revision"`
		IssueRevision int64 `json:"issue_revision"`
	}
	if !controllerDecode(w, r, &p) {
		return
	}
	tx, issue, ok := h.controllerTx(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	s, err := loadController(r, tx, issue)
	if err != nil {
		writeError(w, 404, "issue is not controlled")
		return
	}
	var state struct {
		Status         string `json:"status"`
		Progress       string `json:"progress"`
		CurrentPicture string `json:"current_picture"`
	}
	_ = json.Unmarshal(s.State, &state)
	meta := parseIssueMetadata(issue.Metadata)
	props := parseIssueProperties(issue.Properties)
	if s.Revision != p.Revision || issue.Revision != p.IssueRevision || issue.Status != state.Status || meta["last_progress_what"] != state.CurrentPicture {
		writeError(w, 409, "native projection readback disagrees")
		return
	}
	if (s.Policy.ProgressPropertyID != "" && props[s.Policy.ProgressPropertyID] != s.Policy.ProgressOptions[state.Progress]) || (s.Policy.PicturePropertyID != "" && props[s.Policy.PicturePropertyID] != state.CurrentPicture) {
		writeError(w, 409, "native property readback disagrees")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE controller_outbox SET delivered_at=now() WHERE workspace_id=$1 AND issue_id=$2 AND revision<=$3 AND delivered_at IS NULL`, issue.WorkspaceID, issue.ID, p.Revision)
	if err != nil {
		writeError(w, 503, "outbox acknowledgment failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "outbox commit uncertain")
		return
	}
	writeJSON(w, 200, map[string]any{"verified_revision": p.Revision})
}
