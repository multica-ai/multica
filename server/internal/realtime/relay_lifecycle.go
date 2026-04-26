package realtime

import (
	"context"

	"github.com/oklog/ulid/v2"
)

// ManagedRelay is a Redis-backed realtime relay with explicit goroutine
// lifecycle management.
type ManagedRelay interface {
	RelayPublisher
	Broadcaster

	NodeID() string
	Start(context.Context)
	Stop()
	Wait()
}

// MirroredRelay is a temporary rollout helper: it starts two relay backends,
// reads from both, and publishes every event to both with the same event id.
// Client-side dedup keeps loopback delivery idempotent.
type MirroredRelay struct {
	primary ManagedRelay
	mirror  ManagedRelay
}

func NewMirroredRelay(primary, mirror ManagedRelay) *MirroredRelay {
	return &MirroredRelay{primary: primary, mirror: mirror}
}

func (r *MirroredRelay) NodeID() string {
	return r.primary.NodeID()
}

func (r *MirroredRelay) Start(ctx context.Context) {
	r.primary.Start(ctx)
	r.mirror.Start(ctx)
	M.NodeID.Store(r.NodeID())
}

func (r *MirroredRelay) Stop() {
	r.primary.Stop()
	r.mirror.Stop()
}

func (r *MirroredRelay) Wait() {
	r.primary.Wait()
	r.mirror.Wait()
}

func (r *MirroredRelay) BroadcastToScope(scopeType, scopeID string, message []byte) {
	r.PublishWithID(scopeType, scopeID, "", message, ulid.Make().String())
}

func (r *MirroredRelay) BroadcastToWorkspace(workspaceID string, message []byte) {
	r.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (r *MirroredRelay) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	r.PublishWithID(ScopeUser, userID, exclude, message, ulid.Make().String())
}

func (r *MirroredRelay) Broadcast(message []byte) {
	r.PublishWithID("global", "all", "", message, ulid.Make().String())
}

func (r *MirroredRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) {
	r.primary.PublishWithID(scopeType, scopeID, exclude, frame, id)
	r.mirror.PublishWithID(scopeType, scopeID, exclude, frame, id)
}

var _ ManagedRelay = (*RedisRelay)(nil)
var _ ManagedRelay = (*ShardedStreamRelay)(nil)
var _ ManagedRelay = (*MirroredRelay)(nil)
