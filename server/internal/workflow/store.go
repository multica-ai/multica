package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"strings"
	"time"
)

type Workflow struct {
	ID                 string             `json:"id"`
	WorkspaceID        string             `json:"workspace_id"`
	OwnerID            string             `json:"owner_id,omitempty"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Graph              Graph              `json:"graph"`
	Revision           int64              `json:"revision"`
	DraftRevision      int64              `json:"draft_revision,omitempty"`
	PublishedReleaseID string             `json:"published_release_id,omitempty"`
	ArchivedAt         *time.Time         `json:"archived_at,omitempty"`
	CanUndo            bool               `json:"can_undo"`
	CanRedo            bool               `json:"can_redo"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	LastEditError      string             `json:"last_edit_error,omitempty"`
	LastEditMessageID  string             `json:"last_edit_message_id,omitempty"`
	UpgradeIssues      []ValidationDetail `json:"upgrade_issues,omitempty"`
}
type Edit struct {
	Name             *string             `json:"name"`
	Description      *string             `json:"description"`
	Graph            *Graph              `json:"graph"`
	ExpectedRevision int64               `json:"expected_revision"`
	UpgradeIssues    *[]ValidationDetail `json:"-"`
}
type Service struct {
	DB                 db.DBTX
	Tx                 service.TxStarter
	Tasks              *service.TaskService
	Bus                *events.Bus
	ValidateAgent      func(context.Context, string, string, string) error
	ValidateAttachment func(context.Context, string, string, string) error
	workerID           string
	wake               chan struct{}
}

