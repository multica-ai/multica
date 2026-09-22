package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type SimulationFixture struct {
	Output string         `json:"output,omitempty"`
	Values map[string]any `json:"values,omitempty"`
	Action string         `json:"action,omitempty"`
}

type TestRunRequest struct {
	Mode     string
	Revision int64
	Input    map[string]any
	Key      string
	Fixtures map[string]SimulationFixture
}

func (s *Service) StartTestRun(ctx context.Context, ws, user, wid string, request TestRunRequest) (Run, error) {
	if request.Mode != "simulation" && request.Mode != "test" {
		return Run{}, Bad("mode must be simulation or test")
	}
	if request.Key == "" || len(request.Key) > 200 {
		return Run{}, Bad("idempotency_key is required")
	}
	if request.Input == nil {
		request.Input = map[string]any{}
	}
	inputBytes, err := json.Marshal(request.Input)
	if err != nil || len(inputBytes) > 100000 {
		return Run{}, Bad("input_values is invalid or too large")
	}
	if request.Mode == "test" {
		return s.startRun(ctx, ws, user, wid, string(inputBytes), request.Key, request.Revision, "", "test")
	}
	return s.startSimulationRun(ctx, ws, user, wid, request, string(inputBytes))
}

func (s *Service) startSimulationRun(ctx context.Context, ws, user, wid string, request TestRunRequest, input string) (Run, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	if _, err = q.LockWorkspaceForDelete(ctx, uuid(ws)); err != nil {
		return Run{}, err
	}
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return Run{}, err
	}
	requestHash := simulationRequestHash(wid, request)
	var existing []byte
	var existingHash *string
	err = tx.QueryRow(ctx, "SELECT body,request_hash FROM workflow_run WHERE workflow_id=$1 AND creator_id=$2 AND idempotency_key=$3", wid, user, request.Key).Scan(&existing, &existingHash)
	if err == nil {
		if existingHash != nil && *existingHash != "" && *existingHash != requestHash {
			return Run{}, Conflict("idempotency key was already used for a different test run request")
		}
		var existingRun Run
		if unmarshalErr := json.Unmarshal(existing, &existingRun); unmarshalErr != nil {
			return Run{}, unmarshalErr
		}
		existingRun.WorkspaceID = ws
		populateRunResponse(&existingRun)
		return existingRun, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Run{}, err
	}
	if w.Revision != request.Revision {
		return Run{}, Conflict("save the latest workflow before starting a simulation")
	}
	if err = requireV2Graph(w.Graph); err != nil {
		return Run{}, err
	}
	if len(w.UpgradeIssues) > 0 {
		return Run{}, &Error{Status: 422, Code: "upgrade_required", Message: "repair the workflow upgrade issues before running", Details: append([]ValidationDetail(nil), w.UpgradeIssues...)}
	}
	if err = Validate(w.Graph, true); err != nil {
		return Run{}, err
	}
	if err = validateRunInput(w.Graph, input); err != nil {
		return Run{}, err
	}
	if err = s.validateRunInputArtifacts(ctx, ws, user, w.Graph, input); err != nil {
		return Run{}, err
	}
	if err = s.validateMembers(ctx, ws, w.Graph, true); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	var deadlineAt *time.Time
	if w.Graph.Defaults.RunTimeoutSeconds > 0 {
		deadline := now.Add(time.Duration(w.Graph.Defaults.RunTimeoutSeconds) * time.Second)
		deadlineAt = &deadline
	}
	r := Run{ID: id(), WorkspaceID: ws, WorkflowID: wid, WorkflowRevision: w.Revision, Mode: "simulation", StateRevision: 1, OwnerID: user, DeadlineAt: deadlineAt, Graph: w.Graph, Input: input, Status: "queued", Nodes: make([]NodeRun, 0, len(w.Graph.Nodes)), RootScopeID: id(), CreatedAt: now, UpdatedAt: now}
	r.InputValues = request.Input
	for _, node := range w.Graph.Nodes {
		state := NodeRun{NodeID: node.ID, Status: "pending", Generation: 1}
		if node.Type == "start" {
			state.Status = "succeeded"
			state.Output = input
			state.ActivationID = id()
			state.ScopeInstanceID = r.RootScopeID
		}
		r.Nodes = append(r.Nodes, state)
	}
	r = simulateRun(r, request.Fixtures)
	if err = ensureRootScope(ctx, tx, &r); err != nil {
		return Run{}, err
	}
	if err = persistNormalizedRun(ctx, tx, &r); err != nil {
		return Run{}, err
	}
	if terminal(r.Status) {
		finished := time.Now().UTC()
		r.FinishedAt = &finished
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_run(id,workflow_id,workspace_id,creator_id,idempotency_key,status,body,engine_version,mode,input_values,owner_id,state_revision,request_hash,deadline_at,finished_at,reason_code)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, r.ID, wid, ws, user, request.Key, r.Status, body(r), 2, "simulation", body(request.Input), user, r.StateRevision, requestHash, r.DeadlineAt, r.FinishedAt, nullableText(r.ReasonCode))
	if err != nil {
		return Run{}, err
	}
	if _, err = appendWorkflowEvent(ctx, tx, r, protocol.EventWorkflowRunUpdated, "member", user, map[string]any{
		"status":         r.Status,
		"state_revision": r.StateRevision,
		"mode":           r.Mode,
	}); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	s.publish(protocol.EventWorkflowRunUpdated, ws, wid, r.ID)
	return r, nil
}

