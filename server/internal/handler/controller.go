package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/runcontrol"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ControllerSettings is server-only deployment configuration. Only a SHA-256
// verifier is held by the API. The bearer lives with the controller, never in a
// daemon, task response, provider environment or ordinary user credential.
type ControllerSettings struct {
	TokenHash      string
	WorkspaceID    string
	AllowedHostIDs []string
}

type ControllerTarget struct {
	ProfileID   string `json:"profile_id"`
	RuntimeID   string `json:"runtime_id"`
	HostID      string `json:"host_id"`
	ProfileHash string `json:"profile_hash"`
}

type ControllerPolicy struct {
	AllowedEffects        []ControllerEffectAuthority `json:"allowed_effects"`
	ScopeRevision         string                      `json:"scope_revision"`
	ExpectedIssueRevision int64                       `json:"expected_issue_revision"`
	SourcePath            string                      `json:"source_path"`
	BaseCommit            string                      `json:"base_commit"`
	AllowedRepositories   []runcontrol.Repository     `json:"allowed_repositories"`
	AuthorityRecordIDs    []string                    `json:"authority_record_ids"`
	Targets               []ControllerTarget          `json:"targets"`
	Statuses              map[string]string           `json:"statuses"`
	ProgressPropertyID    string                      `json:"progress_property_id"`
	PicturePropertyID     string                      `json:"picture_property_id"`
	ProgressOptions       map[string]string           `json:"progress_options"`
	ProtectedPropertyIDs  []string                    `json:"protected_property_ids"`
	BudgetKey             string                      `json:"budget_key"`
	MaxActive             int                         `json:"max_active"`
	BudgetEvidence        string                      `json:"budget_evidence"`
	BudgetExpiresAt       time.Time                   `json:"budget_expires_at"`
	ForbiddenProfileIDs   []string                    `json:"forbidden_profile_ids"`
}

type controllerState struct {
	Revision       int64            `json:"revision"`
	AuthorityEpoch int64            `json:"authority_epoch"`
	Stopped        bool             `json:"stopped"`
	ScopeRevision  string           `json:"scope_revision"`
	Policy         ControllerPolicy `json:"policy"`
	State          json.RawMessage  `json:"state"`
}

func (h *Handler) ControllerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want, err := hex.DecodeString(h.Controller.TokenHash)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		got := sha256.Sum256([]byte(token))
		if err != nil || len(want) != sha256.Size || h.Controller.WorkspaceID == "" || !strings.HasPrefix(token, "mct_") || subtle.ConstantTimeCompare(want, got[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "dedicated controller credential required")
			return
		}
		// User, agent and daemon credentials cannot delegate this principal by header.
		r.Header.Del("X-User-ID")
		r.Header.Del("X-Agent-ID")
		r.Header.Del("X-Task-ID")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ControllerCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": runcontrol.Version, "atomic_projection": true, "launch_authority": true, "effect_lease_ledger": true, "external_effect_gateway": false, "configured": h.Controller.TokenHash != "" && h.Controller.WorkspaceID != ""})
}

func controllerDecode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid controller request: "+err.Error())
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "exactly one JSON request required")
		return false
	}
	return true
}

