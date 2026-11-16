package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	subscribeCommandPrefix   = "/subscribe"
	unsubscribeCommandPrefix = "/unsubscribe"
)

// SubscriptionCommandKind distinguishes /subscribe from /unsubscribe.
type SubscriptionCommandKind uint8

const (
	SubscriptionCommandSubscribe SubscriptionCommandKind = iota + 1
	SubscriptionCommandUnsubscribe
)

// SubscriptionCommand is a parsed /subscribe or /unsubscribe directive. Ref is
// the first token after the command: an issue identifier, number, UUID, or
// Multica issue link. It is empty when the member sent the bare command.
type SubscriptionCommand struct {
	Kind SubscriptionCommandKind
	Ref  string
}

// ParseSubscriptionCommand classifies a first-line /subscribe or /unsubscribe
// directive with the same token rules as /issue, /new and /clear.
func ParseSubscriptionCommand(body string) (SubscriptionCommand, bool) {
	for _, c := range []struct {
		prefix string
		kind   SubscriptionCommandKind
	}{
		{subscribeCommandPrefix, SubscriptionCommandSubscribe},
		{unsubscribeCommandPrefix, SubscriptionCommandUnsubscribe},
	} {
		rest, ok := parseLeadingCommand(body, c.prefix)
		if !ok {
			continue
		}
		ref := ""
		if fields := strings.Fields(rest); len(fields) > 0 {
			ref = fields[0]
		}
		return SubscriptionCommand{Kind: c.kind, Ref: ref}, true
	}
	return SubscriptionCommand{}, false
}

// SubscriptionAction is the product verdict of a subscription command, read by
// repliers through SubscriptionReplyText.
type SubscriptionAction string

const (
	SubscriptionSubscribed        SubscriptionAction = "subscribed"
	SubscriptionAlreadySubscribed SubscriptionAction = "already_subscribed"
	SubscriptionUnsubscribed      SubscriptionAction = "unsubscribed"
	SubscriptionNotSubscribed     SubscriptionAction = "not_subscribed"
	// SubscriptionIssueNotFound also covers issues the sender cannot see, so
	// the reply does not reveal that a private-project issue exists.
	SubscriptionIssueNotFound SubscriptionAction = "issue_not_found"
	SubscriptionUsage         SubscriptionAction = "usage"
	// SubscriptionGroupOnly answers a command sent in a direct message: the
	// member already hears about issues they follow through their inbox.
	SubscriptionGroupOnly   SubscriptionAction = "group_only"
	SubscriptionUnsupported SubscriptionAction = "unsupported"
)

// ChatTarget addresses the chat (and optionally the thread) a subscription
// posts into. AnchorMessageID is the platform message that created the
// subscription inside a thread; platforms that can only post into a thread by
// replying to one of its messages use it.
type ChatTarget struct {
	ChatID          string
	ThreadID        string
	AnchorMessageID string
}

// ChatPoster posts a proactive plain-text message into a group chat, outside
// any inbound reply. A platform without a ChatPoster on its ResolverSet does not
// support chat issue subscriptions.
type ChatPoster interface {
	PostToChat(ctx context.Context, inst db.ChannelInstallation, target ChatTarget, text string) error
}

