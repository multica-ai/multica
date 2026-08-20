package realtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/oklog/ulid/v2"
)

const (
	AuthorizationWatchdogTarget     = 2 * time.Second
	authorizationSocketWriteTimeout = AuthorizationWatchdogTarget / 8
	CloseCodeSessionRevoked         = 4401
	CloseCodeAuthorizationChanged   = 4403
	CloseCodeAuthorizationFeedLost  = 1013
)

// ConnectionMetadata is the immutable authorization identity captured when a
// Tag socket passes AccessGate. It is sufficient to target one VIBES session,
// account, Membership generation/version, Workspace, and server instance.
type ConnectionMetadata struct {
	ConnectionID               string
	InstanceID                 string
	VIBESUserID                string
	WorkspaceID                string
	TagSessionID               string
	VIBESSessionID             string
	AccountEpoch               uint64
	SessionWorkspaceGeneration uint64
	MembershipGeneration       uint64
	AuthorityVersion           uint64
}

type revocationFences struct {
	workspaces        map[string]uint64
	memberships       map[string]uint64
	sessions          map[string]struct{}
	sessionWorkspaces map[string]uint64
	accounts          map[string]struct{}
}

func newRevocationFences() revocationFences {
	return revocationFences{
		workspaces: make(map[string]uint64), memberships: make(map[string]uint64),
		sessions: make(map[string]struct{}), sessionWorkspaces: make(map[string]uint64), accounts: make(map[string]struct{}),
	}
}

func membershipFenceKey(workspaceID, userID string) string { return workspaceID + "\x00" + userID }
func sessionFenceKey(userID, sessionID string) string      { return userID + "\x00" + sessionID }
func sessionWorkspaceFenceKey(userID, sessionID, workspaceID string) string {
	return userID + "\x00" + sessionID + "\x00" + workspaceID
}

func (f *revocationFences) install(command tagaccess.ConnectionCloseCommand) {
	for _, target := range command.Targets {
		switch target.Scope {
		case tagaccess.ConnectionCloseWorkspace:
			if command.AuthorityVersion > f.workspaces[target.WorkspaceID] {
				f.workspaces[target.WorkspaceID] = command.AuthorityVersion
			}
		case tagaccess.ConnectionCloseMembership:
			key := membershipFenceKey(target.WorkspaceID, target.VIBESUserID)
			if command.AuthorityVersion > f.memberships[key] {
				f.memberships[key] = command.AuthorityVersion
			}
		case tagaccess.ConnectionCloseSession:
			f.sessions[sessionFenceKey(target.VIBESUserID, target.VIBESSessionID)] = struct{}{}
		case tagaccess.ConnectionCloseSessionWorkspace:
			key := sessionWorkspaceFenceKey(target.VIBESUserID, target.VIBESSessionID, target.WorkspaceID)
			if command.SessionWorkspaceGeneration > f.sessionWorkspaces[key] {
				f.sessionWorkspaces[key] = command.SessionWorkspaceGeneration
			}
		case tagaccess.ConnectionCloseAccount:
			f.accounts[target.VIBESUserID] = struct{}{}
		}
	}
}

func (f revocationFences) allows(metadata ConnectionMetadata) bool {
	if _, blocked := f.accounts[metadata.VIBESUserID]; blocked {
		return false
	}
	if _, blocked := f.sessions[sessionFenceKey(metadata.VIBESUserID, metadata.VIBESSessionID)]; blocked {
		return false
	}
	if metadata.SessionWorkspaceGeneration < f.sessionWorkspaces[sessionWorkspaceFenceKey(metadata.VIBESUserID, metadata.VIBESSessionID, metadata.WorkspaceID)] {
		return false
	}
	if metadata.AuthorityVersion < f.workspaces[metadata.WorkspaceID] {
		return false
	}
	return metadata.AuthorityVersion >= f.memberships[membershipFenceKey(metadata.WorkspaceID, metadata.VIBESUserID)]
}

// ConnectionCloseBroker dispatches an exact close command to every active
// realtime instance and returns only after each active instance has fenced and
// closed its matching local sockets.
type ConnectionCloseBroker interface {
	ActiveInstances(context.Context) ([]string, error)
	Dispatch(context.Context, tagaccess.ConnectionCloseCommand, []string) error
	Healthy(context.Context) bool
}

type connectionCloseLease interface {
	LeaseValid() bool
}

// ConnectionCloseReceiptStore is the durable idempotency boundary. It keeps
// the original participant boots immutable across delivery retries; a receipt
// is stored only after the broker confirms every captured instance-local close.
type ConnectionCloseReceiptStore interface {
	Load(context.Context, tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, bool, error)
	LoadParticipants(context.Context, tagaccess.ConnectionCloseCommand) ([]string, bool, error)
	ClaimParticipants(context.Context, tagaccess.ConnectionCloseCommand, []string) ([]string, error)
	Save(context.Context, tagaccess.ConnectionCloseCommand, tagaccess.ConnectionCloseReceipt) error
}

