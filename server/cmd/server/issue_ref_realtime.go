package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// An event reaches the audience of the issue it is about, but a duplicate mark
// (MUL-7349) also names a second issue, which can sit in a private project
// part of that audience may not read (MUL-9932). An event that moves the mark
// reaches each recipient with only the references it may read. Any other
// event names the original only because the issue still points at it, which
// every client already has from its own reads, so one frame with those
// references stripped serves everyone.

// issueRefEvents may name issues other than the one they are about.
var issueRefEvents = map[string]bool{
	protocol.EventIssueCreated:    true,
	protocol.EventIssueUpdated:    true,
	protocol.EventActivityCreated: true,
}

// duplicateMarkKeys are an issue event's top-level ids of both ends of a
// duplicate mark change.
var duplicateMarkKeys = []string{"duplicate_of_issue_id", "prev_duplicate_of_issue_id"}

// visitIssueRefs calls strip with each reference top, an event payload, makes
// to an issue other than its own: a duplicate mark's ends and resolved
// original on an issue event, a duplicate activity's other issue. "" stands
// for an activity reference whose issue cannot be told from its details. A
// reference strip returns true for is removed from top: an issue event's id
// becomes null, its original null (or, with omitOriginal, absent, which
// clients merging the issue into their cache read as unchanged), and an
// activity's details lose the id and identifier.
func visitIssueRefs(eventType string, top map[string]json.RawMessage, omitOriginal bool, strip func(id string) bool) {
	if eventType == protocol.EventActivityCreated {
		entry := jsonObject(top["entry"])
		action := jsonString(entry["action"])
		if id, named := handler.ActivityIssueRef(action, entry["details"]); named && strip(util.UUIDToString(id)) {
			entry["details"] = handler.StripActivityIssueRef(action, entry["details"])
			top["entry"], _ = json.Marshal(entry)
		}
		return
	}
	null := json.RawMessage("null")
	for _, key := range duplicateMarkKeys {
		if id := jsonString(top[key]); id != "" && strip(id) {
			top[key] = null
		}
	}
	issue := jsonObject(top["issue"])
	if id := jsonString(jsonObject(issue["duplicate_of"])["id"]); id != "" && strip(id) {
		if omitOriginal {
			delete(issue, "duplicate_of")
		} else {
			issue["duplicate_of"] = null
		}
		top["issue"], _ = json.Marshal(issue)
	}
}

// duplicateMarkMoved reports whether e changes what an issue is a duplicate
// of: a duplicate activity, or an issue event whose two ends differ.
func duplicateMarkMoved(e events.Event) bool {
	if e.Type == protocol.EventActivityCreated {
		return true
	}
	top := jsonObject(e.Payload)
	return jsonString(top[duplicateMarkKeys[0]]) != jsonString(top[duplicateMarkKeys[1]])
}

// strippedFrame marshals e for the wire with the references strip selects
// removed (see visitIssueRefs).
func strippedFrame(e events.Event, omitOriginal bool, strip func(id string) bool) ([]byte, error) {
	payload := jsonObject(projectOutbound(e.Type, e.Payload))
	visitIssueRefs(e.Type, payload, omitOriginal, strip)
	return json.Marshal(map[string]any{"type": e.Type, "payload": payload, "actor_id": e.ActorID, "actor_type": e.ActorType})
}

// jsonObject reads v, a payload value or its raw JSON, as a JSON object; nil
// when it is not one.
func jsonObject(v any) map[string]json.RawMessage {
	raw, ok := v.(json.RawMessage)
	if !ok {
		var err error
		if raw, err = json.Marshal(v); err != nil {
			return nil
		}
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// IssueRefReaders maps each issue e names besides its own (visitIssueRefs)
// that not every member may read to the user ids who may: the members of its
// private project. A reference whose issue is gone, or cannot be told, maps to
// nobody, as in the HTTP reads. nil means every recipient may read every
// reference.
func (a *dbPrivateProjectAudience) IssueRefReaders(ctx context.Context, e events.Event) (map[string]map[string]bool, error) {
	if !issueRefEvents[e.Type] || e.WorkspaceID == "" {
		return nil, nil
	}
	ws, err := a.workspace(ctx, e.WorkspaceID)
	if err != nil || len(ws.projects) == 0 {
		return nil, err
	}
	var refs []string
	visitIssueRefs(e.Type, jsonObject(e.Payload), false, func(id string) bool {
		refs = append(refs, id)
		return false
	})
	if len(refs) == 0 {
		return nil, nil
	}
	var ids []pgtype.UUID
	for _, ref := range refs {
		if id, err := util.ParseUUID(ref); err == nil {
			ids = append(ids, id)
		}
	}
	projects := map[string]string{}
	if len(ids) > 0 {
		rows, err := a.q.ListIssueProjectIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			projects[util.UUIDToString(row.ID)] = util.UUIDToString(row.ProjectID)
		}
	}
	var out map[string]map[string]bool
	for _, ref := range refs {
		projectID, found := projects[ref]
		members, private := ws.projects[projectID]
		if found && !private {
			continue
		}
		set := make(map[string]bool, len(members))
		for _, userID := range members {
			set[userID] = true
		}
		if out == nil {
			out = map[string]map[string]bool{}
		}
		out[ref] = set
	}
	return out, nil
}

// Members lists the members of a workspace who receive realtime events: all
// but guests (MUL-9420).
func (a *dbPrivateProjectAudience) Members(ctx context.Context, workspaceID string) ([]string, error) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, nil
	}
	rows, err := a.q.ListMembers(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, m := range rows {
		if !projectaccess.IsGuestRole(m.Role) {
			out = append(out, util.UUIDToString(m.UserID))
		}
	}
	return out, nil
}

// sendPerReader delivers e to each recipient, stripped of the references
// refReaders says it may not read. full is the frame for a reader of all of
// them.
func sendPerReader(b realtime.Broadcaster, e events.Event, full []byte, recipients []string, refReaders map[string]map[string]bool) {
	frames := map[string][]byte{}
	for _, userID := range recipients {
		var hide []string
		for ref, readers := range refReaders {
			if !readers[userID] {
				hide = append(hide, ref)
			}
		}
		frame := full
		if len(hide) > 0 {
			sort.Strings(hide)
			key := strings.Join(hide, ",")
			var ok bool
			if frame, ok = frames[key]; !ok {
				hidden := make(map[string]bool, len(hide))
				for _, ref := range hide {
					hidden[ref] = true
				}
				var err error
				if frame, err = strippedFrame(e, false, func(id string) bool { return hidden[id] }); err != nil {
					slog.Error("failed to marshal event", "event_type", e.Type, "error", err)
					return
				}
				frames[key] = frame
			}
		}
		b.BroadcastToScope(realtime.ScopeWorkspaceMember, realtime.WorkspaceMemberScopeID(e.WorkspaceID, userID), frame)
	}
}
