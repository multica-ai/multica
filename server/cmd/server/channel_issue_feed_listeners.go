package main

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issueFeedPublisher is the channel engine surface the feed listeners need.
type issueFeedPublisher interface {
	PublishIssueUpdate(workspaceID, issueID pgtype.UUID, render func(context.Context) string)
	PublishIssueCreated(workspaceID, issueID pgtype.UUID, render func(context.Context) string)
}

type issueFeedQueries interface {
	issuestatus.Querier
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	GetIssueInWorkspace(ctx context.Context, arg db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetSquadInWorkspace(ctx context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error)
}

// issueFeedCommentRunes caps a comment quoted into a chat; the link opens the rest.
const issueFeedCommentRunes = 500

var feedMentionLink = regexp.MustCompile(`\[([^\]]*)\]\(mention://[^)]*\)`)

// registerChannelIssueFeedListeners posts new issues, status changes,
// assignee changes and new comments into the IM chats that follow an issue or
// its project (/subscribe, or an /issue created in a group chat).
func registerChannelIssueFeedListeners(bus *events.Bus, q issueFeedQueries, feed issueFeedPublisher, appURL string) {
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		created, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}
		wsID, issueID := feedUUID(created.WorkspaceID), feedUUID(created.ID)
		feed.PublishIssueCreated(wsID, issueID, func(ctx context.Context) string {
			issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsID})
			if err != nil {
				return ""
			}
			header := "🆕 " + feedIssueLabel(feedIssueIdentifier(ctx, q, wsID, issue.Number), issue.Title)
			if actor := feedActorName(ctx, q, wsID, e.ActorType, e.ActorID); actor != "" {
				header += " — created by " + actor
			}
			body := ""
			if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
				if name := feedActorName(ctx, q, wsID, issue.AssigneeType.String, util.UUIDToString(issue.AssigneeID)); name != "" {
					body = "Assignee: " + name
				}
			}
			return feedMessage(header, body, feedIssueLink(ctx, q, wsID, appURL, created.ID))
		})
	})

	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// HTTP updates carry handler.IssueResponse; background writers (an
		// agent run moving the issue, PR automation) carry the IssueToMap
		// map. Only the id and new status are read from the payload; the
		// rest comes from the stored row.
		id, status, ok := issueIDAndStatusFromPayload(payload["issue"])
		if !ok {
			return
		}
		statusChanged, _ := payload["status_changed"].(bool)
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		if !statusChanged && !assigneeChanged {
			return
		}
		prevStatus, _ := payload["prev_status"].(string)
		wsID, issueID := feedUUID(e.WorkspaceID), feedUUID(id)
		feed.PublishIssueUpdate(wsID, issueID, func(ctx context.Context) string {
			issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsID})
			if err != nil {
				return ""
			}
			var lines []string
			if statusChanged {
				lines = append(lines, "Status: "+feedStatusName(ctx, q, wsID, prevStatus)+" → "+feedStatusName(ctx, q, wsID, status))
			}
			if assigneeChanged {
				assignee := "Unassigned"
				if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
					assignee = feedActorName(ctx, q, wsID, issue.AssigneeType.String, util.UUIDToString(issue.AssigneeID))
				}
				lines = append(lines, "Assignee: "+assignee)
			}
			header := "🔄 " + feedIssueLabel(feedIssueIdentifier(ctx, q, wsID, issue.Number), issue.Title)
			if actor := feedActorName(ctx, q, wsID, e.ActorType, e.ActorID); actor != "" {
				header += " — updated by " + actor
			}
			return feedMessage(header, strings.Join(lines, "\n"), feedIssueLink(ctx, q, wsID, appURL, id))
		})
	})

	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		var issueID, content, authorType, authorID, commentType string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID, content, authorType, authorID, commentType = c.IssueID, c.Content, c.AuthorType, c.AuthorID, c.Type
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			content, _ = c["content"].(string)
			authorType, _ = c["author_type"].(string)
			authorID, _ = c["author_id"].(string)
			commentType, _ = c["type"].(string)
		default:
			return
		}
		// Status-change and progress comments restate what the issue update
		// already posted; system comments are platform signals.
		if authorType == "system" || (commentType != "" && commentType != "comment") {
			return
		}
		wsID := feedUUID(e.WorkspaceID)
		feed.PublishIssueUpdate(wsID, feedUUID(issueID), func(ctx context.Context) string {
			issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: feedUUID(issueID), WorkspaceID: wsID})
			if err != nil {
				return ""
			}
			identifier := feedIssueIdentifier(ctx, q, wsID, issue.Number)
			header := "💬 " + feedIssueLabel(identifier, issue.Title)
			if author := feedActorName(ctx, q, wsID, authorType, authorID); author != "" {
				header = "💬 " + author + " commented on " + feedIssueLabel(identifier, issue.Title)
			}
			return feedMessage(header, feedCommentExcerpt(content), feedIssueLink(ctx, q, wsID, appURL, issueID))
		})
	})
}