func New(database db.DBTX, tx service.TxStarter, tasks *service.TaskService, bus *events.Bus) *Service {
	return &Service{DB: database, Tx: tx, Tasks: tasks, Bus: bus, workerID: id(), wake: make(chan struct{}, 1)}
}
func id() string                { return util.UUIDToString(dbid.NewV7()) }
func uuid(s string) pgtype.UUID { v, _ := util.ParseUUID(s); return v }
func body(v any) []byte         { b, _ := json.Marshal(v); return b }
func (s *Service) publish(kind, ws, wf, run string) {
	if s.Bus != nil {
		s.Bus.Publish(events.Event{Type: kind, WorkspaceID: ws, ActorType: "system", Payload: map[string]any{"workflow_id": wf, "run_id": run}})
	}
}
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func load(ctx context.Context, q db.DBTX, ws, wid string, lock bool) (Workflow, []Workflow, int, error) {
	sql := "SELECT body, history, history_cursor FROM workflow WHERE workspace_id=$1 AND id=$2"
	if lock {
		sql += " FOR UPDATE"
	}
	var raw, hist []byte
	var cursor int
	var w Workflow
	var h []Workflow
	err := q.QueryRow(ctx, sql, ws, wid).Scan(&raw, &hist, &cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, h, cursor, &Error{Status: 404, Message: "workflow not found", Code: "workflow_not_found"}
	}
	if err != nil {
		return w, h, cursor, err
	}
	if err = json.Unmarshal(raw, &w); err != nil {
		return w, h, cursor, err
	}
	// The workspace and path identity come from the scoped query, not from a
	// mutable JSON body. Reassert them for old snapshots and manager checks.
	w.WorkspaceID = ws
	w.ID = wid
	if err = json.Unmarshal(hist, &h); err != nil {
		return w, h, cursor, err
	}
	w.CanUndo = cursor > 0
	w.CanRedo = cursor < len(h)-1
	return w, h, cursor, nil
}
func (s *Service) Get(ctx context.Context, ws, wid string) (Workflow, error) {
	w, _, _, err := load(ctx, s.DB, ws, wid, false)
	return w, err
}
func (s *Service) List(ctx context.Context, ws string) ([]Workflow, error) {
	rows, err := s.DB.Query(ctx, "SELECT body,history_cursor,jsonb_array_length(history) FROM workflow WHERE workspace_id=$1 ORDER BY updated_at DESC", ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workflow{}
	for rows.Next() {
		var raw []byte
		var c, n int
		if err = rows.Scan(&raw, &c, &n); err != nil {
			return nil, err
		}
		var w Workflow
		if err = json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		w.CanUndo = c > 0
		w.CanRedo = c < n-1
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *Service) validateAgents(ctx context.Context, ws, user string, g Graph, required bool) error {
	for _, n := range g.Nodes {
		if n.Type != "agent" {
			continue
		}
		a := nodeAssignee(n)
		if a == nil || a.ID == "" {
			if required {
				return Bad("agent nodes require an agent")
			}
			continue
		}
		if _, err := util.ParseUUID(a.ID); err != nil {
			return Bad("invalid agent id")
		}
		if s.ValidateAgent != nil {
			if err := s.ValidateAgent(ctx, ws, user, a.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) validateMembers(ctx context.Context, ws string, g Graph, required bool) error {
	for _, n := range g.Nodes {
		if n.Type != "human_task" && n.Type != "human_review" {
			continue
		}
		assignee := nodeAssignee(n)
		if assignee == nil || assignee.ID == "" {
			if required {
				return &Error{Status: 422, Message: "human nodes require a member assignee", Code: "member_required"}
			}
			continue
		}
		if _, err := util.ParseUUID(assignee.ID); err != nil {
			return Bad("invalid human assignee id")
		}
		var exists bool
		if err := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND (id=$2 OR user_id=$2))", ws, assignee.ID).Scan(&exists); err != nil {
			return err
		}
		if required && !exists {
			return &Error{Status: 403, Message: "human assignee is not a member of this workspace", Code: "member_not_found"}
		}
	}
	return nil
}
func (s *Service) Create(ctx context.Context, ws, user, name, description string, g *Graph) (Workflow, error) {
	return s.createWorkflow(ctx, ws, user, name, description, g, "")
}

func (s *Service) CreateKey(ctx context.Context, ws, user, name, description string, g *Graph, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	return s.createWorkflow(ctx, ws, user, name, description, g, idempotencyKey)
}

func (s *Service) createWorkflow(ctx context.Context, ws, user, name, description string, g *Graph, idempotencyKey string) (Workflow, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return Workflow{}, Bad("name is required and must be at most 200 bytes")
	}
	graph := DefaultGraph()
	if g != nil {
		graph = *g
	}
	if err := Validate(graph, false); err != nil {
		return Workflow{}, err
	}
	if err := s.validateAgents(ctx, ws, user, graph, false); err != nil {
		return Workflow{}, err
	}
	w := Workflow{ID: id(), WorkspaceID: ws, OwnerID: user, Name: name, Description: description, Graph: graph, Revision: 1, DraftRevision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return w, err
	}
	defer tx.Rollback(ctx)
	requestHash := workflowCommandHash("workflow.create", "00000000-0000-0000-0000-000000000000", struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Graph       *Graph `json:"graph"`
	}{Name: name, Description: description, Graph: &graph})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow.create", "00000000-0000-0000-0000-000000000000", idempotencyKey, requestHash)
	if err != nil {
		return w, err
	}
	if len(responseBody) > 0 {
		if err = json.Unmarshal(responseBody, &w); err != nil {
			return Workflow{}, err
		}
		return w, nil
	}
	_, err = tx.Exec(ctx, "INSERT INTO workflow(id,workspace_id,creator_id,owner_id,draft_revision,graph_schema_version,body,history) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", w.ID, ws, user, user, w.Revision, graphVersion(w.Graph), body(w), body([]Workflow{w}))
	if err != nil {
		return w, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO workflow_version(workflow_id,revision,body) VALUES($1,$2,$3)", w.ID, w.Revision, body(w))
	if err != nil {
		return w, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow.create", "00000000-0000-0000-0000-000000000000", idempotencyKey, requestHash, w); err != nil {
		return w, err
	}
	if err = tx.Commit(ctx); err == nil {
		s.publish(protocol.EventWorkflowUpdated, ws, w.ID, "")
	}
	return w, err
}
func (s *Service) Edit(ctx context.Context, ws, user, wid string, e Edit, action string) (Workflow, error) {
	return s.edit(ctx, ws, user, wid, e, action, "")
}

func (s *Service) EditKey(ctx context.Context, ws, user, wid string, e Edit, action, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	return s.edit(ctx, ws, user, wid, e, action, idempotencyKey)
}

func (s *Service) edit(ctx context.Context, ws, user, wid string, e Edit, action, idempotencyKey string) (Workflow, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback(ctx)
	if idempotencyKey != "" {
		if _, _, _, err = load(ctx, tx, ws, wid, true); err != nil {
			return Workflow{}, err
		}
	}
	requestHash := workflowCommandHash("workflow.edit", wid, struct {
		Edit   Edit   `json:"edit"`
		Action string `json:"action"`
	}{Edit: e, Action: action})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow.edit", wid, idempotencyKey, requestHash)
	if err != nil {
		return Workflow{}, err
	}
	if len(responseBody) > 0 {
		var replay Workflow
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Workflow{}, err
		}
		return replay, nil
	}
	w, err := s.editTx(ctx, tx, ws, user, wid, e, action, "")
	if err != nil {
		return w, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow.edit", wid, idempotencyKey, requestHash, w); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(ctx); err == nil {
		s.publish(protocol.EventWorkflowUpdated, ws, wid, "")
	}
	return w, err
}

// recordEditError persists a Chat builder failure without changing the
// workflow revision or history cursor. A later successful edit clears the
// marker in editTx, so the UI can show the latest actionable error while
// keeping the draft itself intact.
func (s *Service) recordEditError(ctx context.Context, ws, wid, messageID string, editErr error) error {
	if editErr == nil {
		return nil
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.recordEditErrorTx(ctx, tx, ws, wid, messageID, editErr); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.publish(protocol.EventWorkflowUpdated, ws, wid, "")
	return nil
}

func (s *Service) recordEditErrorTx(ctx context.Context, tx pgx.Tx, ws, wid, messageID string, editErr error) error {
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return err
	}
	w.LastEditError = editErr.Error()
	w.LastEditMessageID = messageID
	if _, err = tx.Exec(ctx, "UPDATE workflow SET body=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2", ws, wid, body(w)); err != nil {
		return err
	}
	return nil
}
func (s *Service) editTx(ctx context.Context, tx pgx.Tx, ws, user, wid string, e Edit, action, messageID string) (Workflow, error) {
	w, h, c, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return w, err
	}
	if w.Revision != e.ExpectedRevision {
		return w, Conflict("workflow changed; refresh before editing")
	}
	rev, created := w.Revision, w.CreatedAt
	switch action {
	case "undo":
		if c == 0 {
			return w, Conflict("nothing to undo")
		}
		c--
		w = h[c]
	case "redo":
		if c >= len(h)-1 {
			return w, Conflict("nothing to redo")
		}
		c++
		w = h[c]
	default:
		if e.Name != nil {
			w.Name = strings.TrimSpace(*e.Name)
		}
		if e.Description != nil {
			w.Description = *e.Description
		}
		if e.Graph != nil {
			w.Graph = *e.Graph
		}
		if w.Name == "" || len(w.Name) > 200 {
			return w, Bad("invalid workflow name")
		}
		if err = Validate(w.Graph, false); err != nil {
			return w, err
		}
		if e.UpgradeIssues != nil {
			w.UpgradeIssues = append([]ValidationDetail(nil), (*e.UpgradeIssues)...)
		} else {
			w.UpgradeIssues = nil
		}
		h = append(h[:c+1], w)
		c++
	}
	if err = s.validateAgents(ctx, ws, user, w.Graph, false); err != nil {
		return w, err
	}
	w.Revision = rev + 1
	w.DraftRevision = w.Revision
	w.CreatedAt = created
	w.UpdatedAt = time.Now().UTC()
	w.CanUndo = c > 0
	w.CanRedo = c < len(h)-1
	w.LastEditError = ""
	w.LastEditMessageID = messageID
	_, err = tx.Exec(ctx, "UPDATE workflow SET body=$3,history=$4,history_cursor=$5,draft_revision=$6,graph_schema_version=$7,updated_at=now() WHERE workspace_id=$1 AND id=$2", ws, wid, body(w), body(h), c, w.Revision, graphVersion(w.Graph))
	if err != nil {
		return w, err
	}
	var msg any
	if messageID != "" {
		msg = messageID
	}
	_, err = tx.Exec(ctx, "INSERT INTO workflow_version(workflow_id,revision,body,source_message_id) VALUES($1,$2,$3,$4)", wid, w.Revision, body(w), msg)
	return w, err
}
func (s *Service) Delete(ctx context.Context, ws, wid string) error {
	return s.deleteWorkflow(ctx, ws, "", wid, "")
}

