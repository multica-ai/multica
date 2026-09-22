package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

type NodeRun struct {
	NodeID               string     `json:"node_id"`
	Status               string     `json:"status"`
	Generation           int        `json:"generation,omitempty"`
	ActivationNo         int        `json:"activation_no,omitempty"`
	ActivationID         string     `json:"activation_id,omitempty"`
	ScopeInstanceID      string     `json:"scope_instance_id,omitempty"`
	ScopeDefinitionID    string     `json:"scope_definition_id,omitempty"`
	IssueID              string     `json:"issue_id,omitempty"`
	TaskID               string     `json:"task_id,omitempty"`
	WorkItemID           string     `json:"work_item_id,omitempty"`
	Attempt              int        `json:"attempt"`
	AutomaticRetriesUsed int        `json:"automatic_retries_used,omitempty"`
	RetryAt              *time.Time `json:"retry_at,omitempty"`
	Output               string     `json:"output"`
	Error                string     `json:"error"`
	ReasonCode           string     `json:"reason_code,omitempty"`
	OutputID             string     `json:"output_id,omitempty"`
	ReplacesActivationID string     `json:"replaces_activation_id,omitempty"`
	ExecutionAccounted   bool       `json:"execution_accounted,omitempty"`
}
type Run struct {
	ID                 string         `json:"id"`
	WorkspaceID        string         `json:"workspace_id,omitempty"`
	WorkflowID         string         `json:"workflow_id"`
	WorkflowRevision   int64          `json:"workflow_revision"`
	ReleaseID          string         `json:"release_id,omitempty"`
	RootScopeID        string         `json:"root_scope_id,omitempty"`
	Mode               string         `json:"mode,omitempty"`
	StateRevision      int64          `json:"state_revision"`
	ReworkCounts       map[string]int `json:"rework_counts,omitempty"`
	OwnerID            string         `json:"owner_id,omitempty"`
	ReasonCode         string         `json:"reason_code,omitempty"`
	DispatchCount      int            `json:"dispatch_count,omitempty"`
	ActiveExecutionMS  int64          `json:"active_execution_ms,omitempty"`
	DeadlineAt         *time.Time     `json:"deadline_at,omitempty"`
	FinishedAt         *time.Time     `json:"finished_at,omitempty"`
	Graph              Graph          `json:"graph"`
	Input              string         `json:"input"`
	InputValues        map[string]any `json:"input_values,omitempty"`
	Status             string         `json:"status"`
	IssueID            string         `json:"issue_id"`
	Nodes              []NodeRun      `json:"nodes"`
	Output             string         `json:"output"`
	Error              string         `json:"error"`
	CurrentActivations []string       `json:"current_activations,omitempty"`
	PendingWorkItems   []string       `json:"pending_work_items,omitempty"`
	AllowedActions     []string       `json:"allowed_actions,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

func (s *Service) Start(ctx context.Context) {
	if s.Bus != nil {
		for _, kind := range []string{protocol.EventTaskCompleted, protocol.EventTaskFailed, "task:cancelled"} {
			s.Bus.Subscribe(kind, func(events.Event) { s.Wake() })
		}
	}
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		s.Wake()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-s.wake:
			}
			if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
				slog.Error("workflow reconciliation failed", "error", err)
			}
		}
	}()
}
func (s *Service) Reconcile(ctx context.Context) error {
	if err := s.ensureActiveRunJobs(ctx); err != nil {
		return err
	}
	jobs, err := s.claimWorkflowJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, err = s.advanceWithJob(ctx, job); err != nil {
			if errors.Is(err, errWorkflowLeaseLost) {
				continue
			}
			slog.Warn("workflow run reconciliation failed", "run_id", job.RunID, "error", err)
		}
	}
	if err := s.processWorkflowOutbox(ctx); err != nil {
		return err
	}
	return s.processChatTurns(ctx)
}

// processChatTurns applies the one structured edit protocol used by workflow
// builder chats. Chat itself remains an ordinary agent task; this small
// reconciler only acts after the assistant has written its final message. A
// turn is marked processed exactly once, and a revision conflict is recorded
// on the workflow instead of replacing newer user edits.
func (s *Service) processChatTurns(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT task_id::text FROM workflow_chat_turn WHERE processed_at IS NULL ORDER BY task_id LIMIT 100`)
	if err != nil {
		return err
	}
	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			rows.Close()
			return err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	type turn struct {
		workflowID, workspaceID, userID, taskID string
		baseRevision                            int64
	}
	for _, taskID := range taskIDs {
		tx, txErr := s.Tx.Begin(ctx)
		if txErr != nil {
			return txErr
		}
		var t turn
		txErr = tx.QueryRow(ctx, `SELECT workflow_id::text, workspace_id::text, user_id::text, task_id::text, base_revision
FROM workflow_chat_turn WHERE task_id=$1 AND processed_at IS NULL FOR UPDATE SKIP LOCKED`, uuid(taskID)).Scan(&t.workflowID, &t.workspaceID, &t.userID, &t.taskID, &t.baseRevision)
		if errors.Is(txErr, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			continue
		}
		if txErr != nil {
			tx.Rollback(ctx)
			return txErr
		}
		var assistantMessageID, content string
		err := tx.QueryRow(ctx, `SELECT id::text, content FROM chat_message WHERE task_id=$1 AND role='assistant' ORDER BY created_at DESC, id DESC LIMIT 1`, uuid(t.taskID)).Scan(&assistantMessageID, &content)
		if errors.Is(err, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			continue
		}
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		editErr := s.applyChatEditTx(ctx, tx, t.workflowID, t.workspaceID, t.userID, t.baseRevision, assistantMessageID, content)
		publish := strings.Contains(strings.ToLower(content), "<workflow_edit>")
		if editErr != nil {
			slog.Warn("workflow chat edit failed", "workflow_id", t.workflowID, "task_id", t.taskID, "error", editErr)
			if txErr = s.recordEditErrorTx(ctx, tx, t.workspaceID, t.workflowID, assistantMessageID, editErr); txErr != nil {
				tx.Rollback(ctx)
				return txErr
			}
			publish = true
		}
		if _, txErr = tx.Exec(ctx, `UPDATE workflow_chat_turn SET processed_at=now(), error=$2 WHERE task_id=$1 AND processed_at IS NULL`, uuid(t.taskID), errorText(editErr)); txErr != nil {
			tx.Rollback(ctx)
			return txErr
		}
		if txErr = tx.Commit(ctx); txErr != nil {
			return txErr
		}
		if publish {
			s.publish(protocol.EventWorkflowUpdated, t.workspaceID, t.workflowID, "")
		}
	}
	return nil
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func (s *Service) applyChatEdit(ctx context.Context, wid, ws, user string, revision int64, messageID, content string) error {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.applyChatEditTx(ctx, tx, wid, ws, user, revision, messageID, content); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(content), "<workflow_edit>") {
		s.publish(protocol.EventWorkflowUpdated, ws, wid, "")
	}
	return nil
}

