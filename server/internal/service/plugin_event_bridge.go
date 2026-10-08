package service

import (
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Translating the internal event bus into the seven events a plugin may
// subscribe to.
//
// Two vocabularies on purpose. The internal names are realtime transport
// concerns and change when the frontend needs them to; the plugin names are a
// published contract. Mapping in one place means a rename on our side is a
// change to this file rather than a break in every installed plugin.

// EventSink is the half of the dispatcher the bridge needs.
//
// An interface so the vocabulary mapping can be tested without a network: what
// this file decides is WHICH plugin event an internal one becomes, and that is
// worth asserting on its own rather than through a live endpoint.
type EventSink interface {
	Dispatch(eventType, workspaceID string, payload any)
}

// SubscribePluginEvents wires the dispatcher onto the bus.
//
// Bus.Publish calls its listeners INLINE, on the goroutine of the request that
// published — so everything these closures do must be cheap and non-blocking.
// They extract an id and hand off; the network call happens on a worker.
func SubscribePluginEvents(bus *events.Bus, dispatcher EventSink) {
	if bus == nil || dispatcher == nil {
		return
	}

	// The listener does the least it possibly can: hand over the payload
	// unexamined. Extracting the issue id here would put a JSON round-trip of a
	// full issue body on the publishing request's goroutine, for every one of
	// these events, in every workspace — including deployments where plugins are
	// switched off entirely. It happens on a worker instead, after the flag
	// check, where it costs nothing anyone is waiting for.
	forward := func(pluginEvent string) events.Handler {
		return func(e events.Event) {
			dispatcher.Dispatch(pluginEvent, e.WorkspaceID, e.Payload)
		}
	}

	bus.Subscribe(protocol.EventIssueCreated, forward(plugincontract.EventIssueCreated))
	bus.Subscribe(protocol.EventCommentCreated, forward(plugincontract.EventCommentCreated))
	bus.Subscribe(protocol.EventTaskRunning, forward(plugincontract.EventTaskStarted))
	bus.Subscribe(protocol.EventTaskCompleted, forward(plugincontract.EventTaskCompleted))
	bus.Subscribe(protocol.EventTaskFailed, forward(plugincontract.EventTaskFailed))

	// issue.status_changed has no event of its own internally: a status change
	// is an issue:updated carrying status_changed=true. Deriving it here rather
	// than adding a second publish keeps one write producing one internal
	// event, and lets a plugin subscribe to the specific thing it cares about
	// instead of filtering every field change itself.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		dispatcher.Dispatch(plugincontract.EventIssueUpdated, e.WorkspaceID, e.Payload)
		// A map lookup, not a parse: cheap enough for the request goroutine.
		if payloadFlag(e.Payload, "status_changed") {
			dispatcher.Dispatch(plugincontract.EventIssueStatusChanged, e.WorkspaceID, e.Payload)
		}
	})
}

// issueIDFromPayload finds the issue an event is about, so the callback token
// issued for the hook can be narrowed to it. A payload with no issue yields the
// zero UUID, which simply means the grant is not issue-scoped.
func issueIDFromPayload(payload any) pgtype.UUID {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return pgtype.UUID{}
	}
	var shape struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
		Comment struct {
			IssueID string `json:"issue_id"`
		} `json:"comment"`
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(encoded, &shape); err != nil {
		return pgtype.UUID{}
	}
	for _, candidate := range []string{shape.Issue.ID, shape.Comment.IssueID, shape.IssueID} {
		if candidate == "" {
			continue
		}
		if parsed, err := parseUUIDValue(candidate); err == nil {
			return parsed
		}
	}
	return pgtype.UUID{}
}

func payloadFlag(payload any, key string) bool {
	if fields, ok := payload.(map[string]any); ok {
		flag, _ := fields[key].(bool)
		return flag
	}
	return false
}