// ConnectionCloseCoordinator is the real #288 ConnectionClosePort adapter.
type ConnectionCloseCoordinator struct {
	hub    *Hub
	broker ConnectionCloseBroker
	store  ConnectionCloseReceiptStore
	now    func() time.Time
}

var _ tagaccess.ConnectionClosePort = (*ConnectionCloseCoordinator)(nil)

func NewConnectionCloseCoordinator(hub *Hub, broker ConnectionCloseBroker, store ConnectionCloseReceiptStore, now func() time.Time) *ConnectionCloseCoordinator {
	if now == nil {
		now = time.Now
	}
	coordinator := &ConnectionCloseCoordinator{hub: hub, broker: broker, store: store, now: now}
	if hub != nil && broker != nil {
		hub.mu.Lock()
		hub.revocationHealthy = broker.Healthy
		if lease, ok := broker.(connectionCloseLease); ok {
			hub.revocationLease.Store(&revocationLeaseFence{valid: lease.LeaseValid})
		}
		hub.mu.Unlock()
	}
	return coordinator
}

func (c *ConnectionCloseCoordinator) CloseConnections(ctx context.Context, command tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, error) {
	if c == nil || c.hub == nil || c.broker == nil || c.store == nil {
		return tagaccess.ConnectionCloseReceipt{}, errors.New("realtime connection close is not configured")
	}
	if existing, ok, err := c.store.Load(ctx, command); err != nil {
		return tagaccess.ConnectionCloseReceipt{}, err
	} else if ok {
		if !receiptMatchesCommand(existing, command) {
			return tagaccess.ConnectionCloseReceipt{}, errors.New("conflicting durable connection-close receipt")
		}
		return existing, nil
	}
	if !c.broker.Healthy(ctx) {
		return tagaccess.ConnectionCloseReceipt{}, errors.New("realtime revocation broker unavailable")
	}
	participants, ok, err := c.store.LoadParticipants(ctx, command)
	if err != nil {
		return tagaccess.ConnectionCloseReceipt{}, err
	}
	if !ok {
		participants, err = c.broker.ActiveInstances(ctx)
		if err != nil {
			return tagaccess.ConnectionCloseReceipt{}, err
		}
		participants, err = c.store.ClaimParticipants(ctx, command, participants)
		if err != nil {
			return tagaccess.ConnectionCloseReceipt{}, err
		}
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, AuthorizationWatchdogTarget)
	defer cancel()
	if err := c.broker.Dispatch(dispatchCtx, command, participants); err != nil {
		return tagaccess.ConnectionCloseReceipt{}, err
	}
	receipt := tagaccess.ConnectionCloseReceipt{
		ReceiptID: ulid.Make().String(), Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		SessionWorkspaceGeneration: command.SessionWorkspaceGeneration,
		TargetDigest:               command.TargetDigest, CompletedAt: c.now().UTC(),
	}
	if err := c.store.Save(ctx, command, receipt); err != nil {
		if existing, ok, loadErr := c.store.Load(ctx, command); loadErr == nil && ok && receiptMatchesCommand(existing, command) {
			return existing, nil
		}
		return tagaccess.ConnectionCloseReceipt{}, err
	}
	// Return the value read through the durable boundary. Concurrent delivery
	// may have won the unique key with a different receipt ID after our first
	// Load; returning the locally generated value would fabricate evidence that
	// is not actually persisted.
	persisted, ok, err := c.store.Load(ctx, command)
	if err != nil {
		return tagaccess.ConnectionCloseReceipt{}, err
	}
	if !ok || !receiptMatchesCommand(persisted, command) {
		return tagaccess.ConnectionCloseReceipt{}, errors.New("completed connection-close receipt was not durably readable")
	}
	return persisted, nil
}

func normalizeParticipantIDs(participants []string) ([]string, error) {
	unique := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		if participant == "" {
			return nil, errors.New("empty realtime close participant")
		}
		unique[participant] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, errors.New("no active realtime instance can acknowledge close")
	}
	normalized := make([]string, 0, len(unique))
	for participant := range unique {
		normalized = append(normalized, participant)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func receiptMatchesCommand(receipt tagaccess.ConnectionCloseReceipt, command tagaccess.ConnectionCloseCommand) bool {
	return receipt.ReceiptID != "" && receipt.Source == command.Source && receipt.DeliveryID == command.DeliveryID &&
		receipt.CorrelationID == command.CorrelationID && receipt.WorkspaceID == command.WorkspaceID &&
		receipt.AuthorityVersion == command.AuthorityVersion && receipt.IdentityRestrictionVersion == command.IdentityRestrictionVersion &&
		receipt.SessionWorkspaceGeneration == command.SessionWorkspaceGeneration &&
		receipt.TargetDigest == command.TargetDigest && !receipt.CompletedAt.IsZero()
}

func (h *Hub) SetInstanceID(instanceID string) {
	if h == nil || instanceID == "" {
		return
	}
	h.mu.Lock()
	h.instanceID = instanceID
	h.mu.Unlock()
}

func (h *Hub) InstanceID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.instanceID
}

