package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Queued updates ship without a rollout switch, so while a deploy replaces API
// replicas, replicas of the previous release keep serving the same Redis keys.
// previousReleaseUpdates replays that release's writes command for command
// (RedisUpdateStore on main before MUL-9633) so the tests can interleave them
// with this release's.
type previousReleaseUpdates struct {
	rdb *redis.Client
}

// reserve is the previous release's first Create step: the runtime's slot,
// taken before the request is written.
func (p previousReleaseUpdates) reserve(t *testing.T, runtimeID, id string) bool {
	t.Helper()
	ok, err := p.rdb.SetNX(context.Background(), updateActiveKey(runtimeID), id, updateStoreRetention).Result()
	if err != nil {
		t.Fatalf("previous release: reserve: %v", err)
	}
	return ok
}

// write is the previous release's second Create step.
func (p previousReleaseUpdates) write(t *testing.T, runtimeID, id string, status UpdateStatus) {
	t.Helper()
	now := time.Now()
	req := &UpdateRequest{ID: id, RuntimeID: runtimeID, Status: status, TargetVersion: "v1.2.3", CreatedAt: now, UpdatedAt: now}
	if status == UpdateRunning {
		req.RunStartedAt = &now
	}
	p.put(t, req)
	if status == UpdatePending {
		p.rdb.ZAdd(context.Background(), updatePendingKey(runtimeID), redis.Z{Score: float64(now.UnixNano()), Member: id})
	}
}

// put stores req the way the previous release persists it: only the fields it
// knows, with the short retention TTL.
func (p previousReleaseUpdates) put(t *testing.T, req *UpdateRequest) {
	t.Helper()
	data, err := json.Marshal(struct {
		Public       *UpdateRequest `json:"r"`
		RunStartedAt *time.Time     `json:"s,omitempty"`
	}{req, req.RunStartedAt})
	if err != nil {
		t.Fatalf("previous release: encode: %v", err)
	}
	if err := p.rdb.Set(context.Background(), updateKey(req.ID), data, updateStoreRetention).Err(); err != nil {
		t.Fatalf("previous release: persist: %v", err)
	}
}

// fail is the previous release's handling of a daemon's busy report: it
// rewrites the request as failed and frees only the runtime's own slot.
func (p previousReleaseUpdates) fail(t *testing.T, req *UpdateRequest) {
	t.Helper()
	failed := *req
	failed.Status = UpdateFailed
	failed.Error = "runtime update deferred because agent work is starting or still active; retry when the machine is idle"
	p.put(t, &failed)
	deleteIfValueScript.Run(context.Background(), p.rdb, []string{updateActiveKey(req.RuntimeID)}, req.ID)
}

// A previous-release replica took runtime B's slot and has not written its
// request yet. This release must not take that slot for stale and start a
// second update of the machine.
func TestRedisUpdateStore_PreviousReleaseSlotWithoutRequestIsKept(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	prev := previousReleaseUpdates{rdb}
	machine := []string{"runtime-a", "runtime-b"}

	if !prev.reserve(t, "runtime-b", "prev-1") {
		t.Fatal("previous release could not reserve a free slot")
	}
	for _, rt := range machine {
		if _, err := store.Create(ctx, rt, "daemon:owner:m1", "v1.2.3", "user-1", true, machine...); err != errUpdateInProgress {
			t.Fatalf("create on %s while the previous release is creating = %v, want errUpdateInProgress", rt, err)
		}
	}
	prev.write(t, "runtime-b", "prev-1", UpdatePending)

	active, err := store.GetActive(ctx, "daemon:owner:m1", machine...)
	if err != nil || active == nil || active.ID != "prev-1" {
		t.Fatalf("active = %+v, %v; want the previous release's request", active, err)
	}
}

// A request the previous release started on runtime A (before this release
// was deployed) still blocks an update of the machine through runtime B.
func TestRedisUpdateStore_PreviousReleaseRequestBlocksTheMachine(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	prev := previousReleaseUpdates{rdb}
	machine := []string{"runtime-a", "runtime-b"}

	prev.reserve(t, "runtime-a", "prev-running")
	prev.write(t, "runtime-a", "prev-running", UpdateRunning)

	if _, err := store.Create(ctx, "runtime-b", "daemon:owner:m1", "v1.2.3", "user-1", true, machine...); err != errUpdateInProgress {
		t.Fatalf("create through runtime B = %v, want errUpdateInProgress", err)
	}
	active, err := store.GetActive(ctx, "daemon:owner:m1", machine...)
	if err != nil || active == nil || active.ID != "prev-running" {
		t.Fatalf("active = %+v, %v; want the previous release's request", active, err)
	}
}

// A previous-release replica asked to update any runtime of a machine with an
// unfinished request sees that runtime's slot taken, until the request ends.
func TestRedisUpdateStore_PreviousReleaseSeesTheMachineTaken(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	prev := previousReleaseUpdates{rdb}
	machine := []string{"runtime-a", "runtime-b", "runtime-c"}

	for _, queue := range []bool{true, false} {
		req, err := store.Create(ctx, "runtime-a", "daemon:owner:m1", "v1.2.3", "user-1", queue, machine...)
		if err != nil {
			t.Fatalf("create (queue %v): %v", queue, err)
		}
		for _, rt := range machine {
			if prev.reserve(t, rt, "prev-"+rt) {
				t.Fatalf("queue %v: previous release reserved %s while the machine has an update", queue, rt)
			}
		}
		if err := store.Fail(ctx, req.ID, "boom"); err != nil {
			t.Fatalf("fail: %v", err)
		}
		for _, rt := range machine {
			if n, _ := rdb.Exists(ctx, updateActiveKey(rt)).Result(); n != 0 {
				t.Fatalf("queue %v: slot of %s left behind after the request ended", queue, rt)
			}
		}
	}
}

