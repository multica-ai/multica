package realtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type fixtureCloseBroker struct {
	mu       sync.Mutex
	healthy  bool
	handlers map[string]func(context.Context, tagaccess.ConnectionCloseCommand) error
	dispatch int
}

type retryParticipantBroker struct {
	mu             sync.Mutex
	snapshotCalls  int
	dispatchInputs [][]string
}

func (b *retryParticipantBroker) Healthy(context.Context) bool { return true }

func (b *retryParticipantBroker) ActiveInstances(context.Context) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snapshotCalls++
	if b.snapshotCalls == 1 {
		return []string{"instance-a", "instance-b"}, nil
	}
	return []string{"instance-b"}, nil
}

func (b *retryParticipantBroker) Dispatch(_ context.Context, _ tagaccess.ConnectionCloseCommand, participants []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dispatchInputs = append(b.dispatchInputs, append([]string(nil), participants...))
	return errors.New("instance-a has not provided an authenticated close acknowledgement")
}

func newFixtureCloseBroker() *fixtureCloseBroker {
	return &fixtureCloseBroker{healthy: true, handlers: map[string]func(context.Context, tagaccess.ConnectionCloseCommand) error{
		"fixture-instance": func(context.Context, tagaccess.ConnectionCloseCommand) error { return nil },
	}}
}

func (b *fixtureCloseBroker) Register(instanceID string, handler func(context.Context, tagaccess.ConnectionCloseCommand) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[instanceID] = handler
}

func (b *fixtureCloseBroker) ActiveInstances(context.Context) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.healthy {
		return nil, errors.New("broker unavailable")
	}
	participants := make([]string, 0, len(b.handlers))
	for instanceID := range b.handlers {
		participants = append(participants, instanceID)
	}
	return normalizeParticipantIDs(participants)
}

func (b *fixtureCloseBroker) Dispatch(ctx context.Context, command tagaccess.ConnectionCloseCommand, participants []string) error {
	participants, err := normalizeParticipantIDs(participants)
	if err != nil {
		return err
	}
	b.mu.Lock()
	if !b.healthy {
		b.mu.Unlock()
		return errors.New("broker unavailable")
	}
	b.dispatch++
	handlers := make([]func(context.Context, tagaccess.ConnectionCloseCommand) error, 0, len(participants))
	for _, instanceID := range participants {
		handler, ok := b.handlers[instanceID]
		if !ok {
			b.mu.Unlock()
			return fmt.Errorf("participant %q did not acknowledge close", instanceID)
		}
		handlers = append(handlers, handler)
	}
	b.mu.Unlock()
	for _, handler := range handlers {
		if err := handler(ctx, command); err != nil {
			return err
		}
	}
	return nil
}

func (b *fixtureCloseBroker) Healthy(context.Context) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.healthy
}

func (b *fixtureCloseBroker) setHealthy(healthy bool) {
	b.mu.Lock()
	b.healthy = healthy
	b.mu.Unlock()
}

type memoryCloseReceiptStore struct {
	mu           sync.Mutex
	receipts     map[string]tagaccess.ConnectionCloseReceipt
	participants map[string][]string
}

type racingCloseReceiptStore struct {
	winner       tagaccess.ConnectionCloseReceipt
	saved        bool
	participants []string
}

func (s *racingCloseReceiptStore) LoadParticipants(_ context.Context, _ tagaccess.ConnectionCloseCommand) ([]string, bool, error) {
	if len(s.participants) == 0 {
		return nil, false, nil
	}
	return append([]string(nil), s.participants...), true, nil
}

func (s *racingCloseReceiptStore) ClaimParticipants(_ context.Context, _ tagaccess.ConnectionCloseCommand, participants []string) ([]string, error) {
	if len(s.participants) == 0 {
		s.participants = append([]string(nil), participants...)
	}
	return append([]string(nil), s.participants...), nil
}

func (s *racingCloseReceiptStore) Load(_ context.Context, _ tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, bool, error) {
	if !s.saved {
		return tagaccess.ConnectionCloseReceipt{}, false, nil
	}
	return s.winner, true, nil
}

func (s *racingCloseReceiptStore) Save(_ context.Context, command tagaccess.ConnectionCloseCommand, _ tagaccess.ConnectionCloseReceipt) error {
	s.saved = true
	s.winner = tagaccess.ConnectionCloseReceipt{
		ReceiptID: "concurrent-winner", Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		TargetDigest: command.TargetDigest, CompletedAt: time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC),
	}
	return nil
}