func (s *Service) applyChatEditTx(ctx context.Context, tx pgx.Tx, wid, ws, user string, revision int64, messageID, content string) error {
	start := strings.Index(strings.ToLower(content), "<workflow_edit>")
	if start < 0 {
		return nil
	}
	if messageID != "" {
		var applied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workflow_version WHERE workflow_id=$1 AND source_message_id=$2)`, wid, uuid(messageID)).Scan(&applied); err != nil {
			return err
		}
		if applied {
			return nil
		}
	}
	rest := content[start+len("<workflow_edit>"):]
	end := strings.Index(strings.ToLower(rest), "</workflow_edit>")
	if end < 0 {
		return Bad("workflow edit block is incomplete")
	}
	var edit Edit
	if err := json.Unmarshal([]byte(strings.TrimSpace(rest[:end])), &edit); err != nil {
		return Bad("workflow edit block is invalid JSON")
	}
	if edit.ExpectedRevision == 0 {
		edit.ExpectedRevision = revision
	}
	if edit.ExpectedRevision != revision {
		return Conflict("workflow changed while the assistant was editing it")
	}
	_, err := s.editTx(ctx, tx, ws, user, wid, edit, "chat", messageID)
	return err
}
func getRun(ctx context.Context, q db.DBTX, ws, wid, rid string, lock bool) (Run, string, error) {
	sql := "SELECT body,creator_id::text,workspace_id::text FROM workflow_run WHERE workspace_id=$1 AND workflow_id=$2 AND id=$3"
	if lock {
		sql += " FOR UPDATE"
	}
	var r Run
	var raw []byte
	var user string
	var workspaceID string
	err := q.QueryRow(ctx, sql, ws, wid, rid).Scan(&raw, &user, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, user, &Error{Status: 404, Message: "run not found", Code: "run_not_found"}
	}
	if err != nil {
		return r, user, err
	}
	err = json.Unmarshal(raw, &r)
	if err == nil {
		r.WorkspaceID = workspaceID
		populateRunResponse(&r)
	}
	return r, user, err
}

func getRunByID(ctx context.Context, q db.DBTX, ws, rid string, lock bool) (Run, string, error) {
	sql := "SELECT body,creator_id::text,workspace_id::text FROM workflow_run WHERE workspace_id=$1 AND id=$2"
	if lock {
		sql += " FOR UPDATE"
	}
	var r Run
	var raw []byte
	var user string
	var workspaceID string
	err := q.QueryRow(ctx, sql, ws, rid).Scan(&raw, &user, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, user, &Error{Status: 404, Message: "run not found", Code: "run_not_found"}
	}
	if err != nil {
		return r, user, err
	}
	err = json.Unmarshal(raw, &r)
	if err == nil {
		r.WorkspaceID = workspaceID
		populateRunResponse(&r)
	}
	return r, user, err
}
func (s *Service) GetRun(ctx context.Context, ws, wid, rid string) (Run, error) {
	r, _, err := getRun(ctx, s.DB, ws, wid, rid, false)
	return r, err
}
func (s *Service) ListRuns(ctx context.Context, ws, wid string) ([]Run, error) {
	if _, err := s.Get(ctx, ws, wid); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, "SELECT body FROM workflow_run WHERE workspace_id=$1 AND workflow_id=$2 ORDER BY created_at DESC LIMIT 100", ws, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r Run
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		r.WorkspaceID = ws
		populateRunResponse(&r)
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) StartRun(ctx context.Context, ws, user, wid, input, key string, revision int64) (Run, error) {
	return s.startRun(ctx, ws, user, wid, input, key, revision, "", "production")
}

func (s *Service) StartRunFromRelease(ctx context.Context, ws, user, wid, releaseID, input, key string) (Run, error) {
	return s.startRun(ctx, ws, user, wid, input, key, 0, releaseID, "production")
}

func (s *Service) startRun(ctx context.Context, ws, user, wid, input, key string, revision int64, releaseID, mode string) (Run, error) {
	if key == "" || len(key) > 200 {
		return Run{}, Bad("idempotency_key is required")
	}
	if len(input) > 100000 {
		return Run{}, Bad("input is too large")
	}
	if releaseID != "" {
		if _, err := util.ParseUUID(releaseID); err != nil {
			return Run{}, Bad("invalid release id")
		}
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	// Issue numbers and workflow lifecycle changes take the workspace lock first.
	q := db.New(tx)
	if _, err = q.LockWorkspaceForDelete(ctx, uuid(ws)); err != nil {
		return Run{}, err
	}
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return Run{}, err
	}
	graph := w.Graph
	workflowRevision := w.Revision
	if releaseID != "" {
		_, releasedGraph, _, releaseErr := loadRelease(ctx, tx, ws, wid, releaseID)
		if releaseErr != nil {
			return Run{}, releaseErr
		}
		graph = releasedGraph
		var draftRevision int64
		if err = tx.QueryRow(ctx, "SELECT draft_revision FROM workflow_release WHERE workspace_id=$1 AND workflow_id=$2 AND id=$3", ws, wid, releaseID).Scan(&draftRevision); err != nil {
			return Run{}, err
		}
		workflowRevision = draftRevision
	} else if mode == "production" && revision == 0 {
		return Run{}, Bad("expected_revision is required when starting a draft run")
	}
	requestRevision := int64(0)
	if releaseID == "" {
		requestRevision = revision
	}
	requestHash := runRequestHash(wid, releaseID, mode, input, requestRevision)
	var existing []byte
	var existingHash *string
	err = tx.QueryRow(ctx, "SELECT body,request_hash FROM workflow_run WHERE workflow_id=$1 AND creator_id=$2 AND idempotency_key=$3", wid, user, key).Scan(&existing, &existingHash)
	if err == nil {
		var r Run
		err = json.Unmarshal(existing, &r)
		if err == nil {
			existingMode := r.Mode
			if existingMode == "" {
				existingMode = "production"
			}
			if existingHash == nil || *existingHash == "" {
				if legacyRunRequestHash(wid, r.ReleaseID, existingMode, r.Input) != legacyRunRequestHash(wid, releaseID, mode, input) {
					return Run{}, Conflict("idempotency key was already used for a different run request")
				}
			} else if *existingHash != requestHash && *existingHash != legacyRunRequestHash(wid, releaseID, mode, input) {
				return Run{}, Conflict("idempotency key was already used for a different run request")
			}
			r.WorkspaceID = ws
			populateRunResponse(&r)
		}
		return r, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Run{}, err
	}
	if releaseID == "" && w.Revision != revision {
		return Run{}, Conflict("save the latest workflow before running")
	}
	if err = requireV2Graph(graph); err != nil {
		return Run{}, err
	}
	if releaseID == "" && len(w.UpgradeIssues) > 0 {
		return Run{}, &Error{Status: 422, Code: "upgrade_required", Message: "repair the workflow upgrade issues before running", Details: append([]ValidationDetail(nil), w.UpgradeIssues...)}
	}
	if err = Validate(graph, true); err != nil {
		return Run{}, err
	}
	if err = validateRunInput(graph, input); err != nil {
		return Run{}, err
	}
	if err = s.validateRunInputArtifacts(ctx, ws, user, graph, input); err != nil {
		return Run{}, err
	}
	if err = s.validateAgents(ctx, ws, user, graph, true); err != nil {
		return Run{}, err
	}
	if err = s.validateMembers(ctx, ws, graph, true); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	var deadlineAt *time.Time
	if graph.Defaults.RunTimeoutSeconds > 0 {
		deadline := now.Add(time.Duration(graph.Defaults.RunTimeoutSeconds) * time.Second)
		deadlineAt = &deadline
	}
	r := Run{ID: id(), WorkspaceID: ws, WorkflowID: wid, WorkflowRevision: workflowRevision, ReleaseID: releaseID, Mode: mode, StateRevision: 1, OwnerID: user, DeadlineAt: deadlineAt, Graph: graph, Input: input, Status: "queued", Nodes: []NodeRun{}, RootScopeID: id(), CreatedAt: now, UpdatedAt: now}
	parent, err := s.createIssue(ctx, tx, ws, user, r.ID, w.Name, "Workflow run input:\n"+input, "", "")
	if err != nil {
		return r, err
	}
	r.IssueID = util.UUIDToString(parent.ID)
	for _, n := range graph.Nodes {
		nr := NodeRun{NodeID: n.ID, Status: "pending", Generation: 1, ActivationNo: 1, ScopeDefinitionID: "root"}
		if n.Type == "start" {
			nr.Status = "succeeded"
			nr.Output = input
			nr.ActivationID = id()
			nr.ScopeInstanceID = r.RootScopeID
		}
		r.Nodes = append(r.Nodes, nr)
	}
	if err = ensureRootScope(ctx, tx, &r); err != nil {
		return r, err
	}
	inputValues := map[string]any{"text": input}
	var structuredInput map[string]any
	if json.Unmarshal([]byte(input), &structuredInput) == nil && structuredInput != nil {
		inputValues = structuredInput
	}
	r.InputValues = inputValues
	_, err = tx.Exec(ctx, `INSERT INTO workflow_run(id,workflow_id,workspace_id,creator_id,idempotency_key,status,body,engine_version,release_id,mode,input_values,owner_id,state_revision,request_hash,deadline_at,finished_at,reason_code)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, r.ID, wid, ws, user, key, r.Status, body(r), 2, nullableUUID(releaseID), mode, body(inputValues), user, r.StateRevision, requestHash, r.DeadlineAt, r.FinishedAt, nullableText(r.ReasonCode))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Another request may have won the same idempotency key while this
			// transaction was building its issue tree. Roll back before reading
			// the committed winner because PostgreSQL marks this transaction
			// aborted after the uniqueness violation.
			_ = tx.Rollback(ctx)
			var raw []byte
			if lookupErr := s.DB.QueryRow(ctx, "SELECT body FROM workflow_run WHERE workflow_id=$1 AND creator_id=$2 AND idempotency_key=$3", wid, user, key).Scan(&raw); lookupErr == nil {
				var existingRun Run
				if unmarshalErr := json.Unmarshal(raw, &existingRun); unmarshalErr == nil {
					existingRun.WorkspaceID = ws
					populateRunResponse(&existingRun)
					return existingRun, nil
				}
			}
		}
		return r, err
	}
	if err = s.ensureRunJob(ctx, tx, ws, r.ID, nextRunDue(r)); err != nil {
		return r, err
	}
	if _, err = appendWorkflowEvent(ctx, tx, r, protocol.EventWorkflowRunUpdated, "member", user, map[string]any{
		"status":         r.Status,
		"state_revision": r.StateRevision,
		"reason_code":    r.ReasonCode,
	}); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	s.publish(protocol.EventWorkflowRunUpdated, ws, wid, r.ID)
	s.publish(protocol.EventIssueCreated, ws, wid, r.ID)
	s.Wake()
	return r, nil
}

