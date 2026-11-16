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

// SubscriptionTarget is what a chat subscription follows. The zero value is
// a bare command that named neither.
type SubscriptionTarget uint8

const (
	SubscriptionTargetIssue SubscriptionTarget = iota + 1
	SubscriptionTargetProject
)

// SubscriptionCommand is a parsed /subscribe or /unsubscribe directive:
//
//	/subscribe issue <issue>      issue key, number, UUID or link
//	/subscribe project <project>  project name, UUID or link
//	/subscribe <issue>            shorthand for an issue (a project link is
//	                              recognized as a project)
//
// Ref is empty when the member named no subject.
type SubscriptionCommand struct {
	Kind   SubscriptionCommandKind
	Target SubscriptionTarget
	Ref    string
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
		cmd := SubscriptionCommand{Kind: c.kind}
		rest = strings.TrimSpace(rest)
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return cmd, true
		}
		switch {
		case strings.EqualFold(fields[0], "issue"):
			cmd.Target = SubscriptionTargetIssue
			rest = strings.TrimSpace(rest[len(fields[0]):])
		case strings.EqualFold(fields[0], "project"):
			cmd.Target = SubscriptionTargetProject
			rest = strings.TrimSpace(rest[len(fields[0]):])
		}
		if cmd.Target == SubscriptionTargetProject {
			// Project names can contain spaces: the rest of the first
			// non-empty line names the project.
			cmd.Ref, _, _ = strings.Cut(rest, "\n")
			cmd.Ref = strings.TrimSpace(cmd.Ref)
			return cmd, true
		}
		if f := strings.Fields(rest); len(f) > 0 {
			cmd.Ref = f[0]
		}
		if cmd.Target == 0 {
			cmd.Target = SubscriptionTargetIssue
			if isProjectLink(cmd.Ref) {
				cmd.Target = SubscriptionTargetProject
			}
		}
		return cmd, true
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
	// SubscriptionNotFound also covers issues and projects the sender cannot
	// see, so the reply does not reveal that a private project exists.
	SubscriptionNotFound SubscriptionAction = "not_found"
	// SubscriptionAmbiguous answers a project name several visible projects
	// share; the member sends the project link instead.
	SubscriptionAmbiguous SubscriptionAction = "ambiguous"
	SubscriptionUsage     SubscriptionAction = "usage"
	// SubscriptionGroupOnly answers a command sent in a direct message: the
	// member already hears about what they follow through their inbox.
	SubscriptionGroupOnly   SubscriptionAction = "group_only"
	SubscriptionUnsupported SubscriptionAction = "unsupported"
)

// SubscriptionOutcome describes an OutcomeIssueSubscription result. The issue
// a command acted on is carried by Result's Issue* fields.
type SubscriptionOutcome struct {
	Action SubscriptionAction
	Kind   SubscriptionCommandKind
	Target SubscriptionTarget
	// Ref echoes what the member typed, for not-found and ambiguous replies.
	Ref          string
	ProjectID    pgtype.UUID
	ProjectTitle string
}

// ChatTarget addresses the chat (and optionally the thread) a subscription
// posts into. AnchorMessageID is the platform message that created the
// subscription. Platforms that post into a thread by replying to one of its
// messages use it: Feishu replies to every group command in a topic under
// the command, and posts the subscription's updates into that topic.
type ChatTarget struct {
	ChatID          string
	ThreadID        string
	AnchorMessageID string
}

// ChatPoster posts a proactive plain-text message into a group chat, outside
// any inbound reply. A platform without a ChatPoster on its ResolverSet does not
// support chat subscriptions.
type ChatPoster interface {
	PostToChat(ctx context.Context, inst db.ChannelInstallation, target ChatTarget, text string) error
}

