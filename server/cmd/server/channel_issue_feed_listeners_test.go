package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type capturedFeedUpdate struct {
	workspaceID, issueID string
	text                 string
	created              bool
}

type fakeIssueFeed struct {
	mu      sync.Mutex
	updates []capturedFeedUpdate
}

func (f *fakeIssueFeed) PublishIssueUpdate(workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	text := render(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, capturedFeedUpdate{util.UUIDToString(workspaceID), util.UUIDToString(issueID), text, false})
}

func (f *fakeIssueFeed) PublishIssueCreated(workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	text := render(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, capturedFeedUpdate{util.UUIDToString(workspaceID), util.UUIDToString(issueID), text, true})
}

func (f *fakeIssueFeed) take() []capturedFeedUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.updates
	f.updates = nil
	return out
}

func TestChannelIssueFeed_RendersIssueAndCommentUpdates(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	ws, err := queries.GetWorkspace(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	user, err := queries.GetUser(ctx, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	// The comment path names the issue the way channel replies do.
	identifier := service.IssueIdentifier(ws.IssuePrefix, issue.Number)
	link := "https://app.example/" + ws.Slug + "/issues/" + issueID

	bus := events.New()
	feed := &fakeIssueFeed{}
	registerChannelIssueFeedListeners(bus, queries, feed, "https://app.example")

	// The assignee line reads the stored row, not the payload.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issueID, testUserID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	response := handler.IssueResponse{
		ID: issueID, WorkspaceID: testWorkspaceID, Identifier: identifier, Title: issue.Title, Status: "in_progress",
	}
	bus.Publish(events.Event{
		Type: protocol.EventIssueCreated, WorkspaceID: testWorkspaceID, ActorType: "member", ActorID: testUserID,
		Payload: map[string]any{"issue": handler.IssueResponse{
			ID: issueID, WorkspaceID: testWorkspaceID, CreatorType: "member", CreatorID: testUserID,
		}},
	})
	if got := feed.take(); len(got) != 1 || !got[0].created ||
		got[0].text != "🆕 "+identifier+" "+issue.Title+" — created by "+user.Name+"\nAssignee: "+user.Name+"\n"+link {
		t.Fatalf("issue created = %+v", got)
	}

	bus.Publish(events.Event{
		Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID, ActorType: "member", ActorID: testUserID,
		Payload: map[string]any{
			"issue": response, "status_changed": true, "prev_status": "todo", "assignee_changed": true,
		},
	})
	got := feed.take()
	want := "🔄 " + identifier + " " + issue.Title + " — updated by " + user.Name +
		"\nStatus: Todo → In progress\nAssignee: " + user.Name + "\n" + link
	if len(got) != 1 || got[0].issueID != issueID || got[0].text != want {
		t.Fatalf("issue update = %+v\nwant text %q", got, want)
	}

	// Background writers (an agent run finishing, PR automation) publish the
	// IssueToMap shape with a system actor.
	bus.Publish(events.Event{
		Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID, ActorType: "system",
		Payload: map[string]any{
			"issue":          map[string]any{"id": issueID, "workspace_id": testWorkspaceID, "status": "in_review"},
			"status_changed": true, "prev_status": "in_progress",
		},
	})
	got = feed.take()
	want = "🔄 " + identifier + " " + issue.Title + "\nStatus: In progress → In review\n" + link
	if len(got) != 1 || got[0].issueID != issueID || got[0].text != want {
		t.Fatalf("background issue update = %+v\nwant text %q", got, want)
	}

	bus.Publish(events.Event{
		Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID,
		Payload: map[string]any{"issue": response, "priority_changed": true},
	})
	if got := feed.take(); len(got) != 0 {
		t.Fatalf("priority-only update must not post: %+v", got)
	}

	comment := func(authorType, commentType, content string) events.Event {
		return events.Event{
			Type: protocol.EventCommentCreated, WorkspaceID: testWorkspaceID, ActorType: authorType, ActorID: testUserID,
			Payload: map[string]any{"comment": handler.CommentResponse{
				IssueID: issueID, AuthorType: authorType, AuthorID: testUserID, Type: commentType, Content: content,
			}},
		}
	}
	bus.Publish(comment("member", "comment", "Ping [@Ann](mention://member/"+testUserID+") "+strings.Repeat("x", issueFeedCommentRunes)))
	got = feed.take()
	if len(got) != 1 {
		t.Fatalf("comment updates = %+v", got)
	}
	text := got[0].text
	if !strings.HasPrefix(text, "💬 "+user.Name+" commented on "+identifier+" "+issue.Title+"\nPing @Ann xx") ||
		!strings.HasSuffix(text, "…\n"+link) {
		t.Fatalf("comment update text = %q", text)
	}

	bus.Publish(comment("system", "comment", "child done"))
	bus.Publish(comment("member", "status_change", "moved"))
	if got := feed.take(); len(got) != 0 {
		t.Fatalf("system and status-change comments must not post: %+v", got)
	}
}
