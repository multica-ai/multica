package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// withUpdateStore gives one test a fresh in-memory update store.
func withUpdateStore(t *testing.T) UpdateStore {
	t.Helper()
	orig := testHandler.UpdateStore
	store := NewInMemoryUpdateStore()
	testHandler.UpdateStore = store
	t.Cleanup(func() { testHandler.UpdateStore = orig })
	return store
}

// reportUpdate drives ReportUpdateResult as the fixture runtime's daemon.
func reportUpdate(t *testing.T, updateID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParams(
		newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+testRuntimeID+"/update/"+updateID+"/result", body),
		"runtimeId", testRuntimeID,
		"updateId", updateID,
	)
	w := httptest.NewRecorder()
	testHandler.ReportUpdateResult(w, req)
	return w
}

func TestReportUpdateResult_ScheduledRecordsActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	w := reportUpdate(t, req.ID, map[string]any{"status": "scheduled", "active_tasks": 3})
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}

	got := mustGetUpdate(t, store, req.ID)
	if got.Status != UpdateScheduled || got.ActiveTasks == nil || *got.ActiveTasks != 3 {
		t.Fatalf("update = %+v, want scheduled with 3 active tasks", got)
	}
}

func TestReportUpdateResult_OtherFailuresStillFail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	w := reportUpdate(t, req.ID, map[string]any{"status": "failed", "error": "brew upgrade failed: exit status 1"})
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateFailed || got.Error != "brew upgrade failed: exit status 1" {
		t.Fatalf("update = %+v, want failed", got)
	}
}

