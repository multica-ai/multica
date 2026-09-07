package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/runcontrol"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ControllerLaunch struct {
	ExpectedEvidenceVersion string    `json:"expected_evidence_version"`
	ScopeRevision           string    `json:"scope_revision"`
	AuthorityEpoch          int64     `json:"authority_epoch"`
	ActionID                string    `json:"action_id"`
	ProfileID               string    `json:"profile_id"`
	CandidateIdentity       string    `json:"candidate_identity"`
	Attempt                 int32     `json:"attempt"`
	MaxAttempts             int32     `json:"max_attempts"`
	ParentRunID             string    `json:"parent_run_id"`
	NotBefore               time.Time `json:"not_before"`
	Priority                int32     `json:"priority"`
	Instruction             string    `json:"instruction"`
}

func (h *Handler) LaunchControllerRun(w http.ResponseWriter, r *http.Request) {
	var p ControllerLaunch
	if !controllerDecode(w, r, &p) {
		return
	}
	if p.ActionID == "" || len(p.ActionID) > 512 || p.CandidateIdentity == "" || p.Instruction == "" || len(p.Instruction) > 16000 || p.Attempt < 1 || p.MaxAttempts < p.Attempt || p.MaxAttempts > 4 {
		writeError(w, 400, "complete bounded action identity and instruction required")
		return
	}
	tx, issue, ok := h.controllerTx(w, r, p.ProfileID)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	s, err := loadController(r, tx, issue)
	if err != nil {
		writeError(w, 404, "issue is not controlled")
		return
	}
	if s.Stopped || s.ScopeRevision != p.ScopeRevision || s.AuthorityEpoch != p.AuthorityEpoch {
		writeError(w, 409, "launch authority stopped or superseded")
		return
	}
	requestHash := runcontrol.Digest(p)
	var priorHash string
	var prior json.RawMessage
	err = tx.QueryRow(r.Context(), `SELECT manifest,request_hash FROM controlled_run WHERE workspace_id=$1 AND issue_id=$2 AND action_id=$3 AND manifest->>'attempt'=$4`, issue.WorkspaceID, issue.ID, p.ActionID, fmtInt(p.Attempt)).Scan(&prior, &priorHash)
	if err == nil {
		var m runcontrol.Manifest
		_ = json.Unmarshal(prior, &m)
		if priorHash != requestHash {
			writeError(w, 409, "action already bound to different authority")
			return
		}
		writeJSON(w, 200, m)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 503, "launch ledger unavailable")
		return
	}
	if !h.controllerEvidenceMatches(w, r, tx, issue, p.ExpectedEvidenceVersion) {
		return
	}
	var target *ControllerTarget
	for i := range s.Policy.Targets {
		if s.Policy.Targets[i].ProfileID == p.ProfileID {
			target = &s.Policy.Targets[i]
			break
		}
	}
	if target == nil {
		writeError(w, 403, "profile is outside this issue's accepted authority")
		return
	}
	if !s.Policy.BudgetExpiresAt.After(time.Now()) {
		writeError(w, 409, "measured host/provider budget expired")
		return
	}
	agent, err := db.New(tx).GetAgent(r.Context(), parseUUID(target.ProfileID))
	actualHash, hashErr := service.ControllerProfileHash(r.Context(), db.New(tx), agent)
	if err != nil || hashErr != nil || agent.ArchivedAt.Valid || agent.WorkspaceID != issue.WorkspaceID || actualHash != target.ProfileHash {
		writeError(w, 409, "profile definition changed")
		return
	}
	runtime, err := db.New(tx).GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err != nil || runtime.DaemonID.String != target.HostID || uuidToString(runtime.ID) != target.RuntimeID || runtime.WorkspaceID != issue.WorkspaceID {
		writeError(w, 409, "runtime or host changed")
		return
	}
	now := time.Now().UTC()
	queued := now
	if p.NotBefore.IsZero() {
		p.NotBefore = now
	}
	if p.ParentRunID != "" {
		parentID, ok := parseUUIDOrBadRequest(w, p.ParentRunID, "parent_run_id")
		if !ok {
			return
		}
		var parentRaw []byte
		var parentStatus string
		err = tx.QueryRow(r.Context(), `SELECT c.manifest,t.status FROM controlled_run c JOIN agent_task_queue t ON t.id=c.run_id WHERE c.workspace_id=$1 AND c.issue_id=$2 AND c.run_id=$3`, issue.WorkspaceID, issue.ID, parentID).Scan(&parentRaw, &parentStatus)
		var parent runcontrol.Manifest
		if err != nil || json.Unmarshal(parentRaw, &parent) != nil || parentStatus != "failed" || parent.ActionID != p.ActionID || parent.Attempt+1 != p.Attempt || parent.MaxAttempts != p.MaxAttempts || parent.ProfileID != p.ProfileID || parent.CandidateIdentity != p.CandidateIdentity || parent.ScopeRevision != p.ScopeRevision {
			writeError(w, 409, "retry parent is not the matching failed attempt")
			return
		}
		queued = parent.QueuedAt
	} else if p.Attempt != 1 {
		writeError(w, 400, "retry requires its exact parent attempt")
		return
	}
	m := runcontrol.Manifest{Version: 1, WorkspaceID: uuidToString(issue.WorkspaceID), IssueID: uuidToString(issue.ID), ScopeRevision: s.ScopeRevision, ActionID: p.ActionID, Attempt: p.Attempt, MaxAttempts: p.MaxAttempts, ParentRunID: p.ParentRunID, RunID: uuidToString(dbid.NewV7()), ProfileID: p.ProfileID, ProfileHash: target.ProfileHash, RuntimeID: target.RuntimeID, HostID: target.HostID, ExecutionMode: "run_owned", SourcePath: s.Policy.SourcePath, BaseCommit: s.Policy.BaseCommit, AllowedRepositories: s.Policy.AllowedRepositories, AuthorityRecordIDs: s.Policy.AuthorityRecordIDs, RequiredEffects: []string{}, CandidateIdentity: p.CandidateIdentity, CancellationScope: uuidToString(issue.ID) + ":" + s.ScopeRevision, AuthorityEpoch: s.AuthorityEpoch, QueuedAt: queued, NotBefore: p.NotBefore, Priority: p.Priority, BudgetKey: s.Policy.BudgetKey}
	if err = m.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	manifest, _ := json.Marshal(m)
	_, err = tx.Exec(r.Context(), `INSERT INTO controlled_run(run_id,workspace_id,issue_id,action_id,manifest,manifest_hash,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, parseUUID(m.RunID), issue.WorkspaceID, issue.ID, m.ActionID, manifest, runcontrol.Digest(m), requestHash)
	if err == nil {
		var rows pgconn.CommandTag
		rows, err = tx.Exec(r.Context(), `INSERT INTO agent_task_queue(id,agent_id,runtime_id,issue_id,status,priority,force_fresh_session,handoff_note,context,originator_user_id,accountable_user_id,originator_source,original_queued_at,fire_at,max_attempts)
	 SELECT $1,$2,$3,$4,CASE WHEN $5::timestamptz>clock_timestamp() THEN 'deferred' ELSE 'queued' END,$6,true,$7,jsonb_build_object('controller_manifest_hash',$8::text),$9,$9,'controller',$10,$5,1
	 WHERE lock_task_owner_rows($2,$4,$3)`, parseUUID(m.RunID), agent.ID, runtime.ID, issue.ID, m.NotBefore, m.Priority, p.Instruction, runcontrol.Digest(m), runtime.OwnerID, m.QueuedAt)
		if err == nil && rows.RowsAffected() != 1 {
			err = errors.New("task owners disappeared")
		}
	}
	if err != nil {
		writeError(w, 409, "launch could not be admitted; existing conversation or authority conflicts")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 503, "launch commit uncertain; retry the exact action")
		return
	}
	// A lost wake is recovered by the native durable queue; this is only a hint.
	if task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(m.RunID)); err == nil && h.TaskService != nil {
		h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	}
	writeJSON(w, 200, m)
}

func fmtInt(n int32) string { b, _ := json.Marshal(n); return string(b) }

func (h *Handler) controllerManifestForTask(r *http.Request, task db.AgentTaskQueue) (*runcontrol.Manifest, error) {
	return h.controllerManifest(r, task, false)
}

func (h *Handler) controllerManifest(r *http.Request, task db.AgentTaskQueue, allowRevoked bool) (*runcontrol.Manifest, error) {
	if !task.IssueID.Valid {
		return nil, nil
	}
	owned, err := h.Queries.ControllerOwnsIssue(r.Context(), task.IssueID)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, nil
	}
	if h.DB == nil {
		return nil, errors.New("controller store unavailable")
	}
	var raw []byte
	var epoch int64
	var stopped bool
	var scope string
	err = h.DB.QueryRow(r.Context(), `SELECT r.manifest,c.authority_epoch,c.is_stopped,c.scope_revision FROM controlled_run r JOIN issue_controller c ON c.workspace_id=r.workspace_id AND c.issue_id=r.issue_id WHERE r.run_id=$1`, task.ID).Scan(&raw, &epoch, &stopped, &scope)
	if err != nil {
		return nil, errors.New("run has no controller launch authority")
	}
	var m runcontrol.Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if err = m.Validate(); err != nil {
		return nil, err
	}
	if ((!allowRevoked) && (stopped || m.AuthorityEpoch != epoch || m.ScopeRevision != scope)) || m.RunID != uuidToString(task.ID) || m.IssueID != uuidToString(task.IssueID) || m.ProfileID != uuidToString(task.AgentID) || m.RuntimeID != uuidToString(task.RuntimeID) {
		return nil, errors.New("controller launch authority revoked or mismatched")
	}
	return &m, nil
}

func (h *Handler) controllerTaskAccess(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue) bool {
	m, err := h.controllerManifest(r, task, true)
	// Terminal receipts remain writable after STOP; revocation blocks starts
	// and new effects, not the recording of what an already-launched run did.
	if err != nil {
		writeError(w, 409, err.Error())
		return false
	}
	if m == nil {
		return true
	}
	if middleware.DaemonIDFromContext(r.Context()) != m.HostID {
		writeError(w, 403, "controlled lifecycle requires its bound daemon credential")
		return false
	}
	return true
}

func (h *Handler) applyControllerClaim(r *http.Request, task db.AgentTaskQueue, resp *AgentTaskResponse, inputs *runcontrol.ProfileSnapshot) error {
	m, err := h.controllerManifestForTask(r, task)
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	if !requestHasClientCapability(r, protocol.DaemonCapabilityControllerV1) {
		return errors.New("daemon lacks controller launch enforcement")
	}
	if inputs == nil || inputs.Hash() != m.ProfileHash {
		return errors.New("profile changed after launch authority was sealed")
	}
	if middleware.DaemonIDFromContext(r.Context()) != m.HostID {
		return errors.New("controlled claim requires its bound daemon credential")
	}
	ref, _ := json.Marshal(map[string]any{"daemon_id": m.HostID, "local_path": m.SourcePath, "base_commit": m.BaseCommit, "execution_mode": "run_owned", "inherit_workspace_repositories": false})
	resp.ProjectResources = []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: ref}}
	// Source and provider session ownership follow the immutable run, never
	// today's shared resource or a prior conversation's working directory.
	resp.PriorSessionID = ""
	resp.PriorWorkDir = ""
	resp.WorkDir = ""
	resp.Repos = []RepoData{}
	for _, repo := range m.AllowedRepositories {
		resp.Repos = append(resp.Repos, RepoData{URL: repo.URL, Ref: repo.Commit})
	}
	resp.LaunchAuthority = m
	return nil
}
