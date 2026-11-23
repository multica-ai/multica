package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const ppDeletedIssue = "00000000-0000-0000-0000-00000000dead"

// framesByMember returns each member's frame payload, failing on a workspace
// broadcast or a member reached twice.
func framesByMember(t *testing.T, fb *fakeBroadcaster) map[string]map[string]json.RawMessage {
	t.Helper()
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("broadcast to the whole workspace: %s", fb.workspaceCalls[0].msg)
	}
	out := map[string]map[string]json.RawMessage{}
	for _, c := range fb.scopeCalls {
		if c.scopeType != realtime.ScopeWorkspaceMember {
			continue
		}
		userID := strings.TrimPrefix(c.scopeID, ppWorkspace+":")
		if _, dup := out[userID]; dup {
			t.Fatalf("%s reached twice", userID)
		}
		var frame struct {
			Payload map[string]json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(c.msg, &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		out[userID] = frame.Payload
	}
	return out
}

func TestRegisterListeners_DuplicateMarkReachesEachMemberAsTheyMayReadIt(t *testing.T) {
	// A public issue moves its duplicate mark from an issue in the second
	// private project to one in the first.
	fb := publishPrivateAudience(events.Event{Type: protocol.EventIssueUpdated, Payload: map[string]any{
		"issue": map[string]any{"id": ppPublicIssue, "project_id": ppPublic, "title": "public",
			"duplicate_of": map[string]any{"id": ppSecretIssue, "identifier": "SECRET-1", "title": "secret title"}},
		"duplicate_of_issue_id":        ppSecretIssue,
		"prev_duplicate_of_issue_id":   ppSecretIssue2,
		"prev_duplicate_of_identifier": "SECRET2-1",
	}})
	frames := framesByMember(t, fb)
	if len(frames) != 4 {
		t.Fatalf("delivered to %d members, want the 4 non-guest members", len(frames))
	}
	for userID, want := range map[string]struct{ next, prev bool }{
		ppMember: {next: true},
		ppOther:  {prev: true},
		ppShared: {next: true, prev: true},
		ppPlain:  {},
	} {
		p := frames[userID]
		if _, leaked := p["prev_duplicate_of_identifier"]; leaked {
			t.Errorf("%s: prev_duplicate_of_identifier reached the wire", userID)
		}
		if got := jsonString(p["duplicate_of_issue_id"]) == ppSecretIssue; got != want.next {
			t.Errorf("%s: duplicate_of_issue_id = %s, want visible=%v", userID, p["duplicate_of_issue_id"], want.next)
		}
		if got := jsonString(p["prev_duplicate_of_issue_id"]) == ppSecretIssue2; got != want.prev {
			t.Errorf("%s: prev_duplicate_of_issue_id = %s, want visible=%v", userID, p["prev_duplicate_of_issue_id"], want.prev)
		}
		issue := jsonObject(p["issue"])
		if got := jsonString(jsonObject(issue["duplicate_of"])["identifier"]) == "SECRET-1"; got != want.next {
			t.Errorf("%s: issue.duplicate_of = %s, want visible=%v", userID, issue["duplicate_of"], want.next)
		}
		if jsonString(issue["title"]) != "public" {
			t.Errorf("%s: issue = %s, want the rest kept", userID, p["issue"])
		}
	}
}

func TestRegisterListeners_DuplicateActivityReachesEachMemberAsTheyMayReadIt(t *testing.T) {
	// Through the producer the activity log uses, which leaves the details
	// whole for the fanout to project.
	activity := func(issueID, action string, details map[string]string) *fakeBroadcaster {
		body, _ := json.Marshal(details)
		bus := events.New()
		fb := &fakeBroadcaster{}
		registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
		publishActivityEvent(bus, events.Event{WorkspaceID: ppWorkspace}, db.ActivityLog{IssueID: ppUUID(issueID), Action: action, Details: body})
		return fb
	}
	details := func(p map[string]json.RawMessage) map[string]string {
		var d map[string]string
		_ = json.Unmarshal(jsonObject(p["entry"])["details"], &d)
		return d
	}

	t.Run("original in a private project", func(t *testing.T) {
		frames := framesByMember(t, activity(ppPublicIssue, "duplicate_marked",
			map[string]string{"original_id": ppSecretIssue, "original_identifier": "SECRET-1", "to": "cancelled"}))
		for userID, visible := range map[string]bool{ppMember: true, ppShared: true, ppOther: false, ppPlain: false} {
			d := details(frames[userID])
			if (d["original_identifier"] == "SECRET-1" && d["original_id"] == ppSecretIssue) != visible {
				t.Errorf("%s: details = %v, want visible=%v", userID, d, visible)
			}
			if d["to"] != "cancelled" {
				t.Errorf("%s: details = %v, want the rest kept", userID, d)
			}
		}
	})

	// Both issues in one private project: its whole audience keeps the
	// reference, so a client appending the entry shows it.
	t.Run("same private project", func(t *testing.T) {
		frames := framesByMember(t, activity(ppSecretIssue, "duplicate_added",
			map[string]string{"duplicate_id": ppSecretSibling, "duplicate_identifier": "SECRET-2"}))
		if len(frames) != 2 {
			t.Fatalf("delivered to %v, want the project's two members", frames)
		}
		for userID, p := range frames {
			if d := details(p); d["duplicate_identifier"] != "SECRET-2" {
				t.Errorf("%s: details = %v, want the reference kept", userID, d)
			}
		}
	})

	// A deleted original's project is unknown: only members of every private
	// project, who would see it wherever it was, keep it.
	t.Run("deleted original", func(t *testing.T) {
		frames := framesByMember(t, activity(ppPublicIssue, "duplicate_unmarked",
			map[string]string{"original_id": ppDeletedIssue, "original_identifier": "GONE-1", "reason": "original_deleted"}))
		for userID, visible := range map[string]bool{ppShared: true, ppMember: false, ppOther: false, ppPlain: false} {
			d := details(frames[userID])
			if (d["original_identifier"] == "GONE-1") != visible || d["reason"] != "original_deleted" {
				t.Errorf("%s: details = %v, want reference visible=%v and the reason kept", userID, d, visible)
			}
		}
	})
}

func TestRegisterListeners_ReadableDuplicateMarkKeepsWorkspaceFanout(t *testing.T) {
	fb := publishPrivateAudience(events.Event{Type: protocol.EventIssueUpdated, Payload: map[string]any{
		"issue":                 map[string]any{"id": ppPublicIssue, "project_id": nil, "duplicate_of": map[string]any{"id": ppPublicIssue}},
		"duplicate_of_issue_id": ppPublicIssue,
	}})
	if len(fb.workspaceCalls) != 1 || len(privateAudienceScopes(fb)) != 0 {
		t.Fatalf("workspace=%d scoped=%v, want one workspace broadcast", len(fb.workspaceCalls), privateAudienceScopes(fb))
	}
}
