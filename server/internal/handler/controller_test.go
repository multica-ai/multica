package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/runcontrol"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type controllerFixture struct {
	h                             *Handler
	issue, profile, runtime, host string
	policy                        ControllerPolicy
}
type controllerRead struct {
	Controller      controllerState `json:"controller"`
	IssueRevision   int64           `json:"issue_revision"`
	EvidenceVersion string          `json:"evidence_version"`
}

func controllerCall(t *testing.T, f *controllerFixture, fn http.HandlerFunc, body any) *testutil.Response {
	t.Helper()
	r := testutil.WithURLParams(testutil.JSONRequest("POST", "/api/controller/issues/"+f.issue, body), "id", f.issue)
	r.Header.Set("Authorization", "Bearer mct_test_only")
	return testutil.Call(t, f.h.ControllerAuth(fn).ServeHTTP, r)
}
func (f *controllerFixture) read(t *testing.T) controllerRead {
	t.Helper()
	var s controllerRead
	controllerCall(t, f, f.h.GetControllerIssue, nil).Want(200).JSON(&s)
	return s
}
func (f *controllerFixture) addIssue(t *testing.T) {
	t.Helper()
	f.issue = dbfx.Issue(t, "Controller fixture", testutil.Cols{"status": "backlog"})
	id := f.issue
	t.Cleanup(func() {
		for _, table := range []string{"controller_effect_operation", "controller_effect", "controller_event", "controller_outbox", "controlled_run", "issue_controller", "agent_task_queue"} {
			if _, err := testPool.Exec(context.Background(), "DELETE FROM "+table+" WHERE issue_id=$1", id); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	})
	f.policy.ExpectedIssueRevision = 1
	controllerCall(t, f, f.h.EnrollControllerIssue, f.policy).Want(200)
}
func newControllerFixture(t *testing.T) *controllerFixture {
	t.Helper()
	h := *testHandler
	host := uuidToString(dbid.NewV7())
	verifier := sha256.Sum256([]byte("mct_test_only"))
	h.Controller = ControllerSettings{TokenHash: hex.EncodeToString(verifier[:]), WorkspaceID: testWorkspaceID, AllowedHostIDs: []string{host}}
	runtime := dbfx.Runtime(t, "Controller runtime", testutil.Cols{"daemon_id": host, "runtime_mode": "local", "provider": "codex"})
	profile := dbfx.Agent(t, "Controller profile", runtime, testutil.Cols{"runtime_mode": "local", "max_concurrent_tasks": 3, "model": "fixture-model"})
	agent, err := h.Queries.GetAgent(context.Background(), parseUUID(profile))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := service.ControllerProfileHash(context.Background(), h.Queries, agent)
	if err != nil {
		t.Fatal(err)
	}
	live := "live_" + strings.ReplaceAll(host, "-", "")[:8]
	dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": testWorkspaceID, "key": live, "name": "Controller Live", "category": "done", "color": "#00ff00"})
	options := []map[string]string{}
	progressOptions := map[string]string{}
	for _, label := range controllerProgress {
		options = append(options, map[string]string{"id": label, "name": label, "color": "gray"})
		progressOptions[label] = label
	}
	progress := dbfx.Insert(t, "issue_property", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Controller Progress " + host, "type": "select", "config": map[string]any{"options": options}})
	picture := dbfx.Insert(t, "issue_property", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Controller Picture " + host, "type": "text"})
	f := &controllerFixture{h: &h, profile: profile, runtime: runtime, host: host}
	f.policy = ControllerPolicy{ScopeRevision: "accepted-1", SourcePath: t.TempDir(), AllowedRepositories: []runcontrol.Repository{}, AuthorityRecordIDs: []string{"accepted-fixture"}, Targets: []ControllerTarget{{ProfileID: profile, RuntimeID: runtime, HostID: host, ProfileHash: hash}}, Statuses: map[string]string{"Inbox": "backlog", "Queued": "todo", "Working": "in_progress", "Needs You": "in_review", "Blocked": "blocked", "Done": "done", "Live": live}, ProgressPropertyID: progress, PicturePropertyID: picture, ProgressOptions: progressOptions, BudgetKey: host, MaxActive: 2, BudgetEvidence: "isolated fixture limit", BudgetExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second), AllowedEffects: []ControllerEffectAuthority{{ResourceKey: host, AuthorityRecordID: "effect-fixture", CandidateIdentity: "candidate-1"}}}
	f.addIssue(t)
	return f
}
func (f *controllerFixture) projection(t *testing.T, event, progress string) ControllerProjection {
	s := f.read(t)
	return ControllerProjection{EventID: event, ExpectedRevision: s.Controller.Revision, ExpectedIssueRevision: s.IssueRevision, ExpectedEvidenceVersion: s.EvidenceVersion, ScopeRevision: s.Controller.ScopeRevision, Progress: progress, CurrentPicture: "Local test evidence", EvidenceIDs: []string{"fixture-evidence"}}
}
func (f *controllerFixture) launch(t *testing.T, action string) (ControllerLaunch, runcontrol.Manifest) {
	t.Helper()
	s := f.read(t)
	p := ControllerLaunch{ScopeRevision: s.Controller.ScopeRevision, AuthorityEpoch: s.Controller.AuthorityEpoch, ExpectedEvidenceVersion: s.EvidenceVersion, ActionID: action, ProfileID: f.profile, CandidateIdentity: "candidate-1", Attempt: 1, MaxAttempts: 3, Instruction: "Read the isolated canary fixture."}
	var m runcontrol.Manifest
	controllerCall(t, f, f.h.LaunchControllerRun, p).Want(200).JSON(&m)
	return p, m
}

func TestControllerProjectionCASAndOutboxRecovery(t *testing.T) {
	f := newControllerFixture(t)
	a := f.projection(t, "first", "Inbox")
	b := a
	b.EventID = "competing"
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, p := range []ControllerProjection{a, b} {
		wg.Add(1)
		go func(p ControllerProjection) {
			defer wg.Done()
			<-start
			codes <- controllerCall(t, f, f.h.ProjectControllerIssue, p).Code
		}(p)
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("CAS must have one winner and one conflict: %v", counts)
	}
	read := f.read(t)
	if read.Controller.Revision != 1 || read.IssueRevision != 2 {
		t.Fatalf("non-atomic revision: %+v", read)
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM controller_outbox WHERE issue_id=$1 AND delivered_at IS NULL`, f.issue).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable outbox: %d %v", count, err)
	}
	// A fresh handler represents restart after commit but before readback/ack.
	restarted := *f.h
	f.h = &restarted
	controllerCall(t, f, f.h.GetControllerOutbox, nil).Want(200)
	controllerCall(t, f, f.h.AckControllerOutbox, map[string]any{"revision": 1, "issue_revision": 2}).Want(200)
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, f.issue).Scan(&count); err != nil || count != 0 {
		t.Fatalf("projection launched a provider: %d %v", count, err)
	}
}

func TestControllerProtectedWritersAndTruthEvidence(t *testing.T) {
	f := newControllerFixture(t)
	for _, token := range []string{"mat_worker", "mdt_daemon", "mul_owner", "mcn_cloud", "forged.jwt", ""} {
		r := testutil.WithURLParams(testutil.JSONRequest("PUT", "/api/controller/issues/"+f.issue, f.projection(t, "unauthorized", "Inbox")), "id", f.issue)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-User-ID", testUserID)
		r.Header.Set("X-Controller-ID", "owner")
		testutil.Call(t, f.h.ControllerAuth(http.HandlerFunc(f.h.ProjectControllerIssue)).ServeHTTP, r).Want(401)
	}
	for _, headers := range [][]string{{"X-User-ID", testUserID}, {"X-User-ID", testUserID, "X-Agent-ID", f.profile}} {
		r := testutil.WithURLParams(testutil.WithHeaders(testutil.JSONRequest("PUT", "/api/issues/"+f.issue, map[string]any{"status": "done"}), headers...), "id", f.issue)
		r.Header.Set("X-Workspace-ID", testWorkspaceID)
		testutil.Call(t, f.h.UpdateIssue, r).Want(403)
	}
	for _, query := range []string{`UPDATE issue SET status='done' WHERE id=$1`, `UPDATE issue SET metadata='{"last_progress_what":"forged"}' WHERE id=$1`, `INSERT INTO agent_task_queue(agent_id,runtime_id,issue_id) SELECT assignee_id,NULL,id FROM issue WHERE id=$1`} {
		if _, err := testPool.Exec(context.Background(), query, f.issue); err == nil {
			t.Fatal("unguarded direct writer", query)
		}
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET title='Allowed evidence title' WHERE id=$1`, f.issue); err != nil {
		t.Fatal("unrelated edit rejected", err)
	}
	controllerCall(t, f, f.h.ProjectControllerIssue, f.projection(t, "false-working", "Working")).Want(409)
	controllerCall(t, f, f.h.ProjectControllerIssue, f.projection(t, "false-queued", "Queued")).Want(409)
	controllerCall(t, f, f.h.ProjectControllerIssue, f.projection(t, "false-done", "Done")).Want(403)
	p := f.projection(t, "stale-owner", "Inbox")
	dbfx.Comment(t, f.issue, "Owner changed the accepted direction.")
	controllerCall(t, f, f.h.ProjectControllerIssue, p).Want(409)
}

func TestControllerLaunchReplayFailureRetryAndStop(t *testing.T) {
	f := newControllerFixture(t)
	p, m := f.launch(t, "bounded-action")
	var again runcontrol.Manifest
	controllerCall(t, f, f.h.LaunchControllerRun, p).Want(200).JSON(&again)
	if again.RunID != m.RunID {
		t.Fatal("replay created another run")
	}
	p.Instruction = "changed instruction"
	controllerCall(t, f, f.h.LaunchControllerRun, p).Want(409)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, m.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.TaskService.StartTask(context.Background(), parseUUID(m.RunID)); err != nil {
		t.Fatal(err)
	}
	failed, err := f.h.TaskService.FailTask(context.Background(), parseUUID(m.RunID), "temporary provider error", "", "", "", "provider_network", false, "", "")
	if err != nil || failed.Status != "failed" {
		t.Fatalf("failure was undone by native retry: %v %+v", err, failed)
	}
	var count int
	_ = testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, f.issue).Scan(&count)
	if count != 1 {
		t.Fatalf("native retry escaped controller: %d", count)
	}
	s := f.read(t)
	p.ExpectedEvidenceVersion = s.EvidenceVersion
	p.Instruction = "Read the isolated canary fixture."
	p.Attempt = 2
	p.ParentRunID = m.RunID
	p.NotBefore = time.Now().UTC().Add(time.Minute)
	var retry runcontrol.Manifest
	controllerCall(t, f, f.h.LaunchControllerRun, p).Want(200).JSON(&retry)
	if retry.RunID == m.RunID || !retry.QueuedAt.Equal(m.QueuedAt) {
		t.Fatal("retry lost original queue age or attempt identity")
	}
	stop := map[string]any{"expected_revision": s.Controller.Revision, "event_id": "owner-stop", "reason": "Stop fixture execution"}
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	row, err := f.h.Queries.GetAgentTask(context.Background(), parseUUID(retry.RunID))
	if err != nil || row.Status != "cancelled" {
		t.Fatalf("stop did not cancel native pending run: %+v %v", row, err)
	}
	controllerCall(t, f, f.h.ProjectControllerIssue, f.projection(t, "stopped-projection", "Inbox")).Want(409)
}

func TestControllerBudgetAndDaemonBinding(t *testing.T) {
	f := newControllerFixture(t)
	for i := 0; i < 3; i++ {
		if i > 0 {
			f.addIssue(t)
		}
		_, m := f.launch(t, "capacity")
		if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, m.RunID); err != nil {
			t.Fatal(err)
		}
		_, err := f.h.TaskService.StartTask(context.Background(), parseUUID(m.RunID))
		if (i < 2 && err != nil) || (i == 2 && err == nil) {
			t.Fatalf("shared budget admission %d: %v", i, err)
		}
		task, err := f.h.Queries.GetAgentTask(context.Background(), parseUUID(m.RunID))
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range []string{f.host, "wrong-host", ""} {
			r := testutil.JSONRequest("POST", "/lifecycle", nil)
			r = r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, host))
			response := testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
				if f.h.controllerTaskAccess(w, r, task) {
					w.WriteHeader(204)
				}
			}, r)
			if host == f.host {
				response.Want(204)
			} else {
				response.Want(403)
			}
		}
	}
}

