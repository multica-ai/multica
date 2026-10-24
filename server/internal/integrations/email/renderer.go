package email

import (
	"fmt"
	"html"
	"strings"
)

// RenderInput is the structured input to email rendering.
type RenderInput struct {
	NotifType     string // matches notif_type written by notification_listeners.go
	IssueTitle    string
	IssueID       string
	WorkspaceSlug string
}

// RenderOutput is the rendered email pair.
type RenderOutput struct {
	Subject string
	Text    string
	HTML    string
}

// Renderer turns inbox notification metadata into a sendable email.
// It does not localize: emails are English-only in phase 1 because i18n
// would require knowing the recipient's locale (which we don't propagate on
// EventInboxNew today). Future work: read the recipient's user.locale.
type Renderer struct {
	baseURL string // FRONTEND_ORIGIN, trimmed of trailing slash; "" → no deep link
}

func NewRenderer(baseURL string) *Renderer {
	return &Renderer{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}
}

func (r *Renderer) Render(in RenderInput) RenderOutput {
	verb := actionVerb(in.NotifType)
	link := r.issueURL(in.WorkspaceSlug, in.IssueID)
	titleSafe := html.EscapeString(in.IssueTitle)

	subject := fmt.Sprintf("[Multica] %s: %s", verb, in.IssueTitle)

	text := fmt.Sprintf("%s on \"%s\".", verb, in.IssueTitle)
	if link != "" {
		text += "\n\nView issue: " + link
	}
	text += "\n\n— Multica\n(You're receiving this because email notifications are enabled in your workspace settings.)\n"

	htmlBody := fmt.Sprintf(`<!doctype html>
<html><body style="font-family:-apple-system,Segoe UI,sans-serif;color:#111">
<p>%s on &ldquo;%s&rdquo;.</p>`, html.EscapeString(verb), titleSafe)
	if link != "" {
		htmlBody += fmt.Sprintf(`<p><a href="%s">View issue</a></p>`, html.EscapeString(link))
	}
	htmlBody += `<hr style="border:none;border-top:1px solid #eee;margin:24px 0">
<p style="font-size:12px;color:#666">You're receiving this because email notifications are enabled in your workspace settings.</p>
</body></html>`

	return RenderOutput{Subject: subject, Text: text, HTML: htmlBody}
}

func (r *Renderer) issueURL(slug, issueID string) string {
	if r.baseURL == "" || slug == "" || issueID == "" {
		return ""
	}
	return r.baseURL + "/" + slug + "/issues/" + issueID
}

func actionVerb(notifType string) string {
	switch notifType {
	case "issue_assigned":
		return "You were assigned"
	case "unassigned":
		return "You were unassigned"
	case "assignee_changed":
		return "Assignee changed"
	case "status_changed":
		return "Status changed"
	case "new_comment":
		return "New comment"
	case "mentioned":
		return "You were mentioned"
	case "priority_changed":
		return "Priority changed"
	case "start_date_changed":
		return "Start date changed"
	case "due_date_changed":
		return "Due date changed"
	case "task_completed":
		return "Agent task completed"
	case "task_failed":
		return "Agent task failed"
	case "agent_blocked":
		return "Agent blocked"
	case "agent_completed":
		return "Agent completed"
	default:
		return "Update"
	}
}
