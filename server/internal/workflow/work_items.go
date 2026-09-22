package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WorkItem struct {
	ID              string         `json:"id"`
	RunID           string         `json:"run_id"`
	WorkflowID      string         `json:"workflow_id"`
	ActivationID    string         `json:"activation_id,omitempty"`
	NodeID          string         `json:"node_id"`
	Kind            string         `json:"kind"`
	AssigneeID      string         `json:"assignee_id"`
	FormSnapshot    map[string]any `json:"form_snapshot"`
	InputSnapshot   map[string]any `json:"input_snapshot"`
	OutputValues    map[string]any `json:"output_values"`
	Decision        string         `json:"decision,omitempty"`
	Feedback        string         `json:"feedback,omitempty"`
	Version         int64          `json:"version"`
	Status          string         `json:"status"`
	DueAt           *time.Time     `json:"due_at,omitempty"`
	SubmittedBy     string         `json:"submitted_by,omitempty"`
	SubmittedAt     *time.Time     `json:"submitted_at,omitempty"`
	idempotencyKey  string
	idempotencyHash string
}

type WorkItemSubmission struct {
	ExpectedStateRevision int64
	ExpectedItemVersion   int64
	Action                string
	Values                map[string]any
	Feedback              string
	IdempotencyKey        string
}

const workItemSelect = `SELECT wi.id::text,wi.run_id::text,wr.workflow_id::text,COALESCE(wi.activation_id::text,''),wi.node_id,wi.kind,wi.assignee_id::text,wi.form_snapshot,wi.input_snapshot,wi.output_values,COALESCE(wi.decision,''),wi.feedback,wi.version,wi.status,wi.due_at,COALESCE(wi.submitted_by::text,''),wi.submitted_at,COALESCE(wi.idempotency_key,''),COALESCE(wi.idempotency_hash,'')
FROM workflow_work_item wi JOIN workflow_run wr ON wr.id=wi.run_id AND wr.workspace_id=wi.workspace_id`

func scanWorkItem(scan func(...any) error) (WorkItem, error) {
	var item WorkItem
	var formRaw, inputRaw, outputRaw []byte
	var dueAt, submittedAt *time.Time
	err := scan(&item.ID, &item.RunID, &item.WorkflowID, &item.ActivationID, &item.NodeID, &item.Kind, &item.AssigneeID, &formRaw, &inputRaw, &outputRaw, &item.Decision, &item.Feedback, &item.Version, &item.Status, &dueAt, &item.SubmittedBy, &submittedAt, &item.idempotencyKey, &item.idempotencyHash)
	if err != nil {
		return WorkItem{}, err
	}
	item.FormSnapshot, item.InputSnapshot, item.OutputValues = scanJSONMap(formRaw), scanJSONMap(inputRaw), scanJSONMap(outputRaw)
	item.DueAt, item.SubmittedAt = dueAt, submittedAt
	return item, nil
}

func workItemRequestHash(runID, itemID string, submission WorkItemSubmission) string {
	payload, _ := json.Marshal(struct {
		RunID                 string         `json:"run_id"`
		ItemID                string         `json:"item_id"`
		ExpectedStateRevision int64          `json:"expected_state_revision"`
		ExpectedItemVersion   int64          `json:"expected_item_version"`
		Action                string         `json:"action"`
		Values                map[string]any `json:"values"`
		Feedback              string         `json:"feedback"`
	}{runID, itemID, submission.ExpectedStateRevision, submission.ExpectedItemVersion, submission.Action, submission.Values, submission.Feedback})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func legacyWorkItemRequestHash(runID, itemID string, submission WorkItemSubmission) string {
	payload, _ := json.Marshal(struct {
		RunID    string         `json:"run_id"`
		ItemID   string         `json:"item_id"`
		Action   string         `json:"action"`
		Values   map[string]any `json:"values"`
		Feedback string         `json:"feedback"`
	}{runID, itemID, submission.Action, submission.Values, submission.Feedback})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func parseWorkItemUUID(value string) (string, error) {
	if _, err := util.ParseUUID(value); err != nil {
		return "", Bad("invalid work item id")
	}
	return value, nil
}

func scanJSONMap(raw []byte) map[string]any {
	result := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &result)
	}
	return result
}