func newMemoryCloseReceiptStore() *memoryCloseReceiptStore {
	return &memoryCloseReceiptStore{
		receipts: make(map[string]tagaccess.ConnectionCloseReceipt), participants: make(map[string][]string),
	}
}

func (s *memoryCloseReceiptStore) Load(_ context.Context, command tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, ok := s.receipts[command.DeliveryID+":"+command.TargetDigest]
	return receipt, ok, nil
}

func (s *memoryCloseReceiptStore) Save(_ context.Context, command tagaccess.ConnectionCloseCommand, receipt tagaccess.ConnectionCloseReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := command.DeliveryID + ":" + command.TargetDigest
	if current, ok := s.receipts[key]; ok && current != receipt {
		return errors.New("conflicting receipt")
	}
	s.receipts[key] = receipt
	return nil
}

func (s *memoryCloseReceiptStore) LoadParticipants(_ context.Context, command tagaccess.ConnectionCloseCommand) ([]string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	participants, ok := s.participants[command.DeliveryID+":"+command.TargetDigest]
	return append([]string(nil), participants...), ok, nil
}

func (s *memoryCloseReceiptStore) ClaimParticipants(_ context.Context, command tagaccess.ConnectionCloseCommand, participants []string) ([]string, error) {
	participants, err := normalizeParticipantIDs(participants)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := command.DeliveryID + ":" + command.TargetDigest
	if current, ok := s.participants[key]; ok {
		return append([]string(nil), current...), nil
	}
	s.participants[key] = append([]string(nil), participants...)
	return append([]string(nil), participants...), nil
}

func attachedRevocationClient(hub *Hub, metadata ConnectionMetadata) *Client {
	client := &Client{
		hub:           hub,
		send:          make(chan []byte, 8),
		userID:        metadata.VIBESUserID,
		workspaceID:   metadata.WorkspaceID,
		metadata:      metadata,
		subscriptions: map[scopeKey]bool{},
	}
	key := sk(ScopeWorkspace, metadata.WorkspaceID)
	client.subscriptions[key] = true
	hub.mu.Lock()
	hub.clients[client] = true
	hub.rooms[key] = map[*Client]bool{client: true}
	hub.mu.Unlock()
	return client
}