func runRequestHash(workflowID, releaseID, mode, input string, expectedRevision int64) string {
	payload, _ := json.Marshal(struct {
		WorkflowID       string `json:"workflow_id"`
		ReleaseID        string `json:"release_id"`
		Mode             string `json:"mode"`
		Input            string `json:"input"`
		ExpectedRevision int64  `json:"expected_revision"`
	}{workflowID, releaseID, mode, input, expectedRevision})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func legacyRunRequestHash(workflowID, releaseID, mode, input string) string {
	payload, _ := json.Marshal(struct {
		WorkflowID string `json:"workflow_id"`
		ReleaseID  string `json:"release_id"`
		Mode       string `json:"mode"`
		Input      string `json:"input"`
	}{workflowID, releaseID, mode, input})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func populateRunResponse(r *Run) {
	if r.StateRevision <= 0 {
		r.StateRevision = 1
	}
	if r.Mode == "" {
		r.Mode = "production"
	}
	if r.InputValues == nil {
		r.InputValues = map[string]any{"text": r.Input}
		var structured map[string]any
		if json.Unmarshal([]byte(r.Input), &structured) == nil && structured != nil {
			r.InputValues = structured
		}
	}
	r.CurrentActivations = r.CurrentActivations[:0]
	r.PendingWorkItems = r.PendingWorkItems[:0]
	for _, node := range r.Nodes {
		if node.ActivationID != "" && !terminal(node.Status) && node.Status != "skipped" {
			r.CurrentActivations = append(r.CurrentActivations, node.ActivationID)
		}
		if node.WorkItemID != "" && node.Status == "waiting_human" {
			r.PendingWorkItems = append(r.PendingWorkItems, node.WorkItemID)
		}
	}
	r.AllowedActions = allowedRunActions(*r)
}

func allowedRunActions(r Run) []string {
	if terminal(r.Status) {
		return nil
	}
	actions := []string{"cancel", "terminate"}
	hasFailed, hasBlocked := false, false
	for _, node := range r.Nodes {
		switch node.Status {
		case "failed":
			hasFailed = true
		case "blocked":
			hasBlocked = true
		}
	}
	if hasFailed {
		actions = append(actions, "retry")
	}
	if hasBlocked || r.Status == "blocked" {
		actions = append(actions, "takeover", "resolve")
	}
	return actions
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return uuid(value)
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func (s *Service) createIssue(ctx context.Context, tx pgx.Tx, ws, user, run, title, description, agent, parent string) (db.Issue, error) {
	q := db.New(tx)
	policy := service.ResolveIssueCountPolicy(ctx, s.Tasks.Entitlements, uuid(ws))
	number, err := service.AllocateIssueNumber(ctx, q, uuid(ws), policy)
	if err != nil {
		return db.Issue{}, err
	}
	p := db.CreateIssueWithOriginParams{ID: dbid.NewV7(), WorkspaceID: uuid(ws), CreatorID: uuid(user), CreatorType: "member", Title: title, Description: pgtype.Text{String: description, Valid: true}, Priority: "medium", Status: "todo", Number: number, OriginType: pgtype.Text{String: "workflow", Valid: true}, OriginID: uuid(run), ParentIssueID: uuid(parent)}
	if agent != "" {
		p.AssigneeType = pgtype.Text{String: "agent", Valid: true}
		p.AssigneeID = uuid(agent)
	}
	return q.CreateIssueWithOrigin(ctx, p)
}
func saveRun(ctx context.Context, tx pgx.Tx, r *Run) error {
	r.UpdatedAt = time.Now().UTC()
	if r.StateRevision <= 0 {
		r.StateRevision = 1
	}
	if terminal(r.Status) && r.FinishedAt == nil {
		now := time.Now().UTC()
		r.FinishedAt = &now
	}
	if !terminal(r.Status) {
		r.FinishedAt = nil
	}
	if err := persistNormalizedRun(ctx, tx, r); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE workflow_run SET status=$2,body=$3,state_revision=$4,updated_at=now(),finished_at=$5,reason_code=$6,deadline_at=$7,owner_id=$8,active_execution_ms=$9 WHERE id=$1", r.ID, r.Status, body(r), r.StateRevision, r.FinishedAt, nullableText(r.ReasonCode), r.DeadlineAt, nullableUUID(r.OwnerID), r.ActiveExecutionMS)
	if err != nil {
		return err
	}
	for _, n := range r.Nodes {
		if n.Attempt == 0 {
			continue
		}
		_, err = tx.Exec(ctx, "INSERT INTO workflow_node_run(run_id,node_id,attempt,body) VALUES($1,$2,$3,$4) ON CONFLICT(run_id,node_id,attempt) DO UPDATE SET body=EXCLUDED.body", r.ID, n.NodeID, n.Attempt, body(n))
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Cancel(ctx context.Context, ws, wid, rid string) (Run, error) {
	return s.advanceWithRevision(ctx, ws, wid, rid, "cancel", "", 0)
}

func (s *Service) CancelWithRevision(ctx context.Context, ws, actor, wid, rid string, expectedRevision int64) (Run, error) {
	return s.advanceWithRevisionActor(ctx, ws, actor, wid, rid, "cancel", "", expectedRevision)
}

func (s *Service) CancelWithRevisionKey(ctx context.Context, ws, actor, wid, rid string, expectedRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	return s.advanceWithRevisionReasonActorKey(ctx, ws, actor, wid, rid, "cancel", "", expectedRevision, "", idempotencyKey)
}

func (s *Service) TerminateWithRevision(ctx context.Context, ws, actor, wid, rid, reason string, expectedRevision int64) (Run, error) {
	return s.advanceWithRevisionReasonActor(ctx, ws, actor, wid, rid, "terminate", "", expectedRevision, reason)
}

func (s *Service) TerminateWithRevisionKey(ctx context.Context, ws, actor, wid, rid, reason string, expectedRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	return s.advanceWithRevisionReasonActorKey(ctx, ws, actor, wid, rid, "terminate", "", expectedRevision, reason, idempotencyKey)
}
func (s *Service) Retry(ctx context.Context, ws, user, wid, rid, node string) (Run, error) {
	r, err := s.GetRun(ctx, ws, wid, rid)
	if err != nil {
		return r, err
	}
	if err = s.validateAgents(ctx, ws, user, r.Graph, true); err != nil {
		return r, err
	}
	return s.advanceWithRevision(ctx, ws, wid, rid, "retry", node, 0)
}

func (s *Service) RetryWithRevision(ctx context.Context, ws, user, wid, rid, node string, expectedRevision int64) (Run, error) {
	r, err := s.GetRun(ctx, ws, wid, rid)
	if err != nil {
		return r, err
	}
	if err = s.validateAgents(ctx, ws, user, r.Graph, true); err != nil {
		return r, err
	}
	return s.advanceWithRevisionActor(ctx, ws, user, wid, rid, "retry", node, expectedRevision)
}

func (s *Service) RetryWithRevisionKey(ctx context.Context, ws, user, wid, rid, node string, expectedRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	r, err := s.GetRun(ctx, ws, wid, rid)
	if err != nil {
		return r, err
	}
	return s.advanceWithRevisionReasonActorKey(ctx, ws, user, wid, rid, "retry", node, expectedRevision, "", idempotencyKey)
}
func terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}
func (s *Service) advance(ctx context.Context, ws, wid, rid, action, node string) (Run, error) {
	return s.advanceWithRevision(ctx, ws, wid, rid, action, node, 0)
}

func (s *Service) advanceWithRevision(ctx context.Context, ws, wid, rid, action, node string, expectedRevision int64) (Run, error) {
	return s.advanceWithRevisionActor(ctx, ws, "", wid, rid, action, node, expectedRevision)
}

func (s *Service) advanceWithRevisionReason(ctx context.Context, ws, wid, rid, action, node string, expectedRevision int64, reason string) (Run, error) {
	return s.advanceWithRevisionReasonActor(ctx, ws, "", wid, rid, action, node, expectedRevision, reason)
}

func (s *Service) advanceWithRevisionActor(ctx context.Context, ws, actor, wid, rid, action, node string, expectedRevision int64) (Run, error) {
	return s.advanceWithRevisionReasonActor(ctx, ws, actor, wid, rid, action, node, expectedRevision, "")
}

func (s *Service) advanceWithRevisionReasonActor(ctx context.Context, ws, actor, wid, rid, action, node string, expectedRevision int64, reason string) (Run, error) {
	return s.advanceWithRevisionReasonActorLease(ctx, ws, actor, wid, rid, action, node, expectedRevision, reason, nil)
}

func (s *Service) advanceWithJob(ctx context.Context, job workflowJob) (Run, error) {
	return s.advanceWithRevisionReasonActorLease(ctx, job.WorkspaceID, "", job.WorkflowID, job.RunID, "", "", 0, "", &workflowLease{ID: job.ID, Owner: s.workerID, Fence: job.Fence})
}

func (s *Service) advanceWithRevisionReasonActorLease(ctx context.Context, ws, actor, wid, rid, action, node string, expectedRevision int64, reason string, lease *workflowLease) (Run, error) {
	return s.advanceWithRevisionReasonActorLeaseKey(ctx, ws, actor, wid, rid, action, node, expectedRevision, reason, "", lease)
}

func (s *Service) advanceWithRevisionReasonActorKey(ctx context.Context, ws, actor, wid, rid, action, node string, expectedRevision int64, reason, idempotencyKey string) (Run, error) {
	return s.advanceWithRevisionReasonActorLeaseKey(ctx, ws, actor, wid, rid, action, node, expectedRevision, reason, idempotencyKey, nil)
}

func (s *Service) advanceWithRevisionReasonActorLeaseKey(ctx context.Context, ws, actor, wid, rid, action, node string, expectedRevision int64, reason, idempotencyKey string, lease *workflowLease) (Run, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	r, user, err := getRun(ctx, tx, ws, wid, rid, true)
	if err != nil {
		return r, err
	}
	if lease != nil {
		if err = s.lockWorkflowJob(ctx, tx, *lease); err != nil {
			return r, err
		}
	}
	if actor != "" && (action == "cancel" || action == "terminate" || action == "retry") {
		if err = requireRunManager(ctx, tx, ws, actor, r.OwnerID, user); err != nil {
			return r, err
		}
	}
	commandHash := ""
	if strings.TrimSpace(idempotencyKey) != "" {
		commandHash = workflowCommandHash("workflow_run."+action, rid, struct {
			Node             string `json:"node"`
			Reason           string `json:"reason"`
			ExpectedRevision int64  `json:"expected_state_revision"`
		}{Node: node, Reason: reason, ExpectedRevision: expectedRevision})
		replay, commandErr := beginWorkflowRunCommand(ctx, tx, ws, actor, "workflow_run."+action, rid, idempotencyKey, commandHash)
		if commandErr != nil {
			return r, commandErr
		}
		if replay != nil {
			return *replay, nil
		}
	}
	if expectedRevision > 0 && expectedRevision != stateRevision(r) {
		return r, Conflict("run changed; refresh before applying this action")
	}
	if action == "retry" && actor != "" {
		if err = s.validateAgents(ctx, ws, actor, r.Graph, true); err != nil {
			return r, err
		}
	}
	before := string(body(r))
	q := db.New(tx)
	var cancelled, queued []db.AgentTaskQueue
	retryOf := map[string]string{}
	if r.OwnerID == "" {
		r.OwnerID = user
	}
	if action == "" {
		if err = expireOpenWorkItems(ctx, tx, &r); err != nil {
			return r, err
		}
		for index := range r.Nodes {
			if r.Nodes[index].Status != "blocked" || r.Nodes[index].ReasonCode != "human_timeout" {
				continue
			}
			definition := nodeDefinition(r.Graph, r.Nodes[index].NodeID)
			if err = s.applyTimeoutPolicy(ctx, tx, ws, user, &r, &r.Nodes[index], definition); err != nil {
				return r, err
			}
		}
	}
	timedOut := action == "" && r.DeadlineAt != nil && time.Now().UTC().After(*r.DeadlineAt) && !terminal(r.Status)
	if timedOut {
		r.Status, r.ReasonCode, r.Error = "blocked", "run_timeout", "workflow run exceeded its deadline"
		for index := range r.Nodes {
			node := &r.Nodes[index]
			if terminal(node.Status) || node.Status == "skipped" {
				continue
			}
			if node.IssueID != "" {
				cancelledTasks, cancelErr := q.CancelAgentTasksByIssue(ctx, uuid(node.IssueID))
				if cancelErr != nil {
					return r, cancelErr
				}
				cancelled = append(cancelled, cancelledTasks...)
			}
			node.Status = "blocked"
			node.ReasonCode = "run_timeout"
			node.Error = "workflow run exceeded its deadline"
		}
	}
	if action == "cancel" {
		if r.Status == "succeeded" {
			return r, Conflict("completed runs cannot be cancelled")
		}
		r.Status = "cancelled"
	}
	if action == "terminate" {
		if terminal(r.Status) {
			return r, Conflict("completed runs cannot be terminated")
		}
		r.Status = "failed"
		r.ReasonCode = "terminated"
		if strings.TrimSpace(reason) != "" {
			r.Error = strings.TrimSpace(reason)
		} else {
			r.Error = "workflow run was terminated by a human"
		}
	}
	if action == "retry" {
		if r.Status == "cancelled" || r.Status == "succeeded" {
			return r, Conflict("run cannot be retried")
		}
		found := false
		for i := range r.Nodes {
			n := &r.Nodes[i]
			if n.NodeID == node {
				found = true
				if n.Status != "failed" {
					return r, Conflict("only failed nodes can be retried")
				}
				retryOf[n.NodeID] = n.TaskID
				replaceNodeActivation(n, false)
				n.Status = "pending"
				n.Error = ""
			}
		}
		if !found {
			return r, Bad("node not found")
		}
		r.Status = "running"
	}
	if r.Status == "succeeded" {
		return r, nil
	}
	if r.Status == "cancelled" || action == "terminate" {
		if _, err = tx.Exec(ctx, `UPDATE workflow_work_item
	SET status='closed',decision='cancelled',version=version+1,updated_at=now()
	WHERE run_id=$1 AND status='open'`, r.ID); err != nil {
			return r, err
		}
		for i := range r.Nodes {
			n := &r.Nodes[i]
			if n.IssueID != "" {
				tasks, e := q.CancelAgentTasksByIssue(ctx, uuid(n.IssueID))
				if e != nil {
					return r, e
				}
				cancelled = append(cancelled, tasks...)
			}
			if n.Status != "succeeded" {
				if action == "terminate" {
					n.Status = "failed"
					n.ReasonCode = "terminated"
				} else {
					n.Status = "cancelled"
				}
				if n.IssueID != "" {
					issueStatus := "cancelled"
					if action == "terminate" {
						issueStatus = "blocked"
					}
					if _, err = q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: uuid(n.IssueID), WorkspaceID: uuid(ws), Status: issueStatus}); err != nil {
						return r, err
					}
				}
			}
		}
		if r.IssueID != "" {
			issueStatus := "cancelled"
			if action == "terminate" {
				issueStatus = "blocked"
			}
			if _, err = q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: uuid(r.IssueID), WorkspaceID: uuid(ws), Status: issueStatus}); err != nil {
				return r, err
			}
		}
	} else if !timedOut {
		// Follow only retry_of_task_id lineage; unrelated/manual tasks cannot advance a node.
		for i := range r.Nodes {
			n := &r.Nodes[i]
			if n.Status == "retry_wait" {
				if n.RetryAt != nil && time.Now().UTC().Before(*n.RetryAt) {
					continue
				}
				retryOf[n.NodeID] = n.TaskID
				n.Status, n.TaskID, n.RetryAt, n.ExecutionAccounted = "pending", "", nil, false
				continue
			}
			if n.TaskID == "" || n.Status == "succeeded" {
				continue
			}
			task, e := latestTask(ctx, tx, n.TaskID)
			if e != nil {
				return r, e
			}
			definition := nodeDefinition(r.Graph, n.NodeID)
			if timeoutReason, timeoutError := taskTimeoutReason(task, n.Status, r.Graph.Defaults); timeoutReason != "" {
				accountTaskExecution(&r, n, task, time.Now().UTC())
				if n.IssueID != "" {
					cancelledTasks, cancelErr := q.CancelAgentTasksByIssue(ctx, uuid(n.IssueID))
					if cancelErr != nil {
						return r, cancelErr
					}
					cancelled = append(cancelled, cancelledTasks...)
				}
				n.Status, n.ReasonCode, n.Error = "failed", timeoutReason, timeoutError
				if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, n, definition, nil); policyErr != nil {
					return r, policyErr
				}
				continue
			}
			n.TaskID = util.UUIDToString(task.ID)
			switch task.Status {
			case "completed":
				accountTaskExecution(&r, n, task, time.Now().UTC())
				n.Status = "succeeded"
				n.Error = ""
				var result struct {
					Output string `json:"output"`
				}
				if len(task.Result) > 0 {
					if e = json.Unmarshal(task.Result, &result); e != nil {
						n.Status, n.ReasonCode = "failed", "output_invalid"
						n.Error = "invalid task output"
						if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, n, definition, nil); policyErr != nil {
							return r, policyErr
						}
					}
				}
				n.Output = result.Output
				if n.Status == "succeeded" {
					if _, e = q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: uuid(n.IssueID), WorkspaceID: uuid(ws), Status: "done"}); e != nil {
						return r, e
					}
				}
			case "failed", "cancelled":
				accountTaskExecution(&r, n, task, time.Now().UTC())
				n.Error = task.Error.String
				if n.Error == "" {
					n.Error = "agent execution " + task.Status
				}
				definition := nodeDefinition(r.Graph, n.NodeID)
				policy := retryPolicyForNode(r.Graph, definition)
				if task.Status == "failed" && ShouldRetry(classifyTaskError(n.Error), policy, n.AutomaticRetriesUsed) {
					n.Status = "retry_wait"
					n.AutomaticRetriesUsed++
					delay := RetryDelay(policy, n.AutomaticRetriesUsed, rand.Float64()*2-1)
					retryAt := time.Now().UTC().Add(delay)
					n.RetryAt, n.ReasonCode = &retryAt, "automatic_retry_scheduled"
				} else {
					n.Status = "failed"
					n.ReasonCode = classifyTaskError(n.Error)
					if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, n, definition, nil); policyErr != nil {
						return r, policyErr
					}
				}
			case "running", "dispatched":
				n.Status = "running"
			default:
				n.Status = "queued"
			}
		}
		states := map[string]*NodeRun{}
		definitions := map[string]Node{}
		for i := range r.Nodes {
			states[r.Nodes[i].NodeID] = &r.Nodes[i]
		}
		for _, n := range r.Graph.Nodes {
			definitions[n.ID] = n
		}
		for i := range r.Nodes {
			if r.Nodes[i].Status != "succeeded" {
				continue
			}
			if err = validateNodeOutput(definitions[r.Nodes[i].NodeID], r.Nodes[i].Output); err != nil {
				r.Nodes[i].Status = "failed"
				r.Nodes[i].ReasonCode = "output_invalid"
				r.Nodes[i].Error = err.Error()
				if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, &r.Nodes[i], definitions[r.Nodes[i].NodeID], states); policyErr != nil {
					return r, policyErr
				}
			} else if err = s.validateNodeOutputArtifacts(ctx, ws, user, definitions[r.Nodes[i].NodeID], r.Nodes[i].Output); err != nil {
				r.Nodes[i].Status = "failed"
				r.Nodes[i].ReasonCode = "output_attachment_invalid"
				r.Nodes[i].Error = err.Error()
				if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, &r.Nodes[i], definitions[r.Nodes[i].NodeID], states); policyErr != nil {
					return r, policyErr
				}
			}
		}
		// Multiple passes handle end/start nodes irrespective of canvas array order.
		for pass := 0; pass < len(r.Nodes); pass++ {
			changed := false
			for i := range r.Nodes {
				n := &r.Nodes[i]
				if n.Status != "pending" && n.Status != "blocked" {
					continue
				}
				ready, blocked, allSettled, hasSuccess := true, false, true, false
				for _, e := range r.Graph.Edges {
					if !normalFlowEdge(e) || e.Target != n.NodeID {
						continue
					}
					up := states[e.Source]
					if up == nil {
						ready, allSettled, blocked = false, false, true
						continue
					}
					switch up.Status {
					case "succeeded":
						hasSuccess = true
					case "skipped":
						// A non-selected condition branch is settled but does not
						// satisfy an ordinary node by itself.
					case "failed", "blocked", "cancelled":
						if isFailureEdge(e) && up.ReasonCode == "failure_routed" {
							hasSuccess = true
						} else {
							blocked = true
						}
					default:
						ready, allSettled = false, false
					}
				}
				if blocked {
					n.Status = "blocked"
					n.ReasonCode = "upstream_blocked"
					continue
				}
				n.Status = "pending"
				if !ready || !allSettled {
					continue
				}
				if len(incomingEdges(r.Graph, n.NodeID)) > 0 && !hasSuccess {
					n.Status = "skipped"
					n.ReasonCode = "condition_not_selected"
					changed = true
					continue
				}
				def := definitions[n.NodeID]
				ensureNodeActivation(n, r.RootScopeID)
				if def.Type == "end" {
					if n.Generation == 0 {
						n.Generation = 1
					}
					n.Status = "succeeded"
					n.Output = inputs(r, def, states)
					r.Output = n.Output
					changed = true
					continue
				}
				switch def.Type {
				case "start", "parallel", "merge":
					if n.Generation == 0 {
						n.Generation = 1
					}
					n.Status = "succeeded"
					n.Output = inputs(r, def, states)
					changed = true
					continue
				case "condition":
					if n.Generation == 0 {
						n.Generation = 1
					}
					n.Status = "succeeded"
					n.Output = inputs(r, def, states)
					chosen, conditionErr := chooseConditionEdgeForRun(def, r.Graph.Edges, r, states)
					if conditionErr != nil {
						n.Status, n.ReasonCode, n.Error = "blocked", "condition_input_missing", conditionErr.Error()
						changed = true
						continue
					}
					for _, edge := range r.Graph.Edges {
						if edge.Source == def.ID && edge.Kind != "rework" && edge.Kind != "compensation" && edge.Target != chosen.Target {
							markSkipped(states, edge.Target)
						}
					}
					changed = true
					continue
				case "human_task", "human_review":
					if n.Generation == 0 {
						n.Generation = 1
					}
					ensureNodeActivation(n, r.RootScopeID)
					if n.WorkItemID == "" {
						itemID, createErr := createWorkItem(ctx, tx, ws, r.ID, n.NodeID, n.ActivationID, def, r.Input, states, r.Graph.Defaults.HumanTimeoutSeconds)
						if createErr != nil {
							n.Status, n.ReasonCode, n.Error = "blocked", "work_item_create_failed", createErr.Error()
							changed = true
							continue
						}
						n.WorkItemID = itemID
					}
					n.Status, n.ReasonCode = "waiting_human", "awaiting_human"
					changed = true
					continue
				}
				agent := nodeAssignee(def)
				if s.ValidateAgent != nil {
					if agent == nil || agent.Type != "agent" {
						n.Status, n.ReasonCode, n.Error = "blocked", "assignee_required", "agent assignee is missing"
						changed = true
						continue
					}
					if e := s.ValidateAgent(ctx, ws, user, agent.ID); e != nil {
						n.Status = "failed"
						n.ReasonCode = "assignee_unavailable"
						n.Error = e.Error()
						changed = true
						continue
					}
				}
				if n.IssueID == "" {
					issue, createErr := s.createIssue(ctx, tx, ws, user, r.ID, def.Label, nodeInstructions(def), nodeAssigneeID(def), r.IssueID)
					if createErr != nil {
						n.Status, n.ReasonCode, n.Error = "failed", "issue_create_failed", createErr.Error()
						if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, n, def, states); policyErr != nil {
							return r, policyErr
						}
						changed = true
						continue
					}
					n.IssueID = util.UUIDToString(issue.ID)
				}
				issue, e := q.GetIssue(ctx, uuid(n.IssueID))
				if e != nil {
					return r, e
				}
				if r.Graph.Defaults.MaxDispatches > 0 && r.DispatchCount >= r.Graph.Defaults.MaxDispatches {
					n.Status, n.ReasonCode, n.Error = "blocked", "dispatch_budget_exceeded", "workflow dispatch budget has been exhausted"
					changed = true
					continue
				}
				if r.Graph.Defaults.MaxActiveExecutionSeconds > 0 && r.ActiveExecutionMS >= int64(r.Graph.Defaults.MaxActiveExecutionSeconds)*1000 {
					n.Status, n.ReasonCode, n.Error = "blocked", "active_execution_budget_exceeded", "workflow active execution budget has been exhausted"
					changed = true
					continue
				}
				prompt := "This is a workflow node. Execute only this node's instructions. Do not dispatch or advance other workflow tasks. Return your final output and accessible artifact URLs. Local paths are not shared with other agents.\n\nInstructions:\n" + nodeInstructions(def) + "\n\n" + inputs(r, def, states)
				taskService := s.Tasks.WorkflowTransaction(tx)
				var task db.AgentTaskQueue
				if previousTaskID := retryOf[n.NodeID]; previousTaskID != "" {
					task, e = taskService.EnqueueWorkflowRetryTask(ctx, issue, prompt, uuid(user), uuid(previousTaskID))
				} else {
					task, e = taskService.EnqueueWorkflowTask(ctx, issue, prompt, uuid(user))
				}
				if e != nil {
					n.Status = "failed"
					n.ReasonCode = "dispatch_failed"
					n.Error = e.Error()
					if policyErr := s.applyFailurePolicy(ctx, tx, ws, user, &r, n, def, states); policyErr != nil {
						return r, policyErr
					}
					changed = true
					continue
				}
				n.Attempt++
				if n.Generation == 0 {
					n.Generation = 1
				}
				ensureNodeActivation(n, r.RootScopeID)
				r.DispatchCount++
				n.TaskID = util.UUIDToString(task.ID)
				n.Status = "queued"
				n.Error = ""
				queued = append(queued, task)
				changed = true
			}
			if !changed {
				break
			}
		}
		r.Status = "succeeded"
		hasActive, hasFailure, hasHumanWait, endSucceeded := false, false, false, false
		for _, n := range r.Nodes {
			if n.Status == "queued" || n.Status == "running" || n.Status == "pending" || n.Status == "retry_wait" {
				hasActive = true
			}
			if n.Status == "waiting_human" {
				hasHumanWait = true
			}
			if (n.Status == "failed" && n.ReasonCode != "failure_routed") || n.Status == "blocked" {
				hasFailure = true
			}
			if definitions[n.NodeID].Type == "end" && n.Status == "succeeded" {
				endSucceeded = true
			}
		}
		if hasActive {
			r.Status = "running"
		} else if hasFailure {
			r.Status = "blocked"
		} else if hasHumanWait {
			r.Status = "waiting"
		} else if !endSucceeded {
			r.Status = "blocked"
			r.Error = "workflow execution stalled before End"
		}
		if r.Status == "succeeded" && r.IssueID != "" {
			if _, err = q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: uuid(r.IssueID), WorkspaceID: uuid(ws), Status: "done"}); err != nil {
				return r, err
			}
		}
	}
	changed := before != string(body(r)) || len(cancelled) > 0 || len(queued) > 0
	if changed {
		r.StateRevision = stateRevision(r) + 1
		if err = saveRun(ctx, tx, &r); err != nil {
			return r, err
		}
		actorType, actorID := "system", ""
		if actor != "" {
			actorType, actorID = "member", actor
		}
		if err = s.recordRunMutation(ctx, tx, r, protocol.EventWorkflowRunUpdated, actorType, actorID); err != nil {
			return r, err
		}
	}
	if err = finishWorkflowRunCommand(ctx, tx, ws, actor, "workflow_run."+action, rid, idempotencyKey, commandHash, r); err != nil {
		return r, err
	}
	if lease != nil {
		if err = s.finishWorkflowJob(ctx, tx, *lease, r); err != nil {
			return r, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	for _, t := range queued {
		s.Tasks.NotifyTaskEnqueued(ctx, t)
	}
	if len(cancelled) > 0 {
		s.Tasks.BroadcastCancelledTasks(ctx, ws, cancelled)
		s.Tasks.CaptureCancelledTasks(ctx, cancelled)
	}
	if changed {
		s.publish(protocol.EventWorkflowRunUpdated, ws, wid, rid)
		s.publish(protocol.EventIssueUpdated, ws, wid, rid)
	}
	return r, nil
}

func incomingEdges(graph Graph, nodeID string) []Edge {
	result := make([]Edge, 0)
	for _, edge := range graph.Edges {
		if normalFlowEdge(edge) && edge.Target == nodeID {
			result = append(result, edge)
		}
	}
	return result
}

func normalFlowEdge(edge Edge) bool {
	return edge.Kind != "rework" && edge.Kind != "compensation"
}

func chooseConditionEdge(node Node, edges []Edge) Edge {
	if node.Config != nil {
		if port, ok := node.Config["selected_port"].(string); ok && port != "" {
			for _, edge := range edges {
				if edge.Source == node.ID && edge.SourcePort == port {
					return edge
				}
			}
		}
	}
	var first Edge
	for _, edge := range edges {
		if !normalFlowEdge(edge) || edge.Source != node.ID {
			continue
		}
		if first.ID == "" {
			first = edge
		}
		if edge.SourcePort == "default" {
			return edge
		}
	}
	return first
}

func markSkipped(states map[string]*NodeRun, nodeID string) {
	if node := states[nodeID]; node != nil && (node.Status == "pending" || node.Status == "blocked") {
		node.Status = "skipped"
		node.ReasonCode = "condition_not_selected"
	}
}

func ensureNodeActivation(node *NodeRun, rootScopeID string) {
	if node.Generation <= 0 {
		node.Generation = 1
	}
	if node.ActivationNo <= 0 {
		node.ActivationNo = 1
	}
	if node.ActivationID == "" {
		node.ActivationID = id()
		if node.ScopeInstanceID == "" {
			node.ScopeInstanceID = rootScopeID
		}
		if node.ScopeDefinitionID == "" {
			node.ScopeDefinitionID = "root"
		}
	}
}

func replaceNodeActivation(node *NodeRun, newGeneration bool) {
	if node.ActivationID != "" {
		node.ReplacesActivationID = node.ActivationID
	}
	if node.Generation <= 0 {
		node.Generation = 1
	}
	if newGeneration {
		node.Generation++
		node.ActivationNo = 1
		node.ScopeInstanceID = ""
		node.ScopeDefinitionID = ""
	} else {
		if node.ActivationNo <= 0 {
			node.ActivationNo = 1
		}
		node.ActivationNo++
	}
	node.ActivationID = id()
	node.TaskID = ""
	node.WorkItemID = ""
	node.Attempt = 0
	node.AutomaticRetriesUsed = 0
	node.RetryAt = nil
	node.Output = ""
	node.OutputID = ""
	node.ExecutionAccounted = false
}

func createWorkItem(ctx context.Context, tx pgx.Tx, workspaceID, runID, nodeID, activationID string, node Node, runInput string, states map[string]*NodeRun, timeoutSeconds int) (string, error) {
	assignee := nodeAssignee(node)
	if assignee == nil || assignee.Type != "member" {
		return "", Bad("human workflow nodes require a member assignee")
	}
	if _, err := util.ParseUUID(assignee.ID); err != nil {
		return "", Bad("invalid human assignee")
	}
	var memberExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND (id=$2 OR user_id=$2))", workspaceID, assignee.ID).Scan(&memberExists); err != nil {
		return "", err
	}
	if !memberExists {
		return "", &Error{Status: 403, Message: "human assignee is not a member of this workspace", Code: "member_not_found"}
	}
	input := map[string]any{"run_input": runInput}
	for nodeID, state := range states {
		if state != nil && state.Status == "succeeded" && state.Output != "" {
			input["output:"+nodeID] = state.Output
		}
	}
	kind := "task"
	if node.Type == "human_review" {
		kind = "review"
	}
	itemID := id()
	if timeoutSeconds <= 0 {
		timeoutSeconds = 72 * 60 * 60
	}
	dueAt := time.Now().UTC().Add(time.Duration(timeoutSeconds) * time.Second)
	_, err := tx.Exec(ctx, `INSERT INTO workflow_work_item(id,workspace_id,run_id,activation_id,node_id,kind,assignee_id,form_snapshot,input_snapshot,status,due_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'open',$10)`, itemID, workspaceID, runID, nullableUUID(activationID), nodeID, kind, assignee.ID, body(node.Config), body(input), dueAt)
	return itemID, err
}

