package realtime

// Broadcaster is the abstraction every realtime event producer should depend on
// instead of *Hub directly. Today it is satisfied by the in-memory *Hub. The
// horizontal-scaling plan (see issue MUL-1138) will introduce additional
// implementations such as a Redis-backed relay and a feature-flagged
// dual-write broadcaster, all without touching producer call sites.
//
// Phase 0 deliberately keeps the surface identical to the methods producers
// already call on *Hub so the migration is a pure type change.
type Broadcaster interface {
	// BroadcastToWorkspace fans a message out to every connection currently
	// registered against workspaceID on this node.
	BroadcastToWorkspace(workspaceID string, message []byte)

	// SendToUser fans a message out to every connection belonging to userID
	// on this node, optionally skipping the given workspace room (which is
	// usually already covered by a parallel BroadcastToWorkspace).
	SendToUser(userID string, message []byte, excludeWorkspace ...string)

	// Broadcast fans a message out to every connection on this node.
	// Currently used for daemon:* events that have no workspace scope.
	Broadcast(message []byte)
}

// Compile-time assertion that *Hub continues to satisfy Broadcaster.
var _ Broadcaster = (*Hub)(nil)