func TestConnectionCloseCoordinatorTargetsAcrossInstancesAndPersistsOneReceipt(t *testing.T) {
	broker := newFixtureCloseBroker()
	store := newMemoryCloseReceiptStore()
	hubA := NewHub()
	hubA.SetInstanceID("instance-a")
	hubB := NewHub()
	hubB.SetInstanceID("instance-b")
	coordinator := NewConnectionCloseCoordinator(hubA, broker, store, time.Now)
	broker.Register("instance-a", hubA.ApplyConnectionClose)
	broker.Register("instance-b", hubB.ApplyConnectionClose)

	targetA := attachedRevocationClient(hubA, ConnectionMetadata{
		ConnectionID: "connection-a", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-a", VIBESSessionID: "session-a", AccountEpoch: 7, MembershipGeneration: 2, AuthorityVersion: 4,
	})
	targetB := attachedRevocationClient(hubB, ConnectionMetadata{
		ConnectionID: "connection-b", InstanceID: "instance-b", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-b", VIBESSessionID: "session-b", AccountEpoch: 7, MembershipGeneration: 2, AuthorityVersion: 4,
	})
	unaffectedWorkspace := attachedRevocationClient(hubA, ConnectionMetadata{
		ConnectionID: "connection-c", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-b",
		TagSessionID: "tag-c", VIBESSessionID: "session-a", AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 9,
	})
	unaffectedSession := attachedRevocationClient(hubB, ConnectionMetadata{
		ConnectionID: "connection-d", InstanceID: "instance-b", VIBESUserID: "user-2", WorkspaceID: "workspace-a",
		TagSessionID: "tag-d", VIBESSessionID: "session-d", AccountEpoch: 3, MembershipGeneration: 1, AuthorityVersion: 4,
	})

	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-5", CorrelationID: "correlation-5",
		WorkspaceID: "workspace-a", AuthorityVersion: 5, TargetDigest: "target-digest-5",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-a", VIBESUserID: "user-1"}},
	}
	receipt, err := coordinator.CloseConnections(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReceiptID == "" || receipt.DeliveryID != command.DeliveryID || receipt.TargetDigest != command.TargetDigest || receipt.CompletedAt.IsZero() {
		t.Fatalf("receipt = %#v, want exact durable completion", receipt)
	}
	if !targetA.revoked.Load() || !targetB.revoked.Load() {
		t.Fatal("targeted connections were not fenced on every instance")
	}
	if unaffectedWorkspace.revoked.Load() || unaffectedSession.revoked.Load() {
		t.Fatal("unaffected Workspace or sibling user connection was revoked")
	}

	duplicate, err := coordinator.CloseConnections(context.Background(), command)
	if err != nil || duplicate != receipt {
		t.Fatalf("duplicate receipt = %#v, err = %v, want %#v", duplicate, err, receipt)
	}
	if broker.dispatch != 1 {
		t.Fatalf("duplicate dispatched %d times, want once", broker.dispatch)
	}

	hubA.BroadcastToWorkspace("workspace-a", []byte(`{"type":"post-revoke"}`))
	hubB.BroadcastToWorkspace("workspace-a", []byte(`{"type":"post-revoke"}`))
	select {
	case frame, open := <-targetA.send:
		if open {
			t.Fatalf("target received post-completion frame %s", frame)
		}
	default:
	}
	select {
	case frame := <-unaffectedSession.send:
		if string(frame) != `{"type":"post-revoke"}` {
			t.Fatalf("unaffected frame = %s", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("unaffected connection stopped receiving Workspace frames")
	}
}

func TestApplyConnectionCloseIsVersionAwareAndScopeExact(t *testing.T) {
	hub := NewHub()
	hub.SetInstanceID("instance-a")
	fresh := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "fresh", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-fresh", VIBESSessionID: "session-fresh", AccountEpoch: 8, MembershipGeneration: 3, AuthorityVersion: 6,
	})
	oldSiblingSession := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "old", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-old", VIBESSessionID: "session-old", AccountEpoch: 7, MembershipGeneration: 2, AuthorityVersion: 4,
	})

	staleWorkspaceCommand := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "old-delivery", CorrelationID: "old-correlation",
		WorkspaceID: "workspace-a", AuthorityVersion: 5, TargetDigest: "old-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-a", VIBESUserID: "user-1"}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), staleWorkspaceCommand); err != nil {
		t.Fatal(err)
	}
	if fresh.revoked.Load() {
		t.Fatal("out-of-order old command closed a fresh version/generation")
	}
	if !oldSiblingSession.revoked.Load() {
		t.Fatal("old Membership connection was not closed")
	}

	sessionCommand := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseIdentityRestriction, DeliveryID: "logout", CorrelationID: "logout-correlation",
		IdentityRestrictionVersion: 3, TargetDigest: "logout-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "session-old"}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), sessionCommand); err != nil {
		t.Fatal(err)
	}
	if fresh.revoked.Load() {
		t.Fatal("single-session logout closed the sibling VIBES session")
	}
}

func TestSessionWorkspaceSupersessionClosesOnlyOldGenerationInExactWorkspace(t *testing.T) {
	hub := NewHub()
	old := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "old-a", VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: "workspace-a",
		SessionWorkspaceGeneration: 1, AuthorityVersion: 4,
	})
	sibling := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "sibling-a", VIBESUserID: "user-1", VIBESSessionID: "session-sibling", WorkspaceID: "workspace-a",
		SessionWorkspaceGeneration: 1, AuthorityVersion: 4,
	})
	newWorkspace := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "new-b", VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: "workspace-b",
		SessionWorkspaceGeneration: 2, AuthorityVersion: 11,
	})
	newerReturn := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "newer-a", VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: "workspace-a",
		SessionWorkspaceGeneration: 4, AuthorityVersion: 15,
	})
	command := tagaccess.ConnectionCloseCommand{
		Source:     tagaccess.ConnectionCloseSessionWorkspaceSupersession,
		DeliveryID: "switch-2", CorrelationID: "switch-correlation-2", WorkspaceID: "workspace-a",
		SessionWorkspaceGeneration: 2, TargetDigest: "switch-target-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{
			Scope: tagaccess.ConnectionCloseSessionWorkspace, VIBESUserID: "user-1",
			VIBESSessionID: "session-1", WorkspaceID: "workspace-a",
		}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if !old.revoked.Load() || hub.hasClient(old) || old.trySend([]byte(`{"type":"post-completion"}`)) {
		t.Fatal("old socket was not irreversibly fenced before completion")
	}
	for _, unaffected := range []*Client{sibling, newWorkspace, newerReturn} {
		if unaffected.revoked.Load() || !hub.hasClient(unaffected) {
			t.Fatalf("unaffected socket %s was closed", unaffected.metadata.ConnectionID)
		}
	}
	if hub.fences.allows(ConnectionMetadata{
		VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: "workspace-a", SessionWorkspaceGeneration: 1,
	}) {
		t.Fatal("old reconnect generation passed the installed session Workspace fence")
	}
	if !hub.fences.allows(ConnectionMetadata{
		VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: "workspace-a", SessionWorkspaceGeneration: 2,
	}) {
		t.Fatal("new generation could not reconnect to a later valid Workspace binding")
	}
}