func createRecoveryWorkItem(ctx context.Context, tx pgx.Tx, workspaceID, runID, nodeID, activationID, ownerID string, node Node, runInput string, states map[string]*NodeRun, timeoutSeconds int) (string, error) {
	if _, err := util.ParseUUID(ownerID); err != nil {
		return "", Bad("invalid workflow owner")
	}
	var memberExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND (id=$2 OR user_id=$2))", workspaceID, ownerID).Scan(&memberExists); err != nil {
		return "", err
	}
	if !memberExists {
		return "", &Error{Status: 403, Message: "workflow owner is not a member of this workspace", Code: "member_not_found"}
	}
	input := map[string]any{"run_input": runInput, "recovery_node_id": nodeID}
	for upstreamID, state := range states {
		if state != nil && state.Status == "succeeded" && state.Output != "" {
			input["output:"+upstreamID] = state.Output
		}
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 72 * 60 * 60
	}
	dueAt := time.Now().UTC().Add(time.Duration(timeoutSeconds) * time.Second)
	itemID := id()
	_, err := tx.Exec(ctx, `INSERT INTO workflow_work_item(id,workspace_id,run_id,activation_id,node_id,kind,assignee_id,form_snapshot,input_snapshot,status,due_at)
VALUES($1,$2,$3,$4,$5,'recovery',$6,$7,$8,'open',$9)`, itemID, workspaceID, runID, nullableUUID(activationID), nodeID, ownerID, body(node.Config), body(input), dueAt)
	return itemID, err
}