func TestReportUpdateResult_RejectsStartingACancelledUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Cancel(context.Background(), req.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	for _, status := range []string{"running", "scheduled"} {
		if w := reportUpdate(t, req.ID, map[string]any{"status": status}); w.Code != http.StatusConflict {
			t.Fatalf("%s report = %d, want 409 so the daemon drops it: %s", status, w.Code, w.Body.String())
		}
	}
	// A late terminal report is still accepted and ignored.
	if w := reportUpdate(t, req.ID, map[string]any{"status": "completed", "output": "Updated"}); w.Code != http.StatusOK {
		t.Fatalf("late completed report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestReportUpdateResult_RunningStartsInstallation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	w := reportUpdate(t, req.ID, map[string]any{"status": "running"})
	if w.Code != http.StatusOK {
		t.Fatalf("running report = %d: %s", w.Code, w.Body.String())
	}
	// The daemon installs only on this explicit acceptance.
	var answer map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || answer["accepted"] != "running" {
		t.Fatalf("answer = %s, want accepted=running", w.Body.String())
	}
	got := mustGetUpdate(t, store, req.ID)
	if got.Status != UpdateRunning || got.RunStartedAt == nil {
		t.Fatalf("update = %+v, want running with a start time", got)
	}
}

func TestCancelUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	cancel := func(updateID string) *httptest.ResponseRecorder {
		req := withURLParams(
			newRequest(http.MethodPost, "/api/runtimes/"+testRuntimeID+"/update/"+updateID+"/cancel", nil),
			"runtimeId", testRuntimeID,
			"updateId", updateID,
		)
		w := httptest.NewRecorder()
		testHandler.CancelUpdate(w, req)
		return w
	}

	queued := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Schedule(context.Background(), queued.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	w := cancel(queued.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel queued = %d: %s", w.Code, w.Body.String())
	}
	var body UpdateRequest
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != UpdateCancelled {
		t.Fatalf("status = %s, want cancelled", body.Status)
	}

	installing := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.MarkRunning(context.Background(), installing.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if w := cancel(installing.ID); w.Code != http.StatusConflict {
		t.Fatalf("cancel installing = %d, want 409: %s", w.Code, w.Body.String())
	}

	if w := cancel("missing"); w.Code != http.StatusNotFound {
		t.Fatalf("cancel missing = %d, want 404", w.Code)
	}
}

func TestGetActiveUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	getActive := func() *UpdateRequest {
		t.Helper()
		req := withURLParam(
			newRequest(http.MethodGet, "/api/runtimes/"+testRuntimeID+"/update", nil),
			"runtimeId", testRuntimeID,
		)
		w := httptest.NewRecorder()
		testHandler.GetActiveUpdate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get active = %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Update *UpdateRequest `json:"update"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Update
	}

	if got := getActive(); got != nil {
		t.Fatalf("active = %+v, want none", got)
	}
	req := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Schedule(context.Background(), req.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	got := getActive()
	if got == nil || got.ID != req.ID || got.Status != UpdateScheduled {
		t.Fatalf("active = %+v, want the queued request", got)
	}
}

func TestHeartbeatOffersDeferrableOnlyWhenBothSidesQueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	tests := []struct {
		name           string
		queueWhenBusy  bool
		daemonDefers   bool
		wantDeferrable bool
	}{
		{name: "new client, new daemon", queueWhenBusy: true, daemonDefers: true, wantDeferrable: true},
		{name: "new client, old daemon", queueWhenBusy: true, daemonDefers: false},
		{name: "old client, new daemon", queueWhenBusy: false, daemonDefers: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := withUpdateStore(t)
			req, err := store.Create(context.Background(), testRuntimeID, "runtime:"+testRuntimeID, "v1.2.3", "user-1", tc.queueWhenBusy)
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			ack, _, err := testHandler.processHeartbeat(context.Background(), testRuntimeID,
				protocol.DaemonHeartbeatRequestPayload{RuntimeID: testRuntimeID, SupportsDeferredUpdate: tc.daemonDefers})
			if err != nil {
				t.Fatalf("heartbeat: %v", err)
			}
			if ack.PendingUpdate == nil || ack.PendingUpdate.ID != req.ID {
				t.Fatalf("pending update = %+v, want %s", ack.PendingUpdate, req.ID)
			}
			if ack.PendingUpdate.Deferrable != tc.wantDeferrable {
				t.Fatalf("deferrable = %v, want %v", ack.PendingUpdate.Deferrable, tc.wantDeferrable)
			}

			// A daemon on the fail-fast path installs even when its "running"
			// report is refused, so once it has the request nothing may
			// cancel it.
			_, err = store.Cancel(context.Background(), req.ID)
			switch {
			case !tc.queueWhenBusy:
				if !errors.Is(err, errUpdateNotCancellable) {
					t.Fatalf("cancel of an older client's request = %v, want errUpdateNotCancellable", err)
				}
			case tc.wantDeferrable:
				if err != nil {
					t.Fatalf("cancel after a deferrable offer: %v", err)
				}
			default:
				if !errors.Is(err, errUpdateNotCancellable) {
					t.Fatalf("cancel after a fail-fast delivery = %v, want errUpdateNotCancellable", err)
				}
			}
		})
	}
}

// A daemon released before queued updates fails a busy offer. Without
// matching its text, that ends the request: such a daemon cannot hold one.
// The Runtimes page shows this failure as a neutral notice.
func TestReportUpdateResult_BusyFailureFromAFailFastDaemonEndsTheRequest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.PopPending(context.Background(), testRuntimeID, false); err != nil {
		t.Fatalf("pop: %v", err)
	}

	if w := reportUpdate(t, req.ID, map[string]any{"status": "failed", "error": protocol.RuntimeUpdateBusyError}); w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateFailed || got.Error != protocol.RuntimeUpdateBusyError {
		t.Fatalf("update = %+v, want the busy failure", got)
	}
}

// A workspace owner manages two public runtimes of someone else's machine. The
// update was queued through A; the machine card now uses B (A went offline).
func TestActiveUpdateIsFoundThroughAnyRuntimeOfTheMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	suffix := randomID()
	daemonID := "mul-9633-machine-" + suffix
	var machineOwner string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Machine Owner', $1) RETURNING id`,
		"machine-owner-"+suffix+"@multica.test").Scan(&machineOwner); err != nil {
		t.Fatalf("create machine owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, machineOwner)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, machineOwner)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, machineOwner); err != nil {
		t.Fatalf("add machine owner: %v", err)
	}
	var runtimeA, runtimeB string
	for i, dst := range []*string{&runtimeA, &runtimeB} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
			)
			VALUES ($1, $2, $3, 'local', $4, 'online', 'machine test', '{}'::jsonb, $5, 'public', now())
			RETURNING id
		`, testWorkspaceID, daemonID, "Machine Runtime", []string{"machine_test_a", "machine_test_b"}[i], machineOwner).Scan(dst); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
	}

	machineKey := "daemon:" + machineOwner + ":" + daemonID
	queued, err := store.Create(ctx, runtimeA, machineKey, "v1.2.3", machineOwner, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := withURLParam(newRequest(http.MethodGet, "/api/runtimes/"+runtimeB+"/update", nil), "runtimeId", runtimeB)
	w := httptest.NewRecorder()
	testHandler.GetActiveUpdate(w, req)
	var body struct {
		Update *UpdateRequest `json:"update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %d %s: %v", w.Code, w.Body.String(), err)
	}
	if body.Update == nil || body.Update.ID != queued.ID || body.Update.RuntimeID != runtimeA {
		t.Fatalf("active = %+v, want the update queued through runtime A", body.Update)
	}

	// A second update for the same machine is refused, whichever runtime asks.
	initReq := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+runtimeB+"/update", map[string]any{
		"target_version":  "v1.2.3",
		"queue_when_busy": true,
	}), "runtimeId", runtimeB)
	initW := httptest.NewRecorder()
	testHandler.InitiateUpdate(initW, initReq)
	if initW.Code != http.StatusConflict {
		t.Fatalf("second update on the machine = %d, want 409: %s", initW.Code, initW.Body.String())
	}
}

