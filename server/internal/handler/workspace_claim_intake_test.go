package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
)

type claimIntakeStatusTestResponse struct {
	WorkspaceID   string  `json:"workspace_id"`
	State         string  `json:"state"`
	Generation    int64   `json:"generation"`
	UpdatedByType string  `json:"updated_by_type"`
	UpdatedByID   *string `json:"updated_by_id"`
	AuthSource    string  `json:"auth_source"`
	Reason        string  `json:"reason"`
	LastActionID  *string `json:"last_action_id"`
	EffectiveAt   string  `json:"effective_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type claimIntakeMutationTestResponse struct {
	ActionID        string  `json:"action_id"`
	LastActionID    *string `json:"last_action_id"`
	WorkspaceID     string  `json:"workspace_id"`
	RequestedAction string  `json:"requested_action"`
	PreviousState   string  `json:"previous_state"`
	State           string  `json:"state"`
	ResultingState  string  `json:"resulting_state"`
	Generation      int64   `json:"generation"`
	ActorType       string  `json:"actor_type"`
	ActorID         string  `json:"actor_id"`
	Actor           struct {
		UserID  string `json:"user_id"`
		Display string `json:"display"`
	} `json:"actor"`
	IdempotencyKey string    `json:"idempotency_key"`
	Reason         string    `json:"reason"`
	RequestedAt    time.Time `json:"requested_at"`
	EffectiveAt    time.Time `json:"effective_at"`
	Result         string    `json:"result"`
	ErrorClass     *string   `json:"error_class"`
}

type claimIntakeActionsTestResponse struct {
	Actions []workspaceClaimIntakeActionResponse `json:"actions"`
	Limit   int32                                `json:"limit"`
	Offset  int32                                `json:"offset"`
}

type claimIntakeLedgerTestTask struct {
	TaskID                string     `json:"task_id"`
	Status                string     `json:"status"`
	AgentID               string     `json:"agent_id"`
	RuntimeID             *string    `json:"runtime_id"`
	ConsumerID            *string    `json:"consumer_id"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	PrepareLeaseExpiresAt *time.Time `json:"prepare_lease_expires_at"`
	StaleReclaimable      bool       `json:"stale_reclaimable"`
	ClaimGeneration       *int64     `json:"claim_generation"`
	ClaimActionID         *string    `json:"claim_action_id"`
	FenceClassification   string     `json:"fence_classification"`
}

type claimIntakeLedgerTestResponse struct {
	WorkspaceID  string                      `json:"workspace_id"`
	State        string                      `json:"state"`
	Generation   int64                       `json:"generation"`
	LastActionID *string                     `json:"last_action_id"`
	EffectiveAt  time.Time                   `json:"effective_at"`
	Counts       map[string]int64            `json:"counts"`
	Tasks        []claimIntakeLedgerTestTask `json:"tasks"`
	Limit        int32                       `json:"limit"`
	Offset       int32                       `json:"offset"`
}

func resetWorkspaceClaimIntakeForTest(t *testing.T, workspaceID string) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM workspace_claim_intake_action WHERE workspace_id = $1`, workspaceID); err != nil {
		t.Fatalf("delete claim-intake actions: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
UPDATE workspace_claim_intake_control
SET state = 'resumed',
    generation = 0,
    updated_by_type = 'system',
    updated_by_id = NULL,
    auth_source = 'system',
    actor_display = 'system',
    reason = 'test reset',
    authoritative_action_id = NULL,
    effective_at = now(),
    updated_at = now()
WHERE workspace_id = $1
`, workspaceID); err != nil {
		t.Fatalf("reset claim-intake control: %v", err)
	}
}

func claimIntakeMutationRequest(t *testing.T, method, workspaceID, idempotencyKey string, body any) *http.Request {
	t.Helper()
	req := newRequest(method, "/api/workspaces/"+workspaceID+"/claim-intake", body)
	req = withURLParam(req, "id", workspaceID)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Auth-Source", "jwt")
	return req
}

func decodeClaimIntakeMutationTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) claimIntakeMutationTestResponse {
	t.Helper()
	var response claimIntakeMutationTestResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	return response
}

func TestWorkspaceClaimIntakePause_PersistsActorProvenance(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"actor-provenance",
		map[string]any{"reason": "audit operator provenance"},
	)
	testHandler.PauseWorkspaceClaimIntake(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var actorType, actorID, authSource string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT actor_type, actor_id, auth_source
		   FROM workspace_claim_intake_action
		  WHERE workspace_id = $1 AND idempotency_key = $2`,
		testWorkspaceID,
		"actor-provenance",
	).Scan(&actorType, &actorID, &authSource); err != nil {
		t.Fatalf("load action provenance: %v", err)
	}
	if actorType != "member" || actorID != testUserID || authSource != "jwt" {
		t.Fatalf(
			"persisted provenance = type:%q id:%q source:%q, want member %s jwt",
			actorType,
			actorID,
			authSource,
			testUserID,
		)
	}
}

func TestWorkspaceClaimIntakeStatus_UsesCanonicalAuthoritativeStateContract(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pauseRecorder := httptest.NewRecorder()
	pauseRequest := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"authoritative-state-contract",
		map[string]any{"reason": "record authoritative actor"},
	)
	testHandler.PauseWorkspaceClaimIntake(pauseRecorder, pauseRequest)
	if pauseRecorder.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pauseRecorder.Code, pauseRecorder.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pauseRecorder)

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.GetWorkspaceClaimIntakeStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response claimIntakeStatusTestResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode authoritative status: %v", err)
	}
	if response.UpdatedByType != "member" ||
		response.UpdatedByID == nil ||
		*response.UpdatedByID != testUserID ||
		response.AuthSource != "jwt" {
		t.Fatalf("authoritative provenance = type:%q id:%v source:%q", response.UpdatedByType, response.UpdatedByID, response.AuthSource)
	}
	if response.LastActionID == nil ||
		*response.LastActionID != pauseResponse.ActionID {
		t.Fatalf("last action = %v, want %s", response.LastActionID, pauseResponse.ActionID)
	}
}

func TestWorkspaceClaimIntakeStatus_UsesCanonicalLastActionID(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pauseRecorder := httptest.NewRecorder()
	pauseRequest := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"canonical-last-action-id",
		map[string]any{"reason": "record canonical last action"},
	)
	testHandler.PauseWorkspaceClaimIntake(pauseRecorder, pauseRequest)
	if pauseRecorder.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pauseRecorder.Code, pauseRecorder.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pauseRecorder)

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.GetWorkspaceClaimIntakeStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode claim-intake status: %v", err)
	}
	if response["last_action_id"] != pauseResponse.ActionID {
		t.Fatalf(
			"last_action_id = %v, want %s: %+v",
			response["last_action_id"],
			pauseResponse.ActionID,
			response,
		)
	}
	if _, legacy := response["authoritative_action_id"]; legacy {
		t.Fatalf("legacy authoritative_action_id must not be emitted: %+v", response)
	}
}

func TestWorkspaceClaimIntakeActions_UsesCanonicalRequestedActionContract(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pauseRecorder := httptest.NewRecorder()
	pauseRequest := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"canonical-action-history",
		map[string]any{"reason": "canonical action history"},
	)
	testHandler.PauseWorkspaceClaimIntake(pauseRecorder, pauseRequest)
	if pauseRecorder.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pauseRecorder.Code, pauseRecorder.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake/actions",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeActions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("actions status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Actions []map[string]any `json:"actions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode action history: %v", err)
	}
	if len(response.Actions) != 1 {
		t.Fatalf("actions = %+v, want one", response.Actions)
	}
	action := response.Actions[0]
	if action["requested_action"] != "pause" {
		t.Fatalf("requested_action = %v, want pause", action["requested_action"])
	}
	if action["resulting_state"] != "paused" {
		t.Fatalf("resulting_state = %v, want paused", action["resulting_state"])
	}
	if _, ok := action["action"]; ok {
		t.Fatalf("legacy action field must not be emitted: %+v", action)
	}
}