func failureEdges(graph Graph, nodeID string) []Edge {
	result := make([]Edge, 0)
	for _, edge := range graph.Edges {
		if edge.Source == nodeID && isFailureEdge(edge) {
			result = append(result, edge)
		}
	}
	return result
}

func isFailureEdge(edge Edge) bool {
	if !normalFlowEdge(edge) {
		return false
	}
	port := strings.ToLower(strings.TrimSpace(edge.SourcePort))
	return strings.HasPrefix(port, "failure") || strings.HasPrefix(port, "timeout") || port == "error"
}

func nodeFailureAction(node Node) string {
	if node.OnFailure != "" {
		return string(node.OnFailure)
	}
	if node.Config != nil {
		if action, ok := node.Config["on_failure"].(string); ok {
			return action
		}
		if config, ok := node.Config["on_failure"].(map[string]any); ok {
			if action, ok := config["action"].(string); ok {
				return action
			}
		}
	}
	return "block"
}

func nodeTimeoutAction(node Node) string {
	if node.OnTimeout != "" {
		return string(node.OnTimeout)
	}
	if node.Config != nil {
		if action, ok := node.Config["on_timeout"].(string); ok {
			return action
		}
		if config, ok := node.Config["on_timeout"].(map[string]any); ok {
			if action, ok := config["action"].(string); ok {
				return action
			}
		}
	}
	return ""
}

