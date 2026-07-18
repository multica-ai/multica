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
