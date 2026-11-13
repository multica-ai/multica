package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type changingMembership struct{ revoked atomic.Bool }

func (m *changingMembership) IsMember(context.Context, string, string) (bool, error) {
	return !m.revoked.Load(), nil
}

func (m *changingMembership) Members(ctx context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
	return membershipFunc(m.IsMember).Members(ctx, identities)
}

// A missed revocation signal must not let a live socket receive new content.
func TestMembershipRevocationWithoutRelay(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	mc := &changingMembership{}
	hub.SetMembershipChecker(mc)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleWebSocket(hub, mc, nil, nil, w, r)
	}))
	defer server.Close()
	conn := connectWS(t, server)
	defer conn.Close()
	waitFor(t, "workspace subscription", func() bool { return hub.HasLocalSubscribers(ScopeWorkspace, testWorkspaceID) })
	mc.revoked.Store(true)
	hub.BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"comment:created","payload":{"body":"after-revocation"}}`))
	wantMembershipClosed(t, conn)
}

type membershipFunc func(context.Context, string, string) (bool, error)

func (f membershipFunc) IsMember(ctx context.Context, userID, workspaceID string) (bool, error) {
	return f(ctx, userID, workspaceID)
}

func (f membershipFunc) Members(ctx context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
	members := make(map[MembershipIdentity]bool, len(identities))
	for _, identity := range identities {
		member, err := f(ctx, identity.UserID, identity.WorkspaceID)
		if err != nil {
			return nil, err
		}
		members[identity] = member
	}
	return members, nil
}

func membershipServer(t *testing.T, hub *Hub, mc MembershipChecker) *httptest.Server {
	t.Helper()
	hub.SetMembershipChecker(mc)
	go hub.Run()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleWebSocket(hub, mc, nil, nil, w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func membershipConn(t *testing.T, server *httptest.Server, userID, workspaceID string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/?workspace_id="+workspaceID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.WriteJSON(map[string]any{"type": "auth", "payload": map[string]string{"token": makeTestTokenForUser(t, userID, "")}}); err != nil {
		t.Fatal(err)
	}
	wantMembershipFrame(t, conn, "auth_ack")
	// A ping response goes through the registered client's writer.
	if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	wantMembershipFrame(t, conn, "pong")
	return conn
}

func wantMembershipFrame(t *testing.T, conn *websocket.Conn, typ string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	var frame struct{ Type string }
	if err != nil || json.Unmarshal(raw, &frame) != nil || frame.Type != typ {
		t.Fatalf("want %s, got %s, err=%v", typ, raw, err)
	}
}

func wantMembershipClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatalf("revoked socket remained open: %v", err)
			}
			return
		}
		var frame struct{ Type string }
		if json.Unmarshal(raw, &frame) != nil || frame.Type != "member:removed" {
			t.Fatalf("revoked socket received content: %s", raw)
		}
	}
}

func TestMembershipRemovalAcrossHubsAndScopes(t *testing.T) {
	var revoked atomic.Bool
	mc := membershipFunc(func(_ context.Context, userID, workspaceID string) (bool, error) {
		return !(revoked.Load() && userID == testUserID && workspaceID == testWorkspaceID), nil
	})
	local, remote := NewHub(), NewHub()
	localServer := membershipServer(t, local, mc)
	remoteServer := membershipServer(t, remote, mc)
	oldLocal := membershipConn(t, localServer, testUserID, testWorkspaceID)
	oldRemote := membershipConn(t, remoteServer, testUserID, testWorkspaceID)
	secondRemote := membershipConn(t, remoteServer, testUserID, testWorkspaceID)
	current := membershipConn(t, remoteServer, "current-user", testWorkspaceID)
	otherWS := membershipConn(t, remoteServer, testUserID, "other-workspace")
	for _, scope := range []string{ScopeTask, ScopeChat} {
		if err := oldRemote.WriteJSON(map[string]any{"type": "subscribe", "payload": map[string]string{"scope": scope, "id": "resource-id"}}); err != nil {
			t.Fatal(err)
		}
		wantMembershipFrame(t, oldRemote, "subscribe_ack")
	}
	revoked.Store(true)
	local.DisconnectMember(testWorkspaceID, testUserID, "member-id")
	removed := `{"type":"member:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`
	// Exercise the exact common receiver used by legacy, sharded and mirrored
	// relays, including duplicate processing on another node.
	for i := 0; i < 2; i++ {
		deliverEnvelope(remote, nil, nil, envelope{Scope: ScopeWorkspace, ScopeID: testWorkspaceID, PayloadJSON: removed, EventID: "removal"})
	}
	for _, conn := range []*websocket.Conn{oldLocal, oldRemote, secondRemote} {
		wantMembershipClosed(t, conn)
	}
	for _, scope := range []string{ScopeTask, ScopeChat} {
		if remote.HasLocalSubscribers(scope, "resource-id") {
			t.Fatalf("revocation left %s subscription", scope)
		}
	}
	wantMembershipFrame(t, current, "member:removed")
	remote.BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"comment:created"}`))
	remote.BroadcastToWorkspace("other-workspace", []byte(`{"type":"issue:updated"}`))
	wantMembershipFrame(t, current, "comment:created")
	wantMembershipFrame(t, otherWS, "issue:updated")
}

type failedMembershipPublisher struct{}

func (failedMembershipPublisher) PublishWithID(string, string, string, []byte, string) error {
	return errors.New("relay unavailable")
}

func TestMembershipRelayFailureAndLookupError(t *testing.T) {
	for _, lookupError := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "database unavailable"}[lookupError], func(t *testing.T) {
			var failed atomic.Bool
			mc := membershipFunc(func(context.Context, string, string) (bool, error) {
				if !failed.Load() {
					return true, nil
				}
				if lookupError {
					return true, errors.New("database unavailable")
				}
				return false, nil
			})
			hub := NewHub()
			conn := membershipConn(t, membershipServer(t, hub, mc), testUserID, testWorkspaceID)
			failed.Store(true)
			// The remote node processes a new local event while the removal
			// signal is missing and Redis publication also fails.
			NewDualWriteBroadcaster(hub, failedMembershipPublisher{}).BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"issue:updated"}`))
			wantMembershipClosed(t, conn)
		})
	}
}