// ApplyConnectionClose fences matching clients before removing their rooms or
// writing the protocol close frame. Duplicate and out-of-order delivery is
// safe: already-fenced clients are ignored, and Workspace-version commands
// never close a socket authorized at the command's new version or later.
func (h *Hub) ApplyConnectionClose(_ context.Context, command tagaccess.ConnectionCloseCommand) error {
	if h == nil {
		return errors.New("nil realtime hub")
	}
	h.authorize.Lock()
	defer h.authorize.Unlock()
	h.mu.Lock()
	h.fences.install(command)
	clients := make([]*Client, 0)
	for client := range h.clients {
		if clientMatchesCloseCommand(client, command) && client.revoked.CompareAndSwap(false, true) {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()

	code, reason := connectionClosePolicy(command)
	for _, client := range clients {
		// revoked is already true, so every inbound/outbound enqueue seam is
		// closed before room removal and before the wire close.
		client.frameMu.Lock()
		client.frameMu.Unlock()
		h.removeClient(client)
		client.closeTransport(code, reason)
	}
	return nil
}

func (h *Hub) closeAllForBrokerShutdown() {
	if h == nil {
		return
	}
	h.authorize.Lock()
	defer h.authorize.Unlock()
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.requiresAuthorizationFeed() && client.revoked.CompareAndSwap(false, true) {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()
	for _, client := range clients {
		client.frameMu.Lock()
		client.frameMu.Unlock()
		h.removeClient(client)
		client.closeTransport(CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
	}
}

func clientMatchesCloseCommand(client *Client, command tagaccess.ConnectionCloseCommand) bool {
	metadata := client.metadata
	if metadata.ConnectionID == "" || metadata.VIBESUserID == "" || metadata.WorkspaceID == "" {
		return false
	}
	if command.Source == tagaccess.ConnectionCloseWorkspaceProjection && metadata.AuthorityVersion >= command.AuthorityVersion {
		return false
	}
	for _, target := range command.Targets {
		if target.WorkspaceID != "" && metadata.WorkspaceID != target.WorkspaceID {
			continue
		}
		switch target.Scope {
		case tagaccess.ConnectionCloseSession:
			if metadata.VIBESUserID == target.VIBESUserID && metadata.VIBESSessionID == target.VIBESSessionID {
				return true
			}
		case tagaccess.ConnectionCloseSessionWorkspace:
			if metadata.VIBESUserID == target.VIBESUserID && metadata.VIBESSessionID == target.VIBESSessionID &&
				metadata.WorkspaceID == target.WorkspaceID && metadata.SessionWorkspaceGeneration < command.SessionWorkspaceGeneration {
				return true
			}
		case tagaccess.ConnectionCloseAccount:
			if metadata.VIBESUserID == target.VIBESUserID {
				return true
			}
		case tagaccess.ConnectionCloseMembership:
			if metadata.WorkspaceID == target.WorkspaceID && metadata.VIBESUserID == target.VIBESUserID {
				return true
			}
		case tagaccess.ConnectionCloseWorkspace:
			if metadata.WorkspaceID == target.WorkspaceID {
				return true
			}
		}
	}
	return false
}

func connectionClosePolicy(command tagaccess.ConnectionCloseCommand) (int, string) {
	if command.Source == tagaccess.ConnectionCloseIdentityRestriction {
		return CloseCodeSessionRevoked, "vibes_session_revoked"
	}
	if command.Source == tagaccess.ConnectionCloseSessionWorkspaceSupersession {
		return CloseCodeAuthorizationChanged, fmt.Sprintf("session_workspace_generation_%d_required", command.SessionWorkspaceGeneration)
	}
	return CloseCodeAuthorizationChanged, fmt.Sprintf("authority_version_%d_required", command.AuthorityVersion)
}

func (c *Client) closeTransport(code int, reason string) {
	if c == nil || c.conn == nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	deadline := time.Now().Add(c.writeTimeout())
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), deadline)
	_ = c.conn.Close()
}

func (c *Client) writeTimeout() time.Duration {
	if c != nil && (c.metadata.TagSessionID != "" || c.metadata.VIBESSessionID != "") {
		return authorizationSocketWriteTimeout
	}
	return writeWait
}

func (c *Client) trySend(frame []byte) bool {
	if !c.authorizationLeaseValid() {
		return false
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed || c.revoked.Load() || !c.authorizationLeaseValid() {
		return false
	}
	select {
	case c.send <- frame:
		return true
	default:
		return false
	}
}