func TestControllerEffectFencesAndAmbiguousRecovery(t *testing.T) {
	f := newControllerFixture(t)
	p := effectRequest{EventID: "reserve", OperationID: "op1", ResourceKey: f.host, CandidateIdentity: "candidate-1", AuthorityRecordID: "effect-fixture", AuthorityEpoch: 1, Phase: "reserve"}
	controllerCall(t, f, f.h.ControllerEffect, p).Want(200)
	if controllerCall(t, f, f.h.ControllerEffect, p).Want(200).Header().Get("X-Controller-Replayed") != "true" {
		t.Fatal("replay not marked")
	}
	p.OperationID = "op2"
	controllerCall(t, f, f.h.ControllerEffect, p).Want(429)
	if _, err := testPool.Exec(context.Background(), `UPDATE controller_effect SET lease_expires_at=now()-interval '1 second' WHERE resource_key=$1`, f.host); err != nil {
		t.Fatal(err)
	}
	controllerCall(t, f, f.h.ControllerEffect, p).Want(200)
	p.EventID = "begin"
	p.Phase = "begin"
	p.FencingToken = 1
	controllerCall(t, f, f.h.ControllerEffect, p).Want(409)
	p.FencingToken = 2
	controllerCall(t, f, f.h.ControllerEffect, p).Want(200)
	if _, err := testPool.Exec(context.Background(), `UPDATE controller_effect SET lease_expires_at=now()-interval '1 second' WHERE resource_key=$1`, f.host); err != nil {
		t.Fatal(err)
	}
	p.EventID = "reserve"
	p.Phase = "reserve"
	p.OperationID = "op3"
	controllerCall(t, f, f.h.ControllerEffect, p).Want(409)
	p.OperationID = "op1"
	p.EventID = "different-event"
	controllerCall(t, f, f.h.ControllerEffect, p).Want(409)
}

