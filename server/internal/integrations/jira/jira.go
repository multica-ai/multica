// Package jira is the integration foundation for Atlassian Jira (Cloud or
// Data Center) connections: workspace-scoped API-token auth, inbound webhook
// authentication + payload parsing, and a minimal REST client used for issue
// enrichment. Unlike the vcs package there is no multi-provider registry —
// Jira is a single provider — so the package exports the pieces directly.
//
// PR 1 scope is inbound-only: a webhook delivery creates or syncs a Multica
// issue mirrored from the Jira issue. Outbound sync, status mapping, and
// agent-context injection are later PRs.
//
// Secrets (the API token and the minted webhook secret) are stored encrypted
// with the MULTICA_JIRA_SECRET_KEY secretbox wired in cmd/server/router.go,
// mirroring MULTICA_VCS_SECRET_KEY.
package jira

import (
	"crypto/hmac"
	"encoding/json"
	"net/http"
	"strings"
)

// EventKind is the normalized webhook event category. Anything not modelled
// in PR 1 maps to EventOther and is acknowledged but ignored.
type EventKind int

const (
	EventOther EventKind = iota
	EventIssueCreated
	EventIssueUpdated
)

// SecretHeader is the header the operator configures on the Jira webhook so
// Multica can authenticate deliveries. Jira webhooks carry no native HMAC
// signature (unlike Forgejo/GitHub), so — like GitLab's X-Gitlab-Token —
// authentication is a constant-time compare of a minted shared secret,
// accepted either as this header or as a `secret` query parameter (Jira
// Cloud webhooks cannot always set custom headers, but can embed query
// params in the URL).
const SecretHeader = "X-Multica-Webhook-Secret"

// VerifySecret authenticates an inbound delivery against the connection's
// stored webhook secret. An empty stored secret never validates.
func VerifySecret(secret string, r *http.Request) bool {
	if secret == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(SecretHeader))
	if got == "" {
		got = strings.TrimSpace(r.URL.Query().Get("secret"))
	}
	if got == "" {
		return false
	}
	return hmac.Equal([]byte(secret), []byte(got))
}

// IssueEvent is the normalized shape of a jira:issue_created /
// jira:issue_updated webhook. Description is flattened to plain text (Jira
// Cloud may deliver it as an Atlassian Document Format tree).
type IssueEvent struct {
	Kind     EventKind
	IssueID  string // Jira's numeric issue id, e.g. "10042"
	IssueKey string // e.g. "PROJ-7"
	Summary  string
	// Description is the plain-text flattening of the issue description
	// (string or ADF). Empty when the issue has none.
	Description string
	Status      string // raw Jira status name, e.g. "In Progress"
	Priority    string // raw Jira priority name, e.g. "High"
	// Assignee identity, empty when unassigned.
	AssigneeAccountID   string
	AssigneeDisplayName string
	AssigneeEmail       string
	// AssigneeChanged reports whether this event's changelog contains an
	// assignee transition — the seam later PRs use for assignment-driven
	// behavior (e.g. handing a Jira issue to a Multica agent).
	AssigneeChanged bool
}

// webhookPayload is the raw Jira webhook envelope, limited to what PR 1
// consumes.
type webhookPayload struct {
	WebhookEvent string `json:"webhookEvent"`
	Issue        struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
			Summary     string          `json:"summary"`
			Description json.RawMessage `json:"description"`
			Status      struct {
				Name string `json:"name"`
			} `json:"status"`
			Priority struct {
				Name string `json:"name"`
			} `json:"priority"`
			Assignee *struct {
				AccountID    string `json:"accountId"`
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
			} `json:"assignee"`
		} `json:"fields"`
	} `json:"issue"`
	Changelog struct {
		Items []struct {
			Field string `json:"field"`
		} `json:"items"`
	} `json:"changelog"`
}

// ParseIssueEvent decodes a Jira webhook body into the normalized IssueEvent.
// Unmodelled webhookEvent values return Kind == EventOther with the rest of
// the struct zeroed; callers acknowledge and ignore them.
func ParseIssueEvent(body []byte) (IssueEvent, error) {
	var d webhookPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return IssueEvent{}, err
	}
	var kind EventKind
	switch d.WebhookEvent {
	case "jira:issue_created":
		kind = EventIssueCreated
	case "jira:issue_updated":
		kind = EventIssueUpdated
	default:
		return IssueEvent{Kind: EventOther}, nil
	}
	ev := IssueEvent{
		Kind:        kind,
		IssueID:     d.Issue.ID,
		IssueKey:    d.Issue.Key,
		Summary:     d.Issue.Fields.Summary,
		Description: DescriptionText(d.Issue.Fields.Description),
		Status:      d.Issue.Fields.Status.Name,
		Priority:    d.Issue.Fields.Priority.Name,
	}
	if a := d.Issue.Fields.Assignee; a != nil {
		ev.AssigneeAccountID = a.AccountID
		ev.AssigneeDisplayName = a.DisplayName
		ev.AssigneeEmail = a.EmailAddress
	}
	for _, item := range d.Changelog.Items {
		if strings.EqualFold(item.Field, "assignee") {
			ev.AssigneeChanged = true
			break
		}
	}
	return ev, nil
}

// DescriptionText flattens a Jira description field to plain text. Jira
// Server/DC (REST v2) sends a plain string; Jira Cloud (REST v3 / webhooks)
// sends an Atlassian Document Format tree. Anything unparseable yields "".
func DescriptionText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var node adfNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	var b strings.Builder
	node.writeText(&b)
	return strings.TrimSpace(b.String())
}

// adfNode is the minimal recursive ADF shape needed to extract text.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
}

func (n adfNode) writeText(b *strings.Builder) {
	if n.Type == "text" && n.Text != "" {
		b.WriteString(n.Text)
	}
	for i, c := range n.Content {
		// Separate block-level children with newlines so paragraphs don't
		// run together; inline nodes concatenate naturally.
		if i > 0 && c.isBlock() {
			b.WriteString("\n")
		}
		c.writeText(b)
	}
}

func (n adfNode) isBlock() bool {
	switch n.Type {
	case "paragraph", "heading", "bulletList", "orderedList", "listItem",
		"blockquote", "codeBlock", "rule", "table", "tableRow", "tableCell":
		return true
	}
	return false
}

// NormalizeBaseURL trims whitespace and any trailing slash so stored base
// URLs and derived webhook URLs are stable regardless of input. Mirrors
// vcs.NormalizeInstanceURL.
func NormalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