func workItemActorAllowed(ctx context.Context, q db.DBTX, workspaceID, assigneeID, userID string) (bool, error) {
	if assigneeID == userID {
		return true, nil
	}
	var allowed bool
	err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2 AND user_id=$3)", workspaceID, assigneeID, userID).Scan(&allowed)
	return allowed, err
}

func (s *Service) GetWorkItem(ctx context.Context, ws, runID, itemID string) (WorkItem, error) {
	if _, err := parseWorkItemUUID(itemID); err != nil {
		return WorkItem{}, err
	}
	var item WorkItem
	var formRaw, inputRaw, outputRaw []byte
	var dueAt, submittedAt *time.Time
	err := s.DB.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3`, ws, runID, itemID).Scan(&item.ID, &item.RunID, &item.WorkflowID, &item.ActivationID, &item.NodeID, &item.Kind, &item.AssigneeID, &formRaw, &inputRaw, &outputRaw, &item.Decision, &item.Feedback, &item.Version, &item.Status, &dueAt, &item.SubmittedBy, &submittedAt, &item.idempotencyKey, &item.idempotencyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkItem{}, &Error{Status: 404, Message: "work item not found", Code: "work_item_not_found"}
	}
	if err != nil {
		return WorkItem{}, err
	}
	item.FormSnapshot, item.InputSnapshot, item.OutputValues = scanJSONMap(formRaw), scanJSONMap(inputRaw), scanJSONMap(outputRaw)
	item.DueAt, item.SubmittedAt = dueAt, submittedAt
	return item, nil
}

func (s *Service) ListWorkItems(ctx context.Context, ws, assignee, status string) ([]WorkItem, error) {
	query := workItemSelect + ` WHERE wi.workspace_id=$1 AND (wi.assignee_id=$2 OR EXISTS (SELECT 1 FROM member WHERE member.workspace_id=$1 AND member.id=wi.assignee_id AND member.user_id=$2))`
	args := []any{ws, assignee}
	if status != "" {
		query += " AND status=$3"
		args = append(args, status)
	}
	query += " ORDER BY wi.due_at NULLS LAST,wi.created_at LIMIT 200"
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkItem, 0)
	for rows.Next() {
		item, scanErr := scanWorkItem(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SubmitWorkItem(ctx context.Context, ws, user, runID, itemID string, submission WorkItemSubmission) (Run, error) {
	if _, err := parseWorkItemUUID(itemID); err != nil {
		return Run{}, err
	}
	if submission.Action != "submit" && submission.Action != "approve" && submission.Action != "rework" {
		return Run{}, Bad("invalid work item action")
	}
	if submission.IdempotencyKey == "" || len(submission.IdempotencyKey) > 200 {
		return Run{}, Bad("idempotency_key is required")
	}
	if submission.Action == "rework" && len([]rune(submission.Feedback)) == 0 {
		return Run{}, Bad("feedback is required when returning work for rework")
	}
	requestHash := workItemRequestHash(runID, itemID, submission)
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	var item WorkItem
	var formRaw, inputRaw, outputRaw []byte
	var dueAt, submittedAt *time.Time
	err = tx.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3 FOR UPDATE OF wi`, ws, runID, itemID).Scan(&item.ID, &item.RunID, &item.WorkflowID, &item.ActivationID, &item.NodeID, &item.Kind, &item.AssigneeID, &formRaw, &inputRaw, &outputRaw, &item.Decision, &item.Feedback, &item.Version, &item.Status, &dueAt, &item.SubmittedBy, &submittedAt, &item.idempotencyKey, &item.idempotencyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, &Error{Status: 404, Message: "work item not found", Code: "work_item_not_found"}
	}
	if err != nil {
		return Run{}, err
	}
	item.FormSnapshot, item.InputSnapshot, item.OutputValues = scanJSONMap(formRaw), scanJSONMap(inputRaw), scanJSONMap(outputRaw)
	item.DueAt, item.SubmittedAt = dueAt, submittedAt
	allowed, allowedErr := workItemActorAllowed(ctx, tx, ws, item.AssigneeID, user)
	if allowedErr != nil {
		return Run{}, allowedErr
	}
	if !allowed {
		return Run{}, &Error{Status: 403, Message: "only the current assignee can submit this work item", Code: "action_forbidden"}
	}
	if item.idempotencyKey != "" {
		if item.idempotencyKey != submission.IdempotencyKey {
			return Run{}, Conflict("work item idempotency key was already used for a different decision")
		}
		if item.idempotencyHash != "" && item.idempotencyHash != requestHash {
			return Run{}, Conflict("work item idempotency key was already used for a different decision")
		}
		if item.idempotencyHash == "" {
			storedHash := legacyWorkItemRequestHash(runID, itemID, WorkItemSubmission{Action: item.Decision, Values: item.OutputValues, Feedback: item.Feedback})
			if storedHash != legacyWorkItemRequestHash(runID, itemID, submission) {
				return Run{}, Conflict("work item idempotency key was already used for a different decision")
			}
		}
		r, _, err := getRunByID(ctx, tx, ws, runID, false)
		return r, err
	}
	if item.Status != "open" {
		return Run{}, Conflict("work item has already been resolved")
	}
	if submission.ExpectedItemVersion != item.Version {
		return Run{}, Conflict("work item changed; refresh before submitting")
	}
	r, _, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return Run{}, err
	}
	if submission.ExpectedStateRevision > 0 && submission.ExpectedStateRevision != stateRevision(r) {
		return Run{}, Conflict("run changed; refresh before submitting")
	}
	var node *NodeRun
	for i := range r.Nodes {
		if r.Nodes[i].NodeID == item.NodeID {
			node = &r.Nodes[i]
			break
		}
	}
	if node == nil {
		return Run{}, Bad("work item node not found")
	}
	if submission.Action == "rework" {
		if err = applySimpleRework(&r, item.NodeID, submission.Feedback); err != nil {
			return Run{}, err
		}
	} else {
		if submission.Values == nil {
			submission.Values = map[string]any{}
		}
		encoded, marshalErr := json.Marshal(submission.Values)
		if marshalErr != nil {
			return Run{}, Bad("invalid work item values")
		}
		definition := Node{}
		for _, candidate := range r.Graph.Nodes {
			if candidate.ID == item.NodeID {
				definition = candidate
				break
			}
		}
		if outputErr := validateNodeOutput(definition, string(encoded)); outputErr != nil {
			return Run{}, outputErr
		}
		if outputErr := s.validateNodeOutputArtifacts(ctx, ws, user, definition, string(encoded)); outputErr != nil {
			return Run{}, outputErr
		}
		node.Status, node.Output, node.Error, node.ReasonCode = "succeeded", string(encoded), "", "human_submitted"
	}
	if _, err = tx.Exec(ctx, `UPDATE workflow_work_item SET output_values=$4,decision=$5,feedback=$6,status='closed',version=version+1,submitted_by=$7,submitted_at=now(),idempotency_key=$8,idempotency_hash=$9,updated_at=now() WHERE workspace_id=$1 AND run_id=$2 AND id=$3 AND status='open' AND version=$10`, ws, runID, itemID, body(submission.Values), submission.Action, submission.Feedback, user, submission.IdempotencyKey, requestHash, item.Version); err != nil {
		return Run{}, err
	}
	r.StateRevision++
	if r.StateRevision == 0 {
		r.StateRevision = 1
	}
	if err = saveRun(ctx, tx, &r); err != nil {
		return Run{}, err
	}
	if err = s.recordRunMutation(ctx, tx, r, protocol.EventWorkflowWorkItemUpdated, "member", user); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	s.Wake()
	s.publish(protocol.EventWorkflowRunUpdated, ws, r.WorkflowID, r.ID)
	return r, nil
}