func TestControllerBulkRecoveryCannotForgeNativeFailure(t *testing.T) {
	f := newControllerFixture(t)
	_, m := f.launch(t, "bulk-recovery")
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1`, m.RunID); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"wrong-host", ""} {
		for _, runtimePath := range []string{f.runtime, strings.ToUpper(f.runtime), strings.ReplaceAll(f.runtime, "-", "")} {
			r := testutil.WithURLParams(testutil.JSONRequest("POST", "/recover-orphans", map[string]any{}), "runtimeId", runtimePath)
			if host == "" {
				r.Header.Set("X-User-ID", testUserID)
			} else {
				r = r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, host))
			}
			testutil.Call(t, f.h.RecoverOrphanedTasks, r).Want(403)
			task, err := f.h.Queries.GetAgentTask(context.Background(), parseUUID(m.RunID))
			if err != nil || task.Status != "running" {
				t.Fatalf("unauthorized recovery changed native task: %+v %v", task, err)
			}
		}
	}
	r := testutil.WithURLParams(testutil.JSONRequest("POST", "/recover-orphans", map[string]any{}), "runtimeId", f.runtime)
	r = r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, f.host))
	testutil.Call(t, f.h.RecoverOrphanedTasks, r).Want(200)
	task, _ := f.h.Queries.GetAgentTask(context.Background(), parseUUID(m.RunID))
	if task.Status != "failed" {
		t.Fatalf("bound daemon did not record orphan: %s", task.Status)
	}
}

func TestControllerWorkingStopProjectsAndReplaysOnce(t *testing.T) {
	f := newControllerFixture(t)
	_, m := f.launch(t, "working-stop")
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='running',started_at=now(),session_id='native-session' WHERE id=$1`, m.RunID); err != nil {
		t.Fatal(err)
	}
	controllerCall(t, f, f.h.ProjectControllerIssue, f.projection(t, "observed-working", "Working")).Want(200)
	s := f.read(t)
	stop := map[string]any{"expected_revision": s.Controller.Revision, "event_id": "stop-working", "reason": "Owner stops canary"}
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	current := f.read(t)
	if current.Controller.Revision != s.Controller.Revision+1 || !current.Controller.Stopped {
		t.Fatalf("STOP replay revised twice: %+v", current)
	}
	issue, _ := f.h.Queries.GetIssue(context.Background(), parseUUID(f.issue))
	if issue.Status != f.policy.Statuses["Blocked"] || !strings.Contains(string(issue.Properties), "Stopped:") {
		t.Fatalf("STOP left misleading projection: %+v", issue)
	}
	controllerCall(t, f, f.h.AckControllerOutbox, map[string]any{"revision": current.Controller.Revision, "issue_revision": current.IssueRevision}).Want(200)
}