func TestCloseFenceRejectsAuthorizationThatRegistersAfterCompletion(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "remove-2", CorrelationID: "remove-correlation-2",
		WorkspaceID: "workspace-a", AuthorityVersion: 2, TargetDigest: "remove-digest-2",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-a", VIBESUserID: "user-1"}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	oldDecision := &Client{
		hub: hub, send: make(chan []byte, 1), userID: "user-1", workspaceID: "workspace-a",
		metadata: ConnectionMetadata{ConnectionID: "late-old", VIBESUserID: "user-1", WorkspaceID: "workspace-a", VIBESSessionID: "session-old", AuthorityVersion: 1},
	}
	if hub.registerClient(oldDecision) || !oldDecision.revoked.Load() {
		t.Fatal("authorization decision from before projection commit registered after close completion")
	}
	freshDecision := &Client{
		hub: hub, send: make(chan []byte, 1), userID: "user-1", workspaceID: "workspace-a",
		metadata: ConnectionMetadata{ConnectionID: "late-fresh", VIBESUserID: "user-1", WorkspaceID: "workspace-a", VIBESSessionID: "session-fresh", AuthorityVersion: 3},
	}
	if !hub.registerClient(freshDecision) {
		t.Fatal("fresh higher-version authorization was rejected by the close fence")
	}
	hub.removeClient(freshDecision)
}

func TestAuthorizationToRegistrationFencePrecedesCloseCompletion(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	client := &Client{
		hub: hub, send: make(chan []byte, 1), userID: "user-1", workspaceID: "workspace-a",
		metadata: ConnectionMetadata{ConnectionID: "in-flight-upgrade", VIBESUserID: "user-1", WorkspaceID: "workspace-a", AuthorityVersion: 1},
	}
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "role-2", CorrelationID: "role-correlation-2",
		WorkspaceID: "workspace-a", AuthorityVersion: 2, TargetDigest: "role-digest-2",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-a", VIBESUserID: "user-1"}},
	}
	hub.authorize.RLock()
	done := make(chan error, 1)
	go func() { done <- hub.ApplyConnectionClose(context.Background(), command) }()
	select {
	case err := <-done:
		t.Fatalf("close completed before in-flight authorization registered: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if !hub.registerClient(client) {
		t.Fatal("pre-commit authorization was not registered under its read fence")
	}
	hub.authorize.RUnlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not complete after authorization registry fence released")
	}
	if !client.revoked.Load() || hub.hasClient(client) {
		t.Fatal("close completion did not fence and remove the in-flight upgrade")
	}
}

func TestFinalAdmissionLookupTimeoutReleasesCloseFence(t *testing.T) {
	hub := NewHub()
	authorizationStarted := make(chan struct{})
	authorizationDone := make(chan struct{})
	go func() {
		hub.authorize.RLock()
		close(authorizationStarted)
		_ = authorizeTagWebSocketAccess(context.Background(), blockingTagAccessGate{}, tagaccess.AccessRequest{
			TagSessionID: "tag-session", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		})
		hub.authorize.RUnlock()
		close(authorizationDone)
	}()
	<-authorizationStarted

	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "blocked-admission", CorrelationID: "blocked-admission-correlation",
		WorkspaceID: "workspace-a", AuthorityVersion: 2, TargetDigest: "blocked-admission-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-a", VIBESUserID: "user-1"}},
	}
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- hub.ApplyConnectionClose(context.Background(), command) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed > AuthorizationWatchdogTarget {
			t.Fatalf("close fence waited %s for final admission lookup, target <= %s", elapsed, AuthorizationWatchdogTarget)
		}
	case <-time.After(AuthorizationWatchdogTarget + 100*time.Millisecond):
		t.Fatal("blocked final admission lookup held the close fence past the watchdog target")
	}
	<-authorizationDone
}

func TestOutboundQueueRechecksLeaseAfterWaitingForFence(t *testing.T) {
	hub := NewHub()
	var leaseValid atomic.Bool
	leaseValid.Store(true)
	firstCheck := make(chan struct{})
	var checks atomic.Int32
	hub.revocationLease.Store(&revocationLeaseFence{valid: func() bool {
		if checks.Add(1) == 1 {
			close(firstCheck)
		}
		return leaseValid.Load()
	}})
	client := &Client{hub: hub, send: make(chan []byte, 1), metadata: ConnectionMetadata{VIBESSessionID: "vibes-session-1"}}
	client.sendMu.Lock()
	done := make(chan bool, 1)
	go func() { done <- client.trySend([]byte(`{"type":"must-not-enqueue"}`)) }()
	<-firstCheck
	leaseValid.Store(false)
	client.sendMu.Unlock()
	if <-done {
		t.Fatal("frame was enqueued after lease expired while waiting for the queue fence")
	}
	if checks.Load() < 2 {
		t.Fatal("queue path did not recheck the lease inside its fence")
	}
}