// The initiator exception does not reach past the runtime's own visibility:
// once the initiator can no longer see the runtime the update was queued on,
// the machine card through another runtime does not show it either.
func TestActiveUpdateHidesARuntimeTheCallerCannotSee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	suffix := randomID()
	daemonID := "mul-9633-hidden-" + suffix
	var machineOwner string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Hidden Owner', $1) RETURNING id`,
		"hidden-owner-"+suffix+"@multica.test").Scan(&machineOwner); err != nil {
		t.Fatalf("create machine owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, machineOwner)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, machineOwner)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, machineOwner); err != nil {
		t.Fatalf("add machine owner: %v", err)
	}
	var privateA, publicB string
	for i, dst := range []*string{&privateA, &publicB} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
			)
			VALUES ($1, $2, 'Hidden Runtime', 'local', $3, 'online', 'hidden test', '{}'::jsonb, $4, $5, now())
			RETURNING id
		`, testWorkspaceID, daemonID, []string{"hidden_test_a", "hidden_test_b"}[i], machineOwner, []string{"private", "public"}[i]).Scan(dst); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
	}
	// Queued by the caller back when it could still use runtime A.
	if _, err := store.Create(ctx, privateA, "daemon:"+machineOwner+":"+daemonID, "v1.2.3", testUserID, true); err != nil {
		t.Fatalf("create: %v", err)
	}

	req := withURLParam(newRequest(http.MethodGet, "/api/runtimes/"+publicB+"/update", nil), "runtimeId", publicB)
	w := httptest.NewRecorder()
	testHandler.GetActiveUpdate(w, req)
	var body struct {
		Update *UpdateRequest `json:"update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %d %s: %v", w.Code, w.Body.String(), err)
	}
	if body.Update != nil {
		t.Fatalf("active = %+v; the caller cannot see runtime A any more", body.Update)
	}
}

// machineFixture creates a machine owner with one public runtime of daemonID in
// each given workspace.
func machineFixture(t *testing.T, label string, workspaceIDs ...string) (owner, daemonID string, runtimeIDs []string) {
	t.Helper()
	ctx := context.Background()
	suffix := randomID()
	daemonID = "mul-9633-" + label + "-" + suffix
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Machine Owner', $1) RETURNING id`,
		label+"-owner-"+suffix+"@multica.test").Scan(&owner); err != nil {
		t.Fatalf("create machine owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, owner)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, owner)
	})
	for _, ws := range workspaceIDs {
		if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, ws, owner); err != nil {
			t.Fatalf("add machine owner: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
			)
			VALUES ($1, $2, 'Machine Runtime', 'local', $3, 'online', 'machine test', '{}'::jsonb, $4, 'public', now())
			RETURNING id
		`, ws, daemonID, label+"_test", owner).Scan(&id); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
		runtimeIDs = append(runtimeIDs, id)
	}
	return owner, daemonID, runtimeIDs
}

// secondWorkspace creates another workspace the test user belongs to with role.
func secondWorkspace(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	suffix := randomID()
	var ws string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Update Second WS', $1, '', 'USW')
		RETURNING id
	`, "update-second-"+suffix).Scan(&ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, ws)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, ws)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`, ws, testUserID, role); err != nil {
		t.Fatalf("add test user: %v", err)
	}
	return ws
}

func getActiveUpdate(t *testing.T, runtimeID string) *UpdateRequest {
	t.Helper()
	req := withURLParam(newRequest(http.MethodGet, "/api/runtimes/"+runtimeID+"/update", nil), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	testHandler.GetActiveUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("active update = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Update *UpdateRequest `json:"update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return body.Update
}