func TestWorkspaceClaimIntakePause_UsesCanonicalWireContract(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	const idempotencyKey = "canonical-wire-contract"
	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, idempotencyKey, map[string]any{
		"reason": "planned maintenance",
	})
	testHandler.PauseWorkspaceClaimIntake(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var wire map[string]any
	if err := json.NewDecoder(w.Body).Decode(&wire); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"action_id", "workspace_id", "requested_action", "previous_state", "state", "generation", "actor_type", "actor_id", "effective_at", "idempotency_key", "reason"} {
		if _, ok := wire[field]; !ok {
			t.Errorf("missing canonical response field %q: %v", field, wire)
		}
	}
	if wire["last_action_id"] != wire["action_id"] {
		t.Fatalf(
			"last_action_id = %v, want action_id %v: %v",
			wire["last_action_id"],
			wire["action_id"],
			wire,
		)
	}
	for _, legacy := range []string{
		"action",
		"resulting_state",
		"actor",
		"authoritative_action_id",
	} {
		if _, ok := wire[legacy]; ok {
			t.Errorf("legacy response field %q must not be emitted: %v", legacy, wire)
		}
	}
	if wire["requested_action"] != "pause" || wire["previous_state"] != "resumed" || wire["state"] != "paused" {
		t.Fatalf("canonical transition fields = action:%v previous:%v state:%v", wire["requested_action"], wire["previous_state"], wire["state"])
	}
	if wire["actor_type"] != "member" || wire["actor_id"] != testUserID {
		t.Fatalf("canonical actor fields = type:%v id:%v", wire["actor_type"], wire["actor_id"])
	}
	if wire["idempotency_key"] != idempotencyKey {
		t.Fatalf("idempotency_key = %v, want %s", wire["idempotency_key"], idempotencyKey)
	}

	var state string
	if err := testPool.QueryRow(context.Background(), `SELECT state FROM workspace_claim_intake_control WHERE workspace_id = $1`, testWorkspaceID).Scan(&state); err != nil {
		t.Fatalf("load control: %v", err)
	}
	if state != "paused" {
		t.Fatalf("persisted state = %q, want paused", state)
	}
}

func TestWorkspaceClaimIntakePause_OwnerSuccess(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "owner-pause-success", map[string]any{
		"reason": "planned maintenance",
	})
	testHandler.PauseWorkspaceClaimIntake(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, w)
	if response.ActionID == "" || response.LastActionID == nil || *response.LastActionID != response.ActionID {
		t.Fatalf("action ids = action:%q authoritative:%v; want matching non-empty ids", response.ActionID, response.LastActionID)
	}
	if response.PreviousState != "resumed" || response.State != "paused" || response.Generation != 1 || response.Result != "applied" {
		t.Fatalf("unexpected mutation response: %+v", response)
	}
	if response.ActorType != "member" || response.ActorID != testUserID {
		t.Fatalf("actor = type:%q id:%q, want member %s", response.ActorType, response.ActorID, testUserID)
	}
	if response.EffectiveAt.IsZero() {
		t.Fatal("effective_at must be populated")
	}

	var state, actionID string
	var generation int64
	if err := testPool.QueryRow(context.Background(), `
SELECT state, generation, authoritative_action_id::text
FROM workspace_claim_intake_control
WHERE workspace_id = $1
`, testWorkspaceID).Scan(&state, &generation, &actionID); err != nil {
		t.Fatalf("load control: %v", err)
	}
	if state != "paused" || generation != 1 || actionID != response.ActionID {
		t.Fatalf("control = state:%s generation:%d action:%s", state, generation, actionID)
	}
}

func TestWorkspaceClaimIntakePause_AdminSuccess(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	ctx := context.Background()
	email := fmt.Sprintf("claim-intake-admin-%d@multica.test", time.Now().UnixNano())
	var adminID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Claim Intake Admin', $1) RETURNING id`, email).Scan(&adminID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`, testWorkspaceID, adminID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, adminID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, adminID)
	})

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "admin-pause-success", map[string]any{
		"reason": "admin maintenance",
	})
	req.Header.Set("X-User-ID", adminID)
	testHandler.PauseWorkspaceClaimIntake(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, w)
	if response.ActorType != "member" || response.ActorID != adminID {
		t.Fatalf("actor = type:%q id:%q, want member %s", response.ActorType, response.ActorID, adminID)
	}
}

func TestWorkspaceClaimIntakePause_MemberDenied(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	ctx := context.Background()
	email := fmt.Sprintf("claim-intake-member-%d@multica.test", time.Now().UnixNano())
	var memberID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Claim Intake Member', $1) RETURNING id`, email).Scan(&memberID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "member-pause-denied", map[string]any{
		"reason": "not authorized",
	})
	req.Header.Set("X-User-ID", memberID)
	testHandler.PauseWorkspaceClaimIntake(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "resumed", 0)
}

func TestWorkspaceClaimIntakePause_MachineCredentialsDenied(t *testing.T) {
	for _, actorSource := range []string{"task_token", "cloud_pat", "future_machine"} {
		t.Run(actorSource, func(t *testing.T) {
			resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
			w := httptest.NewRecorder()
			req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "machine-pause-denied-"+actorSource, map[string]any{
				"reason": "machine must not remove or create fence",
			})
			req.Header.Set("X-Actor-Source", actorSource)
			testHandler.PauseWorkspaceClaimIntake(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
			assertClaimIntakeStateForTest(t, testWorkspaceID, "resumed", 0)
		})
	}
}

func TestWorkspaceClaimIntakePause_DaemonTokenDenied(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "daemon-pause-denied", map[string]any{
		"reason": "daemon must not mutate fence",
	})
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "synthetic-daemon"))
	testHandler.PauseWorkspaceClaimIntake(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "resumed", 0)
}

func TestWorkspaceClaimIntakePause_CrossWorkspaceDenied(t *testing.T) {
	ctx := context.Background()
	slug := fmt.Sprintf("claim-intake-cross-%d", time.Now().UnixNano())
	var workspaceID string
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('Cross Workspace', $1) RETURNING id`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create cross workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_claim_intake_action WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(t, http.MethodPost, workspaceID, "cross-workspace-denied", map[string]any{
		"reason": "cross workspace attempt",
	})
	testHandler.PauseWorkspaceClaimIntake(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	assertClaimIntakeStateForTest(t, workspaceID, "resumed", 0)
}

func TestWorkspaceClaimIntakeResume_MachineCredentialsCannotRemoveFence(t *testing.T) {
	for _, actorSource := range []string{"task_token", "cloud_pat", "future_machine"} {
		t.Run(actorSource, func(t *testing.T) {
			resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
			if _, err := testPool.Exec(context.Background(), `
UPDATE workspace_claim_intake_control
SET state = 'paused', generation = 7, updated_at = now()
WHERE workspace_id = $1
`, testWorkspaceID); err != nil {
				t.Fatalf("seed paused control: %v", err)
			}
			w := httptest.NewRecorder()
			req := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "machine-resume-denied-"+actorSource, map[string]any{
				"reason":              "machine must not remove fence",
				"expected_generation": 7,
			})
			req.Header.Set("X-Actor-Source", actorSource)
			testHandler.ResumeWorkspaceClaimIntake(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
			assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 7)
		})
	}
}

func TestWorkspaceClaimIntakePause_RecordsRequestArrivalBeforeFenceEffectiveTime(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	lockTx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin control lock transaction: %v", err)
	}
	defer lockTx.Rollback(context.Background())
	if _, err := lockTx.Exec(
		context.Background(),
		`SELECT workspace_id FROM workspace_claim_intake_control WHERE workspace_id = $1 FOR UPDATE`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("lock claim-intake control: %v", err)
	}

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"request-before-fence-effective",
		map[string]any{"reason": "verify request and fence timestamps"},
	)
	requestStartedAt := time.Now().UTC()
	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.PauseWorkspaceClaimIntake(w, req)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := testPool.QueryRow(context.Background(), `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE datname = current_database()
      AND pid <> pg_backend_pid()
      AND wait_event_type = 'Lock'
      AND query LIKE '%workspace_claim_intake_control%'
      AND query LIKE '%FOR UPDATE%'
)
`).Scan(&waiting); err != nil {
			t.Fatalf("detect mutation waiting on control lock: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mutation did not wait on the claim-intake control lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	lockReleasedAt := time.Now().UTC()
	if err := lockTx.Commit(context.Background()); err != nil {
		t.Fatalf("release claim-intake control lock: %v", err)
	}
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, w)
	if response.RequestedAt.Before(requestStartedAt) || !response.RequestedAt.Before(lockReleasedAt) {
		t.Fatalf(
			"requested_at = %s, want within request interval [%s, %s)",
			response.RequestedAt,
			requestStartedAt,
			lockReleasedAt,
		)
	}
	if response.EffectiveAt.Before(lockReleasedAt) {
		t.Fatalf(
			"effective_at = %s, want at or after fence lock release %s",
			response.EffectiveAt,
			lockReleasedAt,
		)
	}
	if !response.RequestedAt.Before(response.EffectiveAt) {
		t.Fatalf(
			"requested_at = %s, effective_at = %s; want request before effective result",
			response.RequestedAt,
			response.EffectiveAt,
		)
	}
}

func TestWorkspaceClaimIntakePause_IdempotencyReplayExact(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	const key = "pause-exact-replay"

	first := httptest.NewRecorder()
	firstReq := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, key, map[string]any{
		"reason": "original reason",
	})
	testHandler.PauseWorkspaceClaimIntake(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200: %s", first.Code, first.Body.String())
	}

	replay := httptest.NewRecorder()
	replayReq := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, key, map[string]any{
		"reason": "different replay body must be ignored",
	})
	testHandler.PauseWorkspaceClaimIntake(replay, replayReq)
	if replay.Code != first.Code {
		t.Fatalf("replay status = %d, want %d", replay.Code, first.Code)
	}
	if !bytes.Equal(replay.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("replay body differs\nfirst:  %s\nreplay: %s", first.Body.String(), replay.Body.String())
	}

	var actionCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_claim_intake_action WHERE workspace_id = $1`, testWorkspaceID).Scan(&actionCount); err != nil {
		t.Fatalf("count actions: %v", err)
	}
	if actionCount != 1 {
		t.Fatalf("action count = %d, want 1", actionCount)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)
}