// IssueSubscriptionQueries is the database surface chat issue subscriptions
// need. *db.Queries satisfies it.
type IssueSubscriptionQueries interface {
	projectaccess.Querier
	GetIssueByNumber(ctx context.Context, arg db.GetIssueByNumberParams) (db.Issue, error)
	GetIssueInWorkspace(ctx context.Context, arg db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	CreateChannelIssueSubscription(ctx context.Context, arg db.CreateChannelIssueSubscriptionParams) (db.ChannelIssueSubscription, error)
	DeleteChannelIssueSubscription(ctx context.Context, arg db.DeleteChannelIssueSubscriptionParams) (int64, error)
	DeleteChannelIssueSubscriptionByID(ctx context.Context, id pgtype.UUID) error
	ListChannelIssueSubscriptionsForIssue(ctx context.Context, arg db.ListChannelIssueSubscriptionsForIssueParams) ([]db.ChannelIssueSubscription, error)
}

// issueFeedPostTimeout bounds one proactive post. Delivery runs detached from
// the mutation that produced the update, so a slow platform only delays that
// one chat's message.
const issueFeedPostTimeout = 10 * time.Second

// issueFeedPublishTimeout bounds one update's whole fanout, including lookups.
const issueFeedPublishTimeout = time.Minute

func chatTargetFromMessage(msg channel.InboundMessage) ChatTarget {
	target := ChatTarget{ChatID: msg.Source.ChatID, ThreadID: msg.Source.ThreadID}
	if target.ThreadID != "" {
		target.AnchorMessageID = msg.MessageID
	}
	return target
}

// issueVisibleTo reports whether the member can see the issue. A chat
// subscription is bounded by the access of the member who created it: they
// choose to share the issue's updates with the chat, and updates pause once
// they lose access. An invalid or former member sees no private project.
func issueVisibleTo(ctx context.Context, q projectaccess.Querier, issue db.Issue, userID pgtype.UUID) (bool, error) {
	if !issue.ProjectID.Valid {
		return true, nil
	}
	hidden, err := projectaccess.ForPrincipals(ctx, q, issue.WorkspaceID, userID)
	if err != nil {
		return false, err
	}
	return hidden.CanSeeProject(issue.ProjectID), nil
}

// resolveIssueRef finds an issue in the workspace by identifier ("MUL-12",
// case-insensitive, prefix must match the workspace), bare number ("12",
// "#12"), UUID, or a Multica issue link whose last path segment is one of
// those.
func resolveIssueRef(ctx context.Context, q IssueSubscriptionQueries, workspaceID pgtype.UUID, prefix, ref string) (db.Issue, bool, error) {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		ref = segments[len(segments)-1]
	}
	ref = strings.TrimPrefix(ref, "#")
	if ref == "" {
		return db.Issue{}, false, nil
	}

	number := int64(0)
	if dash := strings.LastIndex(ref, "-"); dash > 0 && prefix != "" && strings.EqualFold(ref[:dash], prefix) {
		number, _ = strconv.ParseInt(ref[dash+1:], 10, 32)
	} else if n, err := strconv.ParseInt(ref, 10, 32); err == nil {
		number = n
	}
	var (
		issue db.Issue
		err   error
	)
	switch {
	case number > 0:
		issue, err = q.GetIssueByNumber(ctx, db.GetIssueByNumberParams{WorkspaceID: workspaceID, Number: int32(number)})
	default:
		id, parseErr := util.ParseUUID(ref)
		if parseErr != nil || !id.Valid {
			return db.Issue{}, false, nil
		}
		issue, err = q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, false, nil
	}
	if err != nil {
		return db.Issue{}, false, err
	}
	return issue, true, nil
}

// handleSubscriptionCommand runs /subscribe or /unsubscribe for the chat the
// message came from. The command is a control-plane turn: it is not stored in
// the Chat and never starts an agent run.
func (r *Router) handleSubscriptionCommand(ctx context.Context, set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, msg channel.InboundMessage, cmd SubscriptionCommand) (Result, error) {
	res := Result{
		Outcome:        OutcomeIssueSubscription,
		InstallationID: inst.ID,
		Sender:         msg.Source.SenderID,
	}
	if set.Poster == nil || r.subscriptions == nil {
		res.SubscriptionAction = SubscriptionUnsupported
		return res, nil
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		res.SubscriptionAction = SubscriptionGroupOnly
		return res, nil
	}
	if cmd.Ref == "" {
		res.SubscriptionAction = SubscriptionUsage
		res.SubscriptionKind = cmd.Kind
		return res, nil
	}
	prefix, slug := r.issueWorkspaceIdentity(ctx, inst.WorkspaceID)
	issue, found, err := resolveIssueRef(ctx, r.subscriptions, inst.WorkspaceID, prefix, cmd.Ref)
	if err != nil {
		return Result{}, fmt.Errorf("resolve issue for subscription: %w", err)
	}
	visible := false
	if found {
		if visible, err = issueVisibleTo(ctx, r.subscriptions, issue, identity.UserID); err != nil {
			return Result{}, fmt.Errorf("check issue visibility for subscription: %w", err)
		}
	}
	notFound := func() (Result, error) {
		res.SubscriptionAction = SubscriptionIssueNotFound
		res.SubscriptionRef = cmd.Ref
		return res, nil
	}
	if !found {
		return notFound()
	}
	setIssue := func() {
		res.IssueID = issue.ID
		res.IssueNumber = issue.Number
		res.IssueTitle = issue.Title
		res.IssueIdentifier = service.IssueIdentifier(prefix, issue.Number)
		res.IssueWorkspaceSlug = slug
	}

	target := chatTargetFromMessage(msg)
	if cmd.Kind == SubscriptionCommandUnsubscribe {
		// Anyone in the chat may stop updates the chat already receives, even
		// without access to the issue; only a removed row reveals it exists.
		removed, err := r.subscriptions.DeleteChannelIssueSubscription(ctx, db.DeleteChannelIssueSubscriptionParams{
			WorkspaceID: inst.WorkspaceID, IssueID: issue.ID, InstallationID: inst.ID,
			ChatID: target.ChatID, ThreadID: target.ThreadID,
		})
		if err != nil {
			return Result{}, fmt.Errorf("delete chat issue subscription: %w", err)
		}
		if removed == 0 && !visible {
			return notFound()
		}
		setIssue()
		res.SubscriptionAction = SubscriptionNotSubscribed
		if removed > 0 {
			res.SubscriptionAction = SubscriptionUnsubscribed
		}
		return res, nil
	}
	if !visible {
		return notFound()
	}
	setIssue()

	created, err := r.subscribeChat(ctx, inst, msg.Source.ChannelType, issue, identity.UserID, target)
	if err != nil {
		return Result{}, err
	}
	res.SubscriptionAction = SubscriptionAlreadySubscribed
	if created {
		res.SubscriptionAction = SubscriptionSubscribed
	}
	return res, nil
}