func (h *Handler) controllerTx(w http.ResponseWriter, r *http.Request, profileIDs ...string) (pgx.Tx, db.Issue, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue_id")
	if !ok {
		return nil, db.Issue{}, false
	}
	ws, ok := parseUUIDOrBadRequest(w, h.Controller.WorkspaceID, "controller workspace")
	if !ok {
		return nil, db.Issue{}, false
	}
	if h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "controller transactions unavailable")
		return nil, db.Issue{}, false
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 503, "controller database unavailable")
		return nil, db.Issue{}, false
	}

	// Match native enqueue/delete lock order: workspace, agent, issue, runtime.
	// Launch callers lock their profile before the issue. Projection callers
	// never acquire profile/task-owner locks while holding this issue lock.
	var locked string
	if err = tx.QueryRow(r.Context(), `SELECT id::text FROM workspace WHERE id=$1 FOR KEY SHARE`, ws).Scan(&locked); err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, 404, "controller workspace missing")
		return nil, db.Issue{}, false
	}
	sort.Strings(profileIDs)
	for _, profileID := range profileIDs {
		profile, valid := parseUUIDOrBadRequest(w, profileID, "profile_id")
		if !valid {
			_ = tx.Rollback(r.Context())
			return nil, db.Issue{}, false
		}
		if err = tx.QueryRow(r.Context(), `SELECT id::text FROM agent WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, profile, ws).Scan(&locked); err != nil {
			_ = tx.Rollback(r.Context())
			writeError(w, 404, "controller profile missing")
			return nil, db.Issue{}, false
		}
	}
	issue, err := db.New(tx).LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: id, WorkspaceID: ws})
	if err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusNotFound, "issue not found in controller workspace")
		return nil, db.Issue{}, false
	}
	return tx, issue, true
}

func loadController(r *http.Request, tx pgx.Tx, issue db.Issue) (controllerState, error) {
	var s controllerState
	var config []byte
	err := tx.QueryRow(r.Context(), `SELECT revision,authority_epoch,is_stopped,scope_revision,config,state FROM issue_controller WHERE workspace_id=$1 AND issue_id=$2 FOR UPDATE`, issue.WorkspaceID, issue.ID).Scan(&s.Revision, &s.AuthorityEpoch, &s.Stopped, &s.ScopeRevision, &config, &s.State)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(config, &s.Policy)
	return s, err
}

var controllerProgress = []string{"Inbox", "Queued", "Working", "Needs You", "Blocked", "Done", "Live"}

func (h *Handler) EnrollControllerIssue(w http.ResponseWriter, r *http.Request) {
	var p ControllerPolicy
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.ScopeRevision == "" || p.SourcePath == "" || p.AllowedRepositories == nil || len(p.Targets) == 0 || len(p.AuthorityRecordIDs) == 0 || p.BudgetKey == "" || p.BudgetEvidence == "" || p.MaxActive < 1 || p.MaxActive > 20 || !p.BudgetExpiresAt.After(time.Now()) || (p.BaseCommit != "" && !runcontrol.IsCommit(p.BaseCommit)) {
		writeError(w, 400, "complete scope, source, authority, targets and measured budget required")
		return
	}
	tx, issue, ok := h.controllerTx(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if issue.Revision != p.ExpectedIssueRevision {
		writeRevisionConflict(w, "issue", issue.ID, p.ExpectedIssueRevision, issue.Revision)
		return
	}
	var active bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','running','waiting_local_directory','deferred'))`, issue.ID).Scan(&active); err != nil {
		writeError(w, 503, "cannot prove admission drained")
		return
	}
	if active {
		writeError(w, 409, "drain existing attempts before controller enrollment")
		return
	}
	if err := db.New(tx).SeedIssueStatusEntries(r.Context(), issue.WorkspaceID); err != nil {
		writeError(w, 503, "status catalog unavailable")
		return
	}
	seenStatuses := map[string]bool{}
	for _, label := range controllerProgress {
		key := p.Statuses[label]
		if key == "" {
			writeError(w, 400, "all seven status mappings required")
			return
		}
		if seenStatuses[key] {
			writeError(w, 400, "Progress states require distinct native status keys")
			return
		}
		seenStatuses[key] = true
		_, _, valid := h.resolveIssueStatusKeyKind(w, r, issue.WorkspaceID, key)
		if !valid {
			return
		}
		want := map[string]string{"Inbox": "backlog", "Queued": "todo", "Working": "in_progress", "Needs You": "in_review", "Blocked": "blocked", "Done": "done", "Live": "done"}[label]
		entry, err := db.New(tx).GetIssueStatusEntryByKey(r.Context(), db.GetIssueStatusEntryByKeyParams{WorkspaceID: issue.WorkspaceID, Key: key})
		if err != nil || entry.Category != want {
			writeError(w, 400, "status category does not match Progress semantics")
			return
		}
	}

	// The deployment allowlist is authoritative; a request can only narrow it.
	for _, repo := range p.AllowedRepositories {
		if repo.URL == "" || !runcontrol.IsCommit(repo.Commit) {
			writeError(w, 400, "repositories require an exact commit")
			return
		}
	}
	for _, target := range p.Targets {
		allowed := false
		for _, host := range h.Controller.AllowedHostIDs {
			if host != "" && host == target.HostID {
				allowed = true
			}
		}
		if !allowed {
			writeError(w, 403, "host is outside the server controller allowlist")
			return
		}
		for _, denied := range p.ForbiddenProfileIDs {
			if denied == target.ProfileID {
				writeError(w, 403, "target is excluded from this host")
				return
			}
		}
		profileID, ok := parseUUIDOrBadRequest(w, target.ProfileID, "profile_id")
		if !ok {
			return
		}
		agent, err := db.New(tx).GetAgent(r.Context(), profileID)
		if err != nil || agent.WorkspaceID != issue.WorkspaceID || uuidToString(agent.RuntimeID) != target.RuntimeID || agent.ArchivedAt.Valid {
			writeError(w, 409, "profile/runtime identity changed")
			return
		}
		runtime, err := db.New(tx).GetAgentRuntime(r.Context(), agent.RuntimeID)
		if err != nil || runtime.WorkspaceID != issue.WorkspaceID || runtime.DaemonID.String != target.HostID || !runtime.OwnerID.Valid {
			writeError(w, 409, "target host/runtime identity changed")
			return
		}
		actualHash, hashErr := service.ControllerProfileHash(r.Context(), db.New(tx), agent)
		if hashErr != nil || target.ProfileHash != actualHash {
			writeError(w, 409, "profile definition changed")
			return
		}
	}
	p.ProtectedPropertyIDs = []string{}
	for _, id := range []string{p.ProgressPropertyID, p.PicturePropertyID} {
		if id == "" {
			continue
		}
		propID, ok := parseUUIDOrBadRequest(w, id, "property_id")
		if !ok {
			return
		}
		prop, err := db.New(tx).GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: propID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			writeError(w, 400, "unknown controller property")
			return
		}
		if prop.ArchivedAt.Valid || (id == p.PicturePropertyID && prop.Type != "text") || (id == p.ProgressPropertyID && prop.Type != "select") || p.PicturePropertyID == p.ProgressPropertyID {
			writeError(w, 400, "controller properties require distinct active select and text definitions")
			return
		}
		if id == p.ProgressPropertyID {
			for _, label := range controllerProgress {
				raw, _ := json.Marshal(p.ProgressOptions[label])
				if p.ProgressOptions[label] == "" {
					writeError(w, 400, "all progress options required")
					return
				}
				if _, err := validatePropertyValue(prop, raw); err != nil {
					writeError(w, 400, err.Error())
					return
				}
			}
		}
		p.ProtectedPropertyIDs = append(p.ProtectedPropertyIDs, id)
	}
	// All issues sharing a capacity budget must agree on its measured limit and
	// expiry. Serialize enrollment to avoid two different first writers.
	if err := db.New(tx).LockControllerBudget(r.Context(), uuidToString(issue.WorkspaceID)+":"+p.BudgetKey); err != nil {
		writeError(w, 503, "budget lock unavailable")
		return
	}
	var incompatible bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM issue_controller WHERE workspace_id=$1 AND config->>'budget_key'=$2 AND (config->>'max_active'<>$3 OR (config->>'budget_expires_at')::timestamptz<>$4 OR config->>'budget_evidence'<>$5))`, issue.WorkspaceID, p.BudgetKey, fmtInt(int32(p.MaxActive)), p.BudgetExpiresAt, p.BudgetEvidence).Scan(&incompatible); err != nil || incompatible {
		writeError(w, 409, "shared capacity budget differs from accepted measurement")
		return
	}
	config, _ := json.Marshal(p)
	_, err := tx.Exec(r.Context(), `INSERT INTO issue_controller(issue_id,workspace_id,scope_revision,config) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, issue.ID, issue.WorkspaceID, p.ScopeRevision, config)
	if err != nil {
		writeError(w, 503, "cannot enroll controller issue")
		return
	}
	s, err := loadController(r, tx, issue)
	if err != nil || runcontrol.Digest(s.Policy) != runcontrol.Digest(p) {
		writeError(w, 409, "issue already enrolled with different authority")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "enrollment commit failed")
		return
	}
	writeJSON(w, 200, s)
}