func (s *Service) TransferWorkItem(ctx context.Context, ws, user, runID, itemID, memberID, reason string, expectedStateRevision, expectedItemVersion int64) (WorkItem, error) {
	return s.transferWorkItem(ctx, ws, user, runID, itemID, memberID, reason, expectedStateRevision, expectedItemVersion, "")
}

func (s *Service) TransferWorkItemKey(ctx context.Context, ws, user, runID, itemID, memberID, reason string, expectedStateRevision, expectedItemVersion int64, idempotencyKey string) (WorkItem, error) {
	if len(strings.TrimSpace(idempotencyKey)) == 0 {
		return WorkItem{}, Bad("idempotency_key is required")
	}
	return s.transferWorkItem(ctx, ws, user, runID, itemID, memberID, reason, expectedStateRevision, expectedItemVersion, idempotencyKey)
}

func (s *Service) transferWorkItem(ctx context.Context, ws, user, runID, itemID, memberID, reason string, expectedStateRevision, expectedItemVersion int64, idempotencyKey string) (WorkItem, error) {
	if _, err := parseWorkItemUUID(itemID); err != nil {
		return WorkItem{}, err
	}
	if _, err := util.ParseUUID(memberID); err != nil {
		return WorkItem{}, Bad("invalid member id")
	}
	if len([]rune(reason)) > 2000 {
		return WorkItem{}, Bad("transfer reason is too long")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return WorkItem{}, err
	}
	defer tx.Rollback(ctx)
	run, creator, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return WorkItem{}, err
	}
	item, err := scanWorkItem(tx.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3 FOR UPDATE OF wi`, ws, runID, itemID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkItem{}, &Error{Status: 404, Message: "work item not found", Code: "work_item_not_found"}
	}
	if err != nil {
		return WorkItem{}, err
	}
	if item.Status != "open" {
		return WorkItem{}, Conflict("work item has already been resolved")
	}
	allowed, allowedErr := workItemActorAllowed(ctx, tx, ws, item.AssigneeID, user)
	if allowedErr != nil {
		return WorkItem{}, allowedErr
	}
	if user != creator && !allowed {
		return WorkItem{}, &Error{Status: 403, Message: "only the assignee or run owner can transfer this work item", Code: "action_forbidden"}
	}
	requestHash := workflowCommandHash("workflow_work_item.transfer", itemID, struct {
		RunID            string `json:"run_id"`
		MemberID         string `json:"member_id"`
		Reason           string `json:"reason"`
		ExpectedRevision int64  `json:"expected_state_revision"`
		ExpectedItemVer  int64  `json:"expected_item_version"`
	}{RunID: runID, MemberID: memberID, Reason: reason, ExpectedRevision: expectedStateRevision, ExpectedItemVer: expectedItemVersion})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow_work_item.transfer", itemID, idempotencyKey, requestHash)
	if err != nil {
		return WorkItem{}, err
	}
	if len(responseBody) > 0 {
		var replay WorkItem
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return WorkItem{}, err
		}
		return replay, nil
	}
	if expectedStateRevision > 0 && expectedStateRevision != stateRevision(run) {
		return WorkItem{}, Conflict("run changed; refresh before transferring the work item")
	}
	if expectedItemVersion != item.Version {
		return WorkItem{}, Conflict("work item changed; refresh before transferring")
	}
	var memberExists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND (id=$2 OR user_id=$2))", ws, memberID).Scan(&memberExists); err != nil {
		return WorkItem{}, err
	}
	if !memberExists {
		return WorkItem{}, &Error{Status: 404, Message: "member not found in workspace", Code: "member_not_found"}
	}
	if _, err = tx.Exec(ctx, `UPDATE workflow_work_item SET assignee_id=$4,version=version+1,updated_at=now() WHERE workspace_id=$1 AND run_id=$2 AND id=$3 AND status='open' AND version=$5`, ws, runID, itemID, memberID, item.Version); err != nil {
		return WorkItem{}, err
	}
	run.StateRevision = stateRevision(run) + 1
	if err = saveRun(ctx, tx, &run); err != nil {
		return WorkItem{}, err
	}
	if err = s.recordRunMutation(ctx, tx, run, protocol.EventWorkflowWorkItemUpdated, "member", user); err != nil {
		return WorkItem{}, err
	}
	updatedItem, err := scanWorkItem(tx.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3`, ws, runID, itemID).Scan)
	if err != nil {
		return WorkItem{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow_work_item.transfer", itemID, idempotencyKey, requestHash, updatedItem); err != nil {
		return WorkItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return WorkItem{}, err
	}
	s.publish(protocol.EventWorkflowWorkItemUpdated, ws, run.WorkflowID, run.ID)
	return updatedItem, nil
}