func (s *Service) DeleteKey(ctx context.Context, ws, actor, wid, idempotencyKey string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Bad("idempotency_key is required")
	}
	return s.deleteWorkflow(ctx, ws, actor, wid, idempotencyKey)
}

func (s *Service) deleteWorkflow(ctx context.Context, ws, actor, wid, idempotencyKey string) error {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	requestHash := workflowCommandHash("workflow.delete", wid, struct{}{})
	if actor != "" {
		if err = requireWorkflowMember(ctx, tx, ws, actor); err != nil {
			return err
		}
		responseBody, commandErr := beginWorkflowCommand(ctx, tx, ws, actor, "workflow.delete", wid, idempotencyKey, requestHash)
		if commandErr != nil {
			return commandErr
		}
		if len(responseBody) > 0 {
			return nil
		}
	}
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return err
	}
	if actor != "" {
		if err = requireWorkflowManager(ctx, tx, actor, w); err != nil {
			return err
		}
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workflow_run WHERE workflow_id=$1 AND status IN ('queued','running','waiting','blocked','cancelling','compensating','compensation_blocked'))", wid).Scan(&active); err != nil {
		return err
	}
	if active {
		return Conflict("cancel active runs before deleting the workflow")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM workflow_outbox WHERE event_id IN (SELECT id FROM workflow_event WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1))`, wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM workflow_event WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)`, wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM workflow_job WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)`, wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_work_item WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_transition WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_output WHERE activation_id IN (SELECT id FROM workflow_node_activation WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1))", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_node_attempt WHERE activation_id IN (SELECT id FROM workflow_node_activation WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1))", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_node_activation WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_scope_instance WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_node_run WHERE run_id IN (SELECT id FROM workflow_run WHERE workflow_id=$1)", wid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow_command WHERE workspace_id=$1 AND operation <> 'workflow.delete' AND (resource_id=$2 OR resource_id IN (SELECT id FROM workflow_run WHERE workflow_id=$2))", ws, wid); err != nil {
		return err
	}
	for _, table := range []string{"workflow_chat_turn", "workflow_chat", "workflow_release", "workflow_version", "workflow_run"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE workflow_id=$1", wid); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM workflow WHERE id=$1 AND workspace_id=$2", wid, ws); err != nil {
		return err
	}
	if actor != "" {
		if err = finishWorkflowCommandStatus(ctx, tx, ws, actor, "workflow.delete", wid, idempotencyKey, requestHash, map[string]bool{"deleted": true}, 204); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err == nil {
		s.publish(protocol.EventWorkflowUpdated, ws, wid, "")
	}
	return err
}