// feedUUID parses an id read from an event. System actors carry an empty id,
// so a malformed one yields an invalid UUID instead of panicking the listener.
func feedUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

func feedMessage(header, body, link string) string {
	parts := []string{header}
	if body = strings.TrimSpace(body); body != "" {
		parts = append(parts, body)
	}
	if link != "" {
		parts = append(parts, link)
	}
	return strings.Join(parts, "\n")
}

func feedIssueLabel(identifier, title string) string {
	title = strings.TrimSpace(title)
	switch {
	case identifier == "":
		return title
	case title == "":
		return identifier
	default:
		return identifier + " " + title
	}
}

func feedIssueIdentifier(ctx context.Context, q issueFeedQueries, wsID pgtype.UUID, number int32) string {
	ws, err := q.GetWorkspace(ctx, wsID)
	if err != nil {
		return ""
	}
	return service.IssueIdentifier(ws.IssuePrefix, number)
}

// feedIssueLink links by UUID: a workspace without an issue prefix renders the
// identifier as "#N", which is not a routable issue key.
func feedIssueLink(ctx context.Context, q issueFeedQueries, wsID pgtype.UUID, appURL, issueID string) string {
	ws, err := q.GetWorkspace(ctx, wsID)
	if err != nil {
		return ""
	}
	return channel.IssueWebLink(appURL, ws.Slug, issueID)
}

// feedStatusName shows a custom status's catalog name and a humanized key for
// built-ins, which clients otherwise localize from the key.
func feedStatusName(ctx context.Context, q issueFeedQueries, wsID pgtype.UUID, status string) string {
	if status == "" {
		return "None"
	}
	if _, name := issuestatus.CategoryAndName(ctx, q, wsID, status); name != "" {
		return name
	}
	human := strings.ReplaceAll(status, "_", " ")
	return strings.ToUpper(human[:1]) + human[1:]
}

func feedActorName(ctx context.Context, q issueFeedQueries, wsID pgtype.UUID, actorType, actorID string) string {
	id := feedUUID(actorID)
	if !id.Valid {
		return ""
	}
	switch actorType {
	case "member":
		if u, err := q.GetUser(ctx, id); err == nil {
			return u.Name
		}
	case "agent":
		if a, err := q.GetAgent(ctx, id); err == nil && a.WorkspaceID == wsID {
			return a.Name
		}
	case "squad":
		if s, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: wsID}); err == nil {
			return s.Name
		}
	}
	return ""
}

// feedCommentExcerpt flattens mention links to their labels and caps the
// length for a chat message.
func feedCommentExcerpt(content string) string {
	text := strings.TrimSpace(feedMentionLink.ReplaceAllString(content, "$1"))
	if utf8.RuneCountInString(text) <= issueFeedCommentRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:issueFeedCommentRunes])) + "…"
}