func (h *Handler) GetControllerProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "profile_id")
	if !ok {
		return
	}
	a, err := h.Queries.GetAgent(r.Context(), id)
	if err != nil || uuidToString(a.WorkspaceID) != h.Controller.WorkspaceID {
		writeError(w, 404, "profile not found")
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), a.RuntimeID)
	if err != nil {
		writeError(w, 409, "runtime missing")
		return
	}
	definitionHash, err := service.ControllerProfileHash(r.Context(), h.Queries, a)
	if err != nil {
		writeError(w, 503, "profile inputs unavailable")
		return
	}
	writeJSON(w, 200, ControllerTarget{ProfileID: uuidToString(a.ID), RuntimeID: uuidToString(a.RuntimeID), HostID: rt.DaemonID.String, ProfileHash: definitionHash})
}

func (h *Handler) GetControllerIssue(w http.ResponseWriter, r *http.Request) {
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
	evidence, err := h.readControllerEvidence(r, tx, issue)
	if err != nil {
		writeError(w, 503, "current controller evidence unavailable")
		return
	}
	var launches json.RawMessage
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(manifest ORDER BY created_at,run_id),'[]') FROM controlled_run WHERE workspace_id=$1 AND issue_id=$2`, issue.WorkspaceID, issue.ID).Scan(&launches); err != nil {
		writeError(w, 503, "launch receipts unavailable")
		return
	}
	prefix := ""
	if ws, err := db.New(tx).GetWorkspace(r.Context(), issue.WorkspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	writeJSON(w, 200, map[string]any{"controller": s, "issue": issueToResponse(issue, prefix), "issue_revision": issue.Revision, "status": issue.Status, "metadata": parseIssueMetadata(issue.Metadata), "properties": parseIssueProperties(issue.Properties), "evidence_version": evidence.Version, "timeline": evidence.Timeline, "runs": evidence.Runs, "launches": launches})
}

type ControllerProjection struct {
	ExpectedEvidenceVersion string   `json:"expected_evidence_version"`
	EventID                 string   `json:"event_id"`
	ExpectedRevision        int64    `json:"expected_revision"`
	ExpectedIssueRevision   int64    `json:"expected_issue_revision"`
	ScopeRevision           string   `json:"scope_revision"`
	Progress                string   `json:"progress"`
	CurrentPicture          string   `json:"current_picture"`
	OwnerAction             string   `json:"owner_action"`
	EvidenceIDs             []string `json:"evidence_ids"`
}

func (h *Handler) ProjectControllerIssue(w http.ResponseWriter, r *http.Request) {
	var p ControllerProjection
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.EventID == "" || len(p.EventID) > 512 || p.CurrentPicture == "" || len(p.CurrentPicture) > 1800 || len(p.EvidenceIDs) == 0 {
		writeError(w, 400, "event, concise current picture and evidence required")
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
	hash := runcontrol.Digest(p)
	var oldHash string
	var prior []byte
	err = tx.QueryRow(r.Context(), `SELECT request_hash,body FROM controller_event WHERE workspace_id=$1 AND issue_id=$2 AND event_id=$3`, issue.WorkspaceID, issue.ID, p.EventID).Scan(&oldHash, &prior)
	if err == nil {
		if oldHash != hash {
			writeError(w, 409, "event identity already used for different input")
			return
		}
		writeJSON(w, 200, json.RawMessage(prior))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 503, "event ledger unavailable")
		return
	}
	if s.Revision != p.ExpectedRevision || issue.Revision != p.ExpectedIssueRevision || s.ScopeRevision != p.ScopeRevision {
		writeError(w, 409, "controller or issue revision changed; reread before projecting")
		return
	}
	if !h.controllerEvidenceMatches(w, r, tx, issue, p.ExpectedEvidenceVersion) {
		return
	}
	status := s.Policy.Statuses[p.Progress]
	if status == "" {
		writeError(w, 400, "unknown Progress value")
		return
	}
	// Current accepted owner policy reserves closing/release decisions to the
	// owner. The controller can mirror them but cannot create new authority.
	if (p.Progress == "Done" || p.Progress == "Live") && issue.Status != status {
		writeError(w, 403, "controller cannot confer completion or release authority")
		return
	}
	if s.Stopped {
		writeError(w, 409, "owner stop preserves the current disposition")
		return
	}
	if p.Progress == "Needs You" && p.OwnerAction == "" && issue.Status != status {
		writeError(w, 400, "Needs You requires the current exact owner action")
		return
	}
	if p.Progress == "Working" || p.Progress == "Queued" {
		var observed bool
		query := `SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND status='running' AND started_at>now()-interval '6 hours' AND COALESCE(session_id,'')<>'')`
		if p.Progress == "Queued" {
			query = `SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','waiting_local_directory','deferred') AND created_at>now()-interval '6 hours')`
		}
		if err := tx.QueryRow(r.Context(), query, issue.ID).Scan(&observed); err != nil || !observed {
			writeError(w, 409, "native run evidence does not support this projection")
			return
		}
	}
	properties := map[string]any{}
	if s.Policy.ProgressPropertyID != "" {
		properties[s.Policy.ProgressPropertyID] = s.Policy.ProgressOptions[p.Progress]
	}
	if s.Policy.PicturePropertyID != "" {
		properties[s.Policy.PicturePropertyID] = p.CurrentPicture
	}
	propRaw, _ := json.Marshal(properties)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metaRaw, _ := json.Marshal(map[string]any{"controller_revision": s.Revision + 1, "controller_checked_at": now, "controller_state": p.Progress, "last_progress_what": p.CurrentPicture, "last_progress_at": now, "owner_action": p.OwnerAction})
	if _, err = tx.Exec(r.Context(), `SELECT set_config('multica.controller_writer','1',true)`); err != nil {
		writeError(w, 503, "projection writer unavailable")
		return
	}
	var issueRevision int64
	err = tx.QueryRow(r.Context(), `UPDATE issue SET status=$3,properties=COALESCE(properties,'{}')||$4::jsonb,metadata=COALESCE(metadata,'{}')||$5::jsonb,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING revision,properties`, issue.WorkspaceID, issue.ID, status, propRaw, metaRaw).Scan(&issueRevision, &propRaw)
	if err != nil {
		writeError(w, 503, "atomic native projection failed")
		return
	}
	result, _ := json.Marshal(map[string]any{"event_id": p.EventID, "revision": s.Revision + 1, "issue_revision": issueRevision, "progress": p.Progress, "status": status, "current_picture": p.CurrentPicture, "checked_at": now})
	_, err = tx.Exec(r.Context(), `UPDATE issue_controller SET revision=revision+1,state=$3,updated_at=now() WHERE workspace_id=$1 AND issue_id=$2`, issue.WorkspaceID, issue.ID, result)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO controller_event(workspace_id,issue_id,event_id,request_hash,revision,body) VALUES($1,$2,$3,$4,$5,$6)`, issue.WorkspaceID, issue.ID, p.EventID, hash, s.Revision+1, result)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO controller_outbox(workspace_id,event_id,issue_id,revision,body) VALUES($1,$2,$3,$4,$5)`, issue.WorkspaceID, uuidToString(issue.ID)+":"+p.EventID, issue.ID, s.Revision+1, result)
	}
	if err != nil {
		writeError(w, 503, "projection ledger/outbox failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "projection commit failed; retry this exact event")
		return
	}
	// Properties-changed causes query invalidation; unlike status_changed it
	// cannot start assignees/squads/autopilots. Lost notifications remain in the
	// outbox until the controller proves native readback and acknowledges them.
	if fresh, err := h.Queries.GetIssue(r.Context(), issue.ID); err == nil {
		prefix := ""
		if ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil {
			prefix = ws.IssuePrefix
		}
		response := issueToResponse(fresh, prefix)
		h.fillStatusCategory(r.Context(), issue.WorkspaceID, &response)
		h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue": response, "controller_projection": true})
	}
	h.publish(protocol.EventIssuePropertiesChanged, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(issue.ID), "issue_revision": issueRevision, "properties": parseIssueProperties(propRaw)})
	writeJSON(w, 200, json.RawMessage(result))
}