func TestGatewayBrowserSocketUsesAuthorizationWriteDeadline(t *testing.T) {
	browser := &Client{metadata: ConnectionMetadata{VIBESSessionID: "vibes-session-1"}}
	if timeout := browser.writeTimeout(); timeout != authorizationSocketWriteTimeout || timeout >= AuthorizationWatchdogTarget {
		t.Fatalf("browser write timeout = %s, want %s below watchdog %s", timeout, authorizationSocketWriteTimeout, AuthorizationWatchdogTarget)
	}
	ordinary := &Client{}
	if timeout := ordinary.writeTimeout(); timeout != writeWait {
		t.Fatalf("ordinary service socket timeout = %s, want %s", timeout, writeWait)
	}
}

func TestInboundFrameRechecksLeaseAfterWaitingForFence(t *testing.T) {
	hub := NewHub()
	var leaseValid atomic.Bool
	leaseValid.Store(true)
	hub.revocationLease.Store(&revocationLeaseFence{valid: leaseValid.Load})
	client := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "paused-inbound", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		VIBESSessionID: "vibes-session-1", AuthorityVersion: 1,
	})
	client.frameMu.Lock()
	done := make(chan bool, 1)
	go func() { done <- client.processInboundFrame([]byte(`{"type":"ping"}`)) }()
	leaseValid.Store(false)
	client.frameMu.Unlock()
	if <-done {
		t.Fatal("inbound frame was processed after lease expired while waiting for the frame fence")
	}
	select {
	case frame, open := <-client.send:
		if open {
			t.Fatalf("inbound ping emitted post-expiry frame %s", frame)
		}
	default:
	}
}

type blockingTagAccessGate struct{}

func (blockingTagAccessGate) GrantSession(context.Context, tagaccess.SessionGrant) error { return nil }

func (blockingTagAccessGate) Authorize(ctx context.Context, _ tagaccess.AccessRequest) tagaccess.Decision {
	<-ctx.Done()
	return tagaccess.Decision{}
}

func TestAccessWatchdogFailsClosedWhenProjectionLookupBlocks(t *testing.T) {
	hub := NewHub()
	client := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "blocked-lookup", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-session", VIBESSessionID: "vibes-session", AuthorityVersion: 1,
	})
	started := time.Now()
	done := make(chan struct{})
	go func() {
		client.watchWebSocketAccess(blockingTagAccessGate{}, tagaccess.AccessRequest{
			TagSessionID: "tag-session", VIBESSessionID: "vibes-session", VIBESUserID: "user-1",
			WorkspaceID: "workspace-a", AccountEpoch: 1, SessionWorkspaceGeneration: 1,
			MembershipGeneration: 1, AuthorityVersion: 1,
		})
		close(done)
	}()
	select {
	case <-done:
		if !client.revoked.Load() {
			t.Fatal("blocked projection lookup ended without fencing the socket")
		}
		if elapsed := time.Since(started); elapsed > AuthorizationWatchdogTarget+100*time.Millisecond {
			t.Fatalf("blocked projection lookup fenced after %s, target <= %s", elapsed, AuthorizationWatchdogTarget)
		}
	case <-time.After(AuthorizationWatchdogTarget + 250*time.Millisecond):
		t.Fatal("blocked projection lookup exceeded fail-closed watchdog target")
	}
}

func TestConnectionCloseCoordinatorFailsClosedWhenBrokerIsUnavailable(t *testing.T) {
	broker := newFixtureCloseBroker()
	broker.setHealthy(false)
	coordinator := NewConnectionCloseCoordinator(NewHub(), broker, newMemoryCloseReceiptStore(), time.Now)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseIdentityRestriction, DeliveryID: "ban", CorrelationID: "ban-correlation",
		IdentityRestrictionVersion: 1, TargetDigest: "ban-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"}},
	}
	if receipt, err := coordinator.CloseConnections(context.Background(), command); err == nil || receipt.ReceiptID != "" {
		t.Fatalf("broker outage receipt = %#v, err = %v, want pending error without fabricated receipt", receipt, err)
	}
}

