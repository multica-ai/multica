package daemonws

import (
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/multica-ai/multica/server/internal/realtime"
)

// RelayNotifier sends daemon wakeup hints to the local daemon hub and, when
// Redis is configured, publishes the same hint through the shared realtime
// relay so every API node can attempt local delivery.
type RelayNotifier struct {
	local *Hub
	relay realtime.RelayPublisher
}

func NewRelayNotifier(local *Hub, relay realtime.RelayPublisher) *RelayNotifier {
	return &RelayNotifier{local: local, relay: relay}
}

func (n *RelayNotifier) NotifyTaskAvailable(runtimeID, taskID string) {
	if runtimeID == "" {
		return
	}
	eventID := ulid.Make().String()
	if n.local != nil {
		n.local.notifyTaskAvailable(runtimeID, taskID, eventID)
	}
	if n.relay == nil {
		return
	}
	frame, err := taskAvailableFrame(runtimeID, taskID)
	if err != nil {
		M.WakeupPublishErrors.Add(1)
		return
	}
	shardKey := taskID
	if shardKey == "" {
		shardKey = eventID
	}
	if err := n.relay.PublishWithID(realtime.ScopeDaemonRuntime, shardKey, "", frame, eventID); err != nil {
		M.WakeupPublishErrors.Add(1)
		slog.Warn("daemon websocket wakeup publish failed", "error", err, "runtime_id", runtimeID, "task_id", taskID)
		return
	}
	M.WakeupPublishedTotal.Add(1)
}

func (n *RelayNotifier) NotifyRuntimeProfilesChanged(workspaceID, profileID string) {
	if workspaceID == "" {
		return
	}
	eventID := ulid.Make().String()
	if n.local != nil {
		n.local.notifyRuntimeProfilesChanged(workspaceID, profileID, eventID)
	}
	if n.relay == nil {
		return
	}
	frame, err := runtimeProfilesChangedFrame(workspaceID, profileID)
	if err != nil {
		M.WakeupPublishErrors.Add(1)
		return
	}
	if err := n.relay.PublishWithID(realtime.ScopeDaemonRuntime, workspaceID, "", frame, eventID); err != nil {
		M.WakeupPublishErrors.Add(1)
		slog.Warn("daemon websocket profile refresh publish failed", "error", err, "workspace_id", workspaceID, "runtime_profile_id", profileID)
		return
	}
	M.WakeupPublishedTotal.Add(1)
}

func (n *RelayNotifier) NotifyWorkspacesChanged(userID string) {
	if userID == "" {
		return
	}
	eventID := ulid.Make().String()
	if n.local != nil {
		n.local.notifyWorkspacesChanged(userID, eventID)
	}
	if n.relay == nil {
		return
	}
	frame, err := workspacesChangedFrame()
	if err != nil {
		M.WakeupPublishErrors.Add(1)
		return
	}
	// ScopeDaemonRuntime is the relay's daemon-only transport scope; the frame
	// type tells Hub.DeliverDaemonRuntime whether scopeID is a runtime,
	// workspace, or user key. Keeping one transport scope preserves compatibility
	// with existing relay consumers while the hub enforces user-scoped delivery.
	if err := n.relay.PublishWithID(realtime.ScopeDaemonRuntime, userID, "", frame, eventID); err != nil {
		M.WakeupPublishErrors.Add(1)
		slog.Warn("daemon websocket workspace refresh publish failed", "error", err, "user_id", userID)
		return
	}
	M.WakeupPublishedTotal.Add(1)
}

// NotifyPendingWork fans a runtime-scoped "heartbeat now" hint out to the local
// hub and, when Redis is configured, through the relay so the API node that
// actually holds the daemon's WebSocket delivers it (MUL-5444). Shard key is the
// runtime ID: hints for one runtime stay ordered relative to each other, and a
// dropped hint only costs the daemon its normal heartbeat delay.
func (n *RelayNotifier) NotifyPendingWork(runtimeID, kind string) {
	if runtimeID == "" {
		return
	}
	eventID := ulid.Make().String()
	if n.local != nil {
		n.local.notifyPendingWork(runtimeID, kind, eventID)
	}
	if n.relay == nil {
		return
	}
	frame, err := pendingWorkFrame(runtimeID, kind)
	if err != nil {
		M.WakeupPublishErrors.Add(1)
		return
	}
	if err := n.relay.PublishWithID(realtime.ScopeDaemonRuntime, runtimeID, "", frame, eventID); err != nil {
		M.WakeupPublishErrors.Add(1)
		slog.Warn("daemon websocket pending work publish failed", "error", err, "runtime_id", runtimeID, "kind", kind)
		return
	}
	M.WakeupPublishedTotal.Add(1)
}

// NotifyRuntimeGone invalidates a deleted runtime on the local daemon
// connection and on any connection held by another API node.
func (n *RelayNotifier) NotifyRuntimeGone(runtimeID string) {
	if runtimeID == "" {
		return
	}
	eventID := ulid.Make().String()
	if n.local != nil {
		n.local.notifyRuntimeGone(runtimeID, eventID)
	}
	if n.relay == nil {
		return
	}
	frame, err := runtimeGoneFrame(runtimeID)
	if err != nil {
		M.RuntimeGonePublishErrors.Add(1)
		return
	}
	if err := n.relay.PublishWithID(realtime.ScopeDaemonRuntime, runtimeID, "", frame, eventID); err != nil {
		M.RuntimeGonePublishErrors.Add(1)
		slog.Warn("daemon websocket runtime-gone publish failed", "error", err, "runtime_id", runtimeID)
		return
	}
	M.RuntimeGonePublishedTotal.Add(1)
}