func TestWorkspaceClaimIntakePause_AlreadyPausedRecordsNoopWithoutGenerationChange(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	first := httptest.NewRecorder()
	firstReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"pause-before-noop",
		map[string]any{"reason": "establish fence"},
	)
	testHandler.PauseWorkspaceClaimIntake(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("first pause status = %d: %s", first.Code, first.Body.String())
	}
	applied := decodeClaimIntakeMutationTestResponse(t, first)

	noopRecorder := httptest.NewRecorder()
	noopReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"pause-noop",
		map[string]any{"reason": "fence is already established"},
	)
	testHandler.PauseWorkspaceClaimIntake(noopRecorder, noopReq)
	if noopRecorder.Code != http.StatusOK {
		t.Fatalf("noop pause status = %d: %s", noopRecorder.Code, noopRecorder.Body.String())
	}
	noop := decodeClaimIntakeMutationTestResponse(t, noopRecorder)
	if noop.ActionID == "" || noop.ActionID == applied.ActionID {
		t.Fatalf("noop action id = %q, applied action id = %q", noop.ActionID, applied.ActionID)
	}
	if noop.LastActionID == nil || *noop.LastActionID != applied.ActionID {
		t.Fatalf("noop authoritative action = %v, want %s", noop.LastActionID, applied.ActionID)
	}
	if noop.PreviousState != "paused" ||
		noop.State != "paused" ||
		noop.Generation != 1 ||
		noop.Result != "noop" ||
		noop.ErrorClass != nil {
		t.Fatalf("noop pause response = %+v", noop)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)
}

func TestWorkspaceClaimIntakeResume_SucceedsAtExpectedGeneration(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pause := httptest.NewRecorder()
	pauseReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"pause-before-successful-resume",
		map[string]any{"reason": "planned maintenance"},
	)
	testHandler.PauseWorkspaceClaimIntake(pause, pauseReq)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pause.Code, pause.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pause)

	resume := httptest.NewRecorder()
	resumeReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"successful-resume",
		map[string]any{
			"reason":              "maintenance complete",
			"expected_generation": pauseResponse.Generation,
		},
	)
	testHandler.ResumeWorkspaceClaimIntake(resume, resumeReq)
	if resume.Code != http.StatusOK {
		t.Fatalf("resume status = %d: %s", resume.Code, resume.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, resume)
	if response.ActionID == "" ||
		response.LastActionID == nil ||
		*response.LastActionID != response.ActionID ||
		response.PreviousState != "paused" ||
		response.State != "resumed" ||
		response.Generation != pauseResponse.Generation+1 ||
		response.Result != "applied" ||
		response.ErrorClass != nil {
		t.Fatalf("successful resume response = %+v", response)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "resumed", 2)
}

func TestWorkspaceClaimIntakeResume_AlreadyActiveRecordsNoopWithoutGenerationChange(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	w := httptest.NewRecorder()
	req := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"active-resume-noop",
		map[string]any{
			"reason":              "confirm active intake",
			"expected_generation": 0,
		},
	)
	testHandler.ResumeWorkspaceClaimIntake(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resume noop status = %d: %s", w.Code, w.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, w)
	if response.ActionID == "" ||
		response.LastActionID != nil ||
		response.PreviousState != "resumed" ||
		response.State != "resumed" ||
		response.Generation != 0 ||
		response.Result != "noop" ||
		response.ErrorClass != nil {
		t.Fatalf("resume noop response = %+v", response)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "resumed", 0)
}

func TestWorkspaceClaimIntakeActions_ReturnsCompleteAppliedNoopConflictAudit(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pauseApplied := httptest.NewRecorder()
	testHandler.PauseWorkspaceClaimIntake(
		pauseApplied,
		claimIntakeMutationRequest(
			t,
			http.MethodPost,
			testWorkspaceID,
			"audit-applied",
			map[string]any{"reason": "establish audited fence"},
		),
	)
	if pauseApplied.Code != http.StatusOK {
		t.Fatalf("applied status = %d: %s", pauseApplied.Code, pauseApplied.Body.String())
	}
	appliedResponse := decodeClaimIntakeMutationTestResponse(t, pauseApplied)

	pauseNoop := httptest.NewRecorder()
	testHandler.PauseWorkspaceClaimIntake(
		pauseNoop,
		claimIntakeMutationRequest(
			t,
			http.MethodPost,
			testWorkspaceID,
			"audit-noop",
			map[string]any{"reason": "record already-paused request"},
		),
	)
	if pauseNoop.Code != http.StatusOK {
		t.Fatalf("noop status = %d: %s", pauseNoop.Code, pauseNoop.Body.String())
	}
	noopResponse := decodeClaimIntakeMutationTestResponse(t, pauseNoop)

	resumeConflict := httptest.NewRecorder()
	testHandler.ResumeWorkspaceClaimIntake(
		resumeConflict,
		claimIntakeMutationRequest(
			t,
			http.MethodPost,
			testWorkspaceID,
			"audit-conflict",
			map[string]any{
				"reason":              "record stale operator request",
				"expected_generation": 0,
			},
		),
	)
	if resumeConflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d: %s", resumeConflict.Code, resumeConflict.Body.String())
	}
	conflictResponse := decodeClaimIntakeMutationTestResponse(t, resumeConflict)

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake/actions?limit=10&offset=0",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeActions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("actions status = %d: %s", w.Code, w.Body.String())
	}
	var response claimIntakeActionsTestResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode actions response: %v", err)
	}
	if response.Limit != 10 || response.Offset != 0 || len(response.Actions) != 3 {
		t.Fatalf("actions envelope = %+v", response)
	}

	byKey := make(map[string]workspaceClaimIntakeActionResponse, len(response.Actions))
	for _, action := range response.Actions {
		byKey[action.IdempotencyKey] = action
		if action.ActionID == "" ||
			action.WorkspaceID != testWorkspaceID ||
			action.RequestedAt.IsZero() ||
			action.EffectiveAt.IsZero() ||
			action.ActorType != "member" ||
			action.ActorID != testUserID ||
			action.AuthSource != "jwt" ||
			action.ActorDisplay == "" {
			t.Fatalf("incomplete action audit = %+v", action)
		}
	}

	applied := byKey["audit-applied"]
	if applied.ActionID != appliedResponse.ActionID ||
		applied.RequestedAction != "pause" ||
		applied.ExpectedGeneration != nil ||
		applied.Reason != "establish audited fence" ||
		applied.PreviousState != "resumed" ||
		applied.ResultingState != "paused" ||
		applied.Generation != 1 ||
		applied.Result != "applied" ||
		applied.ErrorClass != nil ||
		applied.ResponseStatus != http.StatusOK {
		t.Fatalf("applied audit = %+v", applied)
	}

	noop := byKey["audit-noop"]
	if noop.ActionID != noopResponse.ActionID ||
		noop.RequestedAction != "pause" ||
		noop.ExpectedGeneration != nil ||
		noop.Reason != "record already-paused request" ||
		noop.PreviousState != "paused" ||
		noop.ResultingState != "paused" ||
		noop.Generation != 1 ||
		noop.Result != "noop" ||
		noop.ErrorClass != nil ||
		noop.ResponseStatus != http.StatusOK {
		t.Fatalf("noop audit = %+v", noop)
	}

	conflict := byKey["audit-conflict"]
	if conflict.ActionID != conflictResponse.ActionID ||
		conflict.RequestedAction != "resume" ||
		conflict.ExpectedGeneration == nil ||
		*conflict.ExpectedGeneration != 0 ||
		conflict.Reason != "record stale operator request" ||
		conflict.PreviousState != "paused" ||
		conflict.ResultingState != "paused" ||
		conflict.Generation != 1 ||
		conflict.Result != "conflict" ||
		conflict.ErrorClass == nil ||
		*conflict.ErrorClass != "stale_generation" ||
		conflict.ResponseStatus != http.StatusConflict {
		t.Fatalf("conflict audit = %+v", conflict)
	}
}