// An admin of two workspaces a machine serves queues its update through the
// runtime in one and opens the machine through the runtime in the other: the
// update shows, and can be cancelled through the runtime it was queued on.
func TestActiveUpdateFollowsTheCallerIntoTheMachinesOtherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	other := secondWorkspace(t, "admin")
	owner, daemonID, runtimes := machineFixture(t, "xws", other, testWorkspaceID)
	runtimeInOther, runtimeHere := runtimes[0], runtimes[1]

	queued, err := store.Create(ctx, runtimeInOther, "daemon:"+owner+":"+daemonID, "v1.2.3", testUserID, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	active := getActiveUpdate(t, runtimeHere)
	if active == nil || active.ID != queued.ID || active.RuntimeID != runtimeInOther || !active.Cancellable {
		t.Fatalf("active = %+v; want the cancellable update queued in the other workspace", active)
	}

	cancelReq := withURLParams(newRequest(http.MethodPost, "/api/runtimes/"+runtimeInOther+"/update/"+queued.ID+"/cancel", nil),
		"runtimeId", runtimeInOther, "updateId", queued.ID)
	w := httptest.NewRecorder()
	testHandler.CancelUpdate(w, cancelReq)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel through the original runtime = %d: %s", w.Code, w.Body.String())
	}
}

// The rule in the other workspace is the one that applies there: only someone
// who can manage that runtime now sees the update or cancels it. Having
// started it is not enough once that right is gone (demoted, or left).
func TestActiveUpdateInTheMachinesOtherWorkspaceNeedsAccessThere(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	other := secondWorkspace(t, "admin")
	owner, daemonID, runtimes := machineFixture(t, "xwsdeny", other, testWorkspaceID)
	runtimeInOther, runtimeHere := runtimes[0], runtimes[1]

	queued, err := store.Create(ctx, runtimeInOther, "daemon:"+owner+":"+daemonID, "v1.2.3", testUserID, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active == nil || active.ID != queued.ID {
		t.Fatalf("active = %+v; an admin there must see it", active)
	}

	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, other, testUserID); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active != nil {
		t.Fatalf("active = %+v; the initiator was demoted to a plain member there", active)
	}
	cancelReq := withURLParams(newRequest(http.MethodPost, "/api/runtimes/"+runtimeInOther+"/update/"+queued.ID+"/cancel", nil),
		"runtimeId", runtimeInOther, "updateId", queued.ID)
	w := httptest.NewRecorder()
	testHandler.CancelUpdate(w, cancelReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cancel by the demoted initiator = %d, want 403: %s", w.Code, w.Body.String())
	}

	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'admin' WHERE workspace_id = $1 AND user_id = $2`, other, testUserID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active == nil {
		t.Fatal("an admin there lost sight of the update")
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, other, testUserID); err != nil {
		t.Fatalf("leave workspace: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active != nil {
		t.Fatalf("active = %+v; the caller left the workspace it was queued in", active)
	}
}

// updateDispatchLog drives heartbeats for every runtime of a machine, as both
// kinds of daemon, and records each update ID the server hands out.
type updateDispatchLog struct {
	t          *testing.T
	deferrable bool // whether the machine's daemon can hold an update
	ids        map[string]int
}

func (l *updateDispatchLog) beat(runtimeIDs ...string) {
	l.t.Helper()
	for round := 0; round < 3; round++ {
		for _, rt := range runtimeIDs {
			ack, _, err := testHandler.processHeartbeat(context.Background(), rt,
				protocol.DaemonHeartbeatRequestPayload{RuntimeID: rt, SupportsDeferredUpdate: l.deferrable})
			if err != nil {
				l.t.Fatalf("heartbeat %s: %v", rt, err)
			}
			if ack.PendingUpdate != nil {
				l.ids[ack.PendingUpdate.ID]++
			}
		}
	}
}

func (l *updateDispatchLog) onlyDispatched(id string) {
	l.t.Helper()
	for got := range l.ids {
		if got != id {
			l.t.Fatalf("dispatched %v; while %s may still run, no other update ID may go out", l.ids, id)
		}
	}
}

func initiateUpdateThrough(t *testing.T, runtimeID string) (*UpdateRequest, int) {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/update", map[string]any{
		"target_version":  "v1.2.3",
		"queue_when_busy": true,
	}), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	testHandler.InitiateUpdate(w, req)
	if w.Code != http.StatusOK {
		return nil, w.Code
	}
	var out UpdateRequest
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &out, w.Code
}

// One machine serves runtimes in two workspaces, and gains a third runtime
// while an update is queued. From the click until that update ends, every
// heartbeat of every runtime hands out at most that one update ID, and every
// other attempt to start one is refused — whether the machine's daemon can
// hold updates or predates them. Once it ends, the machine takes the next
// update at once, through any runtime.
func TestOneMachineDispatchesOneUpdateAtATime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	stores := map[string]func() UpdateStore{
		"memory": func() UpdateStore { return NewInMemoryUpdateStore() },
	}
	if os.Getenv("REDIS_TEST_URL") != "" {
		stores["redis"] = func() UpdateStore { return NewRedisUpdateStore(newRedisTestClient(t)) }
	}
	for name, newStore := range stores {
		for _, deferrable := range []bool{true, false} {
			name := name + map[bool]string{true: "/deferrable-daemon", false: "/fail-fast-daemon"}[deferrable]
			t.Run(name, func(t *testing.T) {
				withUpdateReofferInterval(t, 0)
				ctx := context.Background()
				orig := testHandler.UpdateStore
				store := newStore()
				testHandler.UpdateStore = store
				t.Cleanup(func() { testHandler.UpdateStore = orig })

				other := secondWorkspace(t, "admin")
				owner, daemonID, runtimes := machineFixture(t, "dispatch", other, testWorkspaceID)
				runtimeA, runtimeB := runtimes[0], runtimes[1]
				log := &updateDispatchLog{t: t, deferrable: deferrable, ids: map[string]int{}}

				first, code := initiateUpdateThrough(t, runtimeB)
				if first == nil {
					t.Fatalf("initiate = %d", code)
				}
				log.beat(runtimeA, runtimeB)
				log.onlyDispatched(first.ID)
				if log.ids[first.ID] == 0 {
					t.Fatal("the queued update was never offered")
				}

				if deferrable {
					// The daemon is busy and holds it.
					two := 2
					if _, err := store.Schedule(ctx, first.ID, &two); err != nil {
						t.Fatalf("schedule: %v", err)
					}
				}
				if _, code := initiateUpdateThrough(t, runtimeA); code != http.StatusConflict {
					t.Fatalf("initiate through the other workspace's runtime = %d, want 409", code)
				}

				// A runtime registered after the update was queued.
				var runtimeC string
				if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_runtime (
					workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
				)
				VALUES ($1, $2, 'Machine Runtime', 'local', 'dispatch_late', 'online', 'machine test', '{}'::jsonb, $3, 'public', now())
				RETURNING id
			`, testWorkspaceID, daemonID, owner).Scan(&runtimeC); err != nil {
					t.Fatalf("register a new runtime: %v", err)
				}
				if _, code := initiateUpdateThrough(t, runtimeC); code != http.StatusConflict {
					t.Fatalf("initiate through the new runtime = %d, want 409", code)
				}
				if active := getActiveUpdate(t, runtimeC); active == nil || active.ID != first.ID {
					t.Fatalf("active through the new runtime = %+v, want the queued update", active)
				}
				log.beat(runtimeA, runtimeB, runtimeC)
				log.onlyDispatched(first.ID)

				// Installing, then finished.
				if _, err := store.MarkRunning(ctx, first.ID); err != nil {
					t.Fatalf("mark running: %v", err)
				}
				for _, rt := range []string{runtimeA, runtimeB, runtimeC} {
					if _, code := initiateUpdateThrough(t, rt); code != http.StatusConflict {
						t.Fatalf("initiate through %s while installing = %d, want 409", rt, code)
					}
				}
				log.beat(runtimeA, runtimeB, runtimeC)
				log.onlyDispatched(first.ID)
				if err := store.Complete(ctx, first.ID, "Updated to v1.2.3"); err != nil {
					t.Fatalf("complete: %v", err)
				}

				next, code := initiateUpdateThrough(t, runtimeC)
				if next == nil {
					t.Fatalf("initiate after the first update ended = %d, want 200", code)
				}
				log.ids = map[string]int{}
				log.beat(runtimeA, runtimeB, runtimeC)
				log.onlyDispatched(next.ID)
			})
		}
	}
}