func TestControllerReleaseRequiresDrainReadbackAndPreservesReplay(t *testing.T) {
	f := newControllerFixture(t)
	_, m := f.launch(t, "release-drain")
	s := f.read(t)
	stop := map[string]any{"expected_revision": s.Controller.Revision, "event_id": "stop-before-release", "reason": "Stop before releasing ownership"}
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	stopped := f.read(t)
	release := map[string]any{"expected_revision": stopped.Controller.Revision, "event_id": "owner-release", "reason": "Return the drained issue to normal scheduling"}

	// The stop projection must be observed before its guard can be removed.
	controllerCall(t, f, f.h.ReleaseControllerIssue, release).Want(409)
	controllerCall(t, f, f.h.AckControllerOutbox, map[string]any{"revision": stopped.Controller.Revision, "issue_revision": stopped.IssueRevision}).Want(200)
	controllerCall(t, f, f.h.ReleaseControllerIssue, release).Want(200)
	replayed := controllerCall(t, f, f.h.ReleaseControllerIssue, release).Want(200)
	if replayed.Header().Get("X-Controller-Replayed") != "true" {
		t.Fatal("release replay not marked")
	}

	var owned bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM issue_controller WHERE issue_id=$1)`, f.issue).Scan(&owned); err != nil || owned {
		t.Fatalf("controller ownership remains after release: %v %v", owned, err)
	}
	var runCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM controlled_run WHERE issue_id=$1 AND run_id=$2`, f.issue, m.RunID).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("release lost immutable run history: %d %v", runCount, err)
	}
	r := testutil.WithURLParams(testutil.WithHeaders(testutil.JSONRequest("PUT", "/api/issues/"+f.issue, map[string]any{"status": "todo"}), "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID), "id", f.issue)
	testutil.Call(t, f.h.UpdateIssue, r).Want(200)
}

func TestControllerReleaseRejectsUnreconciledExecutingEffect(t *testing.T) {
	f := newControllerFixture(t)
	p := effectRequest{EventID: "reserve-release", OperationID: "release-op", ResourceKey: f.host, CandidateIdentity: "candidate-1", AuthorityRecordID: "effect-fixture", AuthorityEpoch: 1, Phase: "reserve"}
	var reserved struct {
		FencingToken int64 `json:"fencing_token"`
	}
	controllerCall(t, f, f.h.ControllerEffect, p).Want(200).JSON(&reserved)
	p.EventID = "begin-release"
	p.Phase = "begin"
	p.FencingToken = reserved.FencingToken
	controllerCall(t, f, f.h.ControllerEffect, p).Want(200)
	s := f.read(t)
	stop := map[string]any{"expected_revision": s.Controller.Revision, "event_id": "stop-with-effect", "reason": "Stop while effect receipt is pending"}
	controllerCall(t, f, f.h.StopControllerIssue, stop).Want(200)
	stopped := f.read(t)
	controllerCall(t, f, f.h.AckControllerOutbox, map[string]any{"revision": stopped.Controller.Revision, "issue_revision": stopped.IssueRevision}).Want(200)
	release := map[string]any{"expected_revision": stopped.Controller.Revision, "event_id": "unsafe-release", "reason": "Must fail while effect is ambiguous"}
	controllerCall(t, f, f.h.ReleaseControllerIssue, release).Want(409)
}

func TestControllerClaimCannotValidateRevertedPayload(t *testing.T) {
	f := newControllerFixture(t)
	_, m := f.launch(t, "immutable-profile")
	a, _ := f.h.Queries.GetAgent(context.Background(), parseUUID(f.profile))
	accepted, err := service.LoadControllerProfile(context.Background(), f.h.Queries, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(context.Background(), `UPDATE agent SET instructions='temporary unaccepted B' WHERE id=$1`, f.profile); err != nil {
		t.Fatal(err)
	}
	b, _ := f.h.Queries.GetAgent(context.Background(), parseUUID(f.profile))
	assembledB, err := service.LoadControllerProfile(context.Background(), f.h.Queries, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(context.Background(), `UPDATE agent SET instructions=$2 WHERE id=$1`, f.profile, a.Instructions); err != nil {
		t.Fatal(err)
	}
	r := testutil.JSONRequest("POST", "/claim", nil)
	r.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityControllerV1)
	r = r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, f.host))
	task, _ := f.h.Queries.GetAgentTask(context.Background(), parseUUID(m.RunID))
	resp := AgentTaskResponse{}
	if err = f.h.applyControllerClaim(r, task, &resp, &assembledB); err == nil {
		t.Fatal("B payload passed after database reverted to A")
	}
	if err = f.h.applyControllerClaim(r, task, &resp, &accepted); err != nil {
		t.Fatal(err)
	}
	if resp.LaunchAuthority == nil || resp.LaunchAuthority.ProfileHash != accepted.Hash() {
		t.Fatal("claim lost snapshot authority")
	}
	if _, err = testPool.Exec(context.Background(), `UPDATE agent_runtime SET profile_id=$2 WHERE id=$1`, f.runtime, uuidToString(dbid.NewV7())); err != nil {
		t.Fatal(err)
	}
	if _, err = service.LoadControllerProfile(context.Background(), f.h.Queries, a); err == nil {
		t.Fatal("custom runtime command binding accepted")
	}
}
