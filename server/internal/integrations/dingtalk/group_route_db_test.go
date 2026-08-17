package dingtalk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDingTalkGroupRoute_DiscoverReassignAndFenceStaleSessionDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	var migrated bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.dingtalk_group_route') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Skip("dingtalk group route table not present (database not migrated)")
	}
	observerConn, connectErr := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if connectErr != nil {
		t.Fatalf("connect PostgreSQL lock observer: %v", connectErr)
	}
	t.Cleanup(func() { _ = observerConn.Close(context.Background()) })

	const (
		workspaceID   = "d2480000-0000-4000-8000-000000000001"
		runtimeID     = "d2480000-0000-4000-8000-000000000002"
		defaultAgent  = "d2480000-0000-4000-8000-000000000003"
		routedAgent   = "d2480000-0000-4000-8000-000000000004"
		installerID   = "d2480000-0000-4000-8000-000000000005"
		installation  = "d2480000-0000-4000-8000-000000000006"
		chatSessionID = "d2480000-0000-4000-8000-000000000007"
		chatSessionB  = "d2480000-0000-4000-8000-000000000008"
		conversation  = "cid-dingtalk-multi-agent-group"
		appKey        = "dingtalk_multi_agent_group_app"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_outbound_card_message WHERE chat_session_id IN ($1, $2)`, chatSessionID, chatSessionB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id IN ($1, $2)`, chatSessionID, chatSessionB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, installerID)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed group route fixture: %v", err)
		}
	}
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, 'DingTalk route owner', 'dingtalk-route-owner@multica.test')`, installerID)
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk routes', 'dingtalk-routes-db', '')`, workspaceID)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk route runtime', 'local', 'multica_daemon')`, runtimeID, workspaceID)
	for _, agent := range []string{defaultAgent, routedAgent} {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, $3, 'local', $4)`, agent, workspaceID, "DingTalk route "+agent, runtimeID)
	}
	exec(`INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title) VALUES ($1, $2, $3, $4, 'DingTalk route A')`, chatSessionID, workspaceID, defaultAgent, installerID)
	exec(`INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title) VALUES ($1, $2, $3, $4, 'DingTalk route B')`, chatSessionB, workspaceID, routedAgent, installerID)
	exec(`INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id) VALUES ($1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5)`, installation, workspaceID, defaultAgent, appKey, installerID)

	queries := db.New(pool)
	// Revocation is a query-level fail-closed boundary, not only a Router gate:
	// a callback that reaches discovery after revoke must neither create a route
	// nor expose anything through the workspace list.
	exec(`UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, installation)
	_, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: "cid-revoked-before-discovery", ConversationTitle: "Revoked secret group",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("discover route after revoke error = %v, want no rows", err)
	}
	var revokedDiscoveryCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_group_route WHERE installation_id = $1`, installation).Scan(&revokedDiscoveryCount); err != nil || revokedDiscoveryCount != 0 {
		t.Fatalf("routes created after pre-discovery revoke = %d, err=%v", revokedDiscoveryCount, err)
	}
	revokedRoutes, err := queries.ListDingTalkGroupRoutesByWorkspace(ctx, util.MustParseUUID(workspaceID))
	if err != nil || len(revokedRoutes) != 0 {
		t.Fatalf("routes listed after pre-discovery revoke = %+v, err=%v", revokedRoutes, err)
	}
	exec(`UPDATE channel_installation SET status = 'active' WHERE id = $1`, installation)

	// Controlled revoke-wins interleaving: discovery starts from an active
	// snapshot but must block on the installation row, re-check status after the
	// revoke commits, and return no rows without persisting the group metadata.
	revokeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin controlled pre-discovery revoke: %v", err)
	}
	defer revokeTx.Rollback(context.Background())
	if _, err := revokeTx.Exec(ctx, `UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, installation); err != nil {
		t.Fatalf("lock installation for controlled revoke: %v", err)
	}
	discoveryConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire controlled discovery connection: %v", err)
	}
	defer discoveryConn.Release()
	var discoveryPID int32
	if err := discoveryConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&discoveryPID); err != nil {
		t.Fatalf("read controlled discovery backend pid: %v", err)
	}
	discoveryDone := make(chan error, 1)
	go func() {
		_, discoverErr := db.New(discoveryConn).DiscoverDingTalkGroupRoute(context.Background(), db.DiscoverDingTalkGroupRouteParams{
			InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
			ConversationID: "cid-revoke-race", ConversationTitle: "Revoke race secret group",
		})
		discoveryDone <- discoverErr
	}()
	waitForDingTalkBackendBlocked(t, observerConn, discoveryPID)
	if err := revokeTx.Commit(ctx); err != nil {
		t.Fatalf("commit controlled pre-discovery revoke: %v", err)
	}
	select {
	case discoverErr := <-discoveryDone:
		if !errors.Is(discoverErr, pgx.ErrNoRows) {
			t.Fatalf("revoke-wins discovery error = %v, want no rows", discoverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not resume after revoke committed")
	}
	discoveryConn.Release()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_group_route WHERE installation_id = $1`, installation).Scan(&revokedDiscoveryCount); err != nil || revokedDiscoveryCount != 0 {
		t.Fatalf("revoke-wins discovery persisted %d routes, err=%v", revokedDiscoveryCount, err)
	}
	exec(`UPDATE channel_installation SET status = 'active' WHERE id = $1`, installation)

	resolved, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team",
	})
	if err != nil {
		t.Fatalf("discover group: %v", err)
	}
	if resolved.AgentID != util.MustParseUUID(defaultAgent) || !resolved.AgentActive {
		t.Fatalf("new group route = %+v, want active installation default", resolved)
	}
	routes, err := queries.ListDingTalkGroupRoutesByWorkspace(ctx, util.MustParseUUID(workspaceID))
	if err != nil || len(routes) != 1 {
		t.Fatalf("list discovered routes = %d, err=%v", len(routes), err)
	}

	// Upgrade regression: pre-routing DingTalk group bindings have no agent_id
	// stamp. They must survive when the effective route still equals the
	// installation's default agent.
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'group', '{}'::jsonb)`, chatSessionID, installation, conversation)
	exec(`INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id) VALUES ($1, 'dingtalk', $2, 'old-agent-reply')`, chatSessionID, conversation)
	preserved, err := queries.DeleteDingTalkStaleGroupChatBinding(ctx, db.DeleteDingTalkStaleGroupChatBindingParams{
		InstallationID: util.MustParseUUID(installation), ConversationID: conversation, AgentID: util.MustParseUUID(defaultAgent),
	})
	if err != nil || preserved != 0 {
		t.Fatalf("legacy binding guard cleared %d bindings, err=%v; want preserve", preserved, err)
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); err != nil {
		t.Fatalf("legacy default-agent binding was not preserved: %v", err)
	}

	updated, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
	})
	if err != nil {
		t.Fatalf("reassign group: %v", err)
	}
	if updated.AgentID != util.MustParseUUID(routedAgent) {
		t.Fatalf("updated group agent = %s", util.UUIDToString(updated.AgentID))
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old group binding survived reassignment: %v", err)
	}

	// Simulate an old-agent inbound turn that resolved just before the update
	// and created its binding just after it. The next routed turn must retire it.
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'group', jsonb_build_object('agent_id', $4::text))`, chatSessionID, installation, conversation, defaultAgent)
	cleared, err := queries.DeleteDingTalkStaleGroupChatBinding(ctx, db.DeleteDingTalkStaleGroupChatBindingParams{
		InstallationID: util.MustParseUUID(installation), ConversationID: conversation, AgentID: util.MustParseUUID(routedAgent),
	})
	if err != nil {
		t.Fatalf("fence stale group binding: %v", err)
	}
	if cleared != 1 {
		t.Fatalf("cleared stale group bindings = %d, want 1", cleared)
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("late old-agent binding survived guard: %v", err)
	}

	resolved, err = queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team renamed",
	})
	if err != nil || resolved.AgentID != util.MustParseUUID(routedAgent) || !resolved.AgentActive {
		t.Fatalf("re-resolve group route = %+v, err=%v", resolved, err)
	}

	// Product decision: every route change starts a fresh session, including
	// A -> B -> A. Returning to A must remove B's active binding rather than
	// resuming A's earlier binding/transcript.
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'group', jsonb_build_object('agent_id', $4::text))`, chatSessionB, installation, conversation, routedAgent)
	returnedToDefault, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(defaultAgent),
	})
	if err != nil || returnedToDefault.AgentID != util.MustParseUUID(defaultAgent) {
		t.Fatalf("return route B -> A = %+v, err=%v", returnedToDefault, err)
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("B binding survived route return to A: %v", err)
	}
	routes, err = queries.ListDingTalkGroupRoutesByWorkspace(ctx, util.MustParseUUID(workspaceID))
	if err != nil || len(routes) != 1 || routes[0].ID != returnedToDefault.ID {
		t.Fatalf("A -> B -> A changed stable route identity: routes=%+v err=%v", routes, err)
	}

	// PATCH-wins cutover: an append carrying the pre-PATCH revision cannot
	// acquire the durable fence, so the Router can refresh and retry that same
	// already-ACKed message on the new route without writing to A.
	staleRevision := returnedToDefault.Revision
	assignmentWon, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
	})
	if err != nil || assignmentWon.Revision <= staleRevision {
		t.Fatalf("assignment-wins fence update = %+v, err=%v", assignmentWon, err)
	}
	staleAppendTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin stale append fence: %v", err)
	}
	_, staleFenceErr := queries.WithTx(staleAppendTx).LockDingTalkGroupRouteForAppend(ctx, db.LockDingTalkGroupRouteForAppendParams{
		InstallationID: util.MustParseUUID(installation), ConversationID: conversation,
		AgentID: util.MustParseUUID(defaultAgent), RouteRevision: staleRevision,
	})
	_ = staleAppendTx.Rollback(ctx)
	if !errors.Is(staleFenceErr, pgx.ErrNoRows) {
		t.Fatalf("stale append fence error = %v, want no rows", staleFenceErr)
	}
	returnedForAppendFence, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(defaultAgent),
	})
	if err != nil {
		t.Fatalf("restore A before append-wins fence: %v", err)
	}

	// Append-wins cutover: exercise the real message transaction. It locks the
	// chat session first, then this route revision, and keeps both locks through
	// the durable message commit. PATCH must be visibly blocked in PostgreSQL.
	appendService := engine.NewChatSession(queries, pool, TypeDingTalk, engine.SessionTitles{Group: "DingTalk group"})
	appendClaim, err := queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: util.MustParseUUID(installation), MessageID: "msg-append-before-cutover",
	})
	if err != nil {
		t.Fatalf("claim append-wins message: %v", err)
	}
	appendFenceAcquired := make(chan struct{})
	allowAppendCommit := make(chan struct{})
	appendDone := make(chan error, 1)
	go func() {
		_, appendErr := appendService.AppendUserMessage(context.Background(), engine.AppendInput{
			SessionID: util.MustParseUUID(chatSessionID), InstallationID: util.MustParseUUID(installation),
			Body: "append before route cutover", MessageID: "msg-append-before-cutover", ClaimToken: appendClaim.ClaimToken,
			BeforeWrite: func(lockCtx context.Context, tx pgx.Tx) error {
				if _, lockErr := queries.WithTx(tx).LockDingTalkGroupRouteForAppend(lockCtx, db.LockDingTalkGroupRouteForAppendParams{
					InstallationID: util.MustParseUUID(installation), ConversationID: conversation,
					AgentID: util.MustParseUUID(defaultAgent), RouteRevision: returnedForAppendFence.Revision,
				}); lockErr != nil {
					return lockErr
				}
				close(appendFenceAcquired)
				select {
				case <-allowAppendCommit:
					return nil
				case <-lockCtx.Done():
					return lockCtx.Err()
				}
			},
		})
		appendDone <- appendErr
	}()
	select {
	case <-appendFenceAcquired:
	case appendErr := <-appendDone:
		t.Fatalf("append failed before acquiring route fence: %v", appendErr)
	case <-time.After(5 * time.Second):
		t.Fatal("append did not acquire route fence")
	}
	reassignConn, err := pool.Acquire(ctx)
	if err != nil {
		close(allowAppendCommit)
		t.Fatalf("acquire controlled PATCH connection: %v", err)
	}
	defer reassignConn.Release()
	var reassignPID int32
	if err := reassignConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&reassignPID); err != nil {
		close(allowAppendCommit)
		t.Fatalf("read controlled PATCH backend pid: %v", err)
	}
	appendWinsReassign := make(chan error, 1)
	go func() {
		_, reassignErr := db.New(reassignConn).ReassignDingTalkGroupRoute(context.Background(), db.ReassignDingTalkGroupRouteParams{
			ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
		})
		appendWinsReassign <- reassignErr
	}()
	waitForDingTalkBackendBlocked(t, observerConn, reassignPID)
	close(allowAppendCommit)
	select {
	case appendErr := <-appendDone:
		if appendErr != nil {
			t.Fatalf("commit append-wins message: %v", appendErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("append did not commit after test released its fence")
	}
	select {
	case reassignErr := <-appendWinsReassign:
		if reassignErr != nil {
			t.Fatalf("PATCH after append fence: %v", reassignErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not resume after append transaction committed")
	}
	var appendedMessages int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND content = 'append before route cutover'`, chatSessionID).Scan(&appendedMessages); err != nil || appendedMessages != 1 {
		t.Fatalf("append-wins durable messages = %d, err=%v, want 1", appendedMessages, err)
	}
	var appendDedupProcessed bool
	if err := pool.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM channel_inbound_message_dedup WHERE installation_id = $1 AND message_id = 'msg-append-before-cutover'`, installation).Scan(&appendDedupProcessed); err != nil || !appendDedupProcessed {
		t.Fatalf("append-wins dedup processed = %t, err=%v", appendDedupProcessed, err)
	}
	routeForWorkspaceFence, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(defaultAgent),
	})
	if err != nil {
		t.Fatalf("restore A after append-wins fence: %v", err)
	}

	// Workspace teardown takes workspace -> chat_session -> route. The real
	// append path must take chat_session before its route fence, so teardown can
	// wait at the session without already holding the route append needs.
	workspaceClaim, err := queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: util.MustParseUUID(installation), MessageID: "msg-append-during-workspace-delete",
	})
	if err != nil {
		t.Fatalf("claim workspace-delete append: %v", err)
	}
	workspaceFenceAcquired := make(chan struct{})
	allowWorkspaceAppend := make(chan struct{})
	workspaceAppendDone := make(chan error, 1)
	go func() {
		_, appendErr := appendService.AppendUserMessage(context.Background(), engine.AppendInput{
			SessionID: util.MustParseUUID(chatSessionID), InstallationID: util.MustParseUUID(installation),
			Body: "append during workspace delete", MessageID: "msg-append-during-workspace-delete", ClaimToken: workspaceClaim.ClaimToken,
			BeforeWrite: func(lockCtx context.Context, tx pgx.Tx) error {
				if _, lockErr := queries.WithTx(tx).LockDingTalkGroupRouteForAppend(lockCtx, db.LockDingTalkGroupRouteForAppendParams{
					InstallationID: util.MustParseUUID(installation), ConversationID: conversation,
					AgentID: util.MustParseUUID(defaultAgent), RouteRevision: routeForWorkspaceFence.Revision,
				}); lockErr != nil {
					return lockErr
				}
				close(workspaceFenceAcquired)
				select {
				case <-allowWorkspaceAppend:
					return nil
				case <-lockCtx.Done():
					return lockCtx.Err()
				}
			},
		})
		workspaceAppendDone <- appendErr
	}()
	select {
	case <-workspaceFenceAcquired:
	case appendErr := <-workspaceAppendDone:
		t.Fatalf("workspace-delete append failed before fence: %v", appendErr)
	case <-time.After(5 * time.Second):
		t.Fatal("workspace-delete append did not acquire its route fence")
	}
	deleteConn, err := pool.Acquire(ctx)
	if err != nil {
		close(allowWorkspaceAppend)
		t.Fatalf("acquire controlled workspace-delete connection: %v", err)
	}
	defer deleteConn.Release()
	var deletePID int32
	if err := deleteConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletePID); err != nil {
		close(allowWorkspaceAppend)
		t.Fatalf("read workspace-delete backend pid: %v", err)
	}
	workspaceDeleteDone := make(chan error, 1)
	go func() {
		deleteErr := func() error {
			deleteTx, beginErr := deleteConn.Begin(context.Background())
			if beginErr != nil {
				return beginErr
			}
			defer deleteTx.Rollback(context.Background())
			deleteQueries := db.New(deleteTx)
			if _, lockErr := deleteQueries.LockWorkspaceForDelete(context.Background(), util.MustParseUUID(workspaceID)); lockErr != nil {
				return lockErr
			}
			if _, lockErr := deleteQueries.LockChatSessionsByWorkspace(context.Background(), util.MustParseUUID(workspaceID)); lockErr != nil {
				return lockErr
			}
			_, deleteRouteErr := deleteTx.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE workspace_id = $1`, workspaceID)
			return deleteRouteErr
		}()
		workspaceDeleteDone <- deleteErr
	}()
	waitForDingTalkBackendBlocked(t, observerConn, deletePID)
	close(allowWorkspaceAppend)
	select {
	case appendErr := <-workspaceAppendDone:
		if appendErr != nil {
			t.Fatalf("append during workspace deletion: %v", appendErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("append remained blocked during workspace deletion")
	}
	select {
	case deleteErr := <-workspaceDeleteDone:
		if deleteErr != nil {
			t.Fatalf("workspace deletion lock protocol after append: %v", deleteErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workspace deletion did not resume after append committed")
	}
	deleteConn.Release()
	var workspaceAppendProcessed bool
	if err := pool.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM channel_inbound_message_dedup WHERE installation_id = $1 AND message_id = 'msg-append-during-workspace-delete'`, installation).Scan(&workspaceAppendProcessed); err != nil || !workspaceAppendProcessed {
		t.Fatalf("workspace-delete append dedup processed = %t, err=%v", workspaceAppendProcessed, err)
	}

	// Archive/restore semantics preserve the selected route. While archived,
	// runtime validation rejects work before session creation; restoring the
	// agent makes the same route active again without silent reassignment.
	exec(`UPDATE agent SET archived_at = now() WHERE id = $1`, defaultAgent)
	archivedRoute, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team archived",
	})
	if err != nil || archivedRoute.AgentID != util.MustParseUUID(defaultAgent) || archivedRoute.AgentActive {
		t.Fatalf("archived route lifecycle = %+v, err=%v", archivedRoute, err)
	}
	matches, err := queries.DingTalkGroupRouteMatchesAgent(ctx, db.DingTalkGroupRouteMatchesAgentParams{
		InstallationID: util.MustParseUUID(installation), ConversationID: conversation,
		AgentID: util.MustParseUUID(defaultAgent), RouteRevision: archivedRoute.Revision,
	})
	if err != nil || matches {
		t.Fatalf("archived route matched active session guard: matches=%t err=%v", matches, err)
	}
	exec(`UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, defaultAgent)
	restoredRoute, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team restored",
	})
	if err != nil || restoredRoute.AgentID != util.MustParseUUID(defaultAgent) || !restoredRoute.AgentActive {
		t.Fatalf("restored route lifecycle = %+v, err=%v", restoredRoute, err)
	}

	// Controlled archive-wins interleaving: Reassign takes a shared lock on the
	// target agent and must re-check archived_at after a concurrent archiver's
	// row lock commits. It cannot install a target from a stale active snapshot.
	archiveTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin controlled archive: %v", err)
	}
	if _, err := archiveTx.Exec(ctx, `SELECT id FROM agent WHERE id = $1 FOR UPDATE`, routedAgent); err != nil {
		_ = archiveTx.Rollback(ctx)
		t.Fatalf("lock route target for archive: %v", err)
	}
	reassignDone := make(chan error, 1)
	go func() {
		_, reassignErr := db.New(reassignConn).ReassignDingTalkGroupRoute(context.Background(), db.ReassignDingTalkGroupRouteParams{
			ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
		})
		reassignDone <- reassignErr
	}()
	waitForDingTalkBackendBlocked(t, observerConn, reassignPID)
	if _, err := archiveTx.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, routedAgent); err != nil {
		_ = archiveTx.Rollback(ctx)
		t.Fatalf("archive controlled route target: %v", err)
	}
	if err := archiveTx.Commit(ctx); err != nil {
		t.Fatalf("commit controlled archive: %v", err)
	}
	select {
	case reassignErr := <-reassignDone:
		if !errors.Is(reassignErr, pgx.ErrNoRows) {
			t.Fatalf("archive-wins reassignment error = %v, want no rows", reassignErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reassignment did not resume after archive commit")
	}
	reassignConn.Release()

	// Assignment-wins ordering is also defined: a later archive preserves the
	// selected B route, inbound validation reports it unavailable, and restore
	// reactivates that same route.
	exec(`UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, routedAgent)
	if _, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
	}); err != nil {
		t.Fatalf("assignment-wins route update: %v", err)
	}
	exec(`UPDATE agent SET archived_at = now() WHERE id = $1`, routedAgent)
	assignmentThenArchive, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team B archived",
	})
	if err != nil || assignmentThenArchive.AgentID != util.MustParseUUID(routedAgent) || assignmentThenArchive.AgentActive {
		t.Fatalf("assignment-then-archive lifecycle = %+v, err=%v", assignmentThenArchive, err)
	}
	exec(`UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, routedAgent)
	restoredAssignedRoute, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID: util.MustParseUUID(installation), WorkspaceID: util.MustParseUUID(workspaceID),
		ConversationID: conversation, ConversationTitle: "Platform team B restored",
	})
	if err != nil || restoredAssignedRoute.AgentID != util.MustParseUUID(routedAgent) || !restoredAssignedRoute.AgentActive {
		t.Fatalf("restored assigned route = %+v, err=%v", restoredAssignedRoute, err)
	}
}

func waitForDingTalkBackendBlocked(t *testing.T, observer *pgx.Conn, pid int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := observer.QueryRow(context.Background(), `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatalf("inspect PostgreSQL lock wait for backend %d: %v", pid, err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d never entered a PostgreSQL lock wait", pid)
}