func TestWorkspaceClaimIntakeMutation_ReusedKeyAcrossDifferentActionReplaysOriginalExactly(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	const key = "cross-action-exact-replay"

	pause := httptest.NewRecorder()
	testHandler.PauseWorkspaceClaimIntake(
		pause,
		claimIntakeMutationRequest(
			t,
			http.MethodPost,
			testWorkspaceID,
			key,
			map[string]any{"reason": "original pause"},
		),
	)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pause.Code, pause.Body.String())
	}

	resumeReplay := httptest.NewRecorder()
	testHandler.ResumeWorkspaceClaimIntake(
		resumeReplay,
		claimIntakeMutationRequest(
			t,
			http.MethodPost,
			testWorkspaceID,
			key,
			map[string]any{
				"reason":              "different action must replay pause",
				"expected_generation": 1,
			},
		),
	)
	if resumeReplay.Code != pause.Code || !bytes.Equal(resumeReplay.Body.Bytes(), pause.Body.Bytes()) {
		t.Fatalf(
			"cross-action replay differs\npause:  %d %s\nresume: %d %s",
			pause.Code,
			pause.Body.String(),
			resumeReplay.Code,
			resumeReplay.Body.String(),
		)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)

	var count int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM workspace_claim_intake_action WHERE workspace_id = $1`,
		testWorkspaceID,
	).Scan(&count); err != nil {
		t.Fatalf("count cross-action audit rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("cross-action audit count = %d, want 1", count)
	}
}

func TestWorkspaceClaimIntakeMutation_ConcurrentSameKeyReplaysOriginalExactly(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	const key = "concurrent-same-key"

	start := make(chan struct{})
	recorders := []*httptest.ResponseRecorder{
		httptest.NewRecorder(),
		httptest.NewRecorder(),
	}
	var wg sync.WaitGroup
	for index := range recorders {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			testHandler.PauseWorkspaceClaimIntake(
				recorders[index],
				claimIntakeMutationRequest(
					t,
					http.MethodPost,
					testWorkspaceID,
					key,
					map[string]any{"reason": "one authoritative concurrent pause"},
				),
			)
		}(index)
	}
	close(start)
	wg.Wait()

	for index, recorder := range recorders {
		if recorder.Code != http.StatusOK {
			t.Fatalf("response %d status = %d: %s", index, recorder.Code, recorder.Body.String())
		}
	}
	if !bytes.Equal(recorders[0].Body.Bytes(), recorders[1].Body.Bytes()) {
		t.Fatalf(
			"concurrent replay differs\nfirst:  %s\nsecond: %s",
			recorders[0].Body.String(),
			recorders[1].Body.String(),
		)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)

	var count int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM workspace_claim_intake_action WHERE workspace_id = $1`,
		testWorkspaceID,
	).Scan(&count); err != nil {
		t.Fatalf("count concurrent same-key actions: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent same-key action count = %d, want 1", count)
	}
}

func TestWorkspaceClaimIntakeMutation_ConcurrentDifferentKeysSerialize(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	start := make(chan struct{})
	type result struct {
		recorder *httptest.ResponseRecorder
		response claimIntakeMutationTestResponse
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for index := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			recorder := httptest.NewRecorder()
			testHandler.PauseWorkspaceClaimIntake(
				recorder,
				claimIntakeMutationRequest(
					t,
					http.MethodPost,
					testWorkspaceID,
					fmt.Sprintf("concurrent-different-key-%d", index),
					map[string]any{"reason": fmt.Sprintf("serialized pause %d", index)},
				),
			)
			results[index].recorder = recorder
			if recorder.Code == http.StatusOK {
				results[index].response = decodeClaimIntakeMutationTestResponse(t, recorder)
			}
		}(index)
	}
	close(start)
	wg.Wait()

	resultCounts := map[string]int{}
	var authoritativeActionID string
	for index, result := range results {
		if result.recorder.Code != http.StatusOK {
			t.Fatalf("response %d status = %d: %s", index, result.recorder.Code, result.recorder.Body.String())
		}
		resultCounts[result.response.Result]++
		if result.response.Generation != 1 || result.response.State != "paused" {
			t.Fatalf("response %d = %+v", index, result.response)
		}
		if result.response.Result == "applied" {
			authoritativeActionID = result.response.ActionID
		}
	}
	if resultCounts["applied"] != 1 || resultCounts["noop"] != 1 || authoritativeActionID == "" {
		t.Fatalf("serialized results = %+v", resultCounts)
	}
	for index, result := range results {
		if result.response.LastActionID == nil ||
			*result.response.LastActionID != authoritativeActionID {
			t.Fatalf("response %d authoritative action = %v, want %s", index, result.response.LastActionID, authoritativeActionID)
		}
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)
}

func TestWorkspaceClaimIntakeResume_StaleGenerationConflictIsAudited(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pause := httptest.NewRecorder()
	pauseReq := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "pause-before-stale-resume", map[string]any{
		"reason": "establish fence",
	})
	testHandler.PauseWorkspaceClaimIntake(pause, pauseReq)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pause.Code, pause.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pause)

	resume := httptest.NewRecorder()
	resumeReq := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "stale-resume-conflict", map[string]any{
		"reason":              "stale operator view",
		"expected_generation": 0,
	})
	testHandler.ResumeWorkspaceClaimIntake(resume, resumeReq)
	if resume.Code != http.StatusConflict {
		t.Fatalf("resume status = %d, want 409: %s", resume.Code, resume.Body.String())
	}
	response := decodeClaimIntakeMutationTestResponse(t, resume)
	if response.Result != "conflict" || response.ErrorClass == nil || *response.ErrorClass != "stale_generation" {
		t.Fatalf("conflict response = %+v", response)
	}
	if response.Generation != 1 || response.State != "paused" {
		t.Fatalf("conflict changed authoritative state: %+v", response)
	}
	if response.LastActionID == nil || *response.LastActionID != pauseResponse.ActionID {
		t.Fatalf("authoritative action = %v, want pause action %s", response.LastActionID, pauseResponse.ActionID)
	}
	assertClaimIntakeStateForTest(t, testWorkspaceID, "paused", 1)

	var result, errorClass string
	var responseStatus int
	if err := testPool.QueryRow(context.Background(), `
SELECT result, error_class, response_status
FROM workspace_claim_intake_action
WHERE workspace_id = $1 AND idempotency_key = 'stale-resume-conflict'
`, testWorkspaceID).Scan(&result, &errorClass, &responseStatus); err != nil {
		t.Fatalf("load conflict audit: %v", err)
	}
	if result != "conflict" || errorClass != "stale_generation" || responseStatus != http.StatusConflict {
		t.Fatalf("conflict audit = result:%s error:%s status:%d", result, errorClass, responseStatus)
	}
}