func TestConnectionCloseCoordinatorRetryKeepsOriginalDurableParticipants(t *testing.T) {
	broker := &retryParticipantBroker{}
	store := newMemoryCloseReceiptStore()
	coordinator := NewConnectionCloseCoordinator(NewHub(), broker, store, time.Now)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "retry-original-participants", CorrelationID: "retry-correlation",
		WorkspaceID: "workspace-a", AuthorityVersion: 9, TargetDigest: "retry-target-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-a"}},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if receipt, err := coordinator.CloseConnections(context.Background(), command); err == nil || receipt.ReceiptID != "" {
			t.Fatalf("attempt %d fabricated completion without instance-a ACK: receipt=%#v err=%v", attempt+1, receipt, err)
		}
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.snapshotCalls != 1 {
		t.Fatalf("active instance set was resnapshotted %d times; want original durable snapshot only", broker.snapshotCalls)
	}
	if len(broker.dispatchInputs) != 2 {
		t.Fatalf("dispatch attempts = %d, want 2", len(broker.dispatchInputs))
	}
	for attempt, participants := range broker.dispatchInputs {
		if len(participants) != 2 || participants[0] != "instance-a" || participants[1] != "instance-b" {
			t.Fatalf("attempt %d participants = %v, want original [instance-a instance-b]", attempt+1, participants)
		}
	}
}

func TestConnectionCloseCoordinatorReturnsOnlyTheDurablyReadableReceipt(t *testing.T) {
	broker := newFixtureCloseBroker()
	store := &racingCloseReceiptStore{}
	coordinator := NewConnectionCloseCoordinator(NewHub(), broker, store, time.Now)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseIdentityRestriction, DeliveryID: "concurrent", CorrelationID: "concurrent-correlation",
		IdentityRestrictionVersion: 2, TargetDigest: "concurrent-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"}},
	}
	receipt, err := coordinator.CloseConnections(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if receipt != store.winner {
		t.Fatalf("CloseConnections() = %#v, want durable concurrent winner %#v", receipt, store.winner)
	}
}

func TestIdentityIngressCompletionUsesRealConnectionClosePort(t *testing.T) {
	broker := newFixtureCloseBroker()
	store := newMemoryCloseReceiptStore()
	hubA := NewHub()
	hubA.SetInstanceID("instance-a")
	hubB := NewHub()
	hubB.SetInstanceID("instance-b")
	coordinator := NewConnectionCloseCoordinator(hubA, broker, store, time.Now)
	broker.Register("instance-a", hubA.ApplyConnectionClose)
	broker.Register("instance-b", hubB.ApplyConnectionClose)

	targetA := attachedRevocationClient(hubA, ConnectionMetadata{
		ConnectionID: "target-a", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-a", VIBESSessionID: "session-target", AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 3,
	})
	targetB := attachedRevocationClient(hubB, ConnectionMetadata{
		ConnectionID: "target-b", InstanceID: "instance-b", VIBESUserID: "user-1", WorkspaceID: "workspace-b",
		TagSessionID: "tag-b", VIBESSessionID: "session-target", AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 9,
	})
	siblingSession := attachedRevocationClient(hubA, ConnectionMetadata{
		ConnectionID: "sibling", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		TagSessionID: "tag-sibling", VIBESSessionID: "session-sibling", AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 3,
	})

	key := []byte("0123456789abcdef0123456789abcdef")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{"vibes-primary": key}, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		Delivery: tagaccess.IdentityRestrictionDelivery{
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "identity-logout-1",
			CorrelationID: "correlation-logout-1", IdempotencyKey: "idempotency-logout-1",
			VIBESUserID: "user-1", VIBESSessionID: "session-target", AccountEpoch: 7, IdentityRestrictionVersion: 1,
			CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "session-target"},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)

	receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted ||
		receipt.ConnectionClose.ReceiptID == "" || receipt.ConnectionClose.CompletedAt == nil {
		t.Fatalf("identity receipt = %#v, want durable apply plus exact close completion", receipt)
	}
	if !targetA.revoked.Load() || !targetB.revoked.Load() {
		t.Fatal("session logout completion reported before all targeted instances were fenced")
	}
	if siblingSession.revoked.Load() {
		t.Fatal("session logout revoked an unaffected sibling session")
	}
	if broker.dispatch != 1 {
		t.Fatalf("identity ingress dispatched %d close commands, want one", broker.dispatch)
	}

	retry, err := access.IdentityIngress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Apply.Result != tagaccess.ApplyDuplicate || retry.ConnectionClose.ReceiptID != receipt.ConnectionClose.ReceiptID || broker.dispatch != 1 {
		t.Fatalf("duplicate identity receipt = %#v, dispatches = %d", retry, broker.dispatch)
	}
}