func TestMembershipRemovalReplayAfterRejoin(t *testing.T) {
	hub := NewHub()
	mc := &mockMembershipChecker{}
	conn := membershipConn(t, membershipServer(t, hub, mc), testUserID, testWorkspaceID)
	removed := `{"type":"member:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`
	deliverEnvelope(hub, nil, nil, envelope{Scope: ScopeWorkspace, ScopeID: testWorkspaceID, PayloadJSON: removed})
	waitFor(t, "stale removal to be checked", func() bool { return hub.memberRemovalPending.Load() == 0 })
	hub.BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"issue:updated"}`))
	// Neither a hard close nor a stale frontend removal notification is valid
	// when the primary database says the user has rejoined.
	wantMembershipFrame(t, conn, "issue:updated")
}

func TestMembershipReplayCannotRestoreRevokedAccess(t *testing.T) {
	hub := NewHub()
	mc := &changingMembership{}
	conn := membershipConn(t, membershipServer(t, hub, mc), testUserID, testWorkspaceID)
	mc.revoked.Store(true)
	deliverEnvelope(hub, nil, nil, envelope{
		Scope: ScopeWorkspace, ScopeID: testWorkspaceID,
		PayloadJSON: `{"type":"comment:created","payload":{"body":"retained-content"}}`,
	})
	wantMembershipClosed(t, conn)
}

func TestMembershipRegistrationDoesNotQueryDatabase(t *testing.T) {
	hub := NewHub()
	var queries atomic.Int32
	checker := membershipFunc(func(context.Context, string, string) (bool, error) {
		queries.Add(1)
		return false, errors.New("database unavailable")
	})
	hub.SetMembershipChecker(checker)
	go hub.Run()
	clients := make([]*Client, 20)
	for i := range clients {
		clients[i] = &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
		hub.register <- clients[i]
		t.Cleanup(func() { hub.removeClient(clients[i]) })
	}
	// A later event is a barrier for all registrations in the single loop.
	hub.unregister <- &Client{}
	if queries.Load() != 0 {
		t.Fatalf("registration issued %d database queries in the Hub event loop", queries.Load())
	}
	if totalClients(hub) != len(clients) {
		t.Fatal("database availability affected in-memory registration")
	}
	hub.unregister <- clients[0]
	hub.Broadcast([]byte(`{"type":"hub-progress"}`))
	if _, open := <-clients[0].send; open {
		t.Fatal("event loop did not clean up the disconnected client")
	}
	for _, client := range clients[1:] {
		select {
		case frame := <-client.send:
			if string(frame) != `{"type":"hub-progress"}` {
				t.Fatalf("unexpected broadcast: %s", frame)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("event loop did not deliver the global broadcast")
		}
	}
}

func TestMembershipStaleHandshakeCannotReceiveContent(t *testing.T) {
	hub := NewHub()
	var checks atomic.Int32
	mc := membershipFunc(func(context.Context, string, string) (bool, error) {
		// The handshake succeeded before the simulated revocation; all later
		// queries see the committed absence.
		return checks.Add(1) == 1, nil
	})
	server := membershipServer(t, hub, mc)
	conn := connectWS(t, server)
	defer conn.Close()
	waitFor(t, "stale handshake registration", func() bool { return totalClients(hub) == 1 })
	if checks.Load() != 1 {
		t.Fatal("registration rechecked membership instead of leaving it to the writer")
	}
	hub.BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"comment:created","payload":{"body":"after-revocation"}}`))
	wantMembershipClosed(t, conn)
	waitFor(t, "revoked connection cleanup", func() bool { return totalClients(hub) == 0 })
	if totalClients(hub) != 0 || hub.HasLocalSubscribers(ScopeWorkspace, testWorkspaceID) {
		t.Fatal("stale handshake left a registered connection")
	}
}