func (s *Service) ExtendWorkItem(ctx context.Context, ws, user, runID, itemID string, dueAt time.Time, reason string, expectedStateRevision, expectedItemVersion int64) (WorkItem, error) {
	return s.extendWorkItem(ctx, ws, user, runID, itemID, dueAt, reason, expectedStateRevision, expectedItemVersion, "")
}

func (s *Service) ExtendWorkItemKey(ctx context.Context, ws, user, runID, itemID string, dueAt time.Time, reason string, expectedStateRevision, expectedItemVersion int64, idempotencyKey string) (WorkItem, error) {
	if len(strings.TrimSpace(idempotencyKey)) == 0 {
		return WorkItem{}, Bad("idempotency_key is required")
	}
	return s.extendWorkItem(ctx, ws, user, runID, itemID, dueAt, reason, expectedStateRevision, expectedItemVersion, idempotencyKey)
}

func (s *Service) extendWorkItem(ctx context.Context, ws, user, runID, itemID string, dueAt time.Time, reason string, expectedStateRevision, expectedItemVersion int64, idempotencyKey string) (WorkItem, error) {
	if _, err := parseWorkItemUUID(itemID); err != nil {
		return WorkItem{}, err
	}
	if len([]rune(reason)) > 2000 {
		return WorkItem{}, Bad("extension reason is too long")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return WorkItem{}, err
	}
	defer tx.Rollback(ctx)
	run, creator, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return WorkItem{}, err
	}
	item, err := scanWorkItem(tx.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3 FOR UPDATE OF wi`, ws, runID, itemID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkItem{}, &Error{Status: 404, Message: "work item not found", Code: "work_item_not_found"}
	}
	if err != nil {
		return WorkItem{}, err
	}
	if item.Status != "open" {
		return WorkItem{}, Conflict("work item has already been resolved")
	}
	allowed, allowedErr := workItemActorAllowed(ctx, tx, ws, item.AssigneeID, user)
	if allowedErr != nil {
		return WorkItem{}, allowedErr
	}
	if user != creator && !allowed {
		return WorkItem{}, &Error{Status: 403, Message: "only the assignee or run owner can extend this work item", Code: "action_forbidden"}
	}
	requestHash := workflowCommandHash("workflow_work_item.extend", itemID, struct {
		RunID            string    `json:"run_id"`
		DueAt            time.Time `json:"due_at"`
		Reason           string    `json:"reason"`
		ExpectedRevision int64     `json:"expected_state_revision"`
		ExpectedItemVer  int64     `json:"expected_item_version"`
	}{RunID: runID, DueAt: dueAt.UTC(), Reason: reason, ExpectedRevision: expectedStateRevision, ExpectedItemVer: expectedItemVersion})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow_work_item.extend", itemID, idempotencyKey, requestHash)
	if err != nil {
		return WorkItem{}, err
	}
	if len(responseBody) > 0 {
		var replay WorkItem
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return WorkItem{}, err
		}
		return replay, nil
	}
	if dueAt.IsZero() || !dueAt.After(time.Now().UTC()) {
		return WorkItem{}, Bad("due_at must be in the future")
	}
	if expectedStateRevision > 0 && expectedStateRevision != stateRevision(run) {
		return WorkItem{}, Conflict("run changed; refresh before extending the work item")
	}
	if expectedItemVersion != item.Version {
		return WorkItem{}, Conflict("work item changed; refresh before extending")
	}
	if _, err = tx.Exec(ctx, `UPDATE workflow_work_item SET due_at=$4,version=version+1,updated_at=now() WHERE workspace_id=$1 AND run_id=$2 AND id=$3 AND status='open' AND version=$5`, ws, runID, itemID, dueAt, item.Version); err != nil {
		return WorkItem{}, err
	}
	run.StateRevision = stateRevision(run) + 1
	if err = saveRun(ctx, tx, &run); err != nil {
		return WorkItem{}, err
	}
	if err = s.recordRunMutation(ctx, tx, run, protocol.EventWorkflowWorkItemUpdated, "member", user); err != nil {
		return WorkItem{}, err
	}
	updatedItem, err := scanWorkItem(tx.QueryRow(ctx, workItemSelect+` WHERE wi.workspace_id=$1 AND wi.run_id=$2 AND wi.id=$3`, ws, runID, itemID).Scan)
	if err != nil {
		return WorkItem{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow_work_item.extend", itemID, idempotencyKey, requestHash, updatedItem); err != nil {
		return WorkItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return WorkItem{}, err
	}
	s.publish(protocol.EventWorkflowWorkItemUpdated, ws, run.WorkflowID, run.ID)
	return updatedItem, nil
}

func stateRevision(run Run) int64 {
	if run.StateRevision <= 0 {
		return 1
	}
	return run.StateRevision
}

func applySimpleRework(run *Run, reviewNodeID, feedback string) error {
	var scope *Scope
	for _, candidate := range run.Graph.Scopes {
		if candidate.ExitNodeID == reviewNodeID && candidate.MaxReworks > 0 {
			copied := candidate
			scope = &copied
			break
		}
	}
	if scope == nil {
		return Bad("rework target is not configured")
	}
	if run.ReworkCounts == nil {
		run.ReworkCounts = map[string]int{}
	}
	limit := scope.MaxReworks
	if limit <= 0 {
		limit = 3
	}
	if run.ReworkCounts[scope.ID] >= limit {
		return Conflict("rework limit has been reached")
	}
	run.ReworkCounts[scope.ID]++
	entry := scope.EntryNodeID
	if entry == "" {
		for _, edge := range run.Graph.Edges {
			if edge.Target == reviewNodeID && normalFlowEdge(edge) {
				entry = edge.Source
				break
			}
		}
	}
	if entry == "" {
		return Bad("rework scope entry is not configured")
	}
	forward := map[string]bool{}
	var visitForward func(string)
	visitForward = func(nodeID string) {
		if forward[nodeID] {
			return
		}
		forward[nodeID] = true
		if nodeID == reviewNodeID {
			return
		}
		for _, edge := range run.Graph.Edges {
			if normalFlowEdge(edge) && edge.Source == nodeID {
				visitForward(edge.Target)
			}
		}
	}
	visitForward(entry)
	backward := map[string]bool{}
	var visitBackward func(string)
	visitBackward = func(nodeID string) {
		if backward[nodeID] {
			return
		}
		backward[nodeID] = true
		for _, edge := range run.Graph.Edges {
			if normalFlowEdge(edge) && edge.Target == nodeID {
				visitBackward(edge.Source)
			}
		}
	}
	visitBackward(reviewNodeID)
	generation := run.ReworkCounts[scope.ID] + 1
	scopeInstanceID := id()
	for i := range run.Nodes {
		node := &run.Nodes[i]
		if forward[node.NodeID] && backward[node.NodeID] {
			replaceNodeActivation(node, true)
			node.Status, node.WorkItemID = "pending", ""
			node.Error, node.ReasonCode, node.RetryAt = "", "rework_requested", nil
			node.Generation = generation
			node.ActivationNo = 1
			node.ScopeInstanceID = scopeInstanceID
			node.ScopeDefinitionID = scope.ID
		}
	}
	run.Error = "rework feedback: " + feedback
	run.Status = "running"
	return nil
}