// subscribeChat stores the subscription and reports whether it is new.
func (r *Router) subscribeChat(ctx context.Context, inst ResolvedInstallation, channelType channel.Type, issue db.Issue, createdBy pgtype.UUID, target ChatTarget) (bool, error) {
	_, err := r.subscriptions.CreateChannelIssueSubscription(ctx, db.CreateChannelIssueSubscriptionParams{
		WorkspaceID: inst.WorkspaceID, IssueID: issue.ID, InstallationID: inst.ID,
		ChannelType: string(channelType), ChatID: target.ChatID, ThreadID: target.ThreadID,
		AnchorMessageID: target.AnchorMessageID, CreatedBy: createdBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create chat issue subscription: %w", err)
	}
	return true, nil
}

// autoSubscribeCreatedIssue makes the group chat that ran /issue follow the
// new issue. A failure leaves the issue created and only drops the note from
// the confirmation.
func (r *Router) autoSubscribeCreatedIssue(ctx context.Context, set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, issue db.Issue, createdBy pgtype.UUID) bool {
	if msg.Source.ChatType != channel.ChatTypeGroup || set.Poster == nil || r.subscriptions == nil {
		return false
	}
	visible, err := issueVisibleTo(ctx, r.subscriptions, issue, createdBy)
	if err == nil && visible {
		_, err = r.subscribeChat(ctx, inst, msg.Source.ChannelType, issue, createdBy, chatTargetFromMessage(msg))
		if err == nil {
			return true
		}
	}
	if err != nil {
		r.logger.Warn("channel router: auto-subscribe chat to created issue failed",
			"channel_type", string(msg.Source.ChannelType),
			"issue_id", util.UUIDToString(issue.ID), "error", err)
	}
	return false
}

// PublishIssueUpdate posts the rendered update into every chat that follows
// the issue. It runs detached and returns immediately; Drain waits for
// in-flight posts. render runs only when some chat follows the issue, so
// callers can do their lookups there; an empty result posts nothing.
// A subscription's updates pause (without deleting it) while its creator
// cannot see the issue; subscriptions whose installation no longer exists in
// the workspace are deleted.
func (r *Router) PublishIssueUpdate(workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	if r.subscriptions == nil || render == nil || !workspaceID.Valid || !issueID.Valid {
		return
	}
	r.replyWg.Add(1)
	go func() {
		defer r.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), issueFeedPublishTimeout)
		defer cancel()
		r.publishIssueUpdate(ctx, workspaceID, issueID, render)
	}()
}