func simulateRun(run Run, fixtures map[string]SimulationFixture) Run {
	states := make(map[string]*NodeRun, len(run.Nodes))
	definitions := make(map[string]Node, len(run.Graph.Nodes))
	for i := range run.Nodes {
		states[run.Nodes[i].NodeID] = &run.Nodes[i]
	}
	for _, node := range run.Graph.Nodes {
		definitions[node.ID] = node
	}
	for pass := 0; pass < len(run.Nodes)+1; pass++ {
		changed := false
		for i := range run.Nodes {
			state := &run.Nodes[i]
			if state.Status != "pending" && state.Status != "blocked" {
				continue
			}
			definition := definitions[state.NodeID]
			incoming := incomingEdges(run.Graph, state.NodeID)
			ready, hasSuccess := true, false
			for _, edge := range incoming {
				upstream := states[edge.Source]
				if upstream == nil {
					ready = false
					break
				}
				switch upstream.Status {
				case "succeeded":
					hasSuccess = true
				case "skipped":
				case "failed", "blocked", "cancelled":
					if isFailureEdge(edge) && upstream.ReasonCode == "failure_routed" {
						hasSuccess = true
					} else {
						ready = false
					}
				default:
					ready = false
				}
			}
			if !ready {
				continue
			}
			if len(incoming) > 0 && !hasSuccess {
				state.Status, state.ReasonCode = "skipped", "condition_not_selected"
				changed = true
				continue
			}
			if state.ActivationID == "" {
				state.ActivationID = id()
				state.ScopeInstanceID = run.RootScopeID
			}
			switch definition.Type {
			case "start", "parallel", "merge":
				state.Status = "succeeded"
				state.Output = inputs(run, definition, states)
				changed = true
			case "condition":
				state.Status = "succeeded"
				state.Output = inputs(run, definition, states)
				chosen, conditionErr := chooseConditionEdgeForRun(definition, run.Graph.Edges, run, states)
				if conditionErr != nil {
					state.Status, state.ReasonCode, state.Error = "blocked", "condition_input_missing", conditionErr.Error()
					changed = true
					continue
				}
				for _, edge := range run.Graph.Edges {
					if normalFlowEdge(edge) && edge.Source == definition.ID && edge.Target != chosen.Target {
						markSkipped(states, edge.Target)
					}
				}
				changed = true
			case "agent", "human_task", "human_review":
				fixture, ok := fixtures[state.NodeID]
				if !ok {
					state.Status = "waiting_human"
					state.ReasonCode = "simulation_fixture_required"
					changed = true
					continue
				}
				if fixture.Action == "rework" {
					state.Status = "failed"
					state.ReasonCode = "simulation_rework_requested"
					state.Error = "simulation fixture requested rework"
					applySimulationFailurePolicy(run, state, definition)
					changed = true
					continue
				}
				output := fixture.Output
				if output == "" && fixture.Values != nil {
					encoded, _ := json.Marshal(fixture.Values)
					output = string(encoded)
				}
				if outputErr := validateNodeOutput(definition, output); outputErr != nil {
					state.Status, state.ReasonCode, state.Error = "failed", "output_invalid", outputErr.Error()
					applySimulationFailurePolicy(run, state, definition)
					changed = true
					continue
				}
				state.Status, state.Output, state.ReasonCode = "succeeded", output, "simulation_fixture"
				changed = true
			case "end":
				state.Status = "succeeded"
				state.Output = inputs(run, definition, states)
				run.Output = state.Output
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	hasWaiting, hasFailure, endSucceeded := false, false, false
	for _, state := range run.Nodes {
		if state.Status == "waiting_human" || state.Status == "pending" {
			hasWaiting = true
		}
		if (state.Status == "failed" && state.ReasonCode != "failure_routed") || state.Status == "blocked" {
			hasFailure = true
		}
		if definitions[state.NodeID].Type == "end" && state.Status == "succeeded" {
			endSucceeded = true
		}
	}
	switch {
	case endSucceeded:
		run.Status = "succeeded"
	case hasFailure:
		run.Status = "blocked"
	case hasWaiting:
		run.Status = "waiting"
	default:
		run.Status = "blocked"
		run.Error = "simulation stopped before End"
	}
	return run
}

func applySimulationFailurePolicy(run Run, nodeRun *NodeRun, node Node) {
	switch nodeFailureAction(node) {
	case "route":
		if len(failureEdges(run.Graph, node.ID)) > 0 {
			if nodeRun.Status == "blocked" {
				nodeRun.Status = "failed"
			}
			nodeRun.ReasonCode = "failure_routed"
		}
	case "takeover":
		nodeRun.Status = "waiting_human"
		nodeRun.ReasonCode = "awaiting_takeover"
	}
}

func simulationRequestHash(workflowID string, request TestRunRequest) string {
	payload, _ := json.Marshal(struct {
		WorkflowID string
		Mode       string
		Revision   int64
		Input      map[string]any
		Fixtures   map[string]SimulationFixture
	}{workflowID, request.Mode, request.Revision, request.Input, request.Fixtures})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