func TestExactVIBESSessionWorkspaceFixtureCompletesThroughRealClosePortWithZeroFrames(t *testing.T) {
	var fixture struct {
		TestKeyBase64    string                                       `json:"testKeyBase64"`
		KeyID            string                                       `json:"keyId"`
		UnsignedEnvelope tagaccess.SessionWorkspaceSupersededEnvelope `json:"unsignedEnvelope"`
		MACBase64        string                                       `json:"macBase64"`
	}
	body, err := os.ReadFile(filepath.Join("..", "tagaccess", "testdata", "session-workspace-supersession-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	mac, err := base64.StdEncoding.DecodeString(fixture.MACBase64)
	if err != nil {
		t.Fatal(err)
	}
	fixture.UnsignedEnvelope.Authentication = tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID, MAC: mac}

	hub := NewHub()
	hub.SetInstanceID("instance-a")
	broker := newFixtureCloseBroker()
	broker.Register("instance-a", hub.ApplyConnectionClose)
	receiptStore := newMemoryCloseReceiptStore()
	coordinator := NewConnectionCloseCoordinator(hub, broker, receiptStore, time.Now)
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{fixture.KeyID: key}, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	project := func(workspaceID string, version, membership uint64) {
		envelope := tagaccess.AuthorityEnvelope{
			SchemaVersion: 1, DeliveryID: "projection-" + workspaceID, CorrelationID: "projection-correlation-" + workspaceID,
			Delivery: tagaccess.ProjectionDelivery{
				Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: version, AuthorityAssertionID: "assertion-" + workspaceID,
				Projections: []tagaccess.ProjectionEvent{{
					EventID: "projection-" + workspaceID, VIBESUserID: "user-1", WorkspaceID: workspaceID,
					Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7,
					MembershipGeneration: membership, AuthorityVersion: version,
				}},
			},
			Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID},
		}
		payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
		if err != nil {
			t.Fatal(err)
		}
		signer := hmac.New(sha256.New, key)
		_, _ = signer.Write(payload)
		envelope.Authentication.MAC = signer.Sum(nil)
		if receipt, err := access.Ingress.Deliver(context.Background(), envelope); err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("projection %s = %#v, %v", workspaceID, receipt, err)
		}
	}
	project("workspace-alpha", 5, 1)
	project("workspace-beta", 11, 3)
	for version := uint64(1); version <= 4; version++ {
		delivery := tagaccess.IdentityRestrictionDelivery{
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: fmt.Sprintf("fixture-sibling-%d", version),
			CorrelationID: fmt.Sprintf("fixture-sibling-correlation-%d", version), IdempotencyKey: fmt.Sprintf("fixture-sibling-idempotency-%d", version),
			VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("fixture-sibling-session-%d", version), AccountEpoch: 7,
			IdentityRestrictionVersion: version,
			CloseTarget:                tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("fixture-sibling-session-%d", version)},
		}
		envelope := tagaccess.IdentityRestrictionEnvelope{SchemaVersion: 1, Delivery: delivery, Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID}}
		payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
		if err != nil {
			t.Fatal(err)
		}
		signer := hmac.New(sha256.New, key)
		_, _ = signer.Write(payload)
		envelope.Authentication.MAC = signer.Sum(nil)
		if receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope); err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("identity %d = %#v, %v", version, receipt, err)
		}
	}
	tagSessionID := tagaccess.BrowserTagSessionID("user-1", "session-1")
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-alpha",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 5,
		SessionExpiresAt: time.Now().Add(time.Hour), GrantExpiresAt: time.Now().Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	oldSocket := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "fixture-old-socket", InstanceID: "instance-a", VIBESUserID: "user-1",
		VIBESSessionID: "session-1", TagSessionID: tagSessionID, WorkspaceID: "workspace-alpha",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 5,
	})
	receipt, err := access.SessionWorkspaceIngress.Deliver(context.Background(), fixture.UnsignedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted ||
		receipt.ConnectionClose.ReceiptID == "" || receipt.ConnectionClose.CompletedAt == nil {
		t.Fatalf("two-stage receipt = %#v", receipt)
	}
	if !oldSocket.revoked.Load() || hub.hasClient(oldSocket) || oldSocket.trySend([]byte(`{"type":"forbidden"}`)) {
		t.Fatal("completed receipt did not mean zero post-revoke frames")
	}
	retry, err := access.SessionWorkspaceIngress.Deliver(context.Background(), fixture.UnsignedEnvelope)
	if err != nil || retry.Apply.Result != tagaccess.ApplyDuplicate || retry.ConnectionClose.ReceiptID != receipt.ConnectionClose.ReceiptID {
		t.Fatalf("durable retry receipt = %#v, %v", retry, err)
	}
}