func (r *Router) publishIssueUpdate(ctx context.Context, workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	log := r.logger.With("issue_id", util.UUIDToString(issueID))
	subs, err := r.subscriptions.ListChannelIssueSubscriptionsForIssue(ctx, db.ListChannelIssueSubscriptionsForIssueParams{
		IssueID: issueID, WorkspaceID: workspaceID,
	})
	if err != nil {
		log.Warn("channel issue feed: list subscriptions failed", "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	issue, err := r.subscriptions.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn("channel issue feed: load issue failed", "error", err)
		}
		return
	}
	visibleTo := map[[16]byte]bool{}
	allowed := subs[:0:0]
	for _, sub := range subs {
		visible, cached := visibleTo[sub.CreatedBy.Bytes]
		if !cached {
			if visible, err = issueVisibleTo(ctx, r.subscriptions, issue, sub.CreatedBy); err != nil {
				log.Warn("channel issue feed: visibility check failed", "error", err)
				return
			}
			visibleTo[sub.CreatedBy.Bytes] = visible
		}
		if visible {
			allowed = append(allowed, sub)
		}
	}
	if len(allowed) == 0 {
		return
	}
	text := render(ctx)
	if strings.TrimSpace(text) == "" {
		return
	}

	for _, sub := range allowed {
		r.mu.RLock()
		set, ok := r.sets[channel.Type(sub.ChannelType)]
		r.mu.RUnlock()
		if !ok || set.Poster == nil {
			continue
		}
		inst, err := r.subscriptions.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
			ID: sub.InstallationID, ChannelType: sub.ChannelType,
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && inst.WorkspaceID != workspaceID) {
			if err := r.subscriptions.DeleteChannelIssueSubscriptionByID(ctx, sub.ID); err != nil {
				log.Warn("channel issue feed: delete orphaned subscription failed", "error", err)
			}
			continue
		}
		if err != nil {
			log.Warn("channel issue feed: load installation failed",
				"installation_id", util.UUIDToString(sub.InstallationID), "error", err)
			continue
		}
		if inst.Status != "active" {
			continue
		}
		postCtx, cancel := context.WithTimeout(ctx, issueFeedPostTimeout)
		err = set.Poster.PostToChat(postCtx, inst, ChatTarget{
			ChatID: sub.ChatID, ThreadID: sub.ThreadID, AnchorMessageID: sub.AnchorMessageID,
		}, text)
		cancel()
		if err != nil {
			log.Warn("channel issue feed: post failed",
				"channel_type", sub.ChannelType,
				"installation_id", util.UUIDToString(sub.InstallationID), "error", err)
		}
	}
}

// IssueChatSubscribedNote is appended to an /issue confirmation when the
// group chat now follows the new issue.
func IssueChatSubscribedNote(res Result) string {
	return "🔔 This chat will get updates on " + resultIssueRef(res) +
		". Send /unsubscribe " + resultIssueRef(res) + " to stop."
}

// SubscriptionReplyText renders the reply to /subscribe or /unsubscribe.
// escapeTitle neutralizes member-authored markup for the platform; nil keeps
// the title as plain text.
func SubscriptionReplyText(res Result, appURL string, escapeTitle func(string) string) string {
	ref := resultIssueRef(res)
	title := strings.TrimSpace(res.IssueTitle)
	if escapeTitle != nil {
		title = escapeTitle(title)
	}
	named := ref
	if title != "" {
		named = ref + " — " + title
	}
	// Link by UUID: without a workspace issue prefix the identifier is "#N",
	// which no issue route resolves.
	link := ""
	if res.IssueID.Valid {
		link = channel.IssueWebLink(appURL, res.IssueWorkspaceSlug, util.UUIDToString(res.IssueID))
	}
	withLink := func(line string) string {
		if link == "" {
			return line
		}
		return line + "\n" + link
	}
	switch res.SubscriptionAction {
	case SubscriptionSubscribed:
		return withLink("🔔 This chat now follows " + named + ". Status changes, assignments and comments will be posted here.")
	case SubscriptionAlreadySubscribed:
		return withLink("This chat already follows " + named + ".")
	case SubscriptionUnsubscribed:
		return "🔕 This chat no longer follows " + ref + "."
	case SubscriptionNotSubscribed:
		return "This chat does not follow " + ref + "."
	case SubscriptionIssueNotFound:
		missing := strings.TrimSpace(res.SubscriptionRef)
		if escapeTitle != nil {
			missing = escapeTitle(missing)
		}
		return "No issue matches " + missing + " that you can access."
	case SubscriptionUsage:
		if res.SubscriptionKind == SubscriptionCommandUnsubscribe {
			return "Usage: /unsubscribe <issue>, for example /unsubscribe ABC-123"
		}
		return "Usage: /subscribe <issue>, for example /subscribe ABC-123"
	case SubscriptionGroupOnly:
		return "Chat subscriptions work in group chats. In a direct message, follow the issue in Multica to get updates in your inbox."
	case SubscriptionUnsupported:
		return "Issue subscriptions aren't available on this platform yet."
	default:
		return ""
	}
}

func resultIssueRef(res Result) string {
	if res.IssueIdentifier != "" {
		return res.IssueIdentifier
	}
	return "#" + strconv.Itoa(int(res.IssueNumber))
}
