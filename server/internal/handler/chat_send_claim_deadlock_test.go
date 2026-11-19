package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// isDeadlock reports whether err carries PostgreSQL's deadlock_detected.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// waitForWaiterPIDBlockedBy is waitForWaiterBlockedBy that also returns the
// waiter's backend pid, so a test can chain "A waits on B, C waits on A".
func waitForWaiterPIDBlockedBy(t *testing.T, holderPID int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var pid int
		err := testPool.QueryRow(context.Background(), `
			SELECT pid FROM pg_stat_activity
			WHERE datname = current_database()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND wait_event IN ('transactionid', 'tuple')
			  AND $1::int = ANY(pg_blocking_pids(pid))
			LIMIT 1
		`, holderPID).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("probe pg_stat_activity for waiters blocked by pid %d: %v", holderPID, err)
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A direct chat send holds the agent row (FOR UPDATE) when CreateChatTask's
// owner fence asks for FOR KEY SHARE on the agent's runtime row. The claim
// delivery gate locks that runtime row first and then the agent row. When the
// gate held the runtime FOR UPDATE, the two waited on each other and Postgres
// killed one with 40P01 (MUL-9501). The gate now holds the runtime in a mode
// that lets the fence's KEY SHARE through.
//
// The interleaving is forced: a third transaction holds the workspace row so
// the send parks inside the fence after taking the agent lock, the gate is
// started and parks on that agent lock, and only then is the workspace
// released so the send continues to the runtime row.
func TestDirectChatSendDoesNotDeadlockWithClaimDeliveryGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, claimedSessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "claim gate deadlock: claimed")
	sendSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":                 "claim gate deadlock: send",
		"explicitly_created_at": testutil.Raw("now()"),
	})

	claimedTaskID := sendDirectChat(t, ctx, agentID, claimedSessionID, "claimed turn")
	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || claimed == nil || uuidToString(claimed.ID) != claimedTaskID {
		t.Fatalf("claim task: task=%+v err=%v, want %s", claimed, err, claimedTaskID)
	}
	runtimeSnapshot, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load claim-time runtime: %v", err)
	}
	sendSession, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sendSessionID))
	if err != nil {
		t.Fatalf("load send session: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	workspaceHolder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace holder: %v", err)
	}
	defer workspaceHolder.Rollback(context.Background())
	var holderPID int
	if err := workspaceHolder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatalf("read holder pid: %v", err)
	}
	if _, err := workspaceHolder.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, testWorkspaceID); err != nil {
		t.Fatalf("hold workspace row: %v", err)
	}

	sendDone := make(chan error, 1)
	go func() {
		_, err := testHandler.TaskService.SendDirectChatMessage(context.Background(), sendSession, agent,
			parseUUID(testUserID), "concurrent turn", nil, "member", parseUUID(testUserID))
		sendDone <- err
	}()
	sendPID := waitForWaiterPIDBlockedBy(t, holderPID, 5*time.Second)
	if sendPID == 0 {
		t.Fatal("send never reached the owner fence's workspace lock")
	}

	gateDone := make(chan error, 1)
	go func() {
		req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
		_, _, _, _, failure, err := testHandler.finalizeClaimDeliveryForTestWithRuntime(
			req, claimed, runtimeSnapshot, runtimeID, testWorkspaceID,
		)
		if err == nil && failure != nil {
			err = fmt.Errorf("delivery failure: %+v", failure)
		}
		gateDone <- err
	}()
	if !waitForWaiterBlockedBy(t, sendPID, 5*time.Second) {
		t.Fatal("claim delivery gate never queued behind the send's agent row lock")
	}

	if err := workspaceHolder.Rollback(ctx); err != nil {
		t.Fatalf("release workspace row: %v", err)
	}

	for name, done := range map[string]chan error{"send": sendDone, "claim delivery gate": gateDone} {
		select {
		case err := <-done:
			if isDeadlock(err) {
				t.Errorf("%s hit a deadlock: %v", name, err)
			} else if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
}

// Many sessions of one agent sending at once while the daemon claims and
// delivers that agent's earlier turns: every send must land its task and user
// message, and every claim must deliver (MUL-9501). Each session starts with one
// queued turn so the claimers run the delivery gate while the sends are in
// flight, not after they have all committed.
func TestDirectChatConcurrentSendsAcrossSessionsWhileDaemonClaims(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	const (
		sessions = 16
		rounds   = 10
		claimers = 8
	)
	ctx := context.Background()
	agentID, firstSessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "concurrent sends 0")
	dbfx.Exec(t, `UPDATE agent SET max_concurrent_tasks = $1 WHERE id = $2`, (rounds+1)*sessions, agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_token WHERE agent_id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
	})
	sessionIDs := []string{firstSessionID}
	for i := 1; i < sessions; i++ {
		sessionIDs = append(sessionIDs, dbfx.ChatSession(t, agentID, testutil.Cols{
			"title":                 fmt.Sprintf("concurrent sends %d", i),
			"explicitly_created_at": testutil.Raw("now()"),
		}))
	}
	chatSessions := make([]db.ChatSession, len(sessionIDs))
	for i, id := range sessionIDs {
		sendDirectChat(t, ctx, agentID, id, fmt.Sprintf("queued %d", i))
		var err error
		if chatSessions[i], err = testHandler.Queries.GetChatSession(ctx, parseUUID(id)); err != nil {
			t.Fatalf("load session %s: %v", id, err)
		}
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	start := make(chan struct{})
	sendErrs := make([]error, len(chatSessions))
	var sends sync.WaitGroup
	for i := range chatSessions {
		sends.Add(1)
		go func(i int) {
			defer sends.Done()
			<-start
			for r := 0; r < rounds && sendErrs[i] == nil; r++ {
				_, sendErrs[i] = testHandler.TaskService.SendDirectChatMessage(context.Background(), chatSessions[i], agent,
					parseUUID(testUserID), fmt.Sprintf("concurrent %d.%d", i, r), nil, "member", parseUUID(testUserID))
			}
		}(i)
	}

	sendsDone := make(chan struct{})
	claimErrs := make(chan error, claimers)
	var claims sync.WaitGroup
	for c := 0; c < claimers; c++ {
		claims.Add(1)
		go func() {
			defer claims.Done()
			<-start
			for {
				req := testutil.WithURLParams(
					newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil),
					"runtimeId", runtimeID,
				)
				w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req)
				if w.Code != http.StatusOK {
					claimErrs <- fmt.Errorf("claim returned %d: %s", w.Code, w.Body.String())
					return
				}
				select {
				case <-sendsDone:
					if strings.TrimSpace(w.Body.String()) == `{"task":null}` {
						return
					}
				default:
				}
			}
		}()
	}

	close(start)
	sends.Wait()
	close(sendsDone)
	claims.Wait()
	close(claimErrs)

	for err := range claimErrs {
		t.Errorf("daemon claim: %v", err)
	}
	for i, err := range sendErrs {
		if err != nil {
			t.Errorf("send to session %d: %v (deadlock=%v)", i, err, isDeadlock(err))
		}
	}
	for i, id := range sessionIDs {
		var tasks, userMessages int
		dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1`, id).Scan(&tasks)
		dbfx.QueryRow(t, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND role = 'user'`, id).Scan(&userMessages)
		if tasks != rounds+1 || userMessages != rounds+1 {
			t.Errorf("session %d: tasks=%d user messages=%d, want %d of each", i, tasks, userMessages, rounds+1)
		}
	}
}