func TestAccountAndWorkspaceCloseScopesRemainExact(t *testing.T) {
	hub := NewHub()
	userWorkspaceA := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "user-a", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		VIBESSessionID: "session-a", AuthorityVersion: 4,
	})
	userWorkspaceB := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "user-b", InstanceID: "instance-a", VIBESUserID: "user-1", WorkspaceID: "workspace-b",
		VIBESSessionID: "session-b", AuthorityVersion: 7,
	})
	otherUserWorkspaceA := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "other-a", InstanceID: "instance-a", VIBESUserID: "user-2", WorkspaceID: "workspace-a",
		VIBESSessionID: "session-c", AuthorityVersion: 4,
	})

	accountBan := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseIdentityRestriction, DeliveryID: "account-ban", CorrelationID: "account-ban-correlation",
		IdentityRestrictionVersion: 1, TargetDigest: "account-ban-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), accountBan); err != nil {
		t.Fatal(err)
	}
	if !userWorkspaceA.revoked.Load() || !userWorkspaceB.revoked.Load() || otherUserWorkspaceA.revoked.Load() {
		t.Fatal("account ban did not close exactly the account across Workspaces")
	}

	workspaceDisable := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "workspace-disable", CorrelationID: "workspace-disable-correlation",
		WorkspaceID: "workspace-a", AuthorityVersion: 5, TargetDigest: "workspace-disable-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-a"}},
	}
	if err := hub.ApplyConnectionClose(context.Background(), workspaceDisable); err != nil {
		t.Fatal(err)
	}
	if !otherUserWorkspaceA.revoked.Load() {
		t.Fatal("workspace disable did not close every old socket in that Workspace")
	}
}

func TestPostgresConnectionCloseReceiptStoreIsDurableAndConflictSafe(t *testing.T) {
	db := openDisposableRealtimeReceiptDatabase(t)
	store := NewPostgresConnectionCloseReceiptStore(db)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-durable", CorrelationID: "correlation-durable",
		WorkspaceID: "workspace-a", AuthorityVersion: 8, TargetDigest: "target-digest-durable",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-a"}},
	}
	receipt := tagaccess.ConnectionCloseReceipt{
		ReceiptID: "receipt-durable", Source: command.Source, DeliveryID: command.DeliveryID, CorrelationID: command.CorrelationID,
		WorkspaceID: command.WorkspaceID, AuthorityVersion: command.AuthorityVersion, TargetDigest: command.TargetDigest,
		CompletedAt: time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC),
	}
	claimed, err := store.ClaimParticipants(context.Background(), command, []string{"instance-b", "instance-a"})
	if err != nil || len(claimed) != 2 || claimed[0] != "instance-a" || claimed[1] != "instance-b" {
		t.Fatalf("ClaimParticipants() = %v, %v, want durable sorted original participants", claimed, err)
	}
	loadedParticipants, ok, err := NewPostgresConnectionCloseReceiptStore(db).LoadParticipants(context.Background(), command)
	if err != nil || !ok || len(loadedParticipants) != 2 || loadedParticipants[0] != "instance-a" || loadedParticipants[1] != "instance-b" {
		t.Fatalf("restarted store LoadParticipants() = %v, %v, %v", loadedParticipants, ok, err)
	}
	if err := store.Save(context.Background(), command, receipt); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := NewPostgresConnectionCloseReceiptStore(db).Load(context.Background(), command)
	if err != nil || !ok || loaded != receipt {
		t.Fatalf("restarted store Load() = %#v, %v, %v, want %#v", loaded, ok, err, receipt)
	}
	conflict := receipt
	conflict.ReceiptID = "receipt-conflict"
	if err := store.Save(context.Background(), command, conflict); err == nil {
		t.Fatal("conflicting receipt unexpectedly replaced durable completion evidence")
	}
}

func openDisposableRealtimeReceiptDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("MULTICA_TAG_ACCESS_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatalf("realtime receipt test database host %q is not loopback", hostname)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("realtime_receipt_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE") })

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve realtime test path")
	}
	migrations := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	for _, name := range []string{
		"368_tag_access_connection_close_receipt.up.sql",
		"369_tag_access_connection_close_receipt_index.up.sql",
		"370_tag_close_dispatch_index.up.sql",
	} {
		body, err := os.ReadFile(filepath.Join(migrations, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return db
}
