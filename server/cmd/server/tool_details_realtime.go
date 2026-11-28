package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// restrictedToolMessage returns the tool call e carries when it comes from an
// agent that restricts its tool details. Prose and reasoning from the same
// agent are not tool details and take the ordinary route.
func restrictedToolMessage(e events.Event) (protocol.TaskMessagePayload, bool) {
	if !e.ToolDetailsRestricted || e.Type != protocol.EventTaskMessage {
		return protocol.TaskMessagePayload{}, false
	}
	payload, ok := e.Payload.(protocol.TaskMessagePayload)
	if !ok || (payload.Type != "tool_use" && payload.Type != "tool_result") {
		return protocol.TaskMessagePayload{}, false
	}
	return payload, true
}

// sendRestrictedToolMessage delivers a tool call whose input and output only
// e.ToolDetailReaders may read, to the same people the ordinary route would.
// A chat's creator and a private project's audience each get the one frame
// they may read through their own member scope. A run the whole workspace
// sees goes to the workspace without its details, and its readers also get
// the whole call: their clients keep it over the stripped copy of the same
// seq, which costs a handful of extra frames rather than one per member.
func sendRestrictedToolMessage(b realtime.Broadcaster, e events.Event, payload protocol.TaskMessagePayload, audience privateProjectAudience) {
	full, err := taskMessageFrame(e, payload)
	if err != nil {
		slog.Error("failed to marshal event", "event_type", e.Type, "error", err)
		return
	}
	stripped, err := taskMessageFrame(e, payload.WithoutToolDetails())
	if err != nil {
		slog.Error("failed to marshal event", "event_type", e.Type, "error", err)
		return
	}
	frameFor := func(userID string) []byte {
		if e.ToolDetailReaders[userID] {
			return full
		}
		return stripped
	}
	toMember := func(userID string, frame []byte) {
		b.BroadcastToScope(realtime.ScopeWorkspaceMember, realtime.WorkspaceMemberScopeID(e.WorkspaceID, userID), frame)
	}

	if isChatPrivateEvent(e) {
		if e.ChatSessionOwnerID == "" || e.WorkspaceID == "" {
			slog.Warn("realtime: dropping chat event without session owner",
				"event_type", e.Type, "chat_session_id", e.ChatSessionID, "task_id", e.TaskID)
			return
		}
		realtime.M.RecordEvent(e.Type)
		toMember(e.ChatSessionOwnerID, frameFor(e.ChatSessionOwnerID))
		return
	}
	if e.WorkspaceID == "" {
		return
	}
	if audience != nil {
		ctx, cancel := context.WithTimeout(context.Background(), audienceResolveTimeout)
		recipients, private, err := audience.Audience(ctx, e)
		cancel()
		if err != nil {
			logAudienceFailure(e, err)
			return
		}
		if private {
			realtime.M.RecordEvent(e.Type)
			for _, userID := range recipients {
				toMember(userID, frameFor(userID))
			}
			return
		}
	}
	realtime.M.RecordEvent(e.Type)
	readers := make([]string, 0, len(e.ToolDetailReaders))
	for userID, ok := range e.ToolDetailReaders {
		if ok {
			readers = append(readers, userID)
		}
	}
	sort.Strings(readers)
	// The full frames go first: a client that keeps the first copy of a seq,
	// as clients built before details_hidden do, then keeps the full one.
	for _, userID := range readers {
		toMember(userID, full)
	}
	b.BroadcastToWorkspace(e.WorkspaceID, stripped)
}

func taskMessageFrame(e events.Event, payload protocol.TaskMessagePayload) ([]byte, error) {
	return json.Marshal(map[string]any{"type": e.Type, "payload": payload, "actor_id": e.ActorID, "actor_type": e.ActorType})
}
