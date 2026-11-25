package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/rundiff"
)

// FIXME(test-integration): Preserve the confirmed workspace teardown contract, including orphan bodies and tenant isolation.
func TestWorkspaceRunFileChangesDeletion(t *testing.T) {
	if testPool == nil {
		t.Fatal("real database required")
	}
	ws := dbfx.Workspace(t, "QA file changes", uuid.NewString())
	neighbor := dbfx.Workspace(t, "QA file neighbor", uuid.NewString())
	dbfx.Exec(t, "INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner')", ws, testUserID)
	fx := testutil.New(testPool, ws, testUserID)
	runtime := fx.Runtime(t, "QA runtime")
	agent := fx.Agent(t, "QA agent", runtime)
	task := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime})
	orphan := uuid.NewString()
	other := uuid.NewString()
	for _, r := range []struct{ ws, task string }{{ws, task}, {ws, orphan}, {neighbor, other}} {
		dbfx.Exec(t, `INSERT INTO run_file_change(workspace_id,task_id,collection_id,manifest,bodies) VALUES ($1,$2,gen_random_uuid(),'{}','{"qa":"synthetic retained body"}')`, r.ws, r.task)
		dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", r.task)
	}
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	linked := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE task_id=$1", task)
	orphanLeft := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND task_id=$2 AND bodies<>'{}'", ws, orphan)
	otherLeft := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND task_id=$2", neighbor, other)
	t.Logf("after real DeleteWorkspace: linked=%d orphan_with_body=%d neighbor=%d", linked, orphanLeft, otherLeft)
	if linked != 0 || otherLeft != 1 {
		t.Fatal("linked cleanup/neighbor isolation violated")
	}
	if orphanLeft != 0 {
		t.Errorf("deleted workspace retains run_file_change with nonexistent task (no workspace sweep)")
	}
}

// Hold the real transaction at Commit, after the handler has taken its locks
// and performed its writes. There are no production hooks or timing sleeps.
type runFileChangeCommitStarter struct {
	beforeCommit func(context.Context, pgx.Tx) error
}

func (s runFileChangeCommitStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return runFileChangeCommitTx{Tx: tx, beforeCommit: s.beforeCommit}, nil
}

type runFileChangeCommitTx struct {
	pgx.Tx
	beforeCommit func(context.Context, pgx.Tx) error
}

func (tx runFileChangeCommitTx) Commit(ctx context.Context) error {
	if err := tx.beforeCommit(ctx, tx.Tx); err != nil {
		return err
	}
	return tx.Tx.Commit(ctx)
}

func runFileChangeUploadRequest(t *testing.T, ws, task string) *http.Request {
	t.Helper()
	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "r", Label: "root", Kind: "directory", Coverage: "complete"}}
	body := "synthetic directory body"
	c.Files = []rundiff.File{{ID: "f", RootID: "r", Path: "notes.txt", Change: "added", AfterBytes: int64(len(body)), BodyStatus: "available", After: &body}}
	req := testutil.WithURLParams(testutil.JSONRequest(http.MethodPost, "/api/daemon/tasks/"+task+"/file-changes", c), "taskId", task)
	// The fixture runtime's owner, as a real daemon authenticates.
	req.Header.Set("X-User-ID", testUserID)
	return req
}

