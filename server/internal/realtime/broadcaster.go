package realtime

// Scope types recognised by the broadcaster. Producers and consumers should
// use these constants rather than raw strings so a typo can never silently
// route an event to a non-existent room.
const (
	ScopeWorkspace = "workspace"
	ScopeUser      = "user"
	ScopeTask      = "task"
	ScopeChat      = "chat"
	// ScopeDaemonRuntime routes daemon wakeup frames through the Redis relay.
	// It is consumed by the daemon WebSocket hub, not by browser clients.
	ScopeDaemonRuntime = "daemon_runtime"

	// ScopeWecomOutbound routes a WeCom reply to the replica holding that
	// installation's aibot WebSocket.
	//
	// WeCom is the one channel whose outbound has no REST path: a reply can
	// only leave the process that holds the socket, and the WS lease makes
	// that exactly one process. But chat:done is published on the in-process
	// events.Bus by whichever replica served the daemon's completion callback,
	// which is a load-balancer decision. Off-lease, the reply used to be
	// dropped (GH #7215, #6890, and the single-replica exception in
	// SELF_HOSTING.md).
	//
	// The scope id is the installation id, so this is the same shape as
	// ScopeDaemonRuntime: the thing that must act lives on one specific
	// replica, and the relay is how the others reach it.
	ScopeWecomOutbound = "wecom_outbound"
)

// Broadcaster is the abstraction every realtime event producer should depend
// on instead of *Hub directly.
//
// Phase 1 (MUL-1138) extends the surface with BroadcastToScope so events can
// be fanned out to high-frequency per-resource scopes (`task:{id}`,
// `chat:{id}`) instead of the whole workspace. The legacy methods continue to
// work and now route through BroadcastToScope under the hood.
type Broadcaster interface {
	// BroadcastToScope fans a message out to every connection currently
	// subscribed to ({scopeType, scopeID}) on this node.
	BroadcastToScope(scopeType, scopeID string, message []byte)

	// BroadcastToWorkspace is a back-compat shortcut for
	// BroadcastToScope("workspace", workspaceID, message).
	BroadcastToWorkspace(workspaceID string, message []byte)

	// SendToUser is a back-compat shortcut for
	// BroadcastToScope("user", userID, message). The optional
	// excludeWorkspace argument is preserved for the `member:added`
	// dedup path: connections whose workspaceID matches excludeWorkspace
	// are skipped.
	SendToUser(userID string, message []byte, excludeWorkspace ...string)

	// Broadcast fans a message out to every connection on this node.
	// Used for daemon:* events that have no workspace scope.
	Broadcast(message []byte)
}

// DaemonRuntimeDeliverer consumes daemon-runtime scoped relay frames.
type DaemonRuntimeDeliverer interface {
	DeliverDaemonRuntime(scopeID string, frame []byte, eventID string)
}

// WecomOutboundDeliverer consumes wecom-outbound scoped relay frames. Every
// replica receives every frame; the implementation delivers only when it holds
// that installation's socket and ignores the rest, so exactly one acts.
type WecomOutboundDeliverer interface {
	DeliverWecomOutbound(scopeID string, frame []byte, eventID string)
}

// Compile-time assertion that *Hub continues to satisfy Broadcaster.
var _ Broadcaster = (*Hub)(nil)