// TopicPoster is a ChatPoster whose chat-level posts can root a topic that
// later posts reply under (Feishu, through AnchorMessageID). A chat following
// a project then gets one topic per issue: OpenTopic posts the issue's first
// update at chat level and returns the message id that anchors its topic.
type TopicPoster interface {
	ChatPoster
	OpenTopic(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (anchorMessageID string, err error)
}

// ChatSubscriptionQueries is the database surface chat subscriptions need.
// *db.Queries satisfies it.
type ChatSubscriptionQueries interface {
	projectaccess.Querier
	GetIssueByNumber(ctx context.Context, arg db.GetIssueByNumberParams) (db.Issue, error)
	GetIssueInWorkspace(ctx context.Context, arg db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetProjectInWorkspace(ctx context.Context, arg db.GetProjectInWorkspaceParams) (db.Project, error)
	ListProjectsByTitle(ctx context.Context, arg db.ListProjectsByTitleParams) ([]db.Project, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	CreateChannelIssueSubscription(ctx context.Context, arg db.CreateChannelIssueSubscriptionParams) (db.ChannelIssueSubscription, error)
	DeleteChannelIssueSubscriptionByID(ctx context.Context, id pgtype.UUID) error
	SetChannelIssueSubscriptionMuted(ctx context.Context, arg db.SetChannelIssueSubscriptionMutedParams) error
	GetChannelChatSessionBindingBySessionAny(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error)
	ListChannelIssueSubscriptionsForIssue(ctx context.Context, arg db.ListChannelIssueSubscriptionsForIssueParams) ([]db.ChannelIssueSubscription, error)
	CreateChannelProjectSubscription(ctx context.Context, arg db.CreateChannelProjectSubscriptionParams) (db.ChannelProjectSubscription, error)
	DeleteChannelProjectSubscriptionByID(ctx context.Context, id pgtype.UUID) error
	ListChannelProjectSubscriptionsForProject(ctx context.Context, arg db.ListChannelProjectSubscriptionsForProjectParams) ([]db.ChannelProjectSubscription, error)
}

// issueFeedPostTimeout bounds one proactive post. Delivery runs detached from
// the mutation that produced the update, so a slow platform only delays that
// one chat's message.
const issueFeedPostTimeout = 10 * time.Second

// issueFeedPublishTimeout bounds one update's whole fanout, including lookups.
const issueFeedPublishTimeout = time.Minute

func chatTargetFromMessage(msg channel.InboundMessage) ChatTarget {
	return ChatTarget{ChatID: msg.Source.ChatID, ThreadID: msg.Source.ThreadID, AnchorMessageID: msg.MessageID}
}

// chatSpot is where a command was sent. A command sent inside the topic a
// chat-level subscription's reply opened (its root is the subscription's
// anchor message) addresses that subscription: its updates land there.
type chatSpot struct {
	installationID pgtype.UUID
	target         ChatTarget
	rootID         string
}

func chatSpotFromMessage(inst ResolvedInstallation, msg channel.InboundMessage) chatSpot {
	spot := chatSpot{installationID: inst.ID, target: chatTargetFromMessage(msg)}
	if msg.ReplyTo != nil && spot.target.ThreadID != "" {
		spot.rootID = msg.ReplyTo.RootID
	}
	return spot
}

func (s chatSpot) holds(installationID pgtype.UUID, chatID, threadID, anchor string) bool {
	if installationID != s.installationID || chatID != s.target.ChatID {
		return false
	}
	return threadID == s.target.ThreadID || (threadID == "" && s.rootID != "" && anchor == s.rootID)
}

// projectVisibleTo reports whether the member can see the project. A chat
// subscription is bounded by the access of the member who created it: they
// choose to share the updates with the chat, and updates pause once they
// lose access. An invalid or former member sees no private project.
func projectVisibleTo(ctx context.Context, q projectaccess.Querier, workspaceID, projectID, userID pgtype.UUID) (bool, error) {
	hidden, err := projectaccess.ForPrincipals(ctx, q, workspaceID, userID)
	if err != nil {
		return false, err
	}
	return hidden.CanSeeProject(projectID), nil
}

func issueVisibleTo(ctx context.Context, q projectaccess.Querier, issue db.Issue, userID pgtype.UUID) (bool, error) {
	if !issue.ProjectID.Valid {
		return true, nil
	}
	return projectVisibleTo(ctx, q, issue.WorkspaceID, issue.ProjectID, userID)
}

// linkLastSegment returns the last path segment of a URL and whether ref was
// one.
func linkLastSegment(ref string) (string, []string, bool) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ref, nil, false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	return segments[len(segments)-1], segments, true
}

func isProjectLink(ref string) bool {
	_, segments, ok := linkLastSegment(ref)
	return ok && len(segments) >= 2 && segments[len(segments)-2] == "projects"
}

// resolveIssueRef finds an issue in the workspace by identifier ("MUL-12",
// case-insensitive, prefix must match the workspace), bare number ("12",
// "#12"), UUID, or a Multica issue link whose last path segment is one of
// those.
func resolveIssueRef(ctx context.Context, q ChatSubscriptionQueries, workspaceID pgtype.UUID, prefix, ref string) (db.Issue, bool, error) {
	ref, _, _ = linkLastSegment(strings.TrimSpace(ref))
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

// subscriptionSubject is the issue or project a command resolved to.
type subscriptionSubject struct {
	// visible reports whether the sender can see it.
	visible bool
	// reveal fills the reply with the subject's details.
	reveal func(*Result)
	// find lists the chat's existing subscriptions to it (see chatSpot).
	find       func(context.Context) ([]subscriptionMatch, error)
	create     func(context.Context) (bool, error)
	deleteByID func(context.Context, pgtype.UUID) error
	// setMuted mutes or resumes a project-derived issue topic; nil for
	// subjects without them.
	setMuted func(context.Context, pgtype.UUID, bool) error
}

// subscriptionMatch is one of the chat's existing subscriptions to a subject.
// fromProject marks an issue topic a project subscription opened: stopping
// it mutes the row instead of deleting it, so the project does not open a
// new topic for the issue on its next update.
type subscriptionMatch struct {
	id          pgtype.UUID
	fromProject bool
	muted       bool
}

type subjectLookup uint8

const (
	subjectFound subjectLookup = iota
	subjectMissing
	subjectAmbiguous
)

func (r *Router) resolveIssueSubject(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, identity ResolvedIdentity, ref string, spot chatSpot) (subscriptionSubject, subjectLookup, error) {
	prefix, slug := r.issueWorkspaceIdentity(ctx, inst.WorkspaceID)
	issue, found, err := resolveIssueRef(ctx, r.subscriptions, inst.WorkspaceID, prefix, ref)
	if err != nil || !found {
		return subscriptionSubject{}, subjectMissing, err
	}
	visible, err := issueVisibleTo(ctx, r.subscriptions, issue, identity.UserID)
	if err != nil {
		return subscriptionSubject{}, subjectMissing, fmt.Errorf("check issue visibility: %w", err)
	}
	return subscriptionSubject{
		visible: visible,
		reveal: func(res *Result) {
			res.IssueID = issue.ID
			res.IssueNumber = issue.Number
			res.IssueTitle = issue.Title
			res.IssueIdentifier = service.IssueIdentifier(prefix, issue.Number)
			res.IssueWorkspaceSlug = slug
		},
		find: func(ctx context.Context) ([]subscriptionMatch, error) {
			rows, err := r.subscriptions.ListChannelIssueSubscriptionsForIssue(ctx, db.ListChannelIssueSubscriptionsForIssueParams{
				IssueID: issue.ID, WorkspaceID: inst.WorkspaceID,
			})
			var out []subscriptionMatch
			for _, row := range rows {
				if spot.holds(row.InstallationID, row.ChatID, row.ThreadID, row.AnchorMessageID) {
					out = append(out, subscriptionMatch{id: row.ID, fromProject: row.ProjectSubscriptionID.Valid, muted: row.Muted})
				}
			}
			return out, err
		},
		setMuted: func(ctx context.Context, id pgtype.UUID, muted bool) error {
			return r.subscriptions.SetChannelIssueSubscriptionMuted(ctx, db.SetChannelIssueSubscriptionMutedParams{ID: id, Muted: muted})
		},
		create: func(ctx context.Context) (bool, error) {
			return r.subscribeChatToIssue(ctx, inst, msg.Source.ChannelType, issue, identity.UserID, spot.target)
		},
		deleteByID: r.subscriptions.DeleteChannelIssueSubscriptionByID,
	}, subjectFound, nil
}

// resolveProjectSubject finds a project by UUID, Multica project link, or
// case-insensitive name. A name only matches projects the sender can see.
func (r *Router) resolveProjectSubject(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, identity ResolvedIdentity, ref string, spot chatSpot) (subscriptionSubject, subjectLookup, error) {
	target := spot.target
	var project db.Project
	visible := false
	key, _, isLink := linkLastSegment(ref)
	if id, err := util.ParseUUID(key); err == nil && id.Valid {
		p, err := r.subscriptions.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: inst.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return subscriptionSubject{}, subjectMissing, nil
		}
		if err != nil {
			return subscriptionSubject{}, subjectMissing, fmt.Errorf("load project: %w", err)
		}
		if visible, err = projectVisibleTo(ctx, r.subscriptions, inst.WorkspaceID, p.ID, identity.UserID); err != nil {
			return subscriptionSubject{}, subjectMissing, fmt.Errorf("check project visibility: %w", err)
		}
		project = p
	} else if isLink {
		return subscriptionSubject{}, subjectMissing, nil
	} else {
		candidates, err := r.subscriptions.ListProjectsByTitle(ctx, db.ListProjectsByTitleParams{WorkspaceID: inst.WorkspaceID, Title: ref})
		if err != nil {
			return subscriptionSubject{}, subjectMissing, fmt.Errorf("find project by name: %w", err)
		}
		matches := 0
		for _, p := range candidates {
			ok, err := projectVisibleTo(ctx, r.subscriptions, inst.WorkspaceID, p.ID, identity.UserID)
			if err != nil {
				return subscriptionSubject{}, subjectMissing, fmt.Errorf("check project visibility: %w", err)
			}
			if ok {
				project, matches = p, matches+1
			}
		}
		switch {
		case matches == 0:
			return subscriptionSubject{}, subjectMissing, nil
		case matches > 1:
			return subscriptionSubject{}, subjectAmbiguous, nil
		}
		visible = true
	}
	_, slug := r.issueWorkspaceIdentity(ctx, inst.WorkspaceID)
	return subscriptionSubject{
		visible: visible,
		reveal: func(res *Result) {
			res.Subscription.ProjectID = project.ID
			res.Subscription.ProjectTitle = project.Title
			res.IssueWorkspaceSlug = slug
		},
		create: func(ctx context.Context) (bool, error) {
			_, err := r.subscriptions.CreateChannelProjectSubscription(ctx, db.CreateChannelProjectSubscriptionParams{
				WorkspaceID: inst.WorkspaceID, ProjectID: project.ID, InstallationID: inst.ID,
				ChannelType: string(msg.Source.ChannelType), ChatID: target.ChatID, ThreadID: target.ThreadID,
				AnchorMessageID: target.AnchorMessageID, CreatedBy: identity.UserID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("create chat project subscription: %w", err)
			}
			return true, nil
		},
		find: func(ctx context.Context) ([]subscriptionMatch, error) {
			rows, err := r.subscriptions.ListChannelProjectSubscriptionsForProject(ctx, db.ListChannelProjectSubscriptionsForProjectParams{
				ProjectID: project.ID, WorkspaceID: inst.WorkspaceID,
			})
			var out []subscriptionMatch
			for _, row := range rows {
				if spot.holds(row.InstallationID, row.ChatID, row.ThreadID, row.AnchorMessageID) {
					out = append(out, subscriptionMatch{id: row.ID})
				}
			}
			return out, err
		},
		deleteByID: r.subscriptions.DeleteChannelProjectSubscriptionByID,
	}, subjectFound, nil
}

// handleSubscriptionCommand runs /subscribe or /unsubscribe for the chat the
// message came from. The command is a control-plane turn: it is not stored in
// the Chat and never starts an agent run.
func (r *Router) handleSubscriptionCommand(ctx context.Context, set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, msg channel.InboundMessage, cmd SubscriptionCommand) (Result, error) {
	res := Result{
		Outcome:        OutcomeIssueSubscription,
		InstallationID: inst.ID,
		Sender:         msg.Source.SenderID,
		Subscription:   SubscriptionOutcome{Kind: cmd.Kind, Target: cmd.Target},
	}
	verdict := func(action SubscriptionAction) (Result, error) {
		res.Subscription.Action = action
		if action == SubscriptionNotFound || action == SubscriptionAmbiguous {
			res.Subscription.Ref = cmd.Ref
		}
		return res, nil
	}
	if set.Poster == nil || r.subscriptions == nil {
		return verdict(SubscriptionUnsupported)
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		return verdict(SubscriptionGroupOnly)
	}
	if cmd.Ref == "" {
		return verdict(SubscriptionUsage)
	}

	spot := chatSpotFromMessage(inst, msg)
	resolve := r.resolveIssueSubject
	if cmd.Target == SubscriptionTargetProject {
		resolve = r.resolveProjectSubject
	}
	subject, lookup, err := resolve(ctx, inst, msg, identity, cmd.Ref, spot)
	if err != nil {
		return Result{}, fmt.Errorf("resolve subscription subject: %w", err)
	}
	switch lookup {
	case subjectMissing:
		return verdict(SubscriptionNotFound)
	case subjectAmbiguous:
		return verdict(SubscriptionAmbiguous)
	}

	matches, err := subject.find(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list chat subscriptions: %w", err)
	}
	var active, muted []subscriptionMatch
	for _, m := range matches {
		if m.muted {
			muted = append(muted, m)
		} else {
			active = append(active, m)
		}
	}
	if cmd.Kind == SubscriptionCommandUnsubscribe {
		// Anyone in the chat may stop updates the chat already receives, even
		// without access; only an existing subscription reveals the subject.
		if len(active) == 0 && !subject.visible {
			return verdict(SubscriptionNotFound)
		}
		for _, m := range active {
			var err error
			if m.fromProject {
				err = subject.setMuted(ctx, m.id, true)
			} else {
				err = subject.deleteByID(ctx, m.id)
			}
			if err != nil {
				return Result{}, fmt.Errorf("stop chat subscription: %w", err)
			}
		}
		subject.reveal(&res)
		if len(active) > 0 {
			return verdict(SubscriptionUnsubscribed)
		}
		return verdict(SubscriptionNotSubscribed)
	}
	if !subject.visible {
		return verdict(SubscriptionNotFound)
	}
	subject.reveal(&res)
	if len(active) > 0 {
		return verdict(SubscriptionAlreadySubscribed)
	}
	if len(muted) > 0 {
		for _, m := range muted {
			if err := subject.setMuted(ctx, m.id, false); err != nil {
				return Result{}, fmt.Errorf("resume chat subscription: %w", err)
			}
		}
		return verdict(SubscriptionSubscribed)
	}
	created, err := subject.create(ctx)
	if err != nil {
		return Result{}, err
	}
	if created {
		return verdict(SubscriptionSubscribed)
	}
	return verdict(SubscriptionAlreadySubscribed)
}

// subscribeChatToIssue stores the subscription and reports whether it is new.
func (r *Router) subscribeChatToIssue(ctx context.Context, inst ResolvedInstallation, channelType channel.Type, issue db.Issue, createdBy pgtype.UUID, target ChatTarget) (bool, error) {
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
		_, err = r.subscribeChatToIssue(ctx, inst, msg.Source.ChannelType, issue, createdBy, chatTargetFromMessage(msg))
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
// the issue or its project. It runs detached and returns immediately; Drain
// waits for in-flight posts. render runs only when some chat will receive the
// update, so callers can do their lookups there; an empty result posts
// nothing.
func (r *Router) PublishIssueUpdate(workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	r.publishDetached(workspaceID, issueID, true, render)
}

// PublishIssueCreated posts a new issue into the chats that follow its
// project, opening its topic where the platform supports one. Issue-level
// subscriptions cannot exist yet.
func (r *Router) PublishIssueCreated(workspaceID, issueID pgtype.UUID, render func(context.Context) string) {
	r.publishDetached(workspaceID, issueID, false, render)
}

func (r *Router) publishDetached(workspaceID, issueID pgtype.UUID, includeIssueSubs bool, render func(context.Context) string) {
	if r.subscriptions == nil || render == nil || !workspaceID.Valid || !issueID.Valid {
		return
	}
	r.replyWg.Add(1)
	go func() {
		defer r.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), issueFeedPublishTimeout)
		defer cancel()
		r.publishIssueFeed(ctx, workspaceID, issueID, includeIssueSubs, render)
	}()
}

// feedRecipient is one chat subscription an issue update fans out to.
type feedRecipient struct {
	id             pgtype.UUID
	viaProject     bool
	muted          bool
	installationID pgtype.UUID
	channelType    string
	target         ChatTarget
	createdBy      pgtype.UUID
	// openTopic marks a project subscription whose platform roots one topic
	// per issue: the update opens the issue's topic at chat level.
	openTopic bool
}

func feedKey(installationID pgtype.UUID, chatID, threadID string) string {
	return util.UUIDToString(installationID) + "\x00" + chatID + "\x00" + threadID
}

func (r *Router) topicPoster(channelType string) (TopicPoster, bool) {
	r.mu.RLock()
	set, ok := r.sets[channel.Type(channelType)]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	tp, ok := set.Poster.(TopicPoster)
	return tp, ok
}

// feedRecipients merges the issue's and its project's subscriptions, each
// chat (thread) once. An issue subscription wins over the project's for the
// same place: an issue topic a project opened (fromProject) is that issue's
// place in the chat, and a muted one keeps the project from posting there.
// Topics of a project subscription that no longer covers the issue are
// deleted.
func (r *Router) feedRecipients(ctx context.Context, issue db.Issue, includeIssueSubs bool) ([]feedRecipient, error) {
	var projectSubs []db.ChannelProjectSubscription
	if issue.ProjectID.Valid {
		subs, err := r.subscriptions.ListChannelProjectSubscriptionsForProject(ctx, db.ListChannelProjectSubscriptionsForProjectParams{
			ProjectID: issue.ProjectID, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return nil, fmt.Errorf("list project subscriptions: %w", err)
		}
		projectSubs = subs
	}
	covering := map[[16]byte]struct{}{}
	for _, s := range projectSubs {
		covering[s.ID.Bytes] = struct{}{}
	}

	var out []feedRecipient
	seen := map[string]struct{}{}
	fromProject := map[[16]byte]struct{}{}
	add := func(key string, rec feedRecipient) {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, rec)
	}
	if includeIssueSubs {
		subs, err := r.subscriptions.ListChannelIssueSubscriptionsForIssue(ctx, db.ListChannelIssueSubscriptionsForIssueParams{
			IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return nil, fmt.Errorf("list issue subscriptions: %w", err)
		}
		for _, s := range subs {
			if s.ProjectSubscriptionID.Valid {
				if _, ok := covering[s.ProjectSubscriptionID.Bytes]; !ok {
					if err := r.subscriptions.DeleteChannelIssueSubscriptionByID(ctx, s.ID); err != nil {
						return nil, fmt.Errorf("delete stale issue topic: %w", err)
					}
					continue
				}
				fromProject[s.ProjectSubscriptionID.Bytes] = struct{}{}
			}
			add(feedKey(s.InstallationID, s.ChatID, s.ThreadID), feedRecipient{
				id: s.ID, muted: s.Muted, installationID: s.InstallationID, channelType: s.ChannelType, createdBy: s.CreatedBy,
				target: ChatTarget{ChatID: s.ChatID, ThreadID: s.ThreadID, AnchorMessageID: s.AnchorMessageID},
			})
		}
	}
	for _, s := range projectSubs {
		if _, ok := fromProject[s.ID.Bytes]; ok {
			continue
		}
		rec := feedRecipient{
			id: s.ID, viaProject: true, installationID: s.InstallationID, channelType: s.ChannelType, createdBy: s.CreatedBy,
			target: ChatTarget{ChatID: s.ChatID, ThreadID: s.ThreadID, AnchorMessageID: s.AnchorMessageID},
		}
		key := feedKey(s.InstallationID, s.ChatID, s.ThreadID)
		if _, ok := r.topicPoster(s.ChannelType); ok {
			rec.openTopic = true
			key = feedKey(s.InstallationID, s.ChatID, "")
		}
		add(key, rec)
	}
	active := out[:0]
	for _, rec := range out {
		if !rec.muted {
			active = append(active, rec)
		}
	}
	return active, nil
}

// issueOriginChat returns the chat an issue was created from with /issue, if
// any. That chat follows the issue in its /issue reply topic, so a project
// subscription there does not open a second topic for it.
func (r *Router) issueOriginChat(ctx context.Context, issue db.Issue) (pgtype.UUID, string, bool) {
	if !issue.OriginID.Valid {
		return pgtype.UUID{}, "", false
	}
	binding, err := r.subscriptions.GetChannelChatSessionBindingBySessionAny(ctx, issue.OriginID)
	if err != nil {
		return pgtype.UUID{}, "", false
	}
	return binding.InstallationID, binding.ChannelChatID, true
}

func originChatHolds(key, chatID string) bool {
	// Topic-isolated sessions key their binding "chat:thread".
	return key == chatID || strings.HasPrefix(key, chatID+":")
}

// publishIssueFeed delivers one update. A subscription's updates pause
// (without deleting it) while its creator cannot see the issue; subscriptions
// whose installation no longer exists in the workspace are deleted.
func (r *Router) publishIssueFeed(ctx context.Context, workspaceID, issueID pgtype.UUID, includeIssueSubs bool, render func(context.Context) string) {
	log := r.logger.With("issue_id", util.UUIDToString(issueID))
	issue, err := r.subscriptions.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn("channel issue feed: load issue failed", "error", err)
		}
		return
	}
	recipients, err := r.feedRecipients(ctx, issue, includeIssueSubs)
	if err != nil {
		log.Warn("channel issue feed: list subscriptions failed", "error", err)
		return
	}
	originInst, originChat, hasOrigin := r.issueOriginChat(ctx, issue)
	visibleTo := map[[16]byte]bool{}
	allowed := recipients[:0:0]
	for _, rec := range recipients {
		if rec.openTopic && hasOrigin && rec.installationID == originInst && originChatHolds(originChat, rec.target.ChatID) {
			continue
		}
		visible, cached := visibleTo[rec.createdBy.Bytes]
		if !cached {
			if visible, err = issueVisibleTo(ctx, r.subscriptions, issue, rec.createdBy); err != nil {
				log.Warn("channel issue feed: visibility check failed", "error", err)
				return
			}
			visibleTo[rec.createdBy.Bytes] = visible
		}
		if visible {
			allowed = append(allowed, rec)
		}
	}
	if len(allowed) == 0 {
		return
	}
	text := render(ctx)
	if strings.TrimSpace(text) == "" {
		return
	}

	for _, rec := range allowed {
		r.mu.RLock()
		set, ok := r.sets[channel.Type(rec.channelType)]
		r.mu.RUnlock()
		if !ok || set.Poster == nil {
			continue
		}
		inst, err := r.subscriptions.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
			ID: rec.installationID, ChannelType: rec.channelType,
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && inst.WorkspaceID != workspaceID) {
			deleteByID := r.subscriptions.DeleteChannelIssueSubscriptionByID
			if rec.viaProject {
				deleteByID = r.subscriptions.DeleteChannelProjectSubscriptionByID
			}
			if err := deleteByID(ctx, rec.id); err != nil {
				log.Warn("channel issue feed: delete orphaned subscription failed", "error", err)
			}
			continue
		}
		if err != nil {
			log.Warn("channel issue feed: load installation failed",
				"installation_id", util.UUIDToString(rec.installationID), "error", err)
			continue
		}
		if inst.Status != "active" {
			continue
		}
		postCtx, cancel := context.WithTimeout(ctx, issueFeedPostTimeout)
		if tp, ok := set.Poster.(TopicPoster); ok && rec.openTopic {
			err = r.openIssueTopic(postCtx, tp, inst, issue, rec, text)
		} else {
			err = set.Poster.PostToChat(postCtx, inst, rec.target, text)
		}
		cancel()
		if err != nil {
			log.Warn("channel issue feed: post failed",
				"channel_type", rec.channelType,
				"installation_id", util.UUIDToString(rec.installationID), "error", err)
		}
	}
}