func (s *Service) applyFailurePolicy(ctx context.Context, tx pgx.Tx, workspaceID, userID string, run *Run, nodeRun *NodeRun, node Node, states map[string]*NodeRun) error {
	return s.applyFailureAction(ctx, tx, workspaceID, userID, run, nodeRun, node, nodeFailureAction(node), states)
}

func (s *Service) applyTimeoutPolicy(ctx context.Context, tx pgx.Tx, workspaceID, userID string, run *Run, nodeRun *NodeRun, node Node) error {
	action := nodeTimeoutAction(node)
	if action == "" {
		action = nodeFailureAction(node)
	}
	return s.applyFailureAction(ctx, tx, workspaceID, userID, run, nodeRun, node, action, nil)
}

func (s *Service) applyFailureAction(ctx context.Context, tx pgx.Tx, workspaceID, userID string, run *Run, nodeRun *NodeRun, node Node, action string, states map[string]*NodeRun) error {
	switch action {
	case "route":
		if len(failureEdges(run.Graph, node.ID)) > 0 {
			if nodeRun.Status == "blocked" {
				nodeRun.Status = "failed"
			}
			nodeRun.ReasonCode = "failure_routed"
		}
	case "takeover":
		ownerID := run.OwnerID
		if ownerID == "" {
			ownerID = userID
		}
		replaceNodeActivation(nodeRun, false)
		itemID, err := createRecoveryWorkItem(ctx, tx, workspaceID, run.ID, node.ID, nodeRun.ActivationID, ownerID, node, run.Input, states, run.Graph.Defaults.HumanTimeoutSeconds)
		if err != nil {
			return err
		}
		nodeRun.WorkItemID = itemID
		nodeRun.Status = "waiting_human"
		nodeRun.ReasonCode = "awaiting_takeover"
	}
	return nil
}