func (h *Handler) StopControllerIssue(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
		EventID          string `json:"event_id"`
	}
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.Reason == "" || p.EventID == "" {
		writeError(w, 400, "stop event and reason required")
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
	var oldHash string
	var prior json.RawMessage
	key := "stop:" + p.EventID
	err = tx.QueryRow(r.Context(), `SELECT request_hash,body FROM controller_event WHERE workspace_id=$1 AND issue_id=$2 AND event_id=$3`, issue.WorkspaceID, issue.ID, key).Scan(&oldHash, &prior)
	if err == nil {
		if oldHash != runcontrol.Digest(p) {
			writeError(w, 409, "stop event reused with different input")
			return
		}
		_ = tx.Rollback(r.Context())
		if h.TaskService == nil || h.TaskService.CancelTasksForIssue(r.Context(), issue.ID) != nil {
			writeError(w, 503, "authority stopped; native cancellation requires retry of this event")
			return
		}
		writeJSON(w, 200, prior)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 503, "stop ledger unavailable")
		return
	}
	if s.Stopped {
		writeError(w, 409, "authority is already stopped")
		return
	}
	if s.Revision != p.ExpectedRevision {
		writeError(w, 409, "controller revision changed")
		return
	}
	if len(p.Reason) > 1000 {
		writeError(w, 400, "stop reason must be concise")
		return
	}
	progress := "Blocked"
	status := s.Policy.Statuses[progress]
	for _, terminal := range []string{"Done", "Live"} {
		if issue.Status == s.Policy.Statuses[terminal] {
			progress = terminal
			status = issue.Status
		}
	}
	picture := "Stopped: " + p.Reason + ". Native cancellation requested."
	props := map[string]any{}
	if s.Policy.ProgressPropertyID != "" {
		props[s.Policy.ProgressPropertyID] = s.Policy.ProgressOptions[progress]
	}
	if s.Policy.PicturePropertyID != "" {
		props[s.Policy.PicturePropertyID] = picture
	}
	propRaw, _ := json.Marshal(props)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metaRaw, _ := json.Marshal(map[string]any{"controller_revision": s.Revision + 1, "controller_state": progress, "controller_checked_at": now, "last_progress_what": picture, "last_progress_at": now, "owner_action": ""})
	if _, err = tx.Exec(r.Context(), `SELECT set_config('multica.controller_writer','1',true)`); err != nil {
		writeError(w, 503, "stop projection writer unavailable")
		return
	}
	var issueRevision int64
	if err = tx.QueryRow(r.Context(), `UPDATE issue SET status=$3,properties=COALESCE(properties,'{}')||$4::jsonb,metadata=COALESCE(metadata,'{}')||$5::jsonb,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING revision`, issue.WorkspaceID, issue.ID, status, propRaw, metaRaw).Scan(&issueRevision); err != nil {
		writeError(w, 503, "atomic stop projection failed")
		return
	}
	body, _ := json.Marshal(map[string]any{"stopped": true, "revision": s.Revision + 1, "authority_epoch": s.AuthorityEpoch + 1, "reason": p.Reason, "issue_revision": issueRevision, "status": status, "progress": progress, "current_picture": picture, "checked_at": now})
	_, err = tx.Exec(r.Context(), `UPDATE issue_controller SET is_stopped=true,authority_epoch=authority_epoch+1,revision=revision+1,state=$3,updated_at=now() WHERE workspace_id=$1 AND issue_id=$2`, issue.WorkspaceID, issue.ID, body)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO controller_event(workspace_id,issue_id,event_id,request_hash,revision,body) VALUES($1,$2,$3,$4,$5,$6)`, issue.WorkspaceID, issue.ID, key, runcontrol.Digest(p), s.Revision+1, body)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO controller_outbox(workspace_id,event_id,issue_id,revision,body) VALUES($1,$2,$3,$4,$5)`, issue.WorkspaceID, uuidToString(issue.ID)+":"+key, issue.ID, s.Revision+1, body)
	}
	if err != nil {
		writeError(w, 503, "stop ledger failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "stop commit uncertain; reread")
		return
	}
	// Revocation commits first; no start/effect can race cancellation. The exact
	// event replay retries native cancellation after a crash or lost response.
	h.publishControllerReadback(r, issue)
	if h.TaskService == nil || h.TaskService.CancelTasksForIssue(r.Context(), issue.ID) != nil {
		writeError(w, 503, "authority stopped; native cancellation requires retry of this event")
		return
	}
	writeJSON(w, 200, json.RawMessage(body))
}

