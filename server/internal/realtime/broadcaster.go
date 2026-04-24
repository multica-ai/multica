package realtime

// Scope types recognised by the broadcaster. Producers and consumers should
// use these constants rather than raw strings so a typo can never silently
// route an event to a non-existent room.
const (
	ScopeWorkspace = "workspace"
	ScopeUser      = "user"
	ScopeTask      = "task"
	ScopeChat      = "chat"
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

// Compile-time assertion that *Hub continues to satisfy Broadcaster.
var _ Broadcaster = (*Hub)(nil)