// A previous-release replica fails a queued request on a busy report and frees
// only the runtime's own slot. The other slots it leaves must not block the
// next update.
func TestRedisUpdateStore_SlotsOfARequestThePreviousReleaseEndedAreReleased(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	prev := previousReleaseUpdates{rdb}
	machine := []string{"runtime-a", "runtime-b"}

	req, err := store.Create(ctx, "runtime-a", "daemon:owner:m1", "v1.2.3", "user-1", true, machine...)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	prev.fail(t, req)

	if active, err := store.GetActive(ctx, "daemon:owner:m1", machine...); err != nil || active != nil {
		t.Fatalf("active = %+v, %v; want none once the request failed", active, err)
	}
	if _, err := store.Create(ctx, "runtime-b", "daemon:owner:m1", "v1.2.3", "user-1", true, machine...); err != nil {
		t.Fatalf("create after the previous release ended the request: %v", err)
	}
}

// A slot this release wrote together with its request does not outlive a
// request that is gone (evicted, say) by a whole day.
func TestRedisUpdateStore_SlotOfAVanishedRequestIsReleased(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	machine := []string{"runtime-a", "runtime-b"}

	req, err := store.Create(ctx, "runtime-a", "daemon:owner:m1", "v1.2.3", "user-1", true, machine...)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := rdb.Del(ctx, updateKey(req.ID)).Err(); err != nil {
		t.Fatalf("drop request: %v", err)
	}
	if _, err := store.Create(ctx, "runtime-b", "daemon:owner:m1", "v1.2.3", "user-1", true, machine...); err != nil {
		t.Fatalf("create behind the slots of a vanished request: %v", err)
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

// The rule in the other workspace is the one that applies there: a plain
// member who did not start the update does not see it, and neither does a
// caller who left that workspace.
func TestActiveUpdateInTheMachinesOtherWorkspaceNeedsAccessThere(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	other := secondWorkspace(t, "member")
	owner, daemonID, runtimes := machineFixture(t, "xwsdeny", other, testWorkspaceID)
	runtimeInOther, runtimeHere := runtimes[0], runtimes[1]

	if _, err := store.Create(ctx, runtimeInOther, "daemon:"+owner+":"+daemonID, "v1.2.3", owner, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active != nil {
		t.Fatalf("active = %+v; a plain member there who did not start it must not see it", active)
	}

	// The initiator sees it while a member there, and not after leaving.
	store = withUpdateStore(t)
	if _, err := store.Create(ctx, runtimeInOther, "daemon:"+owner+":"+daemonID, "v1.2.3", testUserID, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active == nil {
		t.Fatal("the initiator lost sight of its update")
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, other, testUserID); err != nil {
		t.Fatalf("leave workspace: %v", err)
	}
	if active := getActiveUpdate(t, runtimeHere); active != nil {
		t.Fatalf("active = %+v; the caller left the workspace it was queued in", active)
	}
}

// End to end on the shared store: a request the previous release left running
// on one runtime of the machine (any workspace) blocks queueing through
// another, and shows on the machine card meanwhile.
func TestInitiateUpdateRespectsAPreviousReleaseRequestOnTheMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rdb := newRedisTestClient(t)
	orig := testHandler.UpdateStore
	testHandler.UpdateStore = NewRedisUpdateStore(rdb)
	t.Cleanup(func() { testHandler.UpdateStore = orig })
	prev := previousReleaseUpdates{rdb}

	other := secondWorkspace(t, "admin")
	_, _, runtimes := machineFixture(t, "rollout", other, testWorkspaceID)
	runtimeInOther, runtimeHere := runtimes[0], runtimes[1]

	prev.reserve(t, runtimeInOther, "prev-running")
	prev.write(t, runtimeInOther, "prev-running", UpdateRunning)

	initiate := func() *httptest.ResponseRecorder {
		req := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+runtimeHere+"/update", map[string]any{
			"target_version":  "v1.2.3",
			"queue_when_busy": true,
		}), "runtimeId", runtimeHere)
		w := httptest.NewRecorder()
		testHandler.InitiateUpdate(w, req)
		return w
	}
	if w := initiate(); w.Code != http.StatusConflict {
		t.Fatalf("initiate while the previous release's update runs = %d, want 409: %s", w.Code, w.Body.String())
	}
	if active := getActiveUpdate(t, runtimeHere); active == nil || active.ID != "prev-running" {
		t.Fatalf("active = %+v; want the previous release's update", active)
	}

	// Once it ends, the update queues (no rollout switch), and a
	// previous-release replica cannot start another on any runtime of the
	// machine.
	if err := testHandler.UpdateStore.Complete(context.Background(), "prev-running", "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	w := initiate()
	if w.Code != http.StatusOK {
		t.Fatalf("initiate = %d: %s", w.Code, w.Body.String())
	}
	var created UpdateRequest
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if score, err := rdb.ZScore(context.Background(), updateOffersKey(runtimeHere), created.ID).Result(); err != nil || score == 0 {
		t.Fatalf("request is not in the queued-offer index (%v, %v); queueing must not need a switch", score, err)
	}
	for _, rt := range runtimes {
		if prev.reserve(t, rt, "prev-2") {
			t.Fatalf("previous release reserved %s while the machine has a queued update", rt)
		}
	}
}