// ReleaseControllerIssue relinquishes controller ownership only after the
// stopped or terminal issue is fully drained and its final projection has been
// read back. Historical run, event and effect receipts remain immutable. A
// later enrollment must establish fresh scope and action identities.
func (h *Handler) ReleaseControllerIssue(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ExpectedRevision int64  `json:"expected_revision"`
		EventID          string `json:"event_id"`
		Reason           string `json:"reason"`
	}
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.EventID == "" || p.Reason == "" || len(p.EventID) > 512 || len(p.Reason) > 1000 {
		writeError(w, 400, "release event and concise reason required")
		return
	}
	tx, issue, ok := h.controllerTx(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())

	key := "release:" + p.EventID
	hash := runcontrol.Digest(p)
	var oldHash string
	var prior json.RawMessage
	err := tx.QueryRow(r.Context(), `SELECT request_hash,body FROM controller_event WHERE workspace_id=$1 AND issue_id=$2 AND event_id=$3`, issue.WorkspaceID, issue.ID, key).Scan(&oldHash, &prior)
	if err == nil {
		if oldHash != hash {
			writeError(w, 409, "release event reused with different input")
			return
		}
		w.Header().Set("X-Controller-Replayed", "true")
		writeJSON(w, 200, prior)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 503, "release ledger unavailable")
		return
	}

	s, err := loadController(r, tx, issue)
	if err != nil {
		writeError(w, 404, "issue is not controlled")
		return
	}
	if s.Revision != p.ExpectedRevision {
		writeError(w, 409, "controller revision changed")
		return
	}
	terminal := issue.Status == s.Policy.Statuses["Done"] || issue.Status == s.Policy.Statuses["Live"]
	if !s.Stopped && !terminal {
		writeError(w, 409, "stop or finish the issue before releasing controller ownership")
		return
	}

	var activeRuns, ambiguousEffects, undelivered bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','running','waiting_local_directory','deferred'))`, issue.ID).Scan(&activeRuns); err != nil {
		writeError(w, 503, "cannot prove issue is drained")
		return
	}
	if activeRuns {
		writeError(w, 409, "controller ownership cannot be released while native runs remain active")
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM controller_effect WHERE workspace_id=$1 AND issue_id=$2 AND phase='executing')`, issue.WorkspaceID, issue.ID).Scan(&ambiguousEffects); err != nil {
		writeError(w, 503, "cannot prove effects are reconciled")
		return
	}
	if ambiguousEffects {
		writeError(w, 409, "executing effects require acknowledgment or reconciliation before release")
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM controller_outbox WHERE workspace_id=$1 AND issue_id=$2 AND delivered_at IS NULL)`, issue.WorkspaceID, issue.ID).Scan(&undelivered); err != nil {
		writeError(w, 503, "cannot prove final projection delivery")
		return
	}
	if undelivered {
		writeError(w, 409, "acknowledge the final native projection before releasing ownership")
		return
	}

	body, _ := json.Marshal(map[string]any{
		"released": true, "revision": s.Revision, "authority_epoch": s.AuthorityEpoch,
		"reason": p.Reason, "issue_revision": issue.Revision,
		"released_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if _, err = tx.Exec(r.Context(), `INSERT INTO controller_event(workspace_id,issue_id,event_id,request_hash,revision,body) VALUES($1,$2,$3,$4,$5,$6)`, issue.WorkspaceID, issue.ID, key, hash, s.Revision, body); err != nil {
		writeError(w, 503, "release receipt failed")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM issue_controller WHERE workspace_id=$1 AND issue_id=$2`, issue.WorkspaceID, issue.ID); err != nil {
		writeError(w, 503, "controller ownership release failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "release commit uncertain; retry this exact event")
		return
	}
	h.publishControllerReadback(r, issue)
	writeJSON(w, 200, json.RawMessage(body))
}

func (h *Handler) controllerOwnsMutation(w http.ResponseWriter, r *http.Request, issueID string) bool {
	id, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return true
	}
	owned, err := h.Queries.ControllerOwnsIssue(r.Context(), id)
	if err != nil {
		writeError(w, 503, "controller authority unavailable")
		return true
	}
	if owned {
		writeError(w, 403, "this issue is controller owned; submit evidence instead of changing its projection or launching work")
		return true
	}
	return false
}