func TestMembershipRegistrationAfterRemovalCannotReceiveContent(t *testing.T) {
	conn, peer := membershipSocketPair(t)
	hub := NewHub()
	checker := membershipFunc(func(context.Context, string, string) (bool, error) {
		return false, nil
	})
	hub.SetMembershipChecker(checker)
	// The handshake passed earlier, but registration reached this node after
	// revocation already took its connection snapshot and completed.
	hub.DisconnectMember(testWorkspaceID, testUserID, "member-id")
	client := &Client{hub: hub, conn: conn, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	hub.registerClient(client)
	if totalClients(hub) != 1 {
		t.Fatal("connection was not registered in memory")
	}
	if client.writeBatch([]byte(`{"type":"after-revocation"}`)) {
		t.Fatal("late registration received content after revocation")
	}
	wantMembershipClosed(t, peer)
	if totalClients(hub) != 0 || hub.HasLocalSubscribers(ScopeWorkspace, testWorkspaceID) || !client.stopped.Load() {
		t.Fatal("write-time denial left a registered connection")
	}
}

func TestMembershipRegistrationProgressDuringRemovalLookup(t *testing.T) {
	hub := NewHub()
	entered, release := make(chan struct{}), make(chan struct{})
	var queries atomic.Int32
	hub.SetMembershipChecker(membershipFunc(func(context.Context, string, string) (bool, error) {
		if queries.Add(1) == 1 {
			close(entered)
		}
		<-release
		return false, nil
	}))
	old := &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	hub.registerClient(old)
	go hub.Run()
	removed := make(chan struct{})
	go func() { hub.DisconnectMember(testWorkspaceID, testUserID, "member-id"); close(removed) }()
	<-entered
	clients := make([]*Client, 20)
	progress := make(chan struct{})
	go func() {
		for i := range clients {
			clients[i] = &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
			hub.register <- clients[i]
		}
		hub.unregister <- clients[0]
		hub.Broadcast([]byte(`{"type":"hub-progress"}`))
		close(progress)
	}()
	t.Cleanup(func() {
		close(release)
		<-removed
		<-progress
		for _, client := range clients {
			hub.removeClient(client)
		}
	})
	// The lookup remains blocked until cleanup; completion cannot depend on
	// its latency or on database availability.
	select {
	case <-progress:
	case <-time.After(2 * time.Second):
		t.Fatal("removal lookup blocked registration, cleanup or global broadcast")
	}
	if queries.Load() != 1 {
		t.Fatal("registrations issued extra membership lookups")
	}
	if _, open := <-clients[0].send; open {
		t.Fatal("disconnected client was not cleaned up while the lookup was blocked")
	}
	for _, client := range clients[1:] {
		select {
		case frame := <-client.send:
			if string(frame) != `{"type":"hub-progress"}` {
				t.Fatalf("unexpected broadcast: %s", frame)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("broadcast did not progress while the lookup was blocked")
		}
	}
}

func TestMembershipRemovalLookupDoesNotAffectLaterRejoin(t *testing.T) {
	hub := NewHub()
	old := &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	other := &Client{hub: hub, userID: "current-user", workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	hub.registerClient(old)
	hub.registerClient(other)
	t.Cleanup(func() { hub.removeClient(other) })
	entered, release := make(chan struct{}), make(chan struct{})
	var rejoined atomic.Bool
	hub.SetMembershipChecker(membershipFunc(func(context.Context, string, string) (bool, error) {
		member := rejoined.Load()
		close(entered)
		<-release
		return member, nil
	}))
	removed := []byte(`{"type":"member:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`)
	done := make(chan struct{})
	go func() { hub.BroadcastToWorkspace(testWorkspaceID, removed); close(done) }()
	<-entered
	// The database now has a fresh membership, while the removal query still
	// holds the old absence snapshot. Its result must apply only to old.
	rejoined.Store(true)
	current := &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	hub.registerClient(current)
	t.Cleanup(func() { hub.removeClient(current) })
	close(release)
	<-done
	waitFor(t, "removal lookup and fanout", func() bool { return hub.memberRemovalPending.Load() == 0 })
	if !old.stopped.Load() || current.stopped.Load() || totalClients(hub) != 2 {
		t.Fatal("removal result affected a connection registered after its snapshot")
	}
	select {
	case frame := <-other.send:
		if string(frame) != string(removed) {
			t.Fatalf("remaining member missed removal notification: %s", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remaining member missed removal notification")
	}
	select {
	case frame := <-current.send:
		t.Fatalf("rejoined member received an old frontend removal notification: %s", frame)
	default:
	}
	hub.BroadcastToWorkspace(testWorkspaceID, []byte(`{"type":"issue:updated"}`))
	select {
	case frame := <-current.send:
		if string(frame) != `{"type":"issue:updated"}` {
			t.Fatalf("rejoined connection could not receive new updates: %s", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rejoined connection could not receive new updates")
	}
}

func TestMembershipRemovalCandidateFilter(t *testing.T) {
	for _, tt := range []struct {
		name     string
		raw      string
		excluded string
		removal  bool
	}{
		{"ordinary event", `{"type":"issue:updated"}`, "", false},
		{"marker in content", `{"type":"comment:created","payload":{"body":"member:removed"}}`, "", false},
		{"removal", `{"type":"member:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`, testUserID, true},
		{"escaped type", `{"type":"member:\u0072emoved","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`, testUserID, true},
		{"escaped separator", `{"type":"member\u003aremoved","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`, testUserID, true},
		{"escaped prefix", `{"type":"m\u0065mber:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`, testUserID, true},
		{"fully escaped prefix", `{"type":"\u006d\u0065\u006d\u0062\u0065\u0072:removed","payload":{"workspace_id":"test-workspace","user_id":"test-user"}}`, testUserID, true},
		{"wrong workspace", `{"type":"member:removed","payload":{"workspace_id":"other-workspace","user_id":"test-user"}}`, "", true},
		{"missing user", `{"type":"member:removed","payload":{"workspace_id":"test-workspace"}}`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			excluded, removal := parseMemberRemoval(testWorkspaceID, []byte(tt.raw))
			if excluded != tt.excluded || removal != tt.removal {
				t.Fatalf("got target=%q removal=%v, want %q %v", excluded, removal, tt.excluded, tt.removal)
			}
		})
	}
}

func TestMembershipRemovalPrefilterSkipsEscapedCommentContent(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"type":    "comment:created",
		"payload": map[string]string{"body": "Markdown: `<div>x > y && y < z</div>`\u2028\u2029"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if memberRemovalCandidate(raw) {
		t.Fatalf("HTML and line-separator escapes made an ordinary comment a removal candidate: %s", raw)
	}
}

func TestMembershipRegistrationDoesNotRestoreStoppedClient(t *testing.T) {
	hub := NewHub()
	client := &Client{hub: hub, userID: testUserID, workspaceID: testWorkspaceID, send: make(chan []byte, 2)}
	hub.stopClient(client, nil, time.Time{})
	hub.registerClient(client)
	if totalClients(hub) != 0 || hub.HasLocalSubscribers(ScopeWorkspace, testWorkspaceID) {
		t.Fatal("registration restored an already stopped connection")
	}
}

// A real socket pair lets tests control the queue and lookup without timing
// assumptions about a concurrently running writePump.
func membershipSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(server.Close)
	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := <-accepted
	t.Cleanup(func() { conn.Close(); peer.Close() })
	return conn, peer
}

func TestMembershipWriteBatchDoesNotCacheAuthorization(t *testing.T) {
	conn, peer := membershipSocketPair(t)
	hub := NewHub()
	var revoked atomic.Bool
	var lookups atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	mc := membershipFunc(func(context.Context, string, string) (bool, error) {
		member := !revoked.Load()
		if lookups.Add(1) == 1 {
			close(entered)
			<-release
		}
		return member, nil
	})
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 8), userID: testUserID, workspaceID: testWorkspaceID}
	hub.SetMembershipChecker(mc)
	for i := 0; i < 3; i++ {
		client.send <- []byte(`{"type":"before-revocation"}`)
	}
	done := make(chan bool, 1)
	go func() { done <- client.writeBatch(<-client.send) }()
	<-entered
	revoked.Store(true)
	client.send <- []byte(`{"type":"after-revocation"}`)
	close(release)
	if !<-done {
		t.Fatal("previously authorized batch failed")
	}
	for i := 0; i < 3; i++ {
		wantMembershipFrame(t, peer, "before-revocation")
	}
	if lookups.Load() != 1 {
		t.Fatal("queued frames were checked individually instead of as a batch")
	}
	if client.writeBatch(<-client.send) {
		t.Fatal("new content reused the previous batch's authorization")
	}
	wantMembershipClosed(t, peer)
}

func TestMembershipHardCloseDropsCapturedAndQueuedFrames(t *testing.T) {
	conn, peer := membershipSocketPair(t)
	hub := NewHub()
	var revoked atomic.Bool
	hub.SetMembershipChecker(membershipFunc(func(context.Context, string, string) (bool, error) { return !revoked.Load(), nil }))
	entered, release := make(chan struct{}), make(chan struct{})
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 4), userID: testUserID, workspaceID: testWorkspaceID}
	batchChecker := membershipFunc(func(context.Context, string, string) (bool, error) {
		close(entered)
		<-release
		return true, nil
	})
	hub.SetMembershipChecker(membershipBatchOverride{
		membershipFunc: membershipFunc(func(context.Context, string, string) (bool, error) { return !revoked.Load(), nil }),
		batch:          batchChecker.Members,
	})
	hub.clients[client] = true
	hub.subscribe(client, ScopeWorkspace, testWorkspaceID)
	done := make(chan bool, 1)
	go func() { done <- client.writeBatch([]byte(`{"type":"captured-secret"}`)) }()
	<-entered
	client.send <- []byte(`{"type":"queued-secret"}`)
	revoked.Store(true)
	hub.DisconnectMember(testWorkspaceID, testUserID, "member-id")
	close(release)
	if <-done {
		t.Fatal("stopped writer continued after its membership lookup")
	}
	wantMembershipClosed(t, peer)
	if _, open := <-client.send; open {
		t.Fatal("revoked send queue remained open")
	}
}

type membershipBatchOverride struct {
	membershipFunc
	batch func(context.Context, []MembershipIdentity) (map[MembershipIdentity]bool, error)
}

func (m membershipBatchOverride) Members(ctx context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
	return m.batch(ctx, identities)
}

func TestMembershipWriteCohortDoesNotJoinInflightLookup(t *testing.T) {
	hub := NewHub()
	var revoked atomic.Bool
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	mc := membershipBatchOverride{
		membershipFunc: membershipFunc(func(context.Context, string, string) (bool, error) { return !revoked.Load(), nil }),
		batch: func(_ context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
			member := !revoked.Load()
			if calls.Add(1) == 1 {
				close(entered)
				<-release
			}
			return map[MembershipIdentity]bool{identities[0]: member}, nil
		},
	}
	hub.SetMembershipChecker(mc)
	identity := MembershipIdentity{WorkspaceID: testWorkspaceID, UserID: testUserID}
	first, second := make(chan bool, 1), make(chan bool, 1)
	go func() { first <- hub.authorizeWrites(identity) }()
	<-entered
	revoked.Store(true)
	go func() { second <- hub.authorizeWrites(identity) }()
	if <-second {
		close(release)
		t.Fatal("post-revocation content joined an in-flight membership snapshot")
	}
	close(release)
	if !<-first || calls.Load() != 2 {
		t.Fatal("post-revocation content reused an in-flight membership snapshot")
	}
}

func TestMembershipBatchCoalescesAndDeduplicatesIdentities(t *testing.T) {
	hub := NewHub()
	var calls atomic.Int32
	identities := []MembershipIdentity{
		{WorkspaceID: "one", UserID: "a"},
		{WorkspaceID: "one", UserID: "b"},
		{WorkspaceID: "two", UserID: "a"},
	}
	hub.SetMembershipChecker(membershipBatchOverride{
		batch: func(_ context.Context, requested []MembershipIdentity) (map[MembershipIdentity]bool, error) {
			calls.Add(1)
			if len(requested) != len(identities) {
				return nil, errors.New("identities were not deduplicated")
			}
			return map[MembershipIdentity]bool{identities[0]: true, identities[2]: true}, nil
		},
	})
	requests := make([]membershipWriteRequest, 12)
	for i := range requests {
		requests[i] = membershipWriteRequest{identity: identities[i%3], result: make(chan bool, 1)}
		hub.writeMembership <- requests[i]
	}
	go hub.runMembershipChecks()
	for _, request := range requests {
		select {
		case allowed := <-request.result:
			if allowed != (request.identity.UserID == "a") {
				t.Fatal("batched membership was applied to the wrong user/workspace")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("batch did not complete")
		}
	}
	close(hub.writeMembership)
	if calls.Load() != 1 {
		t.Fatal("one queued wave was checked with multiple queries")
	}
}