// openIssueTopic posts the issue's first update for a project subscription at
// chat level and records the message as the anchor of the issue's topic.
func (r *Router) openIssueTopic(ctx context.Context, tp TopicPoster, inst db.ChannelInstallation, issue db.Issue, rec feedRecipient, text string) error {
	anchor, err := tp.OpenTopic(ctx, inst, rec.target.ChatID, text)
	if err != nil || anchor == "" {
		return err
	}
	_, err = r.subscriptions.CreateChannelIssueSubscription(ctx, db.CreateChannelIssueSubscriptionParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, InstallationID: rec.installationID,
		ChannelType: rec.channelType, ChatID: rec.target.ChatID, AnchorMessageID: anchor,
		CreatedBy: rec.createdBy, ProjectSubscriptionID: rec.id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("record issue topic: %w", err)
	}
	return nil
}

// IssueChatSubscribedNote is appended to an /issue confirmation when the
// group chat now follows the new issue.
func IssueChatSubscribedNote(res Result) string {
	return "🔔 This chat will get updates on " + resultIssueRef(res) +
		". Send /unsubscribe issue " + resultIssueRef(res) + " to stop."
}

// SubscriptionReplyText renders the reply to /subscribe or /unsubscribe.
// escape neutralizes member-authored markup for the platform; nil keeps text
// as is.
func SubscriptionReplyText(res Result, appURL string, escape func(string) string) string {
	if escape == nil {
		escape = func(s string) string { return s }
	}
	sub := res.Subscription
	project := sub.Target == SubscriptionTargetProject
	ref, named, link := resultIssueRef(res), "", ""
	if project {
		ref = "project " + escape(strings.TrimSpace(sub.ProjectTitle))
		named = ref
		if sub.ProjectID.Valid {
			link = channel.ReferenceWebLink(appURL, res.IssueWorkspaceSlug, "project", util.UUIDToString(sub.ProjectID))
		}
	} else {
		named = ref
		if title := escape(strings.TrimSpace(res.IssueTitle)); title != "" {
			named = ref + " — " + title
		}
		// Link by UUID: without a workspace issue prefix the identifier is
		// "#N", which no issue route resolves.
		if res.IssueID.Valid {
			link = channel.IssueWebLink(appURL, res.IssueWorkspaceSlug, util.UUIDToString(res.IssueID))
		}
	}
	withLink := func(line string) string {
		if link == "" {
			return line
		}
		return line + "\n" + link
	}
	switch sub.Action {
	case SubscriptionSubscribed:
		if project {
			return withLink("🔔 This chat now follows " + named + ". New issues, status changes, assignments and comments in it will be posted to this chat.")
		}
		return withLink("🔔 This chat now follows " + named + ". Status changes, assignments and comments will be posted here.")
	case SubscriptionAlreadySubscribed:
		return withLink("This chat already follows " + named + ".")
	case SubscriptionUnsubscribed:
		return "🔕 This chat no longer follows " + ref + "."
	case SubscriptionNotSubscribed:
		return "This chat does not follow " + ref + "."
	case SubscriptionNotFound:
		kind := "issue"
		if project {
			kind = "project"
		}
		return "No " + kind + " matches " + escape(strings.TrimSpace(sub.Ref)) + " that you can access."
	case SubscriptionAmbiguous:
		return "Several projects are named " + escape(strings.TrimSpace(sub.Ref)) + ". Send the project's link instead."
	case SubscriptionUsage:
		verb := "/subscribe"
		if sub.Kind == SubscriptionCommandUnsubscribe {
			verb = "/unsubscribe"
		}
		switch sub.Target {
		case SubscriptionTargetIssue:
			return "Usage: " + verb + " issue <issue>, for example " + verb + " issue ABC-123"
		case SubscriptionTargetProject:
			return "Usage: " + verb + " project <project name or link>"
		}
		return "Usage: " + verb + " issue <issue> or " + verb + " project <project name or link>"
	case SubscriptionGroupOnly:
		return "Chat subscriptions work in group chats. In a direct message, follow the issue or project in Multica to get updates in your inbox."
	case SubscriptionUnsupported:
		return "Subscriptions aren't available on this platform yet."
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