// FIXME(test-integration): Both lock directions must block before either commit,
// including when the first transaction rolls back. An eventual row count alone
// cannot distinguish a fence from a lucky scheduling order.
func TestWorkspaceRunFileChangesCommitOrdering(t *testing.T) {
	if testPool == nil {
		t.Fatal("real database required")
	}
	for _, firstDelete := range []bool{false, true} {
		for _, rollback := range []bool{false, true} {
			name := "upload_first"
			if firstDelete {
				name = "delete_first"
			}
			if rollback {
				name += "_rollback"
			}
			t.Run(name, func(t *testing.T) {
				ws := dbfx.Workspace(t, "file changes ordering", uuid.NewString())
				neighbor := dbfx.Workspace(t, "file changes neighbor", uuid.NewString())
				fx := testutil.New(testPool, ws, testUserID)
				dbfx.Exec(t, "INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner')", ws, testUserID)
				runtime := fx.Runtime(t, "file changes runtime")
				agent := fx.Agent(t, "file changes agent", runtime)
				session := fx.ChatSession(t, agent)
				task := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "chat_session_id": session})
				for _, workspace := range []string{ws, neighbor} {
					dbfx.Exec(t, `INSERT INTO run_file_change(workspace_id,task_id,collection_id,manifest,bodies) VALUES ($1,gen_random_uuid(),gen_random_uuid(),'{}','{"old":"preserve on rollback"}')`, workspace)
					dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE workspace_id=$1", workspace)
				}

				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				atCommit := make(chan int, 1)
				release := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				first := *testHandler
				first.TxStarter = runFileChangeCommitStarter{beforeCommit: func(ctx context.Context, tx pgx.Tx) error {
					atCommit <- int(tx.Conn().PgConn().PID())
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					if rollback {
						return errors.New("injected commit failure")
					}
					return nil
				}}
				upload := runFileChangeUploadRequest(t, ws, task)
				deadline, _ := ctx.Deadline()
				uploadCtx, cancelUpload := context.WithDeadline(upload.Context(), deadline)
				upload = upload.WithContext(uploadCtx)
				deleteReq := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
				deleteCtx, cancelDelete := context.WithDeadline(deleteReq.Context(), deadline)
				deleteReq = deleteReq.WithContext(deleteCtx)
				var workers sync.WaitGroup
				defer func() { unblock(); cancelUpload(); cancelDelete(); workers.Wait() }()
				firstReq, secondReq := upload, deleteReq
				firstCall, secondCall := first.SaveRunFileChanges, testHandler.DeleteWorkspace
				if firstDelete {
					firstReq, secondReq = deleteReq, upload
					firstCall, secondCall = first.DeleteWorkspace, testHandler.SaveRunFileChanges
				}
				firstOut, secondOut := httptest.NewRecorder(), httptest.NewRecorder()
				firstDone, secondDone := make(chan struct{}), make(chan struct{})
				workers.Add(1)
				go func() { defer workers.Done(); defer close(firstDone); firstCall(firstOut, firstReq) }()
				var pid int
				select {
				case pid = <-atCommit:
				case <-firstDone:
					t.Fatalf("first handler never reached commit: %d %s", firstOut.Code, firstOut.Body.String())
				case <-ctx.Done():
					t.Fatal("first handler did not reach commit before deadline")
				}
				workers.Add(1)
				go func() { defer workers.Done(); defer close(secondDone); secondCall(secondOut, secondReq) }()
				if !waitForWaiterBlockedBy(t, pid, 3*time.Second) {
					t.Fatal("opposite handler did not wait for the workspace transaction")
				}
				// The deletion sweep and upload are still uncommitted here.
				if n := dbfx.Count(t, `SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND bodies='{"old":"preserve on rollback"}'::jsonb`, ws); n != 1 {
					t.Fatalf("uncommitted transaction changed visible bodies: %d", n)
				}
				unblock()
				<-firstDone
				<-secondDone
				wantFirst, wantSecond := http.StatusOK, http.StatusNoContent
				if firstDelete {
					wantFirst, wantSecond = http.StatusNoContent, http.StatusNotFound
					if rollback {
						wantSecond = http.StatusOK
					}
				}
				if rollback {
					wantFirst = http.StatusInternalServerError
				}
				if firstOut.Code != wantFirst || secondOut.Code != wantSecond {
					t.Fatalf("first=%d %s second=%d %s; want %d/%d", firstOut.Code, firstOut.Body.String(), secondOut.Code, secondOut.Body.String(), wantFirst, wantSecond)
				}
				wantWorkspace, wantRecords := 0, 0
				if firstDelete && rollback {
					wantWorkspace, wantRecords = 1, 2 // Original orphan plus resumed upload.
					if n := dbfx.Count(t, `SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND bodies='{"old":"preserve on rollback"}'::jsonb`, ws); n != 1 {
						t.Fatal("delete rollback did not restore the original orphan body")
					}
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM workspace WHERE id=$1", ws); n != wantWorkspace {
					t.Fatalf("workspace count=%d want=%d", n, wantWorkspace)
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE workspace_id=$1", ws); n != wantRecords {
					t.Fatalf("file change count=%d want=%d", n, wantRecords)
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND bodies='{"old":"preserve on rollback"}'::jsonb`, neighbor); n != 1 {
					t.Fatal("neighbor body changed")
				}
			})
		}
	}
}

// FIXME(test-integration): A failed commit must not leave a visible upload.
func TestWorkspaceRunFileChangesUploadRollback(t *testing.T) {
	if testPool == nil {
		t.Fatal("real database required")
	}
	task := seedBatchTask(t, "file changes rollback")
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", task)
	h := *testHandler
	h.TxStarter = runFileChangeCommitStarter{beforeCommit: func(context.Context, pgx.Tx) error {
		return errors.New("injected commit failure")
	}}
	testutil.Call(t, h.SaveRunFileChanges, runFileChangeUploadRequest(t, testWorkspaceID, task)).Want(http.StatusInternalServerError)
	if n := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE task_id=$1", task); n != 0 {
		t.Fatal("failed upload committed its body")
	}
	testutil.Call(t, testHandler.SaveRunFileChanges, runFileChangeUploadRequest(t, testWorkspaceID, task)).Want(http.StatusOK)
}

type runFileChangeDeleteOnRead struct {
	io.Reader
	once   sync.Once
	delete func()
}

func (r *runFileChangeDeleteOnRead) Read(p []byte) (int, error) {
	r.once.Do(r.delete)
	return r.Reader.Read(p)
}

// FIXME(test-integration): Pause at the first body read, after authorization.
// SaveRunFileChanges has already authorized and
// loaded its task. DeleteWorkspace commits, then the original upload resumes.
func TestWorkspaceRunFileChangesLateUpload(t *testing.T) {
	if testPool == nil {
		t.Fatal("real DB required")
	}
	ws := dbfx.Workspace(t, "QA delayed upload", uuid.NewString())
	fx := testutil.New(testPool, ws, testUserID)
	dbfx.Exec(t, "INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner')", ws, testUserID)
	runtime := fx.Runtime(t, "QA runtime")
	agent := fx.Agent(t, "QA agent", runtime)
	sid := fx.ChatSession(t, agent)
	task := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "chat_session_id": sid})
	dbfx.Cleanup(t, "DELETE FROM run_file_change WHERE task_id=$1", task)
	c := rundiff.Empty(uuid.NewString(), "ready", "")
	c.Roots = []rundiff.Root{{ID: "r", Label: "root", Kind: "directory", Coverage: "complete"}}
	body := "synthetic retained directory body"
	c.Files = []rundiff.File{{ID: "f", RootID: "r", Path: "notes.txt", Change: "added", AfterBytes: int64(len(body)), BodyStatus: "available", After: &body}}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	deleted := false
	reader := &runFileChangeDeleteOnRead{Reader: bytes.NewReader(payload), delete: func() {
		req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
		testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
		deleted = true
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/"+task+"/file-changes", reader)
	req = testutil.WithURLParams(req, "taskId", task)
	// The fixture runtime's owner, as a real daemon authenticates.
	req.Header.Set("X-User-ID", testUserID)
	out := httptest.NewRecorder()
	testHandler.SaveRunFileChanges(out, req)
	if !deleted {
		t.Fatalf("did not reach post-authorization barrier: HTTP %d %s", out.Code, out.Body.String())
	}
	workspaceRows := dbfx.Count(t, "SELECT count(*) FROM workspace WHERE id=$1", ws)
	taskRows := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE id=$1", task)
	retained := dbfx.Count(t, "SELECT count(*) FROM run_file_change WHERE workspace_id=$1 AND bodies<>'{}'", ws)
	t.Logf("after post-auth DeleteWorkspace + upload resume: save_status=%d workspace=%d task=%d retained_body=%d", out.Code, workspaceRows, taskRows, retained)
	if workspaceRows != 0 || taskRows != 0 {
		t.Fatal("workspace deletion did not complete")
	}
	if out.Code != http.StatusNotFound {
		t.Errorf("late upload status=%d, want 404", out.Code)
	}
	if retained != 0 {
		t.Error("late upload recreated file-change body after workspace deletion")
	}
}