func TestWorkspaceClaimIntakeReads_OwnerAndAdminAllowed(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	email := fmt.Sprintf("claim-intake-read-admin-%d@multica.test", time.Now().UnixNano())
	var adminID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Claim Intake Read Admin', $1)
		RETURNING id
	`, email).Scan(&adminID); err != nil {
		t.Fatalf("create read admin user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'admin')
	`, testWorkspaceID, adminID); err != nil {
		t.Fatalf("create read admin member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, adminID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, adminID)
	})

	for _, actor := range []struct {
		name   string
		userID string
	}{
		{name: "owner", userID: testUserID},
		{name: "admin", userID: adminID},
	} {
		t.Run(actor.name, func(t *testing.T) {
			for _, endpoint := range claimIntakeReadEndpointsForTest() {
				w := httptest.NewRecorder()
				req := newRequest(http.MethodGet, endpoint.path, nil)
				req = withURLParam(req, "id", testWorkspaceID)
				req.Header.Set("X-User-ID", actor.userID)
				endpoint.call(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want 200: %s", endpoint.name, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestWorkspaceClaimIntakeReads_MemberAndMachineDenied(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	email := fmt.Sprintf("claim-intake-read-member-%d@multica.test", time.Now().UnixNano())
	var memberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Claim Intake Read Member', $1)
		RETURNING id
	`, email).Scan(&memberID); err != nil {
		t.Fatalf("create read member user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("create read member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	for _, caller := range []struct {
		name        string
		userID      string
		actorSource string
		daemon      bool
	}{
		{name: "member", userID: memberID},
		{name: "task_token", userID: testUserID, actorSource: "task_token"},
		{name: "cloud_pat", userID: testUserID, actorSource: "cloud_pat"},
		{name: "daemon_token", daemon: true},
	} {
		t.Run(caller.name, func(t *testing.T) {
			for _, endpoint := range claimIntakeReadEndpointsForTest() {
				w := httptest.NewRecorder()
				req := newRequest(http.MethodGet, endpoint.path, nil)
				req = withURLParam(req, "id", testWorkspaceID)
				if caller.userID != "" {
					req.Header.Set("X-User-ID", caller.userID)
				} else {
					req.Header.Del("X-User-ID")
				}
				if caller.actorSource != "" {
					req.Header.Set("X-Actor-Source", caller.actorSource)
				}
				if caller.daemon {
					req = req.WithContext(middleware.WithDaemonContext(
						req.Context(),
						testWorkspaceID,
						"read-machine-daemon",
					))
				}
				endpoint.call(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("%s status = %d, want 403: %s", endpoint.name, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestWorkspaceClaimIntakeReads_CrossWorkspaceDenied(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()
	slug := fmt.Sprintf("claim-intake-read-cross-%d", time.Now().UnixNano())
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Claim Intake Read Cross', $1)
		RETURNING id
	`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create read cross Workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	for _, endpoint := range claimIntakeReadEndpointsForTest() {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodGet, endpoint.pathForWorkspace(workspaceID), nil)
		req = withURLParam(req, "id", workspaceID)
		endpoint.call(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404: %s", endpoint.name, w.Code, w.Body.String())
		}
	}
}

type claimIntakeReadEndpointForTest struct {
	name             string
	path             string
	pathForWorkspace func(string) string
	call             func(http.ResponseWriter, *http.Request)
}

func claimIntakeReadEndpointsForTest() []claimIntakeReadEndpointForTest {
	return []claimIntakeReadEndpointForTest{
		{
			name: "status",
			path: "/api/workspaces/" + testWorkspaceID + "/claim-intake",
			pathForWorkspace: func(workspaceID string) string {
				return "/api/workspaces/" + workspaceID + "/claim-intake"
			},
			call: testHandler.GetWorkspaceClaimIntakeStatus,
		},
		{
			name: "actions",
			path: "/api/workspaces/" + testWorkspaceID + "/claim-intake/actions",
			pathForWorkspace: func(workspaceID string) string {
				return "/api/workspaces/" + workspaceID + "/claim-intake/actions"
			},
			call: testHandler.ListWorkspaceClaimIntakeActions,
		},
		{
			name: "ledger",
			path: "/api/workspaces/" + testWorkspaceID + "/claim-intake/ledger",
			pathForWorkspace: func(workspaceID string) string {
				return "/api/workspaces/" + workspaceID + "/claim-intake/ledger"
			},
			call: testHandler.ListWorkspaceClaimIntakeLedger,
		},
	}
}

func TestWorkspaceClaimIntakeLedger_ListsGlobalTaskStatesAndFenceClassification(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	pause := httptest.NewRecorder()
	pauseReq := claimIntakeMutationRequest(t, http.MethodPost, testWorkspaceID, "ledger-pause", map[string]any{
		"reason": "classify pre-fence ownership",
	})
	testHandler.PauseWorkspaceClaimIntake(pause, pauseReq)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pause.Code, pause.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pause)

	type taskSeed struct {
		status          string
		consumer        *string
		dispatchedAt    *time.Time
		prepareLeaseAt  *time.Time
		claimGeneration *int64
		claimActionID   *string
	}
	consumerA := "synthetic-consumer-a"
	consumerB := "synthetic-consumer-b"
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	preFenceGeneration := int64(0)
	currentGeneration := int64(1)
	seeds := []taskSeed{
		{status: "queued"},
		{status: "deferred"},
		{status: "dispatched", consumer: &consumerA, dispatchedAt: &past, prepareLeaseAt: &past, claimGeneration: &preFenceGeneration},
		{status: "running", consumer: &consumerB, dispatchedAt: &past, claimGeneration: &currentGeneration, claimActionID: &pauseResponse.ActionID},
		{status: "waiting_local_directory", consumer: &consumerA, dispatchedAt: &past, prepareLeaseAt: &future, claimGeneration: &currentGeneration, claimActionID: &pauseResponse.ActionID},
	}

	taskIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority, dispatched_at,
				prepare_lease_expires_at, claim_intake_generation,
				claim_intake_action_id, claim_consumer_id
			)
			VALUES ($1, $2, $3, 0, $4, $5, $6, $7, $8)
			RETURNING id
		`, agentID, testRuntimeID, seed.status, seed.dispatchedAt, seed.prepareLeaseAt, seed.claimGeneration, seed.claimActionID, seed.consumer).Scan(&taskID); err != nil {
			t.Fatalf("seed %s ledger task: %v", seed.status, err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, taskIDs)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/claim-intake/ledger?limit=50&offset=0", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeLedger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var response claimIntakeLedgerTestResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode ledger: %v", err)
	}
	if response.WorkspaceID != testWorkspaceID || response.State != "paused" || response.Generation != 1 {
		t.Fatalf("control = workspace:%s state:%s generation:%d", response.WorkspaceID, response.State, response.Generation)
	}
	if response.LastActionID == nil || *response.LastActionID != pauseResponse.ActionID || response.EffectiveAt.IsZero() {
		t.Fatalf("authoritative control metadata = action:%v effective_at:%s", response.LastActionID, response.EffectiveAt)
	}
	if response.Limit != 50 || response.Offset != 0 {
		t.Fatalf("pagination = %d/%d, want 50/0", response.Limit, response.Offset)
	}

	byStatus := make(map[string]claimIntakeLedgerTestTask, len(response.Tasks))
	for _, task := range response.Tasks {
		for _, taskID := range taskIDs {
			if task.TaskID == taskID {
				byStatus[task.Status] = task
				break
			}
		}
	}
	for _, status := range []string{"queued", "deferred", "dispatched", "running", "waiting_local_directory"} {
		if _, ok := byStatus[status]; !ok {
			t.Fatalf("ledger missing %s task: %+v", status, response.Tasks)
		}
	}
	if got := byStatus["queued"]; got.FenceClassification != "unclassified" || got.StaleReclaimable {
		t.Fatalf("queued classification = %+v", got)
	}
	if got := byStatus["deferred"]; got.FenceClassification != "unclassified" || got.StaleReclaimable {
		t.Fatalf("deferred classification = %+v", got)
	}
	if got := byStatus["dispatched"]; got.ConsumerID == nil || *got.ConsumerID != consumerA || got.DispatchedAt == nil || got.PrepareLeaseExpiresAt == nil || !got.StaleReclaimable || got.FenceClassification != "pre_fence" {
		t.Fatalf("stale dispatched ledger entry = %+v", got)
	}
	if got := byStatus["running"]; got.ConsumerID == nil || *got.ConsumerID != consumerB || got.ClaimGeneration == nil || *got.ClaimGeneration != 1 || got.ClaimActionID == nil || *got.ClaimActionID != pauseResponse.ActionID || got.FenceClassification != "current_generation" || got.StaleReclaimable {
		t.Fatalf("running ledger entry = %+v", got)
	}
	if got := byStatus["waiting_local_directory"]; got.PrepareLeaseExpiresAt == nil || got.FenceClassification != "current_generation" || got.StaleReclaimable {
		t.Fatalf("waiting-local-directory ledger entry = %+v", got)
	}
}

func TestWorkspaceClaimIntakeLedger_UsesCanonicalLastActionID(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)

	pause := httptest.NewRecorder()
	pauseRequest := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"canonical-ledger-last-action-id",
		map[string]any{
			"reason": "record canonical ledger action",
		},
	)
	testHandler.PauseWorkspaceClaimIntake(pause, pauseRequest)
	if pause.Code != http.StatusOK {
		t.Fatalf(
			"pause status = %d: %s",
			pause.Code,
			pause.Body.String(),
		)
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pause)

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+
			testWorkspaceID+
			"/claim-intake/ledger?limit=50&offset=0",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeLedger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf(
			"ledger status = %d: %s",
			w.Code,
			w.Body.String(),
		)
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode claim-intake ledger: %v", err)
	}
	if response["last_action_id"] != pauseResponse.ActionID {
		t.Fatalf(
			"last_action_id = %v, want %s: %+v",
			response["last_action_id"],
			pauseResponse.ActionID,
			response,
		)
	}
	if _, legacy := response["authoritative_action_id"]; legacy {
		t.Fatalf(
			"legacy authoritative_action_id must not be emitted: %+v",
			response,
		)
	}
}

func TestWorkspaceClaimIntakeSyntheticProof_TwoConsumersPauseResumeAndLedger(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 20, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Claim Intake Synthetic Agent %d", time.Now().UnixNano()), testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create synthetic proof agent: %v", err)
	}

	taskIDs := make([]string, 0, 6)
	issueIDs := make([]string, 0, 6)
	insertTask := func(status string, fireAt, dispatchedAt, prepareLease *time.Time, generation *int64, consumer *string) string {
		t.Helper()
		var issueNumber int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = issue_counter + 1
			WHERE id = $1
			RETURNING issue_counter
		`, testWorkspaceID).Scan(&issueNumber); err != nil {
			t.Fatalf("allocate synthetic issue number: %v", err)
		}
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_id,
				creator_type, number, position
			)
			VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, 0)
			RETURNING id
		`, testWorkspaceID, "claim-intake synthetic task", testUserID, issueNumber).Scan(&issueID); err != nil {
			t.Fatalf("create synthetic task issue: %v", err)
		}
		issueIDs = append(issueIDs, issueID)

		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, priority, context, fire_at,
				dispatched_at, prepare_lease_expires_at,
				claim_intake_generation, claim_consumer_id
			)
			VALUES ($1, $2, $3, $4, 0, '{}'::jsonb, $5, $6, $7, $8, $9)
			RETURNING id
		`, agentID, issueID, testRuntimeID, status, fireAt, dispatchedAt, prepareLease, generation, consumer).Scan(&taskID); err != nil {
			t.Fatalf("seed synthetic %s task: %v", status, err)
		}
		taskIDs = append(taskIDs, taskID)
		return taskID
	}

	now := time.Now().UTC()
	staleAt := now.Add(-time.Hour)
	staleLease := now.Add(-30 * time.Minute)
	futureLease := now.Add(time.Hour)
	dueAt := now.Add(-time.Minute)
	generationZero := int64(0)
	consumerA := "synthetic-consumer-a"
	consumerB := "synthetic-consumer-b"

	preFenceTaskID := insertTask("queued", nil, nil, nil, nil, nil)
	preFence, err := testHandler.TaskService.ClaimTaskForRuntimeAsConsumer(
		ctx,
		parseUUID(testRuntimeID),
		consumerA,
	)
	if err != nil {
		t.Fatalf("establish pre-fence ownership: %v", err)
	}
	if preFence.Task == nil || uuidToString(preFence.Task.ID) != preFenceTaskID {
		t.Fatalf("pre-fence claim = %+v, want task %s", preFence, preFenceTaskID)
	}

	queuedTaskID := insertTask("queued", nil, nil, nil, nil, nil)
	deferredTaskID := insertTask("deferred", &dueAt, nil, nil, nil, nil)
	staleTaskID := insertTask("dispatched", nil, &staleAt, &staleLease, &generationZero, &consumerA)
	runningTaskID := insertTask("running", nil, &staleAt, nil, &generationZero, &consumerA)
	waitingTaskID := insertTask("waiting_local_directory", nil, &staleAt, &futureLease, &generationZero, &consumerB)

	pause := httptest.NewRecorder()
	pauseReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"synthetic-proof-pause",
		map[string]any{"reason": "synthetic two-consumer maintenance"},
	)
	testHandler.PauseWorkspaceClaimIntake(pause, pauseReq)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d: %s", pause.Code, pause.Body.String())
	}
	pauseResponse := decodeClaimIntakeMutationTestResponse(t, pause)
	if pauseResponse.Generation != 1 || pauseResponse.Result != "applied" {
		t.Fatalf("pause response = %+v", pauseResponse)
	}

	for _, consumer := range []string{consumerA, consumerB} {
		result, err := testHandler.TaskService.ClaimTasksForRuntimesAsConsumer(
			ctx,
			[]pgtype.UUID{parseUUID(testRuntimeID)},
			10,
			consumer,
		)
		if err != nil {
			t.Fatalf("paused claim for %s: %v", consumer, err)
		}
		if len(result.Tasks) != 0 ||
			len(result.PausedWorkspaces) != 1 ||
			result.PausedWorkspaces[0].Generation != pauseResponse.Generation ||
			uuidToString(result.PausedWorkspaces[0].ActionID) != pauseResponse.ActionID {
			t.Fatalf("paused result for %s = %+v", consumer, result)
		}
	}

	assertTaskState := func(taskID, wantStatus string, wantGeneration *int64, wantConsumer *string) {
		t.Helper()
		var status string
		var generation *int64
		var consumer *string
		if err := testPool.QueryRow(ctx, `
			SELECT status, claim_intake_generation, claim_consumer_id
			FROM agent_task_queue
			WHERE id = $1
		`, taskID).Scan(&status, &generation, &consumer); err != nil {
			t.Fatalf("load synthetic task %s: %v", taskID, err)
		}
		if status != wantStatus || !equalOptionalInt64(generation, wantGeneration) || !equalOptionalClaimIntakeString(consumer, wantConsumer) {
			t.Fatalf(
				"task %s = status %q generation %v consumer %v, want %q/%v/%v",
				taskID,
				status,
				generation,
				consumer,
				wantStatus,
				wantGeneration,
				wantConsumer,
			)
		}
	}
	assertTaskState(preFenceTaskID, "dispatched", &generationZero, &consumerA)
	assertTaskState(queuedTaskID, "queued", nil, nil)
	assertTaskState(deferredTaskID, "deferred", nil, nil)
	assertTaskState(staleTaskID, "dispatched", &generationZero, &consumerA)
	assertTaskState(runningTaskID, "running", &generationZero, &consumerA)
	assertTaskState(waitingTaskID, "waiting_local_directory", &generationZero, &consumerB)

	pausedLedger := listClaimIntakeLedgerForTest(t, testWorkspaceID, 200, 0)
	if pausedLedger.State != "paused" ||
		pausedLedger.Generation != pauseResponse.Generation ||
		pausedLedger.LastActionID == nil ||
		*pausedLedger.LastActionID != pauseResponse.ActionID {
		t.Fatalf("paused ledger control = %+v", pausedLedger)
	}
	pausedByID := claimIntakeLedgerTasksByID(pausedLedger.Tasks)
	for _, taskID := range taskIDs {
		if _, ok := pausedByID[taskID]; !ok {
			t.Fatalf("paused ledger missing task %s: %+v", taskID, pausedLedger.Tasks)
		}
	}
	if !pausedByID[staleTaskID].StaleReclaimable ||
		pausedByID[staleTaskID].FenceClassification != "pre_fence" ||
		pausedByID[queuedTaskID].FenceClassification != "unclassified" ||
		pausedByID[deferredTaskID].FenceClassification != "unclassified" ||
		pausedByID[runningTaskID].FenceClassification != "pre_fence" ||
		pausedByID[waitingTaskID].FenceClassification != "pre_fence" {
		t.Fatalf("paused ledger classifications = %+v", pausedByID)
	}

	resume := httptest.NewRecorder()
	resumeReq := claimIntakeMutationRequest(
		t,
		http.MethodPost,
		testWorkspaceID,
		"synthetic-proof-resume",
		map[string]any{
			"reason":              "synthetic maintenance complete",
			"expected_generation": pauseResponse.Generation,
		},
	)
	testHandler.ResumeWorkspaceClaimIntake(resume, resumeReq)
	if resume.Code != http.StatusOK {
		t.Fatalf("resume status = %d: %s", resume.Code, resume.Body.String())
	}
	resumeResponse := decodeClaimIntakeMutationTestResponse(t, resume)
	if resumeResponse.Generation != 2 ||
		resumeResponse.Result != "applied" ||
		resumeResponse.LastActionID == nil ||
		*resumeResponse.LastActionID != resumeResponse.ActionID {
		t.Fatalf("resume response = %+v", resumeResponse)
	}

	claimedIDs := make(map[string]bool, 3)
	for attempt := 0; attempt < 3 && len(claimedIDs) < 3; attempt++ {
		resumed, err := testHandler.TaskService.ClaimTasksForRuntimesAsConsumer(
			ctx,
			[]pgtype.UUID{parseUUID(testRuntimeID)},
			10,
			consumerB,
		)
		if err != nil {
			t.Fatalf("resumed claim %d: %v", attempt+1, err)
		}
		for _, task := range resumed.Tasks {
			claimedIDs[uuidToString(task.ID)] = true
		}
	}
	for _, taskID := range []string{queuedTaskID, deferredTaskID, staleTaskID} {
		if !claimedIDs[taskID] {
			t.Fatalf("resumed claims missing task %s: %+v", taskID, claimedIDs)
		}
	}
	if claimedIDs[preFenceTaskID] || claimedIDs[runningTaskID] || claimedIDs[waitingTaskID] {
		t.Fatalf("resumed claims replaced existing ownership: %+v", claimedIDs)
	}

	resumeGeneration := int64(2)
	for _, taskID := range []string{queuedTaskID, deferredTaskID, staleTaskID} {
		assertTaskState(taskID, "dispatched", &resumeGeneration, &consumerB)
		var actionID string
		if err := testPool.QueryRow(ctx, `
			SELECT claim_intake_action_id::text
			FROM agent_task_queue
			WHERE id = $1
		`, taskID).Scan(&actionID); err != nil {
			t.Fatalf("load resumed action for task %s: %v", taskID, err)
		}
		if actionID != resumeResponse.ActionID {
			t.Fatalf("task %s action = %s, want %s", taskID, actionID, resumeResponse.ActionID)
		}
	}
	assertTaskState(preFenceTaskID, "dispatched", &generationZero, &consumerA)
	assertTaskState(runningTaskID, "running", &generationZero, &consumerA)
	assertTaskState(waitingTaskID, "waiting_local_directory", &generationZero, &consumerB)

	resumedLedger := listClaimIntakeLedgerForTest(t, testWorkspaceID, 200, 0)
	if resumedLedger.State != "resumed" ||
		resumedLedger.Generation != resumeResponse.Generation ||
		resumedLedger.LastActionID == nil ||
		*resumedLedger.LastActionID != resumeResponse.ActionID {
		t.Fatalf("resumed ledger control = %+v", resumedLedger)
	}
	resumedByID := claimIntakeLedgerTasksByID(resumedLedger.Tasks)
	for _, taskID := range []string{queuedTaskID, deferredTaskID, staleTaskID} {
		entry, ok := resumedByID[taskID]
		if !ok ||
			entry.ConsumerID == nil || *entry.ConsumerID != consumerB ||
			entry.ClaimGeneration == nil || *entry.ClaimGeneration != resumeGeneration ||
			entry.ClaimActionID == nil || *entry.ClaimActionID != resumeResponse.ActionID ||
			entry.FenceClassification != "current_generation" {
			t.Fatalf("resumed ledger task %s = %+v", taskID, entry)
		}
	}
	for _, taskID := range []string{preFenceTaskID, runningTaskID, waitingTaskID} {
		entry, ok := resumedByID[taskID]
		if !ok || entry.FenceClassification != "pre_fence" {
			t.Fatalf("pre-fence ledger task %s = %+v", taskID, entry)
		}
	}

	actionsRecorder := httptest.NewRecorder()
	actionsReq := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake/actions?limit=10&offset=0",
		nil,
	)
	actionsReq = withURLParam(actionsReq, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeActions(actionsRecorder, actionsReq)
	if actionsRecorder.Code != http.StatusOK {
		t.Fatalf("actions status = %d: %s", actionsRecorder.Code, actionsRecorder.Body.String())
	}
	var actions claimIntakeActionsTestResponse
	if err := json.NewDecoder(actionsRecorder.Body).Decode(&actions); err != nil {
		t.Fatalf("decode synthetic action audit: %v", err)
	}
	if len(actions.Actions) != 2 ||
		actions.Actions[0].RequestedAction != "resume" ||
		actions.Actions[0].ActionID != resumeResponse.ActionID ||
		actions.Actions[0].Generation != resumeResponse.Generation ||
		actions.Actions[0].Result != "applied" ||
		actions.Actions[1].RequestedAction != "pause" ||
		actions.Actions[1].ActionID != pauseResponse.ActionID ||
		actions.Actions[1].Generation != pauseResponse.Generation ||
		actions.Actions[1].Result != "applied" {
		t.Fatalf("synthetic action audit = %+v", actions.Actions)
	}

	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, taskIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
	})
}

