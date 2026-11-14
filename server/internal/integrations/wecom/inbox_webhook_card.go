package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type inboxWebhookCardQueries interface {
	GetIssueInWorkspace(context.Context, db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	GetCommentInWorkspace(context.Context, db.GetCommentInWorkspaceParams) (db.Comment, error)
	GetUser(context.Context, pgtype.UUID) (db.User, error)
	GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error)
	GetIssueStatusEntryByKey(context.Context, db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error)
}

type inboxWebhookCardLine struct {
	Key   string `json:"keyname"`
	Value string `json:"value"`
}

type inboxWebhookCardPayload struct {
	MsgType      string `json:"msgtype"`
	TemplateCard struct {
		CardType string `json:"card_type"`
		Source   struct {
			Desc      string `json:"desc"`
			DescColor int    `json:"desc_color"`
		} `json:"source"`
		MainTitle struct {
			Title string `json:"title"`
			Desc  string `json:"desc,omitempty"`
		} `json:"main_title"`
		SubTitleText string                 `json:"sub_title_text,omitempty"`
		Lines        []inboxWebhookCardLine `json:"horizontal_content_list"`
		Jumps        []struct {
			Type  int    `json:"type"`
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"jump_list"`
		Action struct {
			Type int    `json:"type"`
			URL  string `json:"url"`
		} `json:"card_action"`
	} `json:"template_card"`
}

func inboxWebhookAppURL() string {
	for _, name := range []string{"WECOM_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		raw := strings.TrimSpace(os.Getenv(name))
		u, err := url.Parse(raw)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery ||
			strings.Contains(raw, "#") || strings.HasSuffix(u.Host, ":") {
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		ip := net.ParseIP(host)
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
			continue
		}
		// Browsers accept legacy numeric IPv4 forms that net.ParseIP rejects.
		last := host[strings.LastIndex(host, ".")+1:]
		numeric := last != "" && strings.Trim(last, "0123456789") == ""
		hex := strings.HasPrefix(last, "0x") && strings.Trim(strings.TrimPrefix(last, "0x"), "0123456789abcdef") == ""
		if ip == nil && (numeric || hex) {
			continue
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				continue
			}
		}
		return strings.TrimSuffix(u.String(), "/")
	}
	return ""
}

func inboxWebhookString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}

type inboxWebhookCardDetails struct {
	From             string `json:"from"`
	To               string `json:"to"`
	CommentID        string `json:"comment_id"`
	ChangedAt        string `json:"changed_at"`
	Emoji            string `json:"emoji"`
	PrevAssigneeType string `json:"prev_assignee_type"`
	PrevAssigneeID   string `json:"prev_assignee_id"`
	NewAssigneeType  string `json:"new_assignee_type"`
	NewAssigneeID    string `json:"new_assignee_id"`
}

func inboxWebhookDetails(v any) (inboxWebhookCardDetails, error) {
	var details inboxWebhookCardDetails
	var raw []byte
	switch v := v.(type) {
	case nil:
		return details, nil
	case json.RawMessage:
		raw = v
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	case *string:
		if v == nil {
			return details, nil
		}
		raw = []byte(*v)
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return details, errors.New("invalid inbox details")
		}
	}
	if len(raw) == 0 {
		return details, nil
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		return details, errors.New("invalid inbox details")
	}
	return details, nil
}

func inboxWebhookText(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return truncateRunes(s, limit-1) + "…"
}

func inboxWebhookTime(t time.Time, locale Locale) string {
	if t.IsZero() {
		return copyFor(locale).InboxWebhook.Unknown
	}
	if locale == LocaleZhHans {
		return t.In(time.FixedZone("UTC+8", 8*60*60)).Format("01-02 15:04") + copyFor(locale).InboxWebhook.Timezone
	}
	return t.UTC().Format("01-02 15:04") + copyFor(locale).InboxWebhook.Timezone
}

func inboxWebhookStatus(ctx context.Context, q inboxWebhookCardQueries, workspace pgtype.UUID, key string, locale Locale) (string, error) {
	key = strings.TrimSpace(key)
	words := copyFor(locale).InboxWebhook
	if key == "" {
		return words.Unknown, nil
	}
	if name, ok := words.Statuses[key]; ok {
		return name, nil
	}
	status, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: workspace, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return key, nil
	}
	if err != nil {
		return "", errors.New("unable to load inbox status")
	}
	if status.Name == "" {
		return key, nil
	}
	return status.Name, nil
}

func inboxWebhookAuthor(ctx context.Context, q inboxWebhookCardQueries, workspace pgtype.UUID, kind string, id pgtype.UUID, locale Locale) (string, error) {
	name := ""
	var err error
	if id.Valid {
		switch kind {
		case "member":
			var user db.User
			user, err = q.GetUser(ctx, id)
			name = user.Name
		case "agent":
			var agent db.Agent
			agent, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: workspace})
			name = agent.Name
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("unable to load inbox author")
	}
	if strings.TrimSpace(name) == "" {
		return copyFor(locale).InboxWebhook.Unknown, nil
	}
	return name, nil
}

func inboxWebhookAssignee(ctx context.Context, q inboxWebhookCardQueries, workspace pgtype.UUID, kind, id string, locale Locale) (string, error) {
	if id == "" {
		return copyFor(locale).InboxWebhook.NoAssignee, nil
	}
	parsed, err := util.ParseUUID(id)
	if err != nil || !parsed.Valid {
		return copyFor(locale).InboxWebhook.Unknown, nil
	}
	name, err := inboxWebhookAuthor(ctx, q, workspace, kind, parsed, locale)
	return inboxWebhookText(strings.Join(strings.Fields(name), " "), 26), err
}

func inboxWebhookAssignment(ctx context.Context, q inboxWebhookCardQueries, workspace pgtype.UUID, item map[string]any, details inboxWebhookCardDetails, locale Locale) (string, error) {
	words := copyFor(locale).InboxWebhook
	kind := inboxWebhookString(item["type"])
	if kind == "issue_assigned" || kind == "unassigned" {
		actorType, actorID, format := details.NewAssigneeType, details.NewAssigneeID, words.AssignedTo
		if kind == "unassigned" {
			actorType, actorID, format = details.PrevAssigneeType, details.PrevAssigneeID, words.UnassignedFrom
		}
		if actorID == "" {
			actorType, actorID = inboxWebhookString(item["recipient_type"]), inboxWebhookString(item["recipient_id"])
		}
		name := words.Unknown
		if actorID != "" {
			var err error
			name, err = inboxWebhookAssignee(ctx, q, workspace, actorType, actorID, locale)
			if err != nil {
				return "", err
			}
		}
		return fmt.Sprintf(format, name), nil
	}
	before, err := inboxWebhookAssignee(ctx, q, workspace, details.PrevAssigneeType, details.PrevAssigneeID, locale)
	if err != nil {
		return "", err
	}
	after, err := inboxWebhookAssignee(ctx, q, workspace, details.NewAssigneeType, details.NewAssigneeID, locale)
	if err != nil {
		return "", err
	}
	return before + " → " + after, nil
}

func inboxWebhookExcerpt(content string, limit int) string {
	content = util.MentionRe.ReplaceAllString(content, "$1")
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" && (len(lines) == 0 || lines[len(lines)-1] == "") {
			continue
		}
		lines = append(lines, line)
	}
	return inboxWebhookText(strings.TrimSpace(strings.Join(lines, "\n")), limit)
}

func inboxWebhookMentionText(prefix, content string) string {
	content = inboxWebhookExcerpt(content, 112-utf8.RuneCountInString(prefix)-1)
	if content == "" {
		return prefix
	}
	return prefix + "\n" + content
}

func buildInboxWebhookCard(ctx context.Context, q inboxWebhookCardQueries, item map[string]any) ([]byte, error) {
	kind := inboxWebhookString(item["type"])
	if !inboxWebhookKind(kind) {
		return nil, errors.New("unsupported inbox notification")
	}
	appURL := inboxWebhookAppURL()
	if appURL == "" {
		return nil, errors.New("inbox notification requires a valid app origin")
	}
	workspaceID, wsErr := util.ParseUUID(inboxWebhookString(item["workspace_id"]))
	issueID, issueErr := util.ParseUUID(inboxItemIssueID(item))
	if wsErr != nil || issueErr != nil || !workspaceID.Valid || !issueID.Valid {
		return nil, errors.New("invalid inbox workspace or issue")
	}
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, errors.New("unable to load inbox issue")
	}
	return buildInboxWebhookCardForIssue(ctx, q, item, issue)
}

func buildInboxWebhookCardForIssue(ctx context.Context, q inboxWebhookCardQueries, item map[string]any, issue db.Issue) ([]byte, error) {
	kind := inboxWebhookString(item["type"])
	appURL := inboxWebhookAppURL()
	workspaceID, issueID := issue.WorkspaceID, issue.ID
	workspace, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, errors.New("unable to load inbox workspace")
	}
	identifier := util.UUIDToString(issueID)
	title := issue.Title
	if workspace.IssuePrefix != "" && issue.Number > 0 {
		identifier = workspace.IssuePrefix + "-" + strconv.FormatInt(int64(issue.Number), 10)
	}
	if strings.TrimSpace(title) == "" {
		title = identifier
	}
	link := channel.IssueWebLink(appURL, workspace.Slug, identifier)
	if link == "" {
		return nil, errors.New("unable to build inbox issue link")
	}
	details, err := inboxWebhookDetails(item["details"])
	if err != nil {
		return nil, err
	}
	locale := deploymentLocale()
	words := copyFor(locale).InboxWebhook
	var card inboxWebhookCardPayload
	card.MsgType = "template_card"
	card.TemplateCard.CardType = "text_notice"
	eventLabel := copyFor(locale).InboxTypeLabels[kind]

	source := identifier + " " + eventLabel
	card.TemplateCard.Source.Desc = inboxWebhookText(source, 26)
	card.TemplateCard.MainTitle.Title = inboxWebhookText(strings.Join(strings.Fields(title), " "), 26)
	card.TemplateCard.Source.DescColor = 1
	card.TemplateCard.Action.Type = 1
	card.TemplateCard.Action.URL = link
	card.TemplateCard.Jumps = append(card.TemplateCard.Jumps, struct {
		Type  int    `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}{1, words.ViewTask, link})

	actorType := inboxWebhookString(item["actor_type"])
	actorID, _ := util.ParseUUID(inboxWebhookString(item["actor_id"]))
	at, _ := time.Parse(time.RFC3339Nano, inboxWebhookString(item["created_at"]))
	timeLabel := words.Notified
	if kind == "status_changed" {
		from, err := inboxWebhookStatus(ctx, q, workspaceID, details.From, locale)
		if err != nil {
			return nil, err
		}
		to, err := inboxWebhookStatus(ctx, q, workspaceID, details.To, locale)
		if err != nil {
			return nil, err
		}
		card.TemplateCard.SubTitleText = inboxWebhookText(from+" → "+to, 112)
		switch details.To {
		case "blocked":
			card.TemplateCard.Source.DescColor = 2
		case "done":
			card.TemplateCard.Source.DescColor = 3
		}
		if changedAt, parseErr := time.Parse(time.RFC3339Nano, details.ChangedAt); parseErr == nil {
			at = changedAt
			timeLabel = words.Changed
		}
	} else if kind == "new_comment" || kind == "comment_added" || details.CommentID != "" {
		commentID, err := util.ParseUUID(details.CommentID)
		if err != nil || !commentID.Valid {
			return nil, errors.New("invalid inbox comment")
		}
		comment, err := q.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: workspaceID})
		if err != nil || comment.DeletedAt.Valid || comment.IssueID != issueID {
			return nil, errors.New("inbox comment unavailable")
		}
		if kind == "new_comment" || kind == "comment_added" {
			card.TemplateCard.Source.DescColor = 3
			actorType, actorID = comment.AuthorType, comment.AuthorID
			if comment.CreatedAt.Valid {
				at = comment.CreatedAt.Time
			} else {
				at = time.Time{}
			}
			timeLabel = words.Commented
		}
		card.TemplateCard.SubTitleText = inboxWebhookExcerpt(comment.Content, 112)
		if kind == "mentioned" {
			card.TemplateCard.SubTitleText = inboxWebhookMentionText(words.MentionComment, comment.Content)
		}
	} else if kind == "issue_assigned" || kind == "unassigned" || kind == "assignee_changed" {
		body, err := inboxWebhookAssignment(ctx, q, workspaceID, item, details, locale)
		if err != nil {
			return nil, err
		}
		card.TemplateCard.SubTitleText = body
	} else if kind == "mentioned" {
		content := inboxWebhookString(item["body"])
		if strings.TrimSpace(content) == "" {
			content = issue.Description.String
		}
		card.TemplateCard.SubTitleText = inboxWebhookMentionText(words.MentionDescription, content)
	} else {
		body := inboxWebhookString(item["body"])
		if strings.TrimSpace(body) == "" {
			body = eventLabel
			if details.From != "" || details.To != "" {
				body += " · " + details.From + " → " + details.To
			} else if details.Emoji != "" {
				body += " · " + details.Emoji
			}
		}
		card.TemplateCard.SubTitleText = inboxWebhookExcerpt(body, 112)
		if kind == "task_failed" {
			card.TemplateCard.Source.DescColor = 2
		}
	}

	author, err := inboxWebhookAuthor(ctx, q, workspaceID, actorType, actorID, locale)
	if err != nil {
		return nil, err
	}
	timestamp := inboxWebhookTime(at, locale)
	timeKey := timeLabel
	if utf8.RuneCountInString(timeKey) > 5 {
		timeKey = words.When
		timestamp = timeLabel + " " + timestamp
	}
	priority := words.Priorities[issue.Priority]
	if priority == "" {
		priority = issue.Priority
	}
	due := "-"
	if issue.DueDate.Valid {
		due = issue.DueDate.Time.Format("2006-01-02")
	}
	card.TemplateCard.Lines = []inboxWebhookCardLine{
		{words.Priority, inboxWebhookText(priority, 26)}, {words.Due, due},
		{words.Actor, inboxWebhookText(strings.Join(strings.Fields(author), " "), 26)},
		{timeKey, timestamp},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return nil, errors.New("unable to encode inbox card")
	}
	return encoded, nil
}