func taskTimeoutReason(task db.AgentTaskQueue, nodeStatus string, defaults WorkflowDefaults) (string, string) {
	now := time.Now().UTC()
	if nodeStatus == "queued" && defaults.QueueTimeoutSeconds > 0 && task.CreatedAt.Valid && now.Sub(task.CreatedAt.Time) >= time.Duration(defaults.QueueTimeoutSeconds)*time.Second {
		return "queue_timeout", "agent task exceeded the workflow queue timeout"
	}
	if nodeStatus == "running" && defaults.ExecutionTimeoutSeconds > 0 && task.StartedAt.Valid && now.Sub(task.StartedAt.Time) >= time.Duration(defaults.ExecutionTimeoutSeconds)*time.Second {
		return "execution_timeout", "agent task exceeded the workflow execution timeout"
	}
	return "", ""
}

func expireOpenWorkItems(ctx context.Context, tx pgx.Tx, run *Run) error {
	rows, err := tx.Query(ctx, `SELECT id::text,node_id FROM workflow_work_item WHERE run_id=$1 AND status='open' AND due_at IS NOT NULL AND due_at <= now() FOR UPDATE`, run.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type expiredItem struct{ id, nodeID string }
	var expired []expiredItem
	for rows.Next() {
		var item expiredItem
		if err := rows.Scan(&item.id, &item.nodeID); err != nil {
			return err
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range expired {
		if _, err := tx.Exec(ctx, `UPDATE workflow_work_item SET status='expired',version=version+1,updated_at=now() WHERE id=$1 AND status='open'`, item.id); err != nil {
			return err
		}
		for index := range run.Nodes {
			if run.Nodes[index].NodeID == item.nodeID && run.Nodes[index].Status == "waiting_human" {
				run.Nodes[index].Status = "blocked"
				run.Nodes[index].ReasonCode = "human_timeout"
				run.Nodes[index].Error = "human work item expired"
			}
		}
	}
	return nil
}

func latestTask(ctx context.Context, q db.DBTX, taskID string) (db.AgentTaskQueue, error) {
	var current string
	err := q.QueryRow(ctx, `WITH RECURSIVE chain AS (SELECT id,created_at FROM agent_task_queue WHERE id=$1 UNION ALL SELECT t.id,t.created_at FROM agent_task_queue t JOIN chain c ON t.retry_of_task_id=c.id) SELECT id::text FROM chain ORDER BY created_at DESC,id DESC LIMIT 1`, taskID).Scan(&current)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	return db.New(q).GetAgentTask(ctx, uuid(current))
}

func accountTaskExecution(run *Run, node *NodeRun, task db.AgentTaskQueue, now time.Time) {
	if node.ExecutionAccounted {
		return
	}
	node.ExecutionAccounted = true
	if !task.StartedAt.Valid {
		return
	}
	end := now
	if task.CompletedAt.Valid && task.CompletedAt.Time.Before(end) {
		end = task.CompletedAt.Time
	}
	if !end.After(task.StartedAt.Time) {
		return
	}
	run.ActiveExecutionMS += end.Sub(task.StartedAt.Time).Milliseconds()
}

func inputs(r Run, n Node, states map[string]*NodeRun) string {
	refs := map[string]bool{}
	for _, e := range r.Graph.Edges {
		if normalFlowEdge(e) && e.Target == n.ID {
			refs[e.Source] = true
		}
	}
	for _, ref := range n.InputRefs {
		refs[ref] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Run input:\n%s\n", r.Input)
	for _, up := range r.Graph.Nodes {
		state := states[up.ID]
		if refs[up.ID] && state != nil && state.Status == "succeeded" && state.Output != "" && up.Type != "start" {
			fmt.Fprintf(&b, "\nOutput from %s (%s):\n%s\n", up.Label, up.ID, state.Output)
		}
	}
	return b.String()
}