func equalOptionalInt64(got, want *int64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func equalOptionalClaimIntakeString(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func listClaimIntakeLedgerForTest(t *testing.T, workspaceID string, limit, offset int) claimIntakeLedgerTestResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		fmt.Sprintf(
			"/api/workspaces/%s/claim-intake/ledger?limit=%d&offset=%d",
			workspaceID,
			limit,
			offset,
		),
		nil,
	)
	req = withURLParam(req, "id", workspaceID)
	testHandler.ListWorkspaceClaimIntakeLedger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ledger status = %d: %s", w.Code, w.Body.String())
	}
	var response claimIntakeLedgerTestResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode claim-intake ledger: %v", err)
	}
	return response
}

func claimIntakeLedgerTasksByID(tasks []claimIntakeLedgerTestTask) map[string]claimIntakeLedgerTestTask {
	byID := make(map[string]claimIntakeLedgerTestTask, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
	}
	return byID
}

func TestWorkspaceClaimIntakeLedger_CountsAreGlobalAcrossPagination(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(
		ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load ledger count agent: %v", err)
	}

	statuses := []string{"queued", "deferred", "dispatched", "running", "waiting_local_directory"}
	taskIDs := make([]string, 0, len(statuses))
	for index, status := range statuses {
		var taskID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, agentID, testRuntimeID, status, 10_000+index).Scan(&taskID)
		if err != nil {
			t.Fatalf("seed %s ledger count task: %v", status, err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(
			context.Background(),
			`DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
			taskIDs,
		)
	})

	readPage := func(offset int) claimIntakeLedgerTestResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest(
			http.MethodGet,
			fmt.Sprintf(
				"/api/workspaces/%s/claim-intake/ledger?limit=1&offset=%d",
				testWorkspaceID,
				offset,
			),
			nil,
		)
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.ListWorkspaceClaimIntakeLedger(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ledger page %d status = %d: %s", offset, w.Code, w.Body.String())
		}
		var response claimIntakeLedgerTestResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode ledger page %d: %v", offset, err)
		}
		return response
	}

	first := readPage(0)
	second := readPage(1)
	for _, status := range statuses {
		if first.Counts[status] < 1 {
			t.Errorf("first page global %s count = %d, want at least 1", status, first.Counts[status])
		}
		if second.Counts[status] != first.Counts[status] {
			t.Errorf(
				"%s count changed with pagination: first=%d second=%d",
				status,
				first.Counts[status],
				second.Counts[status],
			)
		}
	}
}

func TestWorkspaceClaimIntakeLedger_PostFenceAnomalyAndPagination(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(
		ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load ledger pagination agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE workspace_claim_intake_control
		SET state = 'paused', generation = 4, effective_at = now(), updated_at = now()
		WHERE workspace_id = $1
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed ledger generation: %v", err)
	}

	taskIDs := make([]string, 0, 3)
	for generation := int64(4); generation <= 6; generation++ {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority,
				claim_intake_generation, claim_consumer_id
			)
			VALUES ($1, $2, 'running', 0, $3, $4)
			RETURNING id
		`, agentID, testRuntimeID, generation, fmt.Sprintf("ledger-page-consumer-%d", generation)).Scan(&taskID); err != nil {
			t.Fatalf("seed ledger task generation %d: %v", generation, err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(
			context.Background(),
			`DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
			taskIDs,
		)
	})

	listPage := func(offset int) claimIntakeLedgerTestResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest(
			http.MethodGet,
			fmt.Sprintf(
				"/api/workspaces/%s/claim-intake/ledger?limit=1&offset=%d",
				testWorkspaceID,
				offset,
			),
			nil,
		)
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.ListWorkspaceClaimIntakeLedger(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ledger page %d status = %d: %s", offset, w.Code, w.Body.String())
		}
		var response claimIntakeLedgerTestResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode ledger page %d: %v", offset, err)
		}
		if response.Limit != 1 || response.Offset != int32(offset) || len(response.Tasks) != 1 {
			t.Fatalf("ledger page %d = %+v", offset, response)
		}
		return response
	}

	// The Workspace may contain unrelated older fixture tasks, so locate the
	// consecutive boundary where these three synthetic rows appear.
	var pages []claimIntakeLedgerTestResponse
	for offset := 0; offset < 100; offset++ {
		page := listPage(offset)
		for _, taskID := range taskIDs {
			if page.Tasks[0].TaskID == taskID {
				pages = append(pages, page)
				break
			}
		}
		if len(pages) == 3 {
			break
		}
	}
	if len(pages) != 3 {
		t.Fatalf("did not find all synthetic ledger pages: %+v", pages)
	}
	seen := map[string]bool{}
	for _, page := range pages {
		entry := page.Tasks[0]
		if seen[entry.TaskID] {
			t.Fatalf("pagination repeated task %s", entry.TaskID)
		}
		seen[entry.TaskID] = true
	}

	classifications := map[int64]string{}
	for _, page := range pages {
		entry := page.Tasks[0]
		if entry.ClaimGeneration == nil {
			t.Fatalf("synthetic ledger entry lacks generation: %+v", entry)
		}
		classifications[*entry.ClaimGeneration] = entry.FenceClassification
	}
	if classifications[4] != "current_generation" ||
		classifications[5] != "post_fence_anomaly" ||
		classifications[6] != "post_fence_anomaly" {
		t.Fatalf("ledger classifications = %+v", classifications)
	}
}

func TestWorkspaceClaimIntakeLedger_ExcludesMalformedCrossWorkspaceTask(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	slug := fmt.Sprintf("claim-intake-ledger-foreign-%d", time.Now().UnixNano())
	var foreignWorkspaceID, foreignUserID, foreignRuntimeID, foreignAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Ledger Foreign User', $1)
		RETURNING id
	`, slug+"@multica.test").Scan(&foreignUserID); err != nil {
		t.Fatalf("create foreign ledger user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Ledger Foreign Workspace', $1)
		RETURNING id
	`, slug).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign ledger Workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, foreignWorkspaceID, foreignUserID); err != nil {
		t.Fatalf("create foreign ledger owner: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, 'Foreign Ledger Runtime', 'cloud', 'test', 'online', '', '{}', now(), 'private', $2)
		RETURNING id
	`, foreignWorkspaceID, foreignUserID).Scan(&foreignRuntimeID); err != nil {
		t.Fatalf("create foreign ledger runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Foreign Ledger Agent', '', 'cloud', '{}', $2, 'private', 1, $3)
		RETURNING id
	`, foreignWorkspaceID, foreignRuntimeID, foreignUserID).Scan(&foreignAgentID); err != nil {
		t.Fatalf("create foreign ledger agent: %v", err)
	}
	var malformedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, claim_intake_generation
		)
		VALUES ($1, $2, 'queued', 0, NULL)
		RETURNING id
	`, foreignAgentID, testRuntimeID).Scan(&malformedTaskID); err != nil {
		t.Fatalf("create malformed cross-Workspace task: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE id = $1`, malformedTaskID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, foreignAgentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, foreignRuntimeID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})

	w := httptest.NewRecorder()
	req := newRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/claim-intake/ledger?limit=200",
		nil,
	)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeLedger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ledger status = %d: %s", w.Code, w.Body.String())
	}
	var response claimIntakeLedgerTestResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode isolated ledger: %v", err)
	}
	for _, task := range response.Tasks {
		if task.TaskID == malformedTaskID {
			t.Fatalf("ledger leaked malformed cross-Workspace task: %+v", task)
		}
	}
}

func TestWorkspaceClaimIntakeLedger_IncludesOwningWorkspaceTasksWithMalformedReferences(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	ctx := context.Background()

	var localAgentID string
	if err := testPool.QueryRow(
		ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&localAgentID); err != nil {
		t.Fatalf("load local ledger agent: %v", err)
	}

	baseline := listClaimIntakeLedgerForTest(t, testWorkspaceID, 200, 0)
	slug := fmt.Sprintf("claim-intake-ledger-reference-%d", time.Now().UnixNano())
	var foreignWorkspaceID, foreignUserID, foreignRuntimeID, foreignIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Ledger Reference User', $1)
		RETURNING id
	`, slug+"@multica.test").Scan(&foreignUserID); err != nil {
		t.Fatalf("create foreign reference user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Ledger Reference Workspace', $1)
		RETURNING id
	`, slug).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign reference Workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, foreignWorkspaceID, foreignUserID); err != nil {
		t.Fatalf("create foreign reference owner: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, 'Foreign Reference Runtime', 'cloud', 'test', 'online', '', '{}', now(), 'private', $2)
		RETURNING id
	`, foreignWorkspaceID, foreignUserID).Scan(&foreignRuntimeID); err != nil {
		t.Fatalf("create foreign reference runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Foreign Reference Issue', 'member', $2)
		RETURNING id
	`, foreignWorkspaceID, foreignUserID).Scan(&foreignIssueID); err != nil {
		t.Fatalf("create foreign reference issue: %v", err)
	}

	var foreignRuntimeTaskID, foreignIssueTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority
		)
		VALUES ($1, $2, 'queued', 0)
		RETURNING id
	`, localAgentID, foreignRuntimeID).Scan(&foreignRuntimeTaskID); err != nil {
		t.Fatalf("create local task with foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority
		)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, localAgentID, testRuntimeID, foreignIssueID).Scan(&foreignIssueTaskID); err != nil {
		t.Fatalf("create local task with foreign issue: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(
			cleanup,
			`DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
			[]string{foreignRuntimeTaskID, foreignIssueTaskID},
		)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, foreignIssueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, foreignRuntimeID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})

	response := listClaimIntakeLedgerForTest(t, testWorkspaceID, 200, 0)
	tasks := claimIntakeLedgerTasksByID(response.Tasks)
	for _, taskID := range []string{foreignRuntimeTaskID, foreignIssueTaskID} {
		if _, ok := tasks[taskID]; !ok {
			t.Errorf("ledger omitted owning-Workspace task %s with malformed reference", taskID)
		}
	}
	if response.Counts["queued"] != baseline.Counts["queued"]+1 {
		t.Errorf(
			"queued count = %d, want %d after malformed runtime reference",
			response.Counts["queued"],
			baseline.Counts["queued"]+1,
		)
	}
	if response.Counts["running"] != baseline.Counts["running"]+1 {
		t.Errorf(
			"running count = %d, want %d after malformed issue reference",
			response.Counts["running"],
			baseline.Counts["running"]+1,
		)
	}
}

func TestWorkspaceClaimIntakeLedger_MissingControlFailsClosed(t *testing.T) {
	resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("delete authoritative control: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO workspace_claim_intake_control (
				workspace_id, state, generation, actor_display, reason,
				effective_at, created_at, updated_at
			)
			VALUES ($1, 'resumed', 0, 'system', 'test restore', now(), now(), now())
			ON CONFLICT (workspace_id) DO NOTHING
		`, testWorkspaceID); err != nil {
			t.Errorf("restore authoritative control: %v", err)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/claim-intake/ledger", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceClaimIntakeLedger(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", w.Code, w.Body.String())
	}
}

func assertClaimIntakeStateForTest(t *testing.T, workspaceID, wantState string, wantGeneration int64) {
	t.Helper()
	var state string
	var generation int64
	if err := testPool.QueryRow(context.Background(), `
SELECT state, generation
FROM workspace_claim_intake_control
WHERE workspace_id = $1
`, workspaceID).Scan(&state, &generation); err != nil {
		t.Fatalf("load claim-intake state: %v", err)
	}
	if state != wantState || generation != wantGeneration {
		t.Fatalf("claim-intake state = %s generation = %d; want %s/%d", state, generation, wantState, wantGeneration)
	}
}
